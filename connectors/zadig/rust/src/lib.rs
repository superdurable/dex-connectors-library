// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Read the Zadig project catalog from a Dex Query Step.

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

/// Select one project page.
#[derive(Clone, Default, Deserialize, Serialize)]
#[serde(default, rename_all = "camelCase")]
pub struct ListProjectsInput {
    /// One-based page number up to 1,000,000. Zero selects the first page.
    pub page_num: i64,
    /// Number of projects per page from 1 through 100. Zero selects 20.
    pub page_size: i64,
}
/// Project page returned to Dex.
#[derive(Clone, Default, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ListProjectsOutput {
    /// Authorized projects returned in provider order.
    pub projects: Vec<Project>,
    /// Total projects reported by Zadig.
    pub total: i64,
}
/// Metadata for one project visible to the connection.
#[derive(Clone, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Project {
    /// Project key.
    pub key: String,
    /// Project display name.
    pub name: String,
    /// Project deployment type, or empty when absent.
    pub deploy_type: String,
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
/// Bound project query. Construct once and share the HTTP client across calls.
pub struct ListProjectsOperation {
    credentials: Arc<dyn CredentialProvider<Credentials>>,
    http: ProviderHttpClient,
    endpoint: String,
    max_response_bytes: usize,
}

impl ListProjectsOperation {
    /// Bind a Dex Web project connection, including its API settings and per-call credential resolution.
    pub fn from_project(
        project: &sdk::projectconfig::LoadedProject,
        connection_name: &str,
    ) -> Result<Self, Box<dyn std::error::Error>> {
        let key = sdk::projectconfig::ConnectionKey::new("zadig", connection_name);
        let entry = project
            .connection_configuration(&key)
            .ok_or("Zadig connection is missing")?;
        if entry.provider != "zadig" {
            return Err("Zadig project connection provider differs".into());
        }
        let mut config = Config::default();
        for (name, value) in entry.configuration.to_json_object()? {
            match name.as_str() {
                "baseUrl" | "timeout" | "maxResponseBytes" if value.as_str() == Some("") => {}
                "baseUrl" => {
                    config.base_url = value
                        .as_str()
                        .ok_or("invalid Zadig API URL setting")?
                        .into()
                }
                "timeout" => {
                    config.timeout = Duration::from_nanos(
                        value
                            .as_i64()
                            .filter(|n| *n > 0)
                            .ok_or("invalid Zadig timeout setting")? as u64,
                    )
                }
                "maxResponseBytes" => {
                    config.max_response_bytes = value
                        .as_u64()
                        .and_then(|n| usize::try_from(n).ok())
                        .ok_or("invalid Zadig response size setting")?
                }
                _ => return Err("unknown Zadig setting".into()),
            }
        }
        let provider = sdk::projectconfig::ProjectCredentialProvider::new(
            project.connections().clone(),
            key,
            Arc::new(|fields| {
                if fields.len() != 1 {
                    return Err("Invalid Zadig credential fields".into());
                }
                fields
                    .get("api_key")
                    .and_then(serde_json::Value::as_str)
                    .filter(|value| !value.is_empty())
                    .map(Credentials::new)
                    .ok_or_else(|| "Missing Zadig API token".into())
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
                "Zadig requires a deployment origin without a path",
            ));
        }
        let endpoint = origin;
        if max_response_bytes == 0 {
            return Err(ValidationError::new(
                "Zadig response size limit must be positive",
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

impl Query for ListProjectsOperation {
    type Input = ListProjectsInput;
    type Output = ListProjectsOutput;

    fn definition(&self) -> QueryDefinition {
        generated_definitions::ListProjects::definition()
    }

    fn invoke(&self, call: &Call<'_>, input: &Self::Input) -> QueryAttempt<Self::Output> {
        let token = match self.credentials.resolve(call) {
            Ok(token) if valid_key(token.reveal()) => token,
            _ => {
                return rejected(
                    "defect",
                    FailureKind::Authentication,
                    "Invalid Zadig API token",
                );
            }
        };
        let page_num = if input.page_num == 0 {
            1
        } else {
            input.page_num
        };
        let page_size = if input.page_size == 0 {
            20
        } else {
            input.page_size
        };
        if !(1..=1000000).contains(&page_num) || !(1..=100).contains(&page_size) {
            return rejected(
                "defect",
                FailureKind::Validation,
                "Invalid Zadig project page",
            );
        }
        let request = ProviderHttpRequest::new(
            "GET",
            format!(
                "{}/openapi/projects/project?pageNum={page_num}&pageSize={page_size}",
                self.endpoint
            ),
        )
        .header("Accept", "application/json")
        .header("Authorization", format!("Bearer {}", token.reveal()));
        let body = match self.send(&request) {
            Ok(bytes) => bytes,
            Err(attempt) => return *attempt,
        };
        match read_projects(&body, page_size) {
            Some(page) => QueryAttempt::branch("listed", page, Receipt::default(), None),
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
                "Zadig request failed",
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
                "Zadig temporarily unavailable",
                delay,
            )));
        }
        let body = read_bounded_body(&mut response, self.max_response_bytes).map_err(|error| {
            Box::new(match error {
                ProviderHttpError::BodyTooLarge(_) => rejected(
                    "invalidResponse",
                    FailureKind::ResponseTooLarge,
                    "Zadig response exceeded the configured limit",
                ),
                _ => retry(
                    FailureKind::Availability,
                    "Zadig response was interrupted",
                    0.0,
                ),
            })
        })?;
        if status != 200 {
            return Err(Box::new(rejected(
                "providerRejected",
                match status {
                    401 => FailureKind::Authentication,
                    403 => FailureKind::Authorization,
                    _ => FailureKind::ProviderRejection,
                },
                "Zadig rejected the request",
            )));
        }
        Ok(body)
    }
}
/// Bind the project operation to its branch targets and validate the Step configuration.
pub fn new_list_projects_step<Input>(
    config: QueryStepConfig<Input, ListProjectsOperation>,
) -> Result<QueryStep<Input, ListProjectsOperation>, ValidationError> {
    if config.connection.provider != r"zadig" {
        return Err(ValidationError::new("Zadig connection provider is invalid"));
    }
    QueryStep::new(config)
}

fn valid_key(key: &str) -> bool {
    !key.is_empty() && key.len() <= 512 && key.bytes().all(|c| c.is_ascii_graphic())
}
fn invalid() -> QueryAttempt<ListProjectsOutput> {
    rejected(
        "invalidResponse",
        FailureKind::Protocol,
        "Invalid Zadig response",
    )
}
fn read_projects(body: &[u8], page_size: i64) -> Option<ListProjectsOutput> {
    let envelope: WireFields = serde_json::from_slice(body).ok()?;
    let total = envelope.optional::<i64>("total").ok()??;
    let items = envelope.optional::<Vec<WireFields>>("projects").ok()??;
    if total < 0 || items.len() > page_size as usize {
        return None;
    }
    let mut projects = Vec::with_capacity(items.len());
    for item in items {
        let key = item.optional::<String>("project_key").ok()??;
        let name = item.optional::<String>("project_name").ok()??;
        let deploy_type = item
            .optional::<String>("deploy_type")
            .ok()?
            .unwrap_or_default();
        if key.is_empty()
            || key.trim() != key
            || key.len() > 256
            || name.is_empty()
            || name.len() > 512
            || deploy_type.len() > 64
        {
            return None;
        }
        projects.push(Project {
            key,
            name,
            deploy_type,
        });
    }
    Some(ListProjectsOutput { projects, total })
}
fn rejected(branch: &str, kind: FailureKind, message: &str) -> QueryAttempt<ListProjectsOutput> {
    QueryAttempt::branch(
        branch,
        ListProjectsOutput::default(),
        Receipt::default(),
        Some(Failure::new(kind, r"zadig", "listProjects", message)),
    )
}

fn retry(kind: FailureKind, message: &str, delay: f64) -> QueryAttempt<ListProjectsOutput> {
    QueryAttempt::retry(Failure::new(kind, r"zadig", "listProjects", message), delay)
}
