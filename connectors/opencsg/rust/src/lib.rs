// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Read the OpenCSG model catalog from a Dex Query Step.

#[rustfmt::skip]
mod generated_definitions;
mod provider_fields;
use provider_fields::BalanceFields as WireFields;

use std::sync::Arc;
use std::time::{Duration, SystemTime};

use dex_connectors_sdk as sdk;
use sdk::providerhttp::{
    AddressPolicy, ProviderHttpClient, ProviderHttpError, ProviderHttpRequest, parse_retry_after,
    read_bounded_body,
};
use sdk::{
    Call, CredentialProvider, Failure, FailureKind, Query, QueryAttempt, QueryDefinition,
    QueryStep, QueryStepConfig, Receipt, SecretString, ValidationError,
};
use serde::{Deserialize, Serialize};

/// The listGatewayModels query takes an empty object.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct ListGatewayModelsInput {}
/// Number of available models and at most fifty IDs in provider order.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct ListGatewayModelsOutput {
    /// Number of rows in the provider response.
    pub total: usize,
    /// The first fifty model IDs, preserving order and duplicates.
    #[serde(rename = "modelIDs")]
    pub model_ids: Vec<String>,
}
/// Settings for a single provider request.
pub struct Config {
    /// Timeout per attempt. Uses the manifest default.
    pub timeout: Duration,
    /// Maximum response size in bytes. Uses the manifest default.
    pub max_response_bytes: usize,
}

impl Default for Config {
    fn default() -> Self {
        Self {
            timeout: Duration::from_secs(15),
            max_response_bytes: 1048576,
        }
    }
}

/// Credentials resolved from the logical connection for each request.
pub type Credentials = SecretString;
/// Bound model query. Construct once and share the HTTP client across calls.
pub struct ListGatewayModelsOperation {
    credentials: Arc<dyn CredentialProvider<Credentials>>,
    http: ProviderHttpClient,
    max_response_bytes: usize,
}

impl ListGatewayModelsOperation {
    /// Bind a Dex Web project connection, including its API settings and per-call credential resolution.
    pub fn from_project(
        project: &sdk::projectconfig::LoadedProject,
        connection_name: &str,
    ) -> Result<Self, Box<dyn std::error::Error>> {
        let key = sdk::projectconfig::ConnectionKey::new("opencsg", connection_name);
        let entry = project
            .connection_configuration(&key)
            .ok_or("OpenCSG connection is missing")?;
        if entry.provider != "opencsg" {
            return Err("OpenCSG project connection provider differs".into());
        }
        let mut config = Config::default();
        for (name, value) in entry.configuration.to_json_object()? {
            match name.as_str() {
                "timeout" | "maxResponseBytes" if value.as_str() == Some("") => {}
                "timeout" => {
                    config.timeout = Duration::from_nanos(
                        value
                            .as_i64()
                            .filter(|n| *n > 0)
                            .ok_or("invalid OpenCSG timeout setting")?
                            as u64,
                    )
                }
                "maxResponseBytes" => {
                    config.max_response_bytes = value
                        .as_u64()
                        .and_then(|n| usize::try_from(n).ok())
                        .ok_or("invalid OpenCSG response size setting")?
                }
                _ => return Err("unknown OpenCSG setting".into()),
            }
        }
        let provider = sdk::projectconfig::ProjectCredentialProvider::new(
            project.connections().clone(),
            key,
            Arc::new(|fields| {
                if fields.len() != 1 {
                    return Err("Invalid OpenCSG credential fields".into());
                }
                fields
                    .get("apiKey")
                    .and_then(serde_json::Value::as_str)
                    .filter(|value| !value.is_empty())
                    .map(Credentials::new)
                    .ok_or_else(|| "Missing OpenCSG API token".into())
            }),
            None,
        )?;
        Ok(Self::new(config, Arc::new(provider))?)
    }

    /// Construct a bounded, redirect-safe transport without contacting the provider.
    pub fn new(
        config: Config,
        credentials: Arc<dyn CredentialProvider<Credentials>>,
    ) -> Result<Self, ValidationError> {
        let http = ProviderHttpClient::new(config.timeout, AddressPolicy::PublicHttps)?;
        Self::with_http_client(credentials, http, config.max_response_bytes)
    }

    /// Bind a caller-supplied transport and a response limit of one byte through sixteen MiB.
    pub fn with_http_client(
        credentials: Arc<dyn CredentialProvider<Credentials>>,
        http: ProviderHttpClient,
        max_response_bytes: usize,
    ) -> Result<Self, ValidationError> {
        if max_response_bytes == 0 || max_response_bytes > 16 << 20 {
            return Err(ValidationError::new(
                "OpenCSG response size limit must be positive",
            ));
        }
        Ok(Self {
            credentials,
            http,
            max_response_bytes,
        })
    }
}

