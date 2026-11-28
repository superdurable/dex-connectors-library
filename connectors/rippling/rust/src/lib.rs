// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Rippling worker directory queries for Dex Flows.

#[rustfmt::skip]
mod generated_definitions;
mod provider_fields;
mod workers;
/// API token resolved for the selected Rippling connection.
pub type Credentials = sdk::SecretString;
pub use workers::{GetWorkerInput, ListWorkersInput, WorkerPage, WorkerSummary};

use dex_connectors_sdk as sdk;

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
    /// Rippling REST base URL. Defaults to https://rest.ripplingapis.com.
    pub base_url: String,
    /// Maximum duration per API read. Defaults to 15 seconds.
    pub timeout: Duration,
    /// Maximum bytes in a successful response. Defaults to 1 MiB.
    pub max_response_bytes: usize,
}
impl Default for Config {
    fn default() -> Self {
        Self {
            base_url: "https://rest.ripplingapis.com".into(),
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
}
impl Client {
    /// Bind a Dex Web project connection, including its API settings and per-call credential resolution.
    pub fn from_project(
        project: &sdk::projectconfig::LoadedProject,
        connection_name: &str,
    ) -> Result<Self, Box<dyn std::error::Error>> {
        let key = sdk::projectconfig::ConnectionKey::new("rippling", connection_name);
        let entry = project
            .connection_configuration(&key)
            .ok_or("Rippling connection is missing")?;
        if entry.provider != "rippling" {
            return Err("Rippling project connection provider differs".into());
        }
        let mut config = Config::default();
        for (name, value) in entry.configuration.to_json_object()? {
            match name.as_str() {
                "baseUrl" | "timeout" | "maxResponseBytes" if value.as_str() == Some("") => {}
                "baseUrl" => {
                    config.base_url = value
                        .as_str()
                        .ok_or("invalid Rippling API URL setting")?
                        .into()
                }
                "timeout" => {
                    config.timeout = Duration::from_nanos(
                        value
                            .as_i64()
                            .filter(|n| *n > 0)
                            .ok_or("invalid Rippling timeout setting")?
                            as u64,
                    )
                }
                "maxResponseBytes" => {
                    config.max_response_bytes = value
                        .as_u64()
                        .and_then(|n| usize::try_from(n).ok())
                        .ok_or("invalid Rippling response size setting")?
                }
                _ => return Err("unknown Rippling setting".into()),
            }
        }
        let provider = sdk::projectconfig::ProjectCredentialProvider::new(
            project.connections().clone(),
            key,
            Arc::new(|fields| {
                if fields.len() != 1 {
                    return Err("Invalid Rippling credential fields".into());
                }
                fields
                    .get("api_token")
                    .and_then(serde_json::Value::as_str)
                    .filter(|value| !value.is_empty())
                    .map(Credentials::new)
                    .ok_or_else(|| "Missing Rippling API token".into())
            }),
            None,
        )?;
        Ok(Self::new(config, Arc::new(provider))?)
    }

    /// Construct a client with a redirect-safe transport and API token credentials.
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
            config.max_response_bytes,
            credentials,
            http,
        )
    }
    /// Supply the transport used for bounded worker reads.
    pub fn with_http_client(
        base_url: &str,
        max_response_bytes: usize,
        credentials: Arc<dyn CredentialProvider<Credentials>>,
        http: ProviderHttpClient,
    ) -> Result<Self, ValidationError> {
        let endpoint = providerhttp::validate_base_url(base_url)?;
        if max_response_bytes == 0 {
            return Err(ValidationError::new(
                "Rippling response size limit must be positive",
            ));
        }
        Ok(Self {
            endpoint,
            max_response_bytes,
            credentials,
            http,
        })
    }
    /// Bind the single-worker lookup.
    pub fn get_worker(self: &Arc<Self>) -> GetWorkerOperation {
        GetWorkerOperation(self.clone())
    }
    /// Bind the worker page query.
    pub fn list_workers(self: &Arc<Self>) -> ListWorkersOperation {
        ListWorkersOperation(self.clone())
    }
    fn request<Output: Default>(
        &self,
        call: &Call<'_>,
        suffix: &str,
        operation: &str,
    ) -> Result<(Vec<u8>, Credentials), Box<QueryAttempt<Output>>> {
        let failure = |kind, message| Failure::new(kind, "rippling", operation, message);
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
        let credentials = self.credentials.resolve(call).map_err(|_| {
            branch(
                "defect",
                FailureKind::Authentication,
                "Rippling API token is unavailable",
            )
        })?;
        let token = credentials.reveal();
        if !providerhttp::is_header_safe_credential(token) {
            return Err(branch(
                "defect",
                FailureKind::Authentication,
                "Rippling access token is unavailable",
            ));
        }
        let request =
            ProviderHttpRequest::new("GET", format!("{}/workers/{}", self.endpoint, suffix))
                .header("Accept", "application/json")
                .header("Authorization", format!("Bearer {token}"));
        let mut response = self
            .http
            .send_traced(&request)
            .map_err(|_| retry(FailureKind::Transport, "Rippling request failed", 0.0))?;
        let status = response.status();
        let delay = providerhttp::parse_retry_after(
            response.headers().get("Retry-After").unwrap_or(""),
            SystemTime::now(),
        );
        let body = providerhttp::read_bounded_body(&mut response, self.max_response_bytes);
        match status {
            429 => {
                return Err(retry(
                    FailureKind::RateLimit,
                    "Rippling rate limited the request",
                    delay,
                ));
            }
            408 | 500.. => {
                return Err(retry(
                    FailureKind::Availability,
                    "Rippling is temporarily unavailable",
                    delay,
                ));
            }
            404 if operation == "getWorker" => {
                return Err(branch(
                    "notFound",
                    FailureKind::NotFound,
                    "Rippling worker was not found",
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
                        "rippling",
                        operation,
                        format!("Rippling rejected HTTP {status}"),
                    )),
                )));
            }
        }
        body.map(|body| (body, credentials))
            .map_err(|error| match error {
                ProviderHttpError::BodyTooLarge(_) => branch(
                    "invalidResponse",
                    FailureKind::ResponseTooLarge,
                    "Rippling response exceeded the configured limit",
                ),
                _ => retry(
                    FailureKind::Transport,
                    "Rippling response was interrupted",
                    0.0,
                ),
            })
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
        Some(Failure::new(kind, "rippling", operation, message)),
    )
}
/// Reads one visible worker, returning only their ID, status and title.
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
                "Rippling worker ID must contain 1 to 64 URL-safe characters",
            );
        }
        let (body, token) =
            match self
                .0
                .request(call, &format!("{}/", input.worker_id), "getWorker")
            {
                Ok(body) => body,
                Err(attempt) => return *attempt,
            };
        match workers::read_profile(&body, &input.worker_id) {
            Some(profile) if profile.contains_credential(token.reveal()) => rejected(
                "getWorker",
                "defect",
                FailureKind::Authentication,
                "Rippling response included a credential",
            ),
            Some(profile) => QueryAttempt::branch("found", profile, Receipt::default(), None),
            None => rejected(
                "getWorker",
                "invalidResponse",
                FailureKind::Protocol,
                "Rippling returned invalid worker JSON",
            ),
        }
    }
}
/// Lists one page, projecting worker IDs, titles and statuses in provider order.
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
                "Rippling list filters are invalid",
            );
        };
        let (body, token) = match self.0.request(call, &suffix, "listWorkers") {
            Ok(body) => body,
            Err(attempt) => return *attempt,
        };
        match workers::read_page(&body) {
            Some(page)
                if page.next_cursor.contains(token.reveal())
                    || page
                        .workers
                        .iter()
                        .any(|worker| worker.contains_credential(token.reveal())) =>
            {
                rejected(
                    "listWorkers",
                    "defect",
                    FailureKind::Authentication,
                    "Rippling response included a credential",
                )
            }
            Some(page) => QueryAttempt::branch("found", page, Receipt::default(), None),
            None => rejected(
                "listWorkers",
                "invalidResponse",
                FailureKind::Protocol,
                "Rippling returned invalid workers JSON",
            ),
        }
    }
}
/// Bind the GetWorker query to its configured Dex Step and branch targets.
pub fn new_get_worker_step<Input>(
    config: QueryStepConfig<Input, GetWorkerOperation>,
) -> Result<QueryStep<Input, GetWorkerOperation>, ValidationError> {
    if config.connection.provider != "rippling" {
        return Err(ValidationError::new(
            "Rippling connection provider is invalid",
        ));
    }
    QueryStep::new(config)
}
/// Bind the ListWorkers query to its configured Dex Step and branch targets.
pub fn new_list_workers_step<Input>(
    config: QueryStepConfig<Input, ListWorkersOperation>,
) -> Result<QueryStep<Input, ListWorkersOperation>, ValidationError> {
    if config.connection.provider != "rippling" {
        return Err(ValidationError::new(
            "Rippling connection provider is invalid",
        ));
    }
    QueryStep::new(config)
}
