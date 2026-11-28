// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Read the Qingflow application catalog from a Dex Query Step.

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

/// The listApplications query takes an empty object.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct ListApplicationsInput {}
/// Authorized applications in provider order.
#[derive(Clone, Default, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ListApplicationsOutput {
    /// Visible applications returned by the provider.
    pub applications: Vec<ApplicationMetadata>,
}
/// Metadata for one application.
#[derive(Clone, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ApplicationMetadata {
    /// Application key.
    pub app_key: String,
    /// Application display name.
    pub app_name: String,
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
/// Bound application query. Construct once and share the HTTP client across calls.
pub struct ListApplicationsOperation {
    credentials: Arc<dyn CredentialProvider<Credentials>>,
    http: ProviderHttpClient,
    max_response_bytes: usize,
}

impl ListApplicationsOperation {
    /// Bind a Dex Web project connection, including its API settings and per-call credential resolution.
    pub fn from_project(
        project: &sdk::projectconfig::LoadedProject,
        connection_name: &str,
    ) -> Result<Self, Box<dyn std::error::Error>> {
        let key = sdk::projectconfig::ConnectionKey::new("qingflow", connection_name);
        let entry = project
            .connection_configuration(&key)
            .ok_or("Qingflow connection is missing")?;
        if entry.provider != "qingflow" {
            return Err("Qingflow project connection provider differs".into());
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
                            .ok_or("invalid Qingflow timeout setting")?
                            as u64,
                    )
                }
                "maxResponseBytes" => {
                    config.max_response_bytes = value
                        .as_u64()
                        .and_then(|n| usize::try_from(n).ok())
                        .ok_or("invalid Qingflow response size setting")?
                }
                _ => return Err("unknown Qingflow setting".into()),
            }
        }
        let provider = sdk::projectconfig::ProjectCredentialProvider::new(
            project.connections().clone(),
            key,
            Arc::new(|fields| {
                if fields.len() != 1 {
                    return Err("Invalid Qingflow credential fields".into());
                }
                fields
                    .get("access_token")
                    .and_then(serde_json::Value::as_str)
                    .filter(|value| !value.is_empty())
                    .map(Credentials::new)
                    .ok_or_else(|| "Missing Qingflow API token".into())
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
                "Qingflow response size limit must be positive",
            ));
        }
        Ok(Self {
            credentials,
            http,
            max_response_bytes,
        })
    }
}

impl Query for ListApplicationsOperation {
    type Input = ListApplicationsInput;
    type Output = ListApplicationsOutput;

    fn definition(&self) -> QueryDefinition {
        generated_definitions::ListApplications::definition()
    }

    fn invoke(&self, call: &Call<'_>, _: &Self::Input) -> QueryAttempt<Self::Output> {
        let token = match self.credentials.resolve(call) {
            Ok(token) if valid_key(token.reveal()) => token,
            _ => {
                return rejected(
                    "defect",
                    FailureKind::Authentication,
                    "Invalid Qingflow API token",
                );
            }
        };
        let request = ProviderHttpRequest::new("GET", "https://api.qingflow.com/app")
            .header("Accept", "application/json")
            .header("Accesstoken", token.reveal());
        self.read_response(&request)
    }
}
impl ListApplicationsOperation {
    fn read_response(&self, request: &ProviderHttpRequest) -> QueryAttempt<ListApplicationsOutput> {
        let mut response = match self.http.send_traced(request) {
            Ok(response) => response,
            Err(_) => return retry(FailureKind::Availability, "Qingflow request failed", 0.0),
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
                "Qingflow temporarily unavailable",
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
                "Qingflow rejected the request",
            );
        }
        let body = match read_bounded_body(&mut response, self.max_response_bytes) {
            Ok(body) => body,
            Err(ProviderHttpError::BodyTooLarge(_)) => {
                return rejected(
                    "invalidResponse",
                    FailureKind::ResponseTooLarge,
                    "Qingflow response exceeded the configured limit",
                );
            }
            Err(_) => {
                return retry(
                    FailureKind::Availability,
                    "Qingflow response interrupted",
                    0.0,
                );
            }
        };
        decode_applications(&body).unwrap_or_else(invalid)
    }
}
/// Bind the application operation to its branch targets and validate the Step configuration.
pub fn new_list_applications_step<Input>(
    config: QueryStepConfig<Input, ListApplicationsOperation>,
) -> Result<QueryStep<Input, ListApplicationsOperation>, ValidationError> {
    if config.connection.provider != r"qingflow" {
        return Err(ValidationError::new(
            "Qingflow connection provider is invalid",
        ));
    }
    QueryStep::new(config)
}

fn valid_key(key: &str) -> bool {
    !key.is_empty() && key.len() <= 1024 && key.bytes().all(|c| c.is_ascii_graphic())
}
fn invalid() -> QueryAttempt<ListApplicationsOutput> {
    rejected(
        "invalidResponse",
        FailureKind::Protocol,
        "Invalid Qingflow response",
    )
}
#[derive(Deserialize)]
#[serde(try_from = "WireFields")]
struct WireItem {
    app_key: Option<String>,
    app_name: Option<String>,
}
impl TryFrom<WireFields> for WireItem {
    type Error = serde_json::Error;
    fn try_from(fields: WireFields) -> Result<Self, Self::Error> {
        Ok(Self {
            app_key: fields.optional("appkey")?,
            app_name: fields.optional("appname")?,
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
            items: fields.optional("applist")?,
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
            code: fields.optional("errcode")?,
            data: fields.optional("result")?,
        })
    }
}
fn decode_applications(body: &[u8]) -> Option<QueryAttempt<ListApplicationsOutput>> {
    let page: DirectoryResponse = serde_json::from_slice(body).ok()?;
    if page.code? != 0 {
        return Some(rejected(
            "providerRejected",
            FailureKind::ProviderRejection,
            "Qingflow rejected the query",
        ));
    }
    let items = page.data?.items?;
    let mut applications = Vec::with_capacity(items.len());
    for item in items {
        let item = item?;
        let app_key = item.app_key?;
        let app_name = item.app_name?;
        if app_key.is_empty() || app_name.is_empty() {
            return None;
        }
        applications.push(ApplicationMetadata { app_key, app_name });
    }
    Some(QueryAttempt::branch(
        "listed",
        ListApplicationsOutput { applications },
        Receipt::default(),
        None,
    ))
}
fn rejected(
    branch: &str,
    kind: FailureKind,
    message: &str,
) -> QueryAttempt<ListApplicationsOutput> {
    QueryAttempt::branch(
        branch,
        ListApplicationsOutput::default(),
        Receipt::default(),
        Some(Failure::new(kind, r"qingflow", "listApplications", message)),
    )
}

fn retry(kind: FailureKind, message: &str, delay: f64) -> QueryAttempt<ListApplicationsOutput> {
    QueryAttempt::retry(
        Failure::new(kind, r"qingflow", "listApplications", message),
        delay,
    )
}
