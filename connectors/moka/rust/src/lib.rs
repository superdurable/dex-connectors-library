// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Read the Moka job function catalog from a Dex Query Step.

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

/// The listJobFunctions query takes an empty object.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct ListJobFunctionsInput {}
/// Functions returned in provider order.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct ListJobFunctionsOutput {
    /// Catalog entries in provider order.
    pub functions: Vec<JobFunction>,
}
/// Metadata for one job function.
#[derive(Clone, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct JobFunction {
    /// Job function identifier.
    pub id: i64,
    /// Job function label.
    pub label: String,
    /// Parent function identifier, or zero for a root.
    pub parent_id: i64,
    /// Number of jobs in the function.
    pub job_count: i64,
}
/// Settings for a single provider request.
pub struct Config {
    /// Required Moka organization identifier.
    pub org_id: String,
    /// Recruiting mode, either social (default) or campus.
    pub mode: String,
    /// Timeout per attempt. Uses the manifest default.
    pub timeout: Duration,
    /// Maximum response size in bytes. Uses the manifest default.
    pub max_response_bytes: usize,
}

impl Default for Config {
    fn default() -> Self {
        Self {
            org_id: String::new(),
            mode: "social".into(),
            timeout: Duration::from_secs(10),
            max_response_bytes: 1048576,
        }
    }
}

/// Credentials resolved from the logical connection for each request.
pub type Credentials = SecretString;
/// Bound job function query. Construct once and share the HTTP client across calls.
pub struct ListJobFunctionsOperation {
    credentials: Arc<dyn CredentialProvider<Credentials>>,
    http: ProviderHttpClient,
    endpoint: String,
    max_response_bytes: usize,
}

