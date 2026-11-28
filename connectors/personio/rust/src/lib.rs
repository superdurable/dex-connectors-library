// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Personio v2 directory queries for Dex Flows.
mod credentials;
#[rustfmt::skip]
mod generated_definitions;
mod persons;
mod provider_fields;
pub use credentials::{Credentials, project_credentials};
pub use persons::{GetPersonInput, ListPersonsInput, PersonPage, PersonProfile};

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
    /// Personio API root ending in /v2. HTTP is supported only for loopback tests.
    pub base_url: String,
    /// Maximum duration per API read. Defaults to 15 seconds.
    pub timeout: Duration,
    /// Maximum bytes in a successful response. Defaults to 1 MiB.
    pub max_response_bytes: usize,
}
impl Default for Config {
    fn default() -> Self {
        Self {
            base_url: "https://api.personio.de/v2".into(),
            timeout: Duration::from_secs(15),
            max_response_bytes: 1 << 20,
        }
    }
}
/// A validated Persons API binding, shared by its operations. Construction performs no network I/O.
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
        let key = sdk::projectconfig::ConnectionKey::new("personio", connection_name);
        let entry = project
            .connection_configuration(&key)
            .ok_or("Personio connection is missing")?;
        if entry.provider != "personio" {
            return Err("Personio project connection provider differs".into());
        }
        let mut config = Config::default();
        for (name, value) in entry.configuration.to_json_object()? {
            match name.as_str() {
                "baseUrl" | "timeout" | "maxResponseBytes" if value.as_str() == Some("") => {}
                "baseUrl" => {
                    config.base_url = value
                        .as_str()
                        .ok_or("invalid Personio API URL setting")?
                        .into()
                }
                "timeout" => {
                    config.timeout = Duration::from_nanos(
                        value
                            .as_i64()
                            .filter(|n| *n > 0)
                            .ok_or("invalid Personio timeout setting")?
                            as u64,
                    )
                }
                "maxResponseBytes" => {
                    config.max_response_bytes = value
                        .as_u64()
                        .and_then(|n| usize::try_from(n).ok())
                        .ok_or("invalid Personio response size setting")?
                }
                _ => return Err("unknown Personio setting".into()),
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
            config.max_response_bytes,
            credentials,
            http,
        )
    }
    /// Supply the transport used for both token exchanges and bounded Persons API reads.
    pub fn with_http_client(
        base_url: &str,
        max_response_bytes: usize,
        credentials: Arc<dyn CredentialProvider<Credentials>>,
        http: ProviderHttpClient,
    ) -> Result<Self, ValidationError> {
        let endpoint = providerhttp::validate_base_url(base_url)?;
        if endpoint
            .split_once("://")
            .and_then(|(_, rest)| rest.split_once('/'))
            .map(|(_, path)| path)
            != Some("v2")
        {
            return Err(ValidationError::new("Personio API URL must end in /v2"));
        }
        if max_response_bytes == 0 {
            return Err(ValidationError::new(
                "Personio response size limit must be positive",
            ));
        }
        let declaration =
            serde_json::from_str(include_str!("generated_refresh_authorization.json"))
                .expect("generated Personio authorization");
        let refresh = ManifestCredentialRefreshDriver::new(
            "Personio",
            &declaration,
            http.clone(),
            Default::default(),
        )?;
        Ok(Self {
            endpoint,
            max_response_bytes,
            credentials,
            http,
            refresh,
        })
    }
    /// Bind the single-person lookup.
    pub fn get_person(self: &Arc<Self>) -> GetPersonOperation {
        GetPersonOperation(self.clone())
    }
    /// Bind the filtered directory query.
    pub fn list_persons(self: &Arc<Self>) -> ListPersonsOperation {
        ListPersonsOperation(self.clone())
    }
    fn request<Output: Default>(
        &self,
        call: &Call<'_>,
        suffix: &str,
        operation: &str,
    ) -> Result<Vec<u8>, Box<QueryAttempt<Output>>> {
        let failure = |kind, message| Failure::new(kind, "personio", operation, message);
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
                        "Personio connection needs authorization",
                    )
                } else {
                    retry(
                        FailureKind::Transport,
                        "Personio credential refresh failed",
                        0.0,
                    )
                }
            })?;
        let token = credentials.access_token.reveal();
        if !providerhttp::is_header_safe_credential(token) {
            return Err(branch(
                "defect",
                FailureKind::Authentication,
                "Personio access token is unavailable",
            ));
        }
        let request =
            ProviderHttpRequest::new("GET", format!("{}/persons{}", self.endpoint, suffix))
                .header("Accept", "application/json")
                .header("Authorization", format!("Bearer {token}"));
        let mut response = self
            .http
            .send_traced(&request)
            .map_err(|_| retry(FailureKind::Transport, "Personio request failed", 0.0))?;
        let status = response.status();
        let delay = providerhttp::parse_retry_after(
            response.headers().get("Retry-After").unwrap_or(""),
            SystemTime::now(),
        );
        match status {
            429 => {
                return Err(retry(
                    FailureKind::RateLimit,
                    "Personio rate limited the request",
                    delay,
                ));
            }
            408 | 500.. => {
                return Err(retry(
                    FailureKind::Availability,
                    "Personio is temporarily unavailable",
                    delay,
                ));
            }
            404 if operation == "getPerson" => {
                return Err(branch(
                    "notFound",
                    FailureKind::NotFound,
                    "Personio person was not found",
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
                        "personio",
                        operation,
                        format!("Personio rejected HTTP {status}"),
                    )),
                )));
            }
        }
        providerhttp::read_bounded_body(&mut response, self.max_response_bytes).map_err(|error| {
            match error {
                ProviderHttpError::BodyTooLarge(_) => branch(
                    "invalidResponse",
                    FailureKind::ResponseTooLarge,
                    "Personio response exceeded the configured limit",
                ),
                _ => retry(
                    FailureKind::Transport,
                    "Personio response was interrupted",
                    0.0,
                ),
            }
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
        Some(Failure::new(kind, "personio", operation, message)),
    )
}
/// Reads one visible person, returning only their ID and names.
pub struct GetPersonOperation(Arc<Client>);
impl Query for GetPersonOperation {
    type Input = GetPersonInput;
    type Output = PersonProfile;
    fn definition(&self) -> QueryDefinition {
        generated_definitions::GetPerson::definition()
    }
    fn invoke(&self, call: &Call<'_>, input: &Self::Input) -> QueryAttempt<Self::Output> {
        if !persons::valid_id(&input.person_id) {
            return rejected(
                "getPerson",
                "defect",
                FailureKind::Validation,
                "Personio person ID must be an API ID",
            );
        }
        let body = match self
            .0
            .request(call, &format!("/{}", input.person_id), "getPerson")
        {
            Ok(body) => body,
            Err(attempt) => return *attempt,
        };
        match persons::read_profile(&body, &input.person_id) {
            Some(profile) => QueryAttempt::branch("found", profile, Receipt::default(), None),
            None => rejected(
                "getPerson",
                "invalidResponse",
                FailureKind::Protocol,
                "Personio returned invalid person JSON",
            ),
        }
    }
}
/// Lists one filtered page, projecting IDs and names in provider order.
pub struct ListPersonsOperation(Arc<Client>);
impl Query for ListPersonsOperation {
    type Input = ListPersonsInput;
    type Output = PersonPage;
    fn definition(&self) -> QueryDefinition {
        generated_definitions::ListPersons::definition()
    }
    fn invoke(&self, call: &Call<'_>, input: &Self::Input) -> QueryAttempt<Self::Output> {
        let Some(suffix) = input.query_suffix() else {
            return rejected(
                "listPersons",
                "defect",
                FailureKind::Validation,
                "Personio list filters are invalid",
            );
        };
        let body = match self.0.request(call, &suffix, "listPersons") {
            Ok(body) => body,
            Err(attempt) => return *attempt,
        };
        match persons::read_page(&body, &self.0.endpoint) {
            Some(page) => QueryAttempt::branch("listed", page, Receipt::default(), None),
            None => rejected(
                "listPersons",
                "invalidResponse",
                FailureKind::Protocol,
                "Personio returned invalid persons JSON",
            ),
        }
    }
}
/// Bind the GetPerson query to its configured Dex Step and branch targets.
pub fn new_get_person_step<Input>(
    config: QueryStepConfig<Input, GetPersonOperation>,
) -> Result<QueryStep<Input, GetPersonOperation>, ValidationError> {
    if config.connection.provider != "personio" {
        return Err(ValidationError::new(
            "Personio connection provider is invalid",
        ));
    }
    QueryStep::new(config)
}
/// Bind the ListPersons query to its configured Dex Step and branch targets.
pub fn new_list_persons_step<Input>(
    config: QueryStepConfig<Input, ListPersonsOperation>,
) -> Result<QueryStep<Input, ListPersonsOperation>, ValidationError> {
    if config.connection.provider != "personio" {
        return Err(ValidationError::new(
            "Personio connection provider is invalid",
        ));
    }
    QueryStep::new(config)
}
