// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Read the ONES team catalog from a Dex Query Step.

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

/// The listTeams query takes an empty object.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct ListTeamsInput {}
/// Authorized teams in provider order.
#[derive(Clone, Default, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ListTeamsOutput {
    /// Visible teams returned by the provider.
    pub teams: Vec<Team>,
}
/// Metadata for one team.
#[derive(Clone, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Team {
    /// Team identifier.
    pub id: String,
    /// Team display name.
    pub name: String,
    /// Team status reported by ONES.
    pub status: i64,
}
/// Settings for a single provider request.
pub struct Config {
    /// Required deployment origin without a path.
    pub base_url: String,
    /// Timeout per attempt. Uses the manifest default.
    pub timeout: Duration,
    /// Maximum response size in bytes. Uses the manifest default.
    pub max_response_bytes: usize,
}

impl Default for Config {
    fn default() -> Self {
        Self {
            base_url: String::new(),
            timeout: Duration::from_secs(10),
            max_response_bytes: 2097152,
        }
    }
}

/// Credentials resolved from the logical connection for each request.
pub type Credentials = SecretString;
/// Bound team query. Construct once and share the HTTP client across calls.
pub struct ListTeamsOperation {
    credentials: Arc<dyn CredentialProvider<Credentials>>,
    http: ProviderHttpClient,
    endpoint: String,
    max_response_bytes: usize,
}

