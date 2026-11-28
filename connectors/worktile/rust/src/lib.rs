// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Read the Worktile project catalog from a Dex Query Step.

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

const BASE_URL: &str = "https://dev.worktile.com";
/// Select one project page.
#[derive(Clone, Default, Deserialize, Serialize)]
#[serde(default, rename_all = "camelCase")]
pub struct ListProjectsInput {
    /// Zero-based page index.
    pub page_index: i64,
    /// Number of projects per page from 1 through 100.
    pub page_size: i64,
}
/// Project page returned to Dex.
#[derive(Clone, Default, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ListProjectsOutput {
    /// Authorized projects returned in provider order.
    pub projects: Vec<Project>,
    /// Zero-based page index.
    pub page_index: i64,
    /// Page size reported by Worktile.
    pub page_size: i64,
    /// Whether another page is available.
    pub has_more: bool,
}
/// Metadata for one project visible to the connection.
#[derive(Clone, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Project {
    /// Project identifier.
    pub id: String,
    /// Project display name.
    pub name: String,
    /// Project code, or empty when absent.
    pub identifier: String,
    /// One for public projects and two for private projects.
    pub visibility: i64,
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
#[derive(Clone)]
pub struct Credentials {
    /// Provider-issued client ID.
    pub client_id: SecretString,
    /// Secret paired with the client ID.
    pub client_secret: SecretString,
}
/// Bound project query. Construct once and share the HTTP client across calls.
pub struct ListProjectsOperation {
    credentials: Arc<dyn CredentialProvider<Credentials>>,
    http: ProviderHttpClient,
    max_response_bytes: usize,
}

