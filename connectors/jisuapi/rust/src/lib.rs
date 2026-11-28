// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Read the JisuAPI province catalog from a Dex Query Step.

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

/// The listProvinces query takes an empty object.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct ListProvincesInput {}
/// Authorized provinces in provider order.
#[derive(Clone, Default, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ListProvincesOutput {
    /// Visible provinces returned by the provider.
    pub provinces: Vec<Province>,
}
/// Metadata for one space.
#[derive(Clone, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Province {
    /// Province identifier.
    pub id: String,
    /// Province name.
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
/// Bound province query. Construct once and share the HTTP client across calls.
pub struct ListProvincesOperation {
    credentials: Arc<dyn CredentialProvider<Credentials>>,
    http: ProviderHttpClient,
    max_response_bytes: usize,
}

impl ListProvincesOperation {
    /// Bind a Dex Web project connection, including its API settings and per-call credential resolution.
    pub fn from_project(
        project: &sdk::projectconfig::LoadedProject,
        connection_name: &str,
    ) -> Result<Self, Box<dyn std::error::Error>> {
        let key = sdk::projectconfig::ConnectionKey::new("jisuapi", connection_name);
        let entry = project
            .connection_configuration(&key)
            .ok_or("JisuAPI connection is missing")?;
        if entry.provider != "jisuapi" {
            return Err("JisuAPI project connection provider differs".into());
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
                            .ok_or("invalid JisuAPI timeout setting")?
                            as u64,
                    )
                }
                "maxResponseBytes" => {
                    config.max_response_bytes = value
                        .as_u64()
                        .and_then(|n| usize::try_from(n).ok())
                        .ok_or("invalid JisuAPI response size setting")?
                }
                _ => return Err("unknown JisuAPI setting".into()),
            }
        }
        let provider = sdk::projectconfig::ProjectCredentialProvider::new(
            project.connections().clone(),
            key,
            Arc::new(|fields| {
                if fields.len() != 1 {
                    return Err("Invalid JisuAPI credential fields".into());
                }
                fields
                    .get("appkey")
                    .and_then(serde_json::Value::as_str)
                    .filter(|value| !value.is_empty())
                    .map(Credentials::new)
                    .ok_or_else(|| "Missing JisuAPI API token".into())
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
                "JisuAPI response size limit must be positive",
            ));
        }
        Ok(Self {
            credentials,
            http,
            max_response_bytes,
        })
    }
}

impl Query for ListProvincesOperation {
    type Input = ListProvincesInput;
    type Output = ListProvincesOutput;

    fn definition(&self) -> QueryDefinition {
        generated_definitions::ListProvinces::definition()
    }

    fn invoke(&self, call: &Call<'_>, _: &Self::Input) -> QueryAttempt<Self::Output> {
        let token = match self.credentials.resolve(call) {
            Ok(token) if valid_key(token.reveal()) => token,
            _ => {
                return rejected(
                    "defect",
                    FailureKind::Authentication,
                    "Invalid JisuAPI API token",
                );
            }
        };
        let request = ProviderHttpRequest::new(
            "GET",
            format!(
                "https://api.jisuapi.com/area/province?appkey={}",
                form_urlencoded::byte_serialize(token.reveal().as_bytes())
                    .collect::<String>()
                    .replace("*", "%2A")
                    .replace("%7E", "~")
            ),
        )
        .header("Accept", "application/json");
        self.read_response(&request)
    }
}
impl ListProvincesOperation {
    fn read_response(&self, request: &ProviderHttpRequest) -> QueryAttempt<ListProvincesOutput> {
        let mut response = match self.http.send_traced(request) {
            Ok(response) => response,
            Err(_) => return retry(FailureKind::Availability, "JisuAPI request failed", 0.0),
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
                "JisuAPI temporarily unavailable",
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
                "JisuAPI rejected the request",
            );
        }
        let body = match read_bounded_body(&mut response, self.max_response_bytes) {
            Ok(body) => body,
            Err(ProviderHttpError::BodyTooLarge(_)) => {
                return rejected(
                    "invalidResponse",
                    FailureKind::ResponseTooLarge,
                    "JisuAPI response exceeded the configured limit",
                );
            }
            Err(_) => {
                return retry(
                    FailureKind::Availability,
                    "JisuAPI response interrupted",
                    0.0,
                );
            }
        };
        decode_provinces(&body).unwrap_or_else(invalid)
    }
}
/// Bind the province operation to its branch targets and validate the Step configuration.
pub fn new_list_provinces_step<Input>(
    config: QueryStepConfig<Input, ListProvincesOperation>,
) -> Result<QueryStep<Input, ListProvincesOperation>, ValidationError> {
    if config.connection.provider != r"jisuapi" {
        return Err(ValidationError::new(
            "JisuAPI connection provider is invalid",
        ));
    }
    QueryStep::new(config)
}