impl Query for ListGatewayModelsOperation {
    type Input = ListGatewayModelsInput;
    type Output = ListGatewayModelsOutput;

    fn definition(&self) -> QueryDefinition {
        generated_definitions::ListGatewayModels::definition()
    }

    fn invoke(&self, call: &Call<'_>, _: &Self::Input) -> QueryAttempt<Self::Output> {
        let token = match self.credentials.resolve(call) {
            Ok(token) if valid_key(token.reveal()) => token,
            _ => {
                return rejected(
                    "defect",
                    FailureKind::Authentication,
                    "Invalid OpenCSG API token",
                );
            }
        };
        let request = ProviderHttpRequest::new("GET", "https://ai.space.opencsg.com/v1/models")
            .header("Accept", "application/json")
            .header("Authorization", format!("Bearer {}", token.reveal()));
        self.read_response(&request, &token)
    }
}
impl ListGatewayModelsOperation {
    fn read_response(
        &self,
        request: &ProviderHttpRequest,
        credential: &SecretString,
    ) -> QueryAttempt<ListGatewayModelsOutput> {
        let mut response = match self.http.send_traced(request) {
            Ok(response) => response,
            Err(_) => return retry(FailureKind::Availability, "OpenCSG request failed", 0.0),
        };
        let status = response.status();

        if matches!(status, 408 | 429 | 500..) {
            let delay = parse_retry_after(
                response.headers().get("Retry-After").unwrap_or(""),
                SystemTime::now(),
            );
            return retry(
                if status == 429 {
                    FailureKind::RateLimit
                } else {
                    FailureKind::Availability
                },
                "OpenCSG temporarily unavailable",
                delay,
            );
        }
        if status != 200 {
            return rejected(
                "providerRejected",
                match status {
                    401 => FailureKind::Authentication,
                    403 => FailureKind::Authorization,
                    _ => FailureKind::ProviderRejection,
                },
                "OpenCSG rejected the request",
            );
        }
        let body = match read_bounded_body(&mut response, self.max_response_bytes) {
            Ok(body) => body,
            Err(ProviderHttpError::BodyTooLarge(_)) => {
                return rejected(
                    "invalidResponse",
                    FailureKind::ResponseTooLarge,
                    "OpenCSG response exceeded the configured limit",
                );
            }
            Err(_) => {
                return retry(
                    FailureKind::Availability,
                    "OpenCSG response interrupted",
                    0.0,
                );
            }
        };
        decode_models(&body, credential).unwrap_or_else(invalid)
    }
}
/// Bind the application operation to its branch targets and validate the Step configuration.
pub fn new_list_gateway_models_step<Input>(
    config: QueryStepConfig<Input, ListGatewayModelsOperation>,
) -> Result<QueryStep<Input, ListGatewayModelsOperation>, ValidationError> {
    if config.connection.provider != r"opencsg" {
        return Err(ValidationError::new(
            "OpenCSG connection provider is invalid",
        ));
    }
    QueryStep::new(config)
}

fn valid_key(key: &str) -> bool {
    !key.is_empty() && key.len() <= 1024 && key.bytes().all(|c| c.is_ascii_graphic())
}
fn invalid() -> QueryAttempt<ListGatewayModelsOutput> {
    rejected(
        "invalidResponse",
        FailureKind::Protocol,
        "Invalid OpenCSG response",
    )
}
fn decode_models(
    body: &[u8],
    credential: &SecretString,
) -> Option<QueryAttempt<ListGatewayModelsOutput>> {
    let catalog: WireFields = serde_json::from_slice(body).ok()?;
    let models: Vec<WireFields> = catalog.optional("data").ok()??;
    let total = models.len();
    let mut model_ids = Vec::with_capacity(total.min(50));
    for model in models {
        let id: String = model.optional("id").ok()??;
        if id.is_empty()
            || id.len() > 256
            || id.contains(['\r', '\n', '\0'])
            || id.contains(credential.reveal())
        {
            return None;
        }
        if model_ids.len() < 50 {
            model_ids.push(id);
        }
    }
    Some(QueryAttempt::branch(
        "found",
        ListGatewayModelsOutput { total, model_ids },
        Receipt::default(),
        None,
    ))
}
fn rejected(
    branch: &str,
    kind: FailureKind,
    message: &str,
) -> QueryAttempt<ListGatewayModelsOutput> {
    QueryAttempt::branch(
        branch,
        ListGatewayModelsOutput::default(),
        Receipt::default(),
        Some(Failure::new(kind, r"opencsg", "listGatewayModels", message)),
    )
}

fn retry(kind: FailureKind, message: &str, delay: f64) -> QueryAttempt<ListGatewayModelsOutput> {
    QueryAttempt::retry(
        Failure::new(kind, r"opencsg", "listGatewayModels", message),
        delay,
    )
}