impl ListProjectsOperation {
    /// Bind a Dex Web project connection, including its API settings and per-call credential resolution.
    pub fn from_project(
        project: &sdk::projectconfig::LoadedProject,
        connection_name: &str,
    ) -> Result<Self, Box<dyn std::error::Error>> {
        let key = sdk::projectconfig::ConnectionKey::new("worktile", connection_name);
        let entry = project
            .connection_configuration(&key)
            .ok_or("Worktile connection is missing")?;
        if entry.provider != "worktile" {
            return Err("Worktile project connection provider differs".into());
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
                            .ok_or("invalid Worktile timeout setting")?
                            as u64,
                    )
                }
                "maxResponseBytes" => {
                    config.max_response_bytes = value
                        .as_u64()
                        .and_then(|n| usize::try_from(n).ok())
                        .ok_or("invalid Worktile response size setting")?
                }
                _ => return Err("unknown Worktile setting".into()),
            }
        }
        let provider = sdk::projectconfig::ProjectCredentialProvider::new(
            project.connections().clone(),
            key,
            Arc::new(|fields| {
                if fields.len() != 2 {
                    return Err("Invalid Worktile credential fields".into());
                }
                let field = |name: &str| {
                    fields
                        .get(name)
                        .and_then(serde_json::Value::as_str)
                        .filter(|v| !v.is_empty())
                        .map(SecretString::new)
                        .ok_or("Missing Worktile client credentials")
                };
                Ok(Credentials {
                    client_id: field("client_id")?,
                    client_secret: field("client_secret")?,
                })
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
    /// Supply the transport used for each request and its response size limit in bytes.
    pub fn with_http_client(
        credentials: Arc<dyn CredentialProvider<Credentials>>,
        http: ProviderHttpClient,
        max_response_bytes: usize,
    ) -> Result<Self, ValidationError> {
        if !(1..=16 << 20).contains(&max_response_bytes) {
            return Err(ValidationError::new(
                "Worktile response limit must be between 1 and 16777216 bytes",
            ));
        }
        Ok(Self {
            credentials,
            http,
            max_response_bytes,
        })
    }
}

impl Query for ListProjectsOperation {
    type Input = ListProjectsInput;
    type Output = ListProjectsOutput;

    fn definition(&self) -> QueryDefinition {
        generated_definitions::ListProjects::definition()
    }

    fn invoke(&self, call: &Call<'_>, input: &Self::Input) -> QueryAttempt<Self::Output> {
        if input.page_index < 0 || !(1..=100).contains(&input.page_size) {
            return rejected(
                "defect",
                FailureKind::Validation,
                "Invalid Worktile project page",
            );
        }
        let keys = match self.credentials.resolve(call) {
            Ok(keys)
                if valid_key(keys.client_id.reveal()) && valid_key(keys.client_secret.reveal()) =>
            {
                keys
            }
            _ => {
                return rejected(
                    "defect",
                    FailureKind::Authentication,
                    "Worktile credentials are unavailable or invalid",
                );
            }
        };
        let body=serde_json::json!({"client_id":keys.client_id.reveal(),"client_secret":keys.client_secret.reveal()}).to_string();
        let request =
            ProviderHttpRequest::new("POST", format!("{}/open-api/tenant-access-token", BASE_URL))
                .header("Accept", "application/json")
                .header("Content-Type", "application/json")
                .body(body);
        let auth = match self.send(&request) {
            Ok(bytes) => bytes,
            Err(attempt) => return *attempt,
        };
        let Some(token) = read_token(&auth) else {
            return invalid();
        };
        let query = form_urlencoded::Serializer::new(String::new())
            .append_pair("access_token", &token)
            .append_pair("page_index", &input.page_index.to_string())
            .append_pair("page_size", &input.page_size.to_string())
            .finish();
        let request = ProviderHttpRequest::new(
            "GET",
            format!(
                "{}/open-api/mission/get-projects-by-page?{}",
                BASE_URL, query
            ),
        )
        .header("Accept", "application/json");
        let body = match self.send(&request) {
            Ok(bytes) => bytes,
            Err(attempt) => return *attempt,
        };
        match read_projects(&body, input.page_index) {
            Some(page) => QueryAttempt::branch("found", page, Receipt::default(), None),
            None => invalid(),
        }
    }
}
impl ListProjectsOperation {
    fn send(
        &self,
        request: &ProviderHttpRequest,
    ) -> Result<Vec<u8>, Box<QueryAttempt<ListProjectsOutput>>> {
        let mut response = self.http.send_traced(request).map_err(|_| {
            Box::new(retry(
                FailureKind::Availability,
                "Worktile request failed",
                0.0,
            ))
        })?;
        let status = response.status();
        if matches!(status, 408 | 429 | 500..) {
            let delay = parse_retry_after(
                response.headers().get("Retry-After").unwrap_or(""),
                SystemTime::now(),
            );
            let kind = if status == 429 {
                FailureKind::RateLimit
            } else {
                FailureKind::Availability
            };
            return Err(Box::new(retry(
                kind,
                "Worktile temporarily unavailable",
                delay,
            )));
        }
        if status != 200 {
            return Err(Box::new(rejected(
                "providerRejected",
                match status {
                    401 => FailureKind::Authentication,
                    403 => FailureKind::Authorization,
                    _ => FailureKind::ProviderRejection,
                },
                "Worktile rejected the request",
            )));
        }
        let body = read_bounded_body(&mut response, self.max_response_bytes).map_err(|error| {
            Box::new(match error {
                ProviderHttpError::BodyTooLarge(_) => rejected(
                    "invalidResponse",
                    FailureKind::ResponseTooLarge,
                    "Worktile response exceeded the configured limit",
                ),
                _ => retry(
                    FailureKind::Availability,
                    "Worktile response was interrupted",
                    0.0,
                ),
            })
        })?;
        Ok(body)
    }
}
/// Bind the project operation to its branch targets and validate the Step configuration.
pub fn new_list_projects_step<Input>(
    config: QueryStepConfig<Input, ListProjectsOperation>,
) -> Result<QueryStep<Input, ListProjectsOperation>, ValidationError> {
    if config.connection.provider != r"worktile" {
        return Err(ValidationError::new(
            "Worktile connection provider is invalid",
        ));
    }
    QueryStep::new(config)
}

fn valid_key(key: &str) -> bool {
    !key.is_empty() && key.len() <= 1024 && key.bytes().all(|c| c.is_ascii_graphic())
}
fn invalid() -> QueryAttempt<ListProjectsOutput> {
    rejected(
        "invalidResponse",
        FailureKind::Protocol,
        "Invalid Worktile response",
    )
}
fn read_token(body: &[u8]) -> Option<String> {
    let envelope: WireFields = serde_json::from_slice(body).ok()?;
    let token = envelope.optional::<String>("tenant_access_token").ok()??;
    let expires_in = envelope.optional::<i64>("expires_in").ok()??;
    (valid_key(&token) && expires_in > 0).then_some(token)
}
fn read_projects(body: &[u8], requested_page: i64) -> Option<ListProjectsOutput> {
    let envelope: WireFields = serde_json::from_slice(body).ok()?;
    let page_index = envelope.optional::<i64>("page_index").ok()??;
    let page_size = envelope.optional::<i64>("page_size").ok()??;
    let has_more = envelope.optional::<bool>("has_more").ok()??;
    if page_index != requested_page || !(1..=100).contains(&page_size) {
        return None;
    }
    let items = envelope.optional::<Vec<WireFields>>("items").ok()??;
    let mut projects = Vec::with_capacity(items.len());
    for item in items {
        let id = item.optional::<String>("_id").ok()??;
        let name = item.optional::<String>("name").ok()??;
        let identifier = item
            .optional::<String>("identifier")
            .ok()?
            .unwrap_or_default();
        let visibility = item.optional::<i64>("visibility").ok()??;
        if id.is_empty() || name.is_empty() || !matches!(visibility, 1 | 2) {
            return None;
        }
        projects.push(Project {
            id,
            name,
            identifier,
            visibility,
        });
    }
    Some(ListProjectsOutput {
        projects,
        page_index,
        page_size,
        has_more,
    })
}
fn rejected(branch: &str, kind: FailureKind, message: &str) -> QueryAttempt<ListProjectsOutput> {
    QueryAttempt::branch(
        branch,
        ListProjectsOutput::default(),
        Receipt::default(),
        Some(Failure::new(kind, r"worktile", "listProjects", message)),
    )
}

fn retry(kind: FailureKind, message: &str, delay: f64) -> QueryAttempt<ListProjectsOutput> {
    QueryAttempt::retry(
        Failure::new(kind, r"worktile", "listProjects", message),
        delay,
    )
}