impl ListTeamsOperation {
    /// Bind a Dex Web project connection, including its API settings and per-call credential resolution.
    pub fn from_project(
        project: &sdk::projectconfig::LoadedProject,
        connection_name: &str,
    ) -> Result<Self, Box<dyn std::error::Error>> {
        let key = sdk::projectconfig::ConnectionKey::new("ones", connection_name);
        let entry = project
            .connection_configuration(&key)
            .ok_or("ONES connection is missing")?;
        if entry.provider != "ones" {
            return Err("ONES project connection provider differs".into());
        }
        let mut config = Config::default();
        for (name, value) in entry.configuration.to_json_object()? {
            match name.as_str() {
                "baseUrl" | "timeout" | "maxResponseBytes" if value.as_str() == Some("") => {}
                "baseUrl" => {
                    config.base_url = value.as_str().ok_or("invalid ONES API URL setting")?.into()
                }
                "timeout" => {
                    config.timeout = Duration::from_nanos(
                        value
                            .as_i64()
                            .filter(|n| *n > 0)
                            .ok_or("invalid ONES timeout setting")? as u64,
                    )
                }
                "maxResponseBytes" => {
                    config.max_response_bytes = value
                        .as_u64()
                        .and_then(|n| usize::try_from(n).ok())
                        .ok_or("invalid ONES response size setting")?
                }
                _ => return Err("unknown ONES setting".into()),
            }
        }
        let provider = sdk::projectconfig::ProjectCredentialProvider::new(
            project.connections().clone(),
            key,
            Arc::new(|fields| {
                if fields.len() != 1 {
                    return Err("Invalid ONES credential fields".into());
                }
                fields
                    .get("api_key")
                    .and_then(serde_json::Value::as_str)
                    .filter(|value| !value.is_empty())
                    .map(Credentials::new)
                    .ok_or_else(|| "Missing ONES API token".into())
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
        let origin = sdk::providerhttp::validate_base_url(base_url)?;
        if origin
            .split_once("://")
            .is_none_or(|(_, authority)| authority.contains('/'))
        {
            return Err(ValidationError::new(
                "ONES requires a deployment origin without a path",
            ));
        }
        let endpoint = origin;
        if max_response_bytes == 0 {
            return Err(ValidationError::new(
                "ONES response size limit must be positive",
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

impl Query for ListTeamsOperation {
    type Input = ListTeamsInput;
    type Output = ListTeamsOutput;

    fn definition(&self) -> QueryDefinition {
        generated_definitions::ListTeams::definition()
    }

    fn invoke(&self, call: &Call<'_>, _: &Self::Input) -> QueryAttempt<Self::Output> {
        let token = match self.credentials.resolve(call) {
            Ok(token) if valid_key(token.reveal()) => token,
            _ => {
                return rejected(
                    "defect",
                    FailureKind::Authentication,
                    "Invalid ONES API token",
                );
            }
        };
        let request =
            ProviderHttpRequest::new("GET", format!("{}/openapi/v2/account/teams", self.endpoint))
                .header("Accept", "application/json")
                .header("Authorization", format!("Bearer {}", token.reveal()));
        self.read_response(&request)
    }
}
impl ListTeamsOperation {
    fn read_response(&self, request: &ProviderHttpRequest) -> QueryAttempt<ListTeamsOutput> {
        let mut response = match self.http.send_traced(request) {
            Ok(response) => response,
            Err(_) => return retry(FailureKind::Availability, "ONES request failed", 0.0),
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
                "ONES temporarily unavailable",
                delay,
            );
        }
        let body = match read_bounded_body(&mut response, self.max_response_bytes) {
            Ok(body) => body,
            Err(ProviderHttpError::BodyTooLarge(_)) => {
                return rejected(
                    "invalidResponse",
                    FailureKind::ResponseTooLarge,
                    "ONES response exceeded the configured limit",
                );
            }
            Err(_) => return retry(FailureKind::Availability, "ONES response interrupted", 0.0),
        };
        if status != 200 {
            return rejected(
                "providerRejected",
                match status {
                    401 => FailureKind::Authentication,
                    403 => FailureKind::Authorization,
                    _ => FailureKind::ProviderRejection,
                },
                "ONES rejected the request",
            );
        }
        decode_teams(&body).unwrap_or_else(invalid)
    }
}
/// Bind the team operation to its branch targets and validate the Step configuration.
pub fn new_list_teams_step<Input>(
    config: QueryStepConfig<Input, ListTeamsOperation>,
) -> Result<QueryStep<Input, ListTeamsOperation>, ValidationError> {
    if config.connection.provider != r"ones" {
        return Err(ValidationError::new("ONES connection provider is invalid"));
    }
    QueryStep::new(config)
}

fn valid_key(key: &str) -> bool {
    !key.is_empty() && key.len() <= 512 && key.bytes().all(|c| c.is_ascii_graphic())
}
fn invalid() -> QueryAttempt<ListTeamsOutput> {
    rejected(
        "invalidResponse",
        FailureKind::Protocol,
        "Invalid ONES response",
    )
}
#[derive(Deserialize)]
#[serde(try_from = "WireFields")]
struct WireItem {
    id: Option<String>,
    name: Option<String>,
    status: Option<i64>,
}
impl TryFrom<WireFields> for WireItem {
    type Error = serde_json::Error;
    fn try_from(fields: WireFields) -> Result<Self, Self::Error> {
        Ok(Self {
            id: fields.optional("id")?,
            name: fields.optional("name")?,
            status: fields.optional("status")?,
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
            items: fields.optional("teams")?,
        })
    }
}
#[derive(Deserialize)]
#[serde(try_from = "WireFields")]
struct DirectoryResponse {
    result: Option<String>,
    error_code: Option<String>,
    data: Option<DirectoryData>,
}
impl TryFrom<WireFields> for DirectoryResponse {
    type Error = serde_json::Error;
    fn try_from(fields: WireFields) -> Result<Self, Self::Error> {
        Ok(Self {
            result: fields.optional("result")?,
            error_code: fields.optional("errorcode")?,
            data: fields.optional("data")?,
        })
    }
}
fn decode_teams(body: &[u8]) -> Option<QueryAttempt<ListTeamsOutput>> {
    let page: DirectoryResponse = serde_json::from_slice(body).ok()?;
    match page.result.as_deref() {
        Some("SUCCESS") => {}
        Some("FAIL") => {
            return Some(rejected(
                "providerRejected",
                match page.error_code.as_deref() {
                    Some("Unauthorized") => FailureKind::Authentication,
                    Some("Forbidden") => FailureKind::Authorization,
                    _ => FailureKind::ProviderRejection,
                },
                "ONES rejected the query",
            ));
        }
        _ => return None,
    }
    let items = page.data?.items?;
    if items.len() > 10000 {
        return None;
    }
    let mut teams = Vec::with_capacity(items.len());
    for item in items {
        let item = item?;
        let id = item.id?;
        let name = item.name?;
        let status = item.status?;
        if id.is_empty() || id.len() > 256 || id.trim() != id || name.is_empty() || name.len() > 512
        {
            return None;
        }
        teams.push(Team { id, name, status });
    }
    Some(QueryAttempt::branch(
        "listed",
        ListTeamsOutput { teams },
        Receipt::default(),
        None,
    ))
}
fn rejected(branch: &str, kind: FailureKind, message: &str) -> QueryAttempt<ListTeamsOutput> {
    QueryAttempt::branch(
        branch,
        ListTeamsOutput::default(),
        Receipt::default(),
        Some(Failure::new(kind, r"ones", "listTeams", message)),
    )
}

fn retry(kind: FailureKind, message: &str, delay: f64) -> QueryAttempt<ListTeamsOutput> {
    QueryAttempt::retry(Failure::new(kind, r"ones", "listTeams", message), delay)
}
