// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Read selected Gitee repository metadata in Dex Flows.
#[rustfmt::skip]
mod generated_definitions;
mod provider_fields;
use dex_connectors_sdk::{
    self as sdk, Call, CredentialProvider, Failure, FailureKind, Query, QueryAttempt,
    QueryDefinition, QueryStep, QueryStepConfig, Receipt, SecretString, ValidationError,
};
use sdk::providerhttp::{
    self, AddressPolicy, ProviderHttpClient, ProviderHttpError, ProviderHttpRequest,
};
use serde::{Deserialize, Serialize};
use std::{
    sync::Arc,
    time::{Duration, SystemTime},
};

/// Owner and repository path segments from a Gitee URL.
#[derive(Clone, Default, Deserialize, Serialize)]
#[serde(default)]
pub struct GetRepositoryInput {
    /// Organization or user path segment.
    pub owner: String,
    /// Repository path segment.
    pub repo: String,
}
/// Public repository metadata projected from the provider response.
#[derive(Clone, Default, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct RepositoryMetadata {
    /// Stable numeric repository ID.
    pub id: i64,
    /// Owner and repository path.
    pub full_name: String,
    /// Whether access is restricted.
    pub private: bool,
    /// Default branch, empty when omitted.
    pub default_branch: String,
    /// Repository name.
    pub name: String,
    /// Repository description, empty when omitted.
    pub description: String,
    /// Repository page URL.
    pub html_url: String,
    /// Current star count.
    pub stars_count: i64,
    /// Current fork count.
    pub forks_count: i64,
}
/// Result of a successful repository lookup.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct GetRepositoryOutput {
    /// Metadata for the selected repository.
    pub repository: RepositoryMetadata,
}
/// Personal access token resolved at the start of each lookup.
pub type Credentials = SecretString;
/// API root, timeout per read, and maximum successful response size.
pub struct Config {
    /// API root. HTTPS is required except for loopback testing.
    pub base_url: String,
    /// Timeout per request. Defaults to ten seconds.
    pub timeout: Duration,
    /// Response limit in bytes. Defaults to 256 KiB.
    pub max_response_bytes: usize,
}
impl Default for Config {
    fn default() -> Self {
        Self {
            base_url: "https://gitee.com".into(),
            timeout: Duration::from_secs(10),
            max_response_bytes: 262144,
        }
    }
}
/// A repository query bound to a credential provider and an HTTP transport.
pub struct GetRepositoryOperation {
    base_url: String,
    credentials: Arc<dyn CredentialProvider<Credentials>>,
    http: ProviderHttpClient,
    max_response_bytes: usize,
}
impl GetRepositoryOperation {
    /// Bind a Dex Web project connection, including its API settings.
    pub fn from_project(
        project: &sdk::projectconfig::LoadedProject,
        connection_name: &str,
    ) -> Result<Self, Box<dyn std::error::Error>> {
        let key = sdk::projectconfig::ConnectionKey::new("gitee", connection_name);
        let entry = project
            .connection_configuration(&key)
            .ok_or("Gitee connection is missing")?;
        if entry.provider != "gitee" {
            return Err("Gitee project connection provider differs".into());
        }
        let mut config = Config::default();
        for (name, value) in entry.configuration.to_json_object()? {
            match name.as_str() {
                "baseUrl" | "timeout" | "maxResponseBytes" if value.as_str() == Some("") => {}
                "baseUrl" => {
                    config.base_url = value
                        .as_str()
                        .ok_or("invalid Gitee API URL setting")?
                        .into()
                }
                "timeout" => {
                    config.timeout = Duration::from_nanos(
                        value
                            .as_i64()
                            .filter(|n| *n > 0)
                            .ok_or("invalid Gitee timeout setting")? as u64,
                    )
                }
                "maxResponseBytes" => {
                    config.max_response_bytes = value
                        .as_u64()
                        .and_then(|n| usize::try_from(n).ok())
                        .ok_or("invalid Gitee response size setting")?
                }
                _ => return Err("unknown Gitee setting".into()),
            }
        }
        let provider = sdk::projectconfig::ProjectCredentialProvider::new(
            project.connections().clone(),
            key,
            Arc::new(|fields| {
                if fields.len() != 1 {
                    return Err("Invalid repository credential fields".into());
                }
                fields
                    .get("access_token")
                    .and_then(serde_json::Value::as_str)
                    .filter(|value| !value.is_empty())
                    .map(SecretString::new)
                    .ok_or_else(|| "Missing repository access token".into())
            }),
            None,
        )?;
        Ok(Self::new(config, Arc::new(provider))?)
    }

