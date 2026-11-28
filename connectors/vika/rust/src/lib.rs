// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Read the Vika space catalog from a Dex Query Step.

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
pub struct SpacePage {
    /// Visible spaces returned by the provider.
    pub spaces: Vec<Space>,
}
/// Metadata for one space.
#[derive(Clone, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Space {
    /// Workspace identifier.
    pub id: String,
    /// Workspace display name.
    pub name: String,
    /// Whether the token owner administers the workspace.
    pub is_admin: bool,
}
/// Settings for a single provider request.
pub struct Config {
    /// Provider API base URL. Defaults to https://vika.cn.
    pub base_url: String,
    /// Timeout per attempt. Uses the manifest default.
    pub timeout: Duration,
    /// Maximum response size in bytes. Uses the manifest default.
    pub max_response_bytes: usize,
}

impl Default for Config {
    fn default() -> Self {
        Self {
            base_url: "https://vika.cn".into(),
            timeout: Duration::from_secs(15),
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
    endpoint: String,
    max_response_bytes: usize,
}

impl ListSpacesOperation {
    /// Bind a Dex Web project connection, including its API settings and per-call credential resolution.
    pub fn from_project(
        project: &sdk::projectconfig::LoadedProject,
        connection_name: &str,
    ) -> Result<Self, Box<dyn std::error::Error>> {
        let key = sdk::projectconfig::ConnectionKey::new("vika", connection_name);
        let entry = project
            .connection_configuration(&key)
            .ok_or("Vika connection is missing")?;
        if entry.provider != "vika" {
            return Err("Vika project connection provider differs".into());
        }
        let mut config = Config::default();
        for (name, value) in entry.configuration.to_json_object()? {
            match name.as_str() {
                "baseUrl" | "timeout" | "maxResponseBytes" if value.as_str() == Some("") => {}
                "baseUrl" => {
                    config.base_url = value.as_str().ok_or("invalid Vika API URL setting")?.into()
                }
                "timeout" => {
                    config.timeout = Duration::from_nanos(
                        value
                            .as_i64()
                            .filter(|n| *n > 0)
                            .ok_or("invalid Vika timeout setting")? as u64,
                    )
                }
                "maxResponseBytes" => {
                    config.max_response_bytes = value
                        .as_u64()
                        .and_then(|n| usize::try_from(n).ok())
                        .ok_or("invalid Vika response size setting")?
                }
                _ => return Err("unknown Vika setting".into()),
            }
        }
        let provider = sdk::projectconfig::ProjectCredentialProvider::new(
            project.connections().clone(),
            key,
            Arc::new(|fields| {
                if fields.len() != 1 {
                    return Err("Invalid Vika credential fields".into());
                }
                fields
                    .get("api_token")
                    .and_then(serde_json::Value::as_str)
                    .filter(|value| !value.is_empty())
                    .map(Credentials::new)
                    .ok_or_else(|| "Missing Vika API token".into())
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
        Self::with_base_url(
            credentials,
            http,
            config.max_response_bytes,
            &config.base_url,
        )
    }

    /// Bind the deployment origin, a caller-supplied transport and a positive byte limit.
    pub fn with_base_url(
        credentials: Arc<dyn CredentialProvider<Credentials>>,
        http: ProviderHttpClient,
        max_response_bytes: usize,
        base_url: &str,
    ) -> Result<Self, ValidationError> {
        let origin = sdk::providerhttp::validate_base_url(if base_url.is_empty() {
            "https://vika.cn"
        } else {
            base_url
        })?;
        let endpoint = origin;
        if max_response_bytes == 0 {
            return Err(ValidationError::new(
                "Vika response size limit must be positive",
            ));
        }
        Ok(Self {
            credentials,
            endpoint,
            http,
            max_response_bytes,
        })
    }
}

impl Query for ListSpacesOperation {
    type Input = ListSpacesInput;
    type Output = SpacePage;

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
                    "Invalid Vika API token",
                );
            }
        };
        let request =
            ProviderHttpRequest::new("GET", format!("{}/fusion/v1/spaces", self.endpoint))
                .header("Accept", "application/json")
                .header("Authorization", format!("Bearer {}", token.reveal()));
        self.read_response(&request)
    }
}
impl ListSpacesOperation {
    fn read_response(&self, request: &ProviderHttpRequest) -> QueryAttempt<SpacePage> {
        let mut response = match self.http.send_traced(request) {
            Ok(response) => response,
            Err(_) => return retry(FailureKind::Availability, "Vika request failed", 0.0),
        };
        let status = response.status();
        let delay = parse_retry_after(
            response.headers().get("Retry-After").unwrap_or(""),
            SystemTime::now(),
        );
        if matches!(status, 408 | 429 | 500..) {
            return retry(
                if status == 429 {
                    FailureKind::RateLimit
                } else {
                    FailureKind::Availability
                },
                "Vika temporarily unavailable",
                delay,
            );
        }
        let body = match read_bounded_body(&mut response, self.max_response_bytes) {
            Ok(body) => body,
            Err(ProviderHttpError::BodyTooLarge(_)) => {
                return rejected(
                    "invalidResponse",
                    FailureKind::ResponseTooLarge,
                    "Vika response exceeded the configured limit",
                );
            }
            Err(_) => return retry(FailureKind::Availability, "Vika response interrupted", 0.0),
        };
        if status != 200 {
            return rejected(
                "providerRejected",
                match status {
                    401 => FailureKind::Authentication,
                    403 => FailureKind::Authorization,
                    400 | 404 => FailureKind::Validation,
                    _ => FailureKind::ProviderRejection,
                },
                "Vika rejected the request",
            );
        }
        decode_spaces(&body, delay).unwrap_or_else(invalid)
    }
}
/// Bind the space operation to its branch targets and validate the Step configuration.
pub fn new_list_spaces_step<Input>(
    config: QueryStepConfig<Input, ListSpacesOperation>,
) -> Result<QueryStep<Input, ListSpacesOperation>, ValidationError> {
    if config.connection.provider != r"vika" {
        return Err(ValidationError::new("Vika connection provider is invalid"));
    }
    QueryStep::new(config)
}

fn valid_key(key: &str) -> bool {
    !key.is_empty() && key.len() <= 512 && key.bytes().all(|c| c.is_ascii_graphic())
}
fn invalid() -> QueryAttempt<SpacePage> {
    rejected(
        "invalidResponse",
        FailureKind::Protocol,
        "Invalid Vika response",
    )
}
#[derive(Deserialize)]
#[serde(try_from = "WireFields")]
struct WireItem {
    id: Option<String>,
    name: Option<String>,
    is_admin: Option<bool>,
}
impl TryFrom<WireFields> for WireItem {
    type Error = serde_json::Error;
    fn try_from(fields: WireFields) -> Result<Self, Self::Error> {
        Ok(Self {
            id: fields.optional("id")?,
            name: fields.optional("name")?,
            is_admin: fields.optional("isadmin")?,
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
    success: Option<bool>,
    code: Option<i64>,
    data: Option<DirectoryData>,
}
impl TryFrom<WireFields> for DirectoryResponse {
    type Error = serde_json::Error;
    fn try_from(fields: WireFields) -> Result<Self, Self::Error> {
        Ok(Self {
            success: fields.optional("success")?,
            code: fields.optional("code")?,
            data: fields.optional("data")?,
        })
    }
}
fn decode_spaces(body: &[u8], delay: f64) -> Option<QueryAttempt<SpacePage>> {
    let page: DirectoryResponse = serde_json::from_slice(body).ok()?;
    let code = page.code?;
    if !page.success? {
        return Some(match code {
            200 => QueryAttempt::branch("listed", SpacePage::default(), Receipt::default(), None),
            429 => retry(FailureKind::RateLimit, "Vika rate limited the query", delay),
            500.. => retry(
                FailureKind::Availability,
                "Vika temporarily unavailable",
                delay,
            ),
            _ => rejected(
                "providerRejected",
                FailureKind::ProviderRejection,
                "Vika rejected the query",
            ),
        });
    }
    if code != 200 {
        return None;
    }
    let items = page.data?.items?;
    if items.len() > 1000 {
        return None;
    }
    let mut spaces = Vec::with_capacity(items.len());
    for item in items {
        let item = item?;
        let id = item.id?;
        let name = item.name?;
        let is_admin = item.is_admin.unwrap_or_default();
        if id.is_empty() || !id.starts_with("spc") || name.is_empty() {
            return None;
        }
        spaces.push(Space { id, name, is_admin });
    }
    Some(QueryAttempt::branch(
        "listed",
        SpacePage { spaces },
        Receipt::default(),
        None,
    ))
}
fn rejected(branch: &str, kind: FailureKind, message: &str) -> QueryAttempt<SpacePage> {
    QueryAttempt::branch(
        branch,
        SpacePage::default(),
        Receipt::default(),
        Some(Failure::new(kind, r"vika", "listSpaces", message)),
    )
}

fn retry(kind: FailureKind, message: &str, delay: f64) -> QueryAttempt<SpacePage> {
    QueryAttempt::retry(Failure::new(kind, r"vika", "listSpaces", message), delay)
}
