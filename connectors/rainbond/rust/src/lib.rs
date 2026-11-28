// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Read the Rainbond team catalog from a Dex Query Step.

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

/// Select one-based team page and page size. Zeros select page one and twenty teams.
#[derive(Clone, Default, Deserialize, Serialize)]
#[serde(default, rename_all = "camelCase")]
pub struct ListTeamsInput {
    /// Page number up to 1,000,000.
    pub page: i64,
    /// Page size up to 100.
    pub page_size: i64,
}
/// Authorized teams in provider order.
#[derive(Clone, Default, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ListTeamsOutput {
    /// Visible teams returned by the provider.
    pub teams: Vec<Team>,
    /// Total number of teams reported by Rainbond.
    pub total: i64,
}
/// Metadata for one team.
#[derive(Clone, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Team {
    /// Team identifier.
    pub id: String,
    /// Team display name.
    pub name: String,
    /// Whether this team is active.
    pub active: bool,
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
        let key = sdk::projectconfig::ConnectionKey::new("rainbond", connection_name);
        let entry = project
            .connection_configuration(&key)
            .ok_or("Rainbond connection is missing")?;
        if entry.provider != "rainbond" {
            return Err("Rainbond project connection provider differs".into());
        }
        let mut config = Config::default();
        for (name, value) in entry.configuration.to_json_object()? {
            match name.as_str() {
                "baseUrl" | "timeout" | "maxResponseBytes" if value.as_str() == Some("") => {}
                "baseUrl" => {
                    config.base_url = value
                        .as_str()
                        .ok_or("invalid Rainbond API URL setting")?
                        .into()
                }
                "timeout" => {
                    config.timeout = Duration::from_nanos(
                        value
                            .as_i64()
                            .filter(|n| *n > 0)
                            .ok_or("invalid Rainbond timeout setting")?
                            as u64,
                    )
                }
                "maxResponseBytes" => {
                    config.max_response_bytes = value
                        .as_u64()
                        .and_then(|n| usize::try_from(n).ok())
                        .ok_or("invalid Rainbond response size setting")?
                }
                _ => return Err("unknown Rainbond setting".into()),
            }
        }
        let provider = sdk::projectconfig::ProjectCredentialProvider::new(
            project.connections().clone(),
            key,
            Arc::new(|fields| {
                if fields.len() != 1 {
                    return Err("Invalid Rainbond credential fields".into());
                }
                fields
                    .get("api_key")
                    .and_then(serde_json::Value::as_str)
                    .filter(|value| !value.is_empty())
                    .map(Credentials::new)
                    .ok_or_else(|| "Missing Rainbond API token".into())
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
                "Rainbond requires a deployment origin without a path",
            ));
        }
        let endpoint = origin;
        if max_response_bytes == 0 {
            return Err(ValidationError::new(
                "Rainbond response size limit must be positive",
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

    fn invoke(&self, call: &Call<'_>, input: &Self::Input) -> QueryAttempt<Self::Output> {
        let token = match self.credentials.resolve(call) {
            Ok(token) if valid_key(token.reveal()) => token,
            _ => {
                return rejected(
                    "defect",
                    FailureKind::Authentication,
                    "Invalid Rainbond API token",
                );
            }
        };
        let page = if input.page == 0 { 1 } else { input.page };
        let page_size = if input.page_size == 0 {
            20
        } else {
            input.page_size
        };
        if !(1..=1000000).contains(&page) || !(1..=100).contains(&page_size) {
            return rejected(
                "defect",
                FailureKind::Validation,
                "Invalid Rainbond team list",
            );
        }
        let request = ProviderHttpRequest::new(
            "GET",
            format!(
                "{}/openapi/v1/teams?page={page}&page_size={page_size}",
                self.endpoint
            ),
        )
        .header("Accept", "application/json")
        .header("Authorization", token.reveal());
        self.read_response(&request, page_size)
    }
}
impl ListTeamsOperation {
    fn read_response(
        &self,
        request: &ProviderHttpRequest,
        page_size: i64,
    ) -> QueryAttempt<ListTeamsOutput> {
        let mut response = match self.http.send_traced(request) {
            Ok(response) => response,
            Err(_) => return retry(FailureKind::Availability, "Rainbond request failed", 0.0),
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
                "Rainbond temporarily unavailable",
                delay,
            );
        }
        let body = match read_bounded_body(&mut response, self.max_response_bytes) {
            Ok(body) => body,
            Err(ProviderHttpError::BodyTooLarge(_)) => {
                return rejected(
                    "invalidResponse",
                    FailureKind::ResponseTooLarge,
                    "Rainbond response exceeded the configured limit",
                );
            }
            Err(_) => {
                return retry(
                    FailureKind::Availability,
                    "Rainbond response interrupted",
                    0.0,
                );
            }
        };
        if status != 200 {
            return rejected(
                "providerRejected",
                match status {
                    401 => FailureKind::Authentication,
                    403 => FailureKind::Authorization,
                    _ => FailureKind::ProviderRejection,
                },
                "Rainbond rejected the request",
            );
        }
        decode_teams(&body, page_size).unwrap_or_else(invalid)
    }
}
/// Bind the team operation to its branch targets and validate the Step configuration.
pub fn new_list_teams_step<Input>(
    config: QueryStepConfig<Input, ListTeamsOperation>,
) -> Result<QueryStep<Input, ListTeamsOperation>, ValidationError> {
    if config.connection.provider != r"rainbond" {
        return Err(ValidationError::new(
            "Rainbond connection provider is invalid",
        ));
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
        "Invalid Rainbond response",
    )
}
#[derive(Deserialize)]
#[serde(try_from = "WireFields")]
struct WireItem {
    id: Option<String>,
    name: Option<String>,
    active: Option<bool>,
}
impl TryFrom<WireFields> for WireItem {
    type Error = serde_json::Error;
    fn try_from(fields: WireFields) -> Result<Self, Self::Error> {
        Ok(Self {
            id: fields.optional("tenant_id")?,
            name: fields.optional("tenant_name")?,
            active: fields.optional("is_active")?,
        })
    }
}
#[derive(Deserialize)]
#[serde(try_from = "WireFields")]
struct DirectoryResponse {
    total: Option<i64>,
    items: Option<Vec<Option<WireItem>>>,
}
impl TryFrom<WireFields> for DirectoryResponse {
    type Error = serde_json::Error;
    fn try_from(fields: WireFields) -> Result<Self, Self::Error> {
        Ok(Self {
            total: fields.optional("total")?,
            items: fields.optional("tenants")?,
        })
    }
}
fn decode_teams(body: &[u8], page_size: i64) -> Option<QueryAttempt<ListTeamsOutput>> {
    let page: DirectoryResponse = serde_json::from_slice(body).ok()?;
    let items = page.items?;
    let total = page.total?;
    if total < 0 || items.len() > page_size as usize {
        return None;
    }
    let mut teams = Vec::with_capacity(items.len());
    for item in items {
        let item = item?;
        let id = item.id?;
        let name = item.name?;
        let active = item.active.unwrap_or_default();
        if id.is_empty() || id.len() > 256 || id.trim() != id || name.is_empty() || name.len() > 512
        {
            return None;
        }
        teams.push(Team { id, name, active });
    }
    Some(QueryAttempt::branch(
        "listed",
        ListTeamsOutput { teams, total },
        Receipt::default(),
        None,
    ))
}
fn rejected(branch: &str, kind: FailureKind, message: &str) -> QueryAttempt<ListTeamsOutput> {
    QueryAttempt::branch(
        branch,
        ListTeamsOutput::default(),
        Receipt::default(),
        Some(Failure::new(kind, r"rainbond", "listTeams", message)),
    )
}

fn retry(kind: FailureKind, message: &str, delay: f64) -> QueryAttempt<ListTeamsOutput> {
    QueryAttempt::retry(Failure::new(kind, r"rainbond", "listTeams", message), delay)
}
