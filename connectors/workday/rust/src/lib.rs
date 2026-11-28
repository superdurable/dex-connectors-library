// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Workday Common REST directory queries for Dex Flows.
mod credentials;
#[rustfmt::skip]
mod generated_definitions;
mod provider_fields;
mod workers;
pub use credentials::{Credentials, project_credentials};
pub use workers::{GetWorkerInput, ListWorkersInput, WorkerPage, WorkerSummary};

use dex_connectors_sdk as sdk;
use sdk::oauthtoken::ManifestCredentialRefreshDriver;
use sdk::providerhttp::{
    self, AddressPolicy, ProviderHttpClient, ProviderHttpError, ProviderHttpRequest,
};
use sdk::{
    Call, CredentialProvider, Failure, FailureKind, Query, QueryAttempt, QueryDefinition,
    QueryStep, QueryStepConfig, Receipt, ValidationError,
};
use std::sync::Arc;
use std::time::{Duration, SystemTime};

/// Endpoint, per-request timeout and maximum body size used by both queries.
pub struct Config {
    /// Tenant-specific Common REST base URL.
    pub base_url: String,
    /// Tenant-specific OAuth token endpoint used to renew access.
    pub token_endpoint: String,
    /// Maximum duration per API read. Defaults to 15 seconds.
    pub timeout: Duration,
    /// Maximum bytes in a successful response. Defaults to 1 MiB.
    pub max_response_bytes: usize,
}
impl Default for Config {
    fn default() -> Self {
        Self {
            base_url: String::new(),
            token_endpoint: String::new(),
            timeout: Duration::from_secs(15),
            max_response_bytes: 1 << 20,
        }
    }
}
/// A validated Workers API binding, shared by its operations. Construction performs no network I/O.
pub struct Client {
    endpoint: String,
    max_response_bytes: usize,
    credentials: Arc<dyn CredentialProvider<Credentials>>,
    http: ProviderHttpClient,
    refresh: ManifestCredentialRefreshDriver<Credentials>,
}
impl Client {
    /// Bind a Dex Web project connection, including its API settings and coordinated token renewal.
    pub fn from_project(
        project: &sdk::projectconfig::LoadedProject,
        connection_name: &str,
    ) -> Result<Self, Box<dyn std::error::Error>> {
        let key = sdk::projectconfig::ConnectionKey::new("workday", connection_name);
        let entry = project
            .connection_configuration(&key)
            .ok_or("Workday connection is missing")?;
        if entry.provider != "workday" {
            return Err("Workday project connection provider differs".into());
        }
        let mut config = Config::default();
        let settings = entry.configuration.to_json_object()?;
        validate_oauth_endpoint(
            settings
                .get("authorizationEndpoint")
                .and_then(|v| v.as_str())
                .unwrap_or(""),
        )?;
        let scopes = settings
            .get("scopes")
            .and_then(|v| v.as_array())
            .ok_or("Workday scopes are required")?;
        if scopes.is_empty() || !scopes.iter().all(|v| v.is_string()) {
            return Err("Invalid Workday scopes".into());
        }
        for (name, value) in settings {
            match name.as_str() {
                "baseUrl" | "timeout" | "maxResponseBytes" if value.as_str() == Some("") => {}
                "baseUrl" => {
                    config.base_url = value
                        .as_str()
                        .ok_or("invalid Workday API URL setting")?
                        .into()
                }
                "tokenEndpoint" => {
                    config.token_endpoint = value
                        .as_str()
                        .ok_or("invalid Workday token endpoint")?
                        .into();
                }
                "authorizationEndpoint" | "scopes" => {}
                "timeout" => {
                    config.timeout = Duration::from_nanos(
                        value
                            .as_i64()
                            .filter(|n| *n > 0)
                            .ok_or("invalid Workday timeout setting")?
                            as u64,
                    )
                }
                "maxResponseBytes" => {
                    config.max_response_bytes = value
                        .as_u64()
                        .and_then(|n| usize::try_from(n).ok())
                        .ok_or("invalid Workday response size setting")?
                }
                _ => return Err("unknown Workday setting".into()),
            }
        }
        Ok(Self::new(
            config,
            Arc::new(project_credentials(project, connection_name)?),
        )?)
    }
    /// Construct a client with a redirect-safe transport and renewable credentials.
    pub fn new(
        config: Config,
        credentials: Arc<dyn CredentialProvider<Credentials>>,
    ) -> Result<Self, ValidationError> {
        let policy = if config
            .base_url
            .get(..7)
            .is_some_and(|scheme| scheme.eq_ignore_ascii_case("http://"))
        {
            AddressPolicy::PrivateNetworkHttp
        } else {
            AddressPolicy::PublicHttps
        };
        let http = ProviderHttpClient::new(config.timeout, policy)?;
        Self::with_http_client(
            &config.base_url,
            &config.token_endpoint,
            config.max_response_bytes,
            credentials,
            http,
        )
    }
    /// Supply the transport used for both token exchanges and bounded Workers API reads.
    pub fn with_http_client(
        base_url: &str,
        token_endpoint: &str,
        max_response_bytes: usize,
        credentials: Arc<dyn CredentialProvider<Credentials>>,
        http: ProviderHttpClient,
    ) -> Result<Self, ValidationError> {
        let endpoint = providerhttp::validate_base_url(base_url)?;
        validate_oauth_endpoint(token_endpoint)?;
        if max_response_bytes == 0 {
            return Err(ValidationError::new(
                "Workday response size limit must be positive",
            ));
        }
        let declaration =
            serde_json::from_str(include_str!("generated_refresh_authorization.json"))
                .expect("generated Workday authorization");
        let refresh = ManifestCredentialRefreshDriver::new(
            "Workday",
            &declaration,
            http.clone(),
            [("tokenEndpoint".to_owned(), token_endpoint.into())].into(),
        )?;
        Ok(Self {
            endpoint,
            max_response_bytes,
            credentials,
            http,
            refresh,
        })
    }
    /// Bind the single-worker lookup.
    pub fn get_worker(self: &Arc<Self>) -> GetWorkerOperation {
        GetWorkerOperation(self.clone())
    }
    /// Bind the filtered directory query.
    pub fn list_workers(self: &Arc<Self>) -> ListWorkersOperation {
        ListWorkersOperation(self.clone())
    }
    fn request<Output: Default>(
        &self,
        call: &Call<'_>,
        suffix: &str,
        operation: &str,
    ) -> Result<Vec<u8>, Box<QueryAttempt<Output>>> {
        let failure = |kind, message| Failure::new(kind, "workday", operation, message);
        let branch = |id, kind, message| {
            Box::new(QueryAttempt::branch(
                id,
                Output::default(),
                Receipt::default(),
                Some(failure(kind, message)),
            ))
        };
        let retry =
            |kind, message, delay| Box::new(QueryAttempt::retry(failure(kind, message), delay));
        let credentials = sdk::resolve_credential(&*self.credentials, call, &self.refresh)
            .map_err(|error| {
                if sdk::is_reauthorization_required(&*error) {
                    branch(
                        "defect",
                        FailureKind::Authentication,
                        "Workday connection needs authorization",
                    )
                } else {
                    retry(
                        FailureKind::Transport,
                        "Workday credential refresh failed",
                        0.0,
                    )
                }
            })?;
        let token = credentials.access_token.reveal();
        if !providerhttp::is_header_safe_credential(token) {
            return Err(branch(
                "defect",
                FailureKind::Authentication,
                "Workday access token is unavailable",
            ));
        }
        let request =
            ProviderHttpRequest::new("GET", format!("{}/workers{}", self.endpoint, suffix))
                .header("Accept", "application/json")
                .header("Authorization", format!("Bearer {token}"));
        let mut response = self
            .http
            .send_traced(&request)
            .map_err(|_| retry(FailureKind::Transport, "Workday request failed", 0.0))?;
        let status = response.status();
        let delay = providerhttp::parse_retry_after(
            response.headers().get("Retry-After").unwrap_or(""),
            SystemTime::now(),
        );
        let body = providerhttp::read_bounded_body(&mut response, self.max_response_bytes);
        if body
            .as_ref()
            .is_ok_and(|bytes| contains_access_token(bytes, token))
        {
            return Err(branch(
                "defect",
                FailureKind::Authentication,
                "Workday response contained a credential",
            ));
        }
        match status {
            429 => {
                return Err(retry(
                    FailureKind::RateLimit,
                    "Workday rate limited the request",
                    delay,
                ));
            }
            408 | 500.. => {
                return Err(retry(
                    FailureKind::Availability,
                    "Workday is temporarily unavailable",
                    delay,
                ));
            }
            404 if operation == "getWorker" => {
                return Err(branch(
                    "notFound",
                    FailureKind::NotFound,
                    "Workday worker was not found",
                ));
            }
            200 => {}
            _ => {
                return Err(Box::new(QueryAttempt::branch(
                    "providerRejected",
                    Output::default(),
                    Receipt::default(),
                    Some(Failure::new(
                        FailureKind::ProviderRejection,
                        "workday",
                        operation,
                        format!("Workday rejected HTTP {status}"),
                    )),
                )));
            }
        }
        body.map_err(|error| match error {
            ProviderHttpError::BodyTooLarge(_) => branch(
                "invalidResponse",
                FailureKind::ResponseTooLarge,
                "Workday response exceeded the configured limit",
            ),
            _ => retry(
                FailureKind::Transport,
                "Workday response was interrupted",
                0.0,
            ),
        })
    }
}
fn contains_access_token(mut body: &[u8], token: &str) -> bool {
    if body
        .windows(token.len())
        .any(|window| window == token.as_bytes())
    {
        return true;
    }
    if !body.contains(&b'\\') {
        return false;
    }
    while let Some(start) = body.iter().position(|&byte| byte == b'"') {
        let mut strings =
            serde_json::Deserializer::from_slice(&body[start..]).into_iter::<String>();
        let Some(Ok(value)) = strings.next() else {
            return false;
        };
        if value.contains(token) {
            return true;
        }
        body = &body[start + strings.byte_offset()..];
    }
    false
}