impl ListJobFunctionsOperation {
    /// Bind a Dex Web project connection, including its API settings and per-call credential resolution.
    pub fn from_project(
        project: &sdk::projectconfig::LoadedProject,
        connection_name: &str,
    ) -> Result<Self, Box<dyn std::error::Error>> {
        let key = sdk::projectconfig::ConnectionKey::new("moka", connection_name);
        let entry = project
            .connection_configuration(&key)
            .ok_or("Moka connection is missing")?;
        if entry.provider != "moka" {
            return Err("Moka project connection provider differs".into());
        }
        let mut config = Config::default();
        for (name, value) in entry.configuration.to_json_object()? {
            match name.as_str() {
                "orgId" => {
                    config.org_id = value
                        .as_str()
                        .ok_or("invalid Moka organization identifier")?
                        .into()
                }
                "mode" => {
                    config.mode = value.as_str().ok_or("invalid Moka recruiting mode")?.into()
                }
                "timeout" | "maxResponseBytes" if value.as_str() == Some("") => {}
                "timeout" => {
                    config.timeout = Duration::from_nanos(
                        value
                            .as_i64()
                            .filter(|n| *n > 0)
                            .ok_or("invalid Moka timeout setting")? as u64,
                    )
                }
                "maxResponseBytes" => {
                    config.max_response_bytes = value
                        .as_u64()
                        .and_then(|n| usize::try_from(n).ok())
                        .ok_or("invalid Moka response size setting")?
                }
                _ => return Err("unknown Moka setting".into()),
            }
        }
        let provider = sdk::projectconfig::ProjectCredentialProvider::new(
            project.connections().clone(),
            key,
            Arc::new(|fields| {
                if fields.len() != 1 {
                    return Err("Invalid Moka credential fields".into());
                }
                fields
                    .get("api_key")
                    .and_then(serde_json::Value::as_str)
                    .filter(|value| !value.is_empty())
                    .map(Credentials::new)
                    .ok_or_else(|| "Missing Moka API token".into())
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
        Self::with_http_client(config, credentials, http)
    }
    /// Supply a caller-owned HTTP transport and a positive response limit in bytes.
    pub fn with_http_client(
        config: Config,
        credentials: Arc<dyn CredentialProvider<Credentials>>,
        http: ProviderHttpClient,
    ) -> Result<Self, ValidationError> {
        let max_response_bytes = config.max_response_bytes;
        let mode = if config.mode.is_empty() {
            "social"
        } else {
            &config.mode
        };
        if config.org_id.is_empty()
            || config.org_id.len() > 128
            || !config
                .org_id
                .bytes()
                .all(|b| b.is_ascii_alphanumeric() || matches!(b, b'-' | b'_'))
            || !matches!(mode, "social" | "campus")
        {
            return Err(ValidationError::new(
                "Invalid Moka organization or recruiting mode",
            ));
        }
        if max_response_bytes == 0 || max_response_bytes > 16 << 20 {
            return Err(ValidationError::new(
                "Moka response size limit must be positive",
            ));
        }
        Ok(Self {
            endpoint: format!(
                "https://api.mokahr.com/api-platform/v1/jobs-groupedby-zhineng/{}?mode={}",
                config.org_id, mode
            ),
            credentials,
            http,
            max_response_bytes,
        })
    }
}

impl Query for ListJobFunctionsOperation {
    type Input = ListJobFunctionsInput;
    type Output = ListJobFunctionsOutput;

    fn definition(&self) -> QueryDefinition {
        generated_definitions::ListJobFunctions::definition()
    }

    fn invoke(&self, call: &Call<'_>, _: &Self::Input) -> QueryAttempt<Self::Output> {
        let token = match self.credentials.resolve(call) {
            Ok(token) if valid_key(token.reveal()) => token,
            _ => {
                return rejected(
                    "defect",
                    FailureKind::Authentication,
                    "Invalid Moka API token",
                );
            }
        };
        use base64::Engine;
        let authentication =
            base64::engine::general_purpose::STANDARD.encode(format!("{}:", token.reveal()));
        let request = ProviderHttpRequest::new("GET", self.endpoint.clone())
            .header("Accept", "application/json")
            .header("Authorization", format!("Basic {authentication}"));
        self.read_response(&request)
    }
}
impl ListJobFunctionsOperation {
    fn read_response(&self, request: &ProviderHttpRequest) -> QueryAttempt<ListJobFunctionsOutput> {
        let mut response = match self.http.send_traced(request) {
            Ok(response) => response,
            Err(_) => return retry(FailureKind::Availability, "Moka request failed", 0.0),
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
                "Moka temporarily unavailable",
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
                "Moka rejected the request",
            );
        }
        let body = match read_bounded_body(&mut response, self.max_response_bytes) {
            Ok(body) => body,
            Err(ProviderHttpError::BodyTooLarge(_)) => {
                return rejected(
                    "invalidResponse",
                    FailureKind::ResponseTooLarge,
                    "Moka response exceeded the configured limit",
                );
            }
            Err(_) => {
                return retry(FailureKind::Availability, "Moka response interrupted", 0.0);
            }
        };
        decode_functions(&body).unwrap_or_else(invalid)
    }
}
/// Bind the job function operation to its branch targets and validate the Step configuration.
pub fn new_list_job_functions_step<Input>(
    config: QueryStepConfig<Input, ListJobFunctionsOperation>,
) -> Result<QueryStep<Input, ListJobFunctionsOperation>, ValidationError> {
    if config.connection.provider != r"moka" {
        return Err(ValidationError::new("Moka connection provider is invalid"));
    }
    QueryStep::new(config)
}

fn valid_key(key: &str) -> bool {
    !key.is_empty() && key.len() <= 1024 && key.bytes().all(|c| c.is_ascii_graphic())
}
fn invalid() -> QueryAttempt<ListJobFunctionsOutput> {
    rejected(
        "invalidResponse",
        FailureKind::Protocol,
        "Invalid Moka response",
    )
}
#[derive(Deserialize)]
#[serde(try_from = "WireFields")]
struct WireItem {
    id: Option<i64>,
    label: Option<String>,
    parent_id: Option<i64>,
    job_count: Option<i64>,
}
impl TryFrom<WireFields> for WireItem {
    type Error = serde_json::Error;
    fn try_from(fields: WireFields) -> Result<Self, Self::Error> {
        Ok(Self {
            id: fields.optional("id")?,
            label: fields.optional("label")?,
            parent_id: fields.optional("parentid")?,
            job_count: fields.optional("jobcount")?,
        })
    }
}
#[derive(Deserialize)]
#[serde(try_from = "WireFields")]
struct DirectoryResponse {
    items: Option<Vec<Option<WireItem>>>,
}
impl TryFrom<WireFields> for DirectoryResponse {
    type Error = serde_json::Error;
    fn try_from(fields: WireFields) -> Result<Self, Self::Error> {
        Ok(Self {
            items: fields.optional("jobs")?,
        })
    }
}
fn decode_functions(body: &[u8]) -> Option<QueryAttempt<ListJobFunctionsOutput>> {
    let page: DirectoryResponse = serde_json::from_slice(body).ok()?;
    let items = page.items?;
    let mut functions = Vec::new();
    for item in items {
        let item = item?;
        let id = item.id?;
        let label = item.label?;
        let parent_id = item.parent_id.unwrap_or_default();
        let job_count = item.job_count?;
        if id <= 0 || label.is_empty() || job_count < 0 || parent_id < 0 {
            return None;
        }
        functions.push(JobFunction {
            id,
            label,
            parent_id,
            job_count,
        });
    }
    Some(QueryAttempt::branch(
        "found",
        ListJobFunctionsOutput { functions },
        Receipt::default(),
        None,
    ))
}
fn rejected(
    branch: &str,
    kind: FailureKind,
    message: &str,
) -> QueryAttempt<ListJobFunctionsOutput> {
    QueryAttempt::branch(
        branch,
        ListJobFunctionsOutput::default(),
        Receipt::default(),
        Some(Failure::new(kind, r"moka", "listJobFunctions", message)),
    )
}

fn retry(kind: FailureKind, message: &str, delay: f64) -> QueryAttempt<ListJobFunctionsOutput> {
    QueryAttempt::retry(
        Failure::new(kind, r"moka", "listJobFunctions", message),
        delay,
    )
}
