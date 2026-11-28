// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Read the Huoban space catalog from a Dex Query Step.

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

/// The listSpaces query takes an empty object.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct ListSpacesInput {}
/// Authorized spaces in provider order.
#[derive(Clone, Default, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ListSpacesOutput {
    /// Visible spaces returned by the provider.
    pub spaces: Vec<SpaceMetadata>,
}
/// Metadata for one space.
#[derive(Clone, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct SpaceMetadata {
    /// Workspace identifier.
    pub space_id: String,
    /// Workspace display name.
    pub name: String,
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
            timeout: Duration::from_secs(10),
            max_response_bytes: 1048576,
        }
    }
}

/// Credentials resolved from the logical connection for each request.
pub type Credentials = SecretString;
/// Bound space query. Construct once and share the HTTP client across calls.
pub struct ListSpacesOperation {
    credentials: Arc<dyn CredentialProvider<Credentials>>,
    http: ProviderHttpClient,
    max_response_bytes: usize,
}

impl ListSpacesOperation {
    /// Bind a Dex Web project connection, including its API settings and per-call credential resolution.
    pub fn from_project(
        project: &sdk::projectconfig::LoadedProject,
        connection_name: &str,
    ) -> Result<Self, Box<dyn std::error::Error>> {
        let key = sdk::projectconfig::ConnectionKey::new("huoban", connection_name);
        let entry = project
            .connection_configuration(&key)
            .ok_or("Huoban connection is missing")?;
        if entry.provider != "huoban" {
            return Err("Huoban project connection provider differs".into());
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
                            .ok_or("invalid Huoban timeout setting")?
                            as u64,
                    )
                }
                "maxResponseBytes" => {
                    config.max_response_bytes = value
                        .as_u64()
                        .and_then(|n| usize::try_from(n).ok())
                        .ok_or("invalid Huoban response size setting")?
                }
                _ => return Err("unknown Huoban setting".into()),
            }
        }
        let provider = sdk::projectconfig::ProjectCredentialProvider::new(
            project.connections().clone(),
            key,
            Arc::new(|fields| {
                if fields.len() != 1 {
                    return Err("Invalid Huoban credential fields".into());
                }
                fields
                    .get("api_key")
                    .and_then(serde_json::Value::as_str)
                    .filter(|value| !value.is_empty())
                    .map(Credentials::new)
                    .ok_or_else(|| "Missing Huoban API token".into())
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
    /// Supply a caller-owned HTTP transport and a positive response limit in bytes.
    pub fn with_http_client(
        credentials: Arc<dyn CredentialProvider<Credentials>>,
        http: ProviderHttpClient,
        max_response_bytes: usize,
    ) -> Result<Self, ValidationError> {
        if max_response_bytes == 0 || max_response_bytes > 16 << 20 {
            return Err(ValidationError::new(
                "Huoban response size limit must be positive",
            ));
        }
        Ok(Self {
            credentials,
            http,
            max_response_bytes,
        })
    }
}

impl Query for ListSpacesOperation {
    type Input = ListSpacesInput;
    type Output = ListSpacesOutput;

    fn definition(&self) -> QueryDefinition {
        generated_definitions::ListSpaces::definition()
    }

    fn invoke(&self, call: &Call<'_>, _: &Self::Input) -> QueryAttempt<Self::Output> {
        let token = match self.credentials.resolve(call) {
            Ok(token) if valid_key(token.reveal()) => token,
            _ => {
                return rejected(
                    "defect",
                    FailureKind::Authentication,
                    "Invalid Huoban API token",
                );
            }
        };
        let request =
            ProviderHttpRequest::new("GET", "https://api.huoban.com/openapi/v1/space/list")
                .header("Accept", "application/json")
                .header("Open-Authorization", format!("Bearer {}", token.reveal()));
        self.read_response(&request)
    }
}
impl ListSpacesOperation {
    fn read_response(&self, request: &ProviderHttpRequest) -> QueryAttempt<ListSpacesOutput> {
        let mut response = match self.http.send_traced(request) {
            Ok(response) => response,
            Err(_) => return retry(FailureKind::Availability, "Huoban request failed", 0.0),
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
                "Huoban temporarily unavailable",
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
                "Huoban rejected the request",
            );
        }
        let body = match read_bounded_body(&mut response, self.max_response_bytes) {
            Ok(body) => body,
            Err(ProviderHttpError::BodyTooLarge(_)) => {
                return rejected(
                    "invalidResponse",
                    FailureKind::ResponseTooLarge,
                    "Huoban response exceeded the configured limit",
                );
            }
            Err(_) => {
                return retry(
                    FailureKind::Availability,
                    "Huoban response interrupted",
                    0.0,
                );
            }
        };
        decode_spaces(&body).unwrap_or_else(invalid)
    }
}
/// Bind the space operation to its branch targets and validate the Step configuration.
pub fn new_list_spaces_step<Input>(
    config: QueryStepConfig<Input, ListSpacesOperation>,
) -> Result<QueryStep<Input, ListSpacesOperation>, ValidationError> {
    if config.connection.provider != r"huoban" {
        return Err(ValidationError::new(
            "Huoban connection provider is invalid",
        ));
    }
    QueryStep::new(config)
}

fn valid_key(key: &str) -> bool {
    !key.is_empty() && key.len() <= 1024 && key.bytes().all(|c| c.is_ascii_graphic())
}
fn invalid() -> QueryAttempt<ListSpacesOutput> {
    rejected(
        "invalidResponse",
        FailureKind::Protocol,
        "Invalid Huoban response",
    )
}
#[derive(Deserialize)]
#[serde(try_from = "WireFields")]
struct WireItem {
    space_id: Option<String>,
    name: Option<String>,
}
impl TryFrom<WireFields> for WireItem {
    type Error = serde_json::Error;
    fn try_from(fields: WireFields) -> Result<Self, Self::Error> {
        Ok(Self {
            space_id: fields.optional("space_id")?,
            name: fields.optional("name")?,
        })
    }
}
#[derive(Deserialize)]
#[serde(try_from = "WireFields")]
struct DirectoryData {
    items: Option<Vec<Option<WireItem>>>,
}
impl TryFrom<WireFields> for DirectoryData {
    type Error = serde_json::Error;
    fn try_from(fields: WireFields) -> Result<Self, Self::Error> {
        Ok(Self {
            items: fields.optional("spaces")?,
        })
    }
}
#[derive(Deserialize)]
#[serde(try_from = "WireFields")]
struct DirectoryResponse {
    code: Option<i64>,
    data: Option<DirectoryData>,
}
impl TryFrom<WireFields> for DirectoryResponse {
    type Error = serde_json::Error;
    fn try_from(fields: WireFields) -> Result<Self, Self::Error> {
        Ok(Self {
            code: fields.optional("code")?,
            data: fields.optional("data")?,
        })
    }
}
fn decode_spaces(body: &[u8]) -> Option<QueryAttempt<ListSpacesOutput>> {
    let page: DirectoryResponse = serde_json::from_slice(body).ok()?;
    if page.code? != 0 {
        return Some(rejected(
            "providerRejected",
            FailureKind::ProviderRejection,
            "Huoban rejected the query",
        ));
    }
    let items = page.data?.items?;
    let mut spaces = Vec::with_capacity(items.len());
    for item in items {
        let item = item?;
        let space_id = item.space_id?;
        let name = item.name?;
        if space_id.is_empty() || name.is_empty() {
            return None;
        }
        spaces.push(SpaceMetadata { space_id, name });
    }
    Some(QueryAttempt::branch(
        "listed",
        ListSpacesOutput { spaces },
        Receipt::default(),
        None,
    ))
}
fn rejected(branch: &str, kind: FailureKind, message: &str) -> QueryAttempt<ListSpacesOutput> {
    QueryAttempt::branch(
        branch,
        ListSpacesOutput::default(),
        Receipt::default(),
        Some(Failure::new(kind, r"huoban", "listSpaces", message)),
    )
}

fn retry(kind: FailureKind, message: &str, delay: f64) -> QueryAttempt<ListSpacesOutput> {
    QueryAttempt::retry(Failure::new(kind, r"huoban", "listSpaces", message), delay)
}