fn valid_key(key: &str) -> bool {
    !key.is_empty() && key.len() <= 1024 && key.bytes().all(|c| c.is_ascii_graphic())
}
fn invalid() -> QueryAttempt<ListProvincesOutput> {
    rejected(
        "invalidResponse",
        FailureKind::Protocol,
        "Invalid JisuAPI response",
    )
}
#[derive(Deserialize)]
#[serde(try_from = "WireFields")]
struct WireItem {
    id: Option<String>,
    name: Option<String>,
}
impl TryFrom<WireFields> for WireItem {
    type Error = serde_json::Error;
    fn try_from(fields: WireFields) -> Result<Self, Self::Error> {
        Ok(Self {
            id: fields.optional("id")?,
            name: fields.optional("name")?,
        })
    }
}
#[derive(Deserialize)]
#[serde(try_from = "WireFields")]
struct DirectoryResponse {
    code: Option<i64>,
    items: Option<Vec<Option<WireItem>>>,
}
impl TryFrom<WireFields> for DirectoryResponse {
    type Error = serde_json::Error;
    fn try_from(fields: WireFields) -> Result<Self, Self::Error> {
        Ok(Self {
            code: fields.optional("status")?,
            items: fields.optional("result")?,
        })
    }
}
fn decode_provinces(body: &[u8]) -> Option<QueryAttempt<ListProvincesOutput>> {
    let page: DirectoryResponse = serde_json::from_slice(body).ok()?;
    match page.code? {
        0 => {}
        104 | 106 => {
            return Some(retry(
                FailureKind::RateLimit,
                "JisuAPI rate limited the query",
                0.0,
            ));
        }
        107 => {
            return Some(retry(
                FailureKind::Availability,
                "JisuAPI is temporarily unavailable",
                0.0,
            ));
        }
        _ => {
            return Some(rejected(
                "providerRejected",
                FailureKind::ProviderRejection,
                "JisuAPI rejected the query",
            ));
        }
    }
    let items = page.items?;
    let mut provinces = Vec::new();
    for item in items {
        let item = item?;
        let id = item.id?;
        if id.is_empty() {
            return None;
        }
        let name = item.name?;
        if name.is_empty() {
            return None;
        }
        provinces.push(Province { id, name });
    }
    Some(QueryAttempt::branch(
        "listed",
        ListProvincesOutput { provinces },
        Receipt::default(),
        None,
    ))
}
fn rejected(branch: &str, kind: FailureKind, message: &str) -> QueryAttempt<ListProvincesOutput> {
    QueryAttempt::branch(
        branch,
        ListProvincesOutput::default(),
        Receipt::default(),
        Some(Failure::new(kind, r"jisuapi", "listProvinces", message)),
    )
}

fn retry(kind: FailureKind, message: &str, delay: f64) -> QueryAttempt<ListProvincesOutput> {
    QueryAttempt::retry(
        Failure::new(kind, r"jisuapi", "listProvinces", message),
        delay,
    )
}