fn validate_oauth_endpoint(endpoint: &str) -> Result<(), ValidationError> {
    if url::Url::parse(endpoint).is_ok_and(|url| {
        matches!(url.scheme(), "https" | "http")
            && url.host().is_some()
            && url.username().is_empty()
            && url.password().is_none()
            && url.fragment().is_none()
    }) {
        Ok(())
    } else {
        Err(ValidationError::new("Invalid Workday OAuth endpoint"))
    }
}
fn rejected<Output: Default>(
    operation: &str,
    branch: &str,
    kind: FailureKind,
    message: &str,
) -> QueryAttempt<Output> {
    QueryAttempt::branch(
        branch,
        Output::default(),
        Receipt::default(),
        Some(Failure::new(kind, "workday", operation, message)),
    )
}
/// Reads one visible worker, returning only their WID and display name.
pub struct GetWorkerOperation(Arc<Client>);
impl Query for GetWorkerOperation {
    type Input = GetWorkerInput;
    type Output = WorkerSummary;
    fn definition(&self) -> QueryDefinition {
        generated_definitions::GetWorker::definition()
    }
    fn invoke(&self, call: &Call<'_>, input: &Self::Input) -> QueryAttempt<Self::Output> {
        if !workers::valid_id(&input.worker_id) {
            return rejected(
                "getWorker",
                "defect",
                FailureKind::Validation,
                "Workday worker WID must contain 32 hexadecimal characters",
            );
        }
        let body = match self
            .0
            .request(call, &format!("/{}", input.worker_id), "getWorker")
        {
            Ok(body) => body,
            Err(attempt) => return *attempt,
        };
        match workers::read_profile(&body, &input.worker_id) {
            Some(profile) => QueryAttempt::branch("found", profile, Receipt::default(), None),
            None => rejected(
                "getWorker",
                "invalidResponse",
                FailureKind::Protocol,
                "Workday returned invalid worker JSON",
            ),
        }
    }
}
/// Lists one filtered page, projecting WIDs and display names in provider order.
pub struct ListWorkersOperation(Arc<Client>);
impl Query for ListWorkersOperation {
    type Input = ListWorkersInput;
    type Output = WorkerPage;
    fn definition(&self) -> QueryDefinition {
        generated_definitions::ListWorkers::definition()
    }
    fn invoke(&self, call: &Call<'_>, input: &Self::Input) -> QueryAttempt<Self::Output> {
        let Some(suffix) = input.query_suffix() else {
            return rejected(
                "listWorkers",
                "defect",
                FailureKind::Validation,
                "Workday list filters are invalid",
            );
        };
        let body = match self.0.request(call, &suffix, "listWorkers") {
            Ok(body) => body,
            Err(attempt) => return *attempt,
        };
        match workers::read_page(&body) {
            Some(page) => QueryAttempt::branch("found", page, Receipt::default(), None),
            None => rejected(
                "listWorkers",
                "invalidResponse",
                FailureKind::Protocol,
                "Workday returned invalid workers JSON",
            ),
        }
    }
}
/// Bind the GetWorker query to its configured Dex Step and branch targets.
pub fn new_get_worker_step<Input>(
    config: QueryStepConfig<Input, GetWorkerOperation>,
) -> Result<QueryStep<Input, GetWorkerOperation>, ValidationError> {
    if config.connection.provider != "workday" {
        return Err(ValidationError::new(
            "Workday connection provider is invalid",
        ));
    }
    QueryStep::new(config)
}
/// Bind the ListWorkers query to its configured Dex Step and branch targets.
pub fn new_list_workers_step<Input>(
    config: QueryStepConfig<Input, ListWorkersOperation>,
) -> Result<QueryStep<Input, ListWorkersOperation>, ValidationError> {
    if config.connection.provider != "workday" {
        return Err(ValidationError::new(
            "Workday connection provider is invalid",
        ));
    }
    QueryStep::new(config)
}