    /// Validate the API root and create a redirect-safe client without contacting the provider.
    pub fn new(
        config: Config,
        credentials: Arc<dyn CredentialProvider<Credentials>>,
    ) -> Result<Self, ValidationError> {
        let base_url = providerhttp::validate_base_url(&config.base_url)?;
        let policy = if base_url.starts_with("http://") {
            AddressPolicy::PrivateNetworkHttp
        } else {
            AddressPolicy::PublicHttps
        };
        let http = ProviderHttpClient::new(config.timeout, policy)?;
        Self::with_http_client(&base_url, credentials, http, config.max_response_bytes)
    }
    /// Bind a caller-supplied transport and a positive response limit in bytes.
    pub fn with_http_client(
        base_url: &str,
        credentials: Arc<dyn CredentialProvider<Credentials>>,
        http: ProviderHttpClient,
        max_response_bytes: usize,
    ) -> Result<Self, ValidationError> {
        if max_response_bytes == 0 {
            return Err(ValidationError::new(
                "Gitee response limit must be positive",
            ));
        }
        Ok(Self {
            base_url: providerhttp::validate_base_url(base_url)?,
            credentials,
            http,
            max_response_bytes,
        })
    }
}
impl Query for GetRepositoryOperation {
    type Input = GetRepositoryInput;
    type Output = GetRepositoryOutput;
    fn definition(&self) -> QueryDefinition {
        generated_definitions::GetRepository::definition()
    }
    fn invoke(&self, call: &Call<'_>, input: &Self::Input) -> QueryAttempt<Self::Output> {
        if !valid_segment(&input.owner) || !valid_segment(&input.repo) {
            return rejected("defect", FailureKind::Validation, "Invalid repository path");
        }
        let token = match self.credentials.resolve(call) {
            Ok(token)
                if token.reveal().len() <= 512
                    && providerhttp::is_header_safe_credential(token.reveal()) =>
            {
                token
            }
            _ => {
                return rejected(
                    "defect",
                    FailureKind::Authentication,
                    "Connection token is unavailable or invalid",
                );
            }
        };
        let url = format!(
            "{}/api/v5/repos/{}/{}",
            self.base_url, input.owner, input.repo
        );
        let query = form_urlencoded::Serializer::new(String::new())
            .append_pair("access_token", token.reveal())
            .finish();
        let request = ProviderHttpRequest::new("GET", format!("{url}?{query}"))
            .header("Accept", "application/json");
        let mut response = match self.http.send_traced(&request) {
            Ok(response) => response,
            Err(_) => return retry(FailureKind::Availability, "Gitee request failed", 0.0),
        };
        let status = response.status();
        if matches!(status, 408 | 429 | 500..) {
            let delay = providerhttp::parse_retry_after(
                response.headers().get("Retry-After").unwrap_or(""),
                SystemTime::now(),
            );
            let kind = if status == 429 {
                FailureKind::RateLimit
            } else {
                FailureKind::Availability
            };
            return retry(kind, "Gitee is temporarily unavailable", delay);
        }
        if status == 404 {
            return rejected(
                "notFound",
                FailureKind::ProviderRejection,
                "Repository was not found or is inaccessible",
            );
        }
        if status != 200 {
            let kind = match status {
                401 => FailureKind::Authentication,
                403 => FailureKind::Authorization,
                _ => FailureKind::ProviderRejection,
            };
            return rejected(
                "providerRejected",
                kind,
                "Provider rejected the repository lookup",
            );
        }
        let body = match providerhttp::read_bounded_body(&mut response, self.max_response_bytes) {
            Ok(bytes) => bytes,
            Err(ProviderHttpError::BodyTooLarge(_)) => {
                return rejected(
                    "invalidResponse",
                    FailureKind::ResponseTooLarge,
                    "Repository response exceeded the configured limit",
                );
            }
            Err(_) => {
                return retry(
                    FailureKind::Availability,
                    "Repository response was interrupted",
                    0.0,
                );
            }
        };

        match decode_repository(&body) {
            Some(output) => QueryAttempt::branch("found", output, Receipt::default(), None),
            None => rejected(
                "invalidResponse",
                FailureKind::Protocol,
                "Invalid repository metadata",
            ),
        }
    }
}
/// Bind the repository lookup to its Dex Step and branch targets.
pub fn new_get_repository_step<Input>(
    config: QueryStepConfig<Input, GetRepositoryOperation>,
) -> Result<QueryStep<Input, GetRepositoryOperation>, ValidationError> {
    if config.connection.provider != "gitee" {
        return Err(ValidationError::new("Gitee connection provider differs"));
    }
    QueryStep::new(config)
}
fn valid_segment(segment: &str) -> bool {
    (1..=128).contains(&segment.len())
        && !matches!(segment, "." | "..")
        && segment
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || matches!(b, b'.' | b'-' | b'_'))
}
fn decode_repository(body: &[u8]) -> Option<GetRepositoryOutput> {
    let record: provider_fields::RepositoryFields = serde_json::from_slice(body).ok()?;
    let repository = RepositoryMetadata {
        id: record.optional("id").ok()??,
        full_name: record.optional("full_name").ok()??,
        private: record.optional("private").ok()?.unwrap_or_default(),
        default_branch: record.optional("default_branch").ok()?.unwrap_or_default(),
        name: record.optional("name").ok()??,
        description: record.optional("description").ok()?.unwrap_or_default(),
        html_url: record.optional("html_url").ok()?.unwrap_or_default(),
        stars_count: record
            .optional("stargazers_count")
            .ok()?
            .unwrap_or_default(),
        forks_count: record.optional("forks_count").ok()?.unwrap_or_default(),
    };
    if repository.id <= 0
        || repository.full_name.is_empty()
        || repository.name.is_empty()
        || repository.stars_count < 0
        || repository.forks_count < 0
    {
        return None;
    }
    Some(GetRepositoryOutput { repository })
}
fn rejected(branch: &str, kind: FailureKind, message: &str) -> QueryAttempt<GetRepositoryOutput> {
    QueryAttempt::branch(
        branch,
        GetRepositoryOutput::default(),
        Receipt::default(),
        Some(Failure::new(kind, "gitee", "getRepository", message)),
    )
}
fn retry(kind: FailureKind, message: &str, delay: f64) -> QueryAttempt<GetRepositoryOutput> {
    QueryAttempt::retry(Failure::new(kind, "gitee", "getRepository", message), delay)
}
