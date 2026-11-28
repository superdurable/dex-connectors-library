// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Read the Weiban tag catalog from a Dex Query Step.

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

const BASE_URL: &str = "https://open.weibanzhushou.com";
/// Empty input for listing visible tags.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct ListTagsInput {}
/// Tags and pagination metadata returned to Dex.
#[derive(Clone, Default, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct TagsResult {
    /// Tags in provider order.
    pub tags: Vec<TagMetadata>,
    /// Total available tags.
    pub total: i64,
}
/// Metadata for one tag visible to the connection.
#[derive(Clone, Deserialize, Serialize)]
pub struct TagMetadata {
    /// Tag identifier.
    pub id: String,
    /// Tag display name.
    pub name: String,
    /// Provider tag group.
    pub group: String,
}
/// Settings for a single provider request.
pub struct Config {
    /// Provider base URL. An empty value selects the manifest default.
    pub base_url: String,
    /// Timeout per attempt. Uses the manifest default.
    pub timeout: Duration,
    /// Maximum response size in bytes. Uses the manifest default.
    pub max_response_bytes: usize,
}

impl Default for Config {
    fn default() -> Self {
        Self {
            base_url: BASE_URL.into(),
            timeout: Duration::from_secs(15),
            max_response_bytes: 262144,
        }
    }
}

/// Credentials resolved from the logical connection for each request.
#[derive(Clone)]
pub struct Credentials {
    /// Provider-issued client ID.
    pub corp_id: String,
    /// Secret paired with the client ID.
    pub secret: SecretString,
}
/// Bound tag query. Construct once and share the HTTP client across calls.
pub struct ListTagsOperation {
    credentials: Arc<dyn CredentialProvider<Credentials>>,
    http: ProviderHttpClient,
    base_url: String,
    max_response_bytes: usize,
}

impl ListTagsOperation {
    /// Bind a Dex Web project connection, including its API settings and per-call credential resolution.
    pub fn from_project(
        project: &sdk::projectconfig::LoadedProject,
        connection_name: &str,
    ) -> Result<Self, Box<dyn std::error::Error>> {
        let key = sdk::projectconfig::ConnectionKey::new("weiban", connection_name);
        let entry = project
            .connection_configuration(&key)
            .ok_or("Weiban connection is missing")?;
        if entry.provider != "weiban" {
            return Err("Weiban project connection provider differs".into());
        }
        let mut config = Config::default();
        for (name, value) in entry.configuration.to_json_object()? {
            match name.as_str() {
                "baseUrl" => {
                    config.base_url = value.as_str().ok_or("Invalid provider base URL")?.into()
                }
                "timeout" | "maxResponseBytes" if value.as_str() == Some("") => {}
                "timeout" => {
                    config.timeout = Duration::from_nanos(
                        value
                            .as_i64()
                            .filter(|n| *n > 0)
                            .ok_or("invalid Weiban timeout setting")?
                            as u64,
                    )
                }
                "maxResponseBytes" => {
                    config.max_response_bytes = value
                        .as_u64()
                        .and_then(|n| usize::try_from(n).ok())
                        .ok_or("invalid Weiban response size setting")?
                }
                _ => return Err("unknown Weiban setting".into()),
            }
        }
        let provider = sdk::projectconfig::ProjectCredentialProvider::new(
            project.connections().clone(),
            key,
            Arc::new(|fields| {
                if fields.len() != 2 {
                    return Err("Invalid Weiban credential fields".into());
                }
                let field = |name: &str| {
                    fields
                        .get(name)
                        .and_then(serde_json::Value::as_str)
                        .filter(|v| !v.is_empty())
                        .map(SecretString::new)
                        .ok_or("Missing Weiban client credentials")
                };
                Ok(Credentials {
                    corp_id: field("corp_id")?.reveal().into(),
                    secret: field("secret")?,
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
        let policy = if config
            .base_url
            .get(..7)
            .is_some_and(|s| s.eq_ignore_ascii_case("http://"))
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
    /// Bind the provider base URL, transport and maximum response size in bytes.
    pub fn with_base_url(
        credentials: Arc<dyn CredentialProvider<Credentials>>,
        http: ProviderHttpClient,
        max_response_bytes: usize,
        base_url: &str,
    ) -> Result<Self, ValidationError> {
        let base_url = sdk::providerhttp::validate_base_url(if base_url.is_empty() {
            BASE_URL
        } else {
            base_url
        })?;
        if max_response_bytes == 0 {
            return Err(ValidationError::new(
                "Weiban response limit must be positive",
            ));
        }
        Ok(Self {
            base_url,
            credentials,
            http,
            max_response_bytes,
        })
    }
}

impl Query for ListTagsOperation {
    type Input = ListTagsInput;
    type Output = TagsResult;

    fn definition(&self) -> QueryDefinition {
        generated_definitions::ListTags::definition()
    }

    fn invoke(&self, call: &Call<'_>, _: &Self::Input) -> QueryAttempt<Self::Output> {
        let keys = match self.credentials.resolve(call) {
            Ok(keys) if valid_key(&keys.corp_id) && valid_key(keys.secret.reveal()) => keys,
            _ => {
                return rejected(
                    "defect",
                    FailureKind::Authentication,
                    "Weiban credentials are unavailable or invalid",
                );
            }
        };
        let body =
            serde_json::json!({"corp_id":keys.corp_id,"secret":keys.secret.reveal()}).to_string();
        let request = ProviderHttpRequest::new(
            "POST",
            format!("{}/open-api/access_token/get", self.base_url),
        )
        .header("Accept", "application/json")
        .header("Content-Type", "application/json")
        .body(body);
        let auth = match self.send(&request) {
            Ok(bytes) => bytes,
            Err(attempt) => return *attempt,
        };
        let token: AuthResponse = match serde_json::from_slice(&auth) {
            Ok(value) => value,
            Err(_) => return invalid(),
        };
        match token.code {
            None => return invalid(),
            Some(0) => {}
            Some(code) => return provider_failure(code, true),
        }
        let Some(token) = token
            .token
            .filter(|_| token.expires_in.is_some_and(|v| v > 0))
            .filter(|token| valid_key(token))
        else {
            return invalid();
        };
        let encoded = form_urlencoded::byte_serialize(token.as_bytes())
            .collect::<String>()
            .replace('*', "%2A")
            .replace("%7E", "~");
        let request = ProviderHttpRequest::new(
            "GET",
            format!(
                "{}/open-api/tag/list?access_token={encoded}&limit=10&offset=0",
                self.base_url
            ),
        )
        .header("Accept", "application/json");
        let body = match self.send(&request) {
            Ok(bytes) => bytes,
            Err(attempt) => return *attempt,
        };
        read_tags(&body).unwrap_or_else(invalid)
    }
}
impl ListTagsOperation {
    fn send(
        &self,
        request: &ProviderHttpRequest,
    ) -> Result<Vec<u8>, Box<QueryAttempt<TagsResult>>> {
        let mut response = self.http.send_traced(request).map_err(|_| {
            Box::new(retry(
                FailureKind::Availability,
                "Weiban request failed",
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
                "Weiban temporarily unavailable",
                delay,
            )));
        }
        let body = read_bounded_body(&mut response, self.max_response_bytes).map_err(|error| {
            Box::new(match error {
                ProviderHttpError::BodyTooLarge(_) => rejected(
                    "invalidResponse",
                    FailureKind::ResponseTooLarge,
                    "Weiban response exceeded the configured limit",
                ),
                _ => retry(
                    FailureKind::Availability,
                    "Weiban response was interrupted",
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
                    400 | 404 | 412 => FailureKind::Validation,
                    _ => FailureKind::ProviderRejection,
                },
                "Weiban rejected the request",
            )));
        }
        Ok(body)
    }
}
/// Bind the tag operation to its branch targets and validate the Step configuration.
pub fn new_list_tags_step<Input>(
    config: QueryStepConfig<Input, ListTagsOperation>,
) -> Result<QueryStep<Input, ListTagsOperation>, ValidationError> {
    if config.connection.provider != r"weiban" {
        return Err(ValidationError::new(
            "Weiban connection provider is invalid",
        ));
    }
    QueryStep::new(config)
}

fn valid_key(key: &str) -> bool {
    !key.is_empty() && key.len() <= 2048 && key.trim() == key
}
fn invalid() -> QueryAttempt<TagsResult> {
    rejected(
        "invalidResponse",
        FailureKind::Protocol,
        "Invalid Weiban response",
    )
}
fn provider_failure(code: i64, authorization: bool) -> QueryAttempt<TagsResult> {
    if matches!(code, 500 | 10051..=10053) {
        return retry(
            FailureKind::Availability,
            "Weiban temporarily unavailable",
            0.0,
        );
    }
    let kind = if authorization {
        FailureKind::Authentication
    } else {
        FailureKind::ProviderRejection
    };
    rejected("providerRejected", kind, "Weiban rejected the request")
}
#[derive(Deserialize)]
#[serde(try_from = "WireFields")]
struct AuthResponse {
    code: Option<i64>,
    token: Option<String>,
    expires_in: Option<i64>,
}
impl TryFrom<WireFields> for AuthResponse {
    type Error = serde_json::Error;
    fn try_from(fields: WireFields) -> Result<Self, Self::Error> {
        Ok(Self {
            code: fields.optional("errcode")?,
            token: fields.optional("access_token")?,
            expires_in: fields.optional("expires_in")?,
        })
    }
}
#[derive(Deserialize)]
#[serde(try_from = "WireFields")]
struct TagResponse {
    code: Option<i64>,
    total: Option<i64>,
    tags: Option<Vec<Option<WireTag>>>,
}
impl TryFrom<WireFields> for TagResponse {
    type Error = serde_json::Error;
    fn try_from(fields: WireFields) -> Result<Self, Self::Error> {
        Ok(Self {
            code: fields.optional("errcode")?,
            total: fields.optional("total")?,
            tags: fields.optional("tag_list")?,
        })
    }
}
#[derive(Deserialize)]
#[serde(try_from = "WireFields")]
struct WireTag {
    id: Option<String>,
    name: Option<String>,
    group: Option<String>,
}
impl TryFrom<WireFields> for WireTag {
    type Error = serde_json::Error;
    fn try_from(fields: WireFields) -> Result<Self, Self::Error> {
        Ok(Self {
            id: fields.optional("tag_id")?,
            name: fields.optional("tag_name")?,
            group: fields.optional("tag_group")?,
        })
    }
}
fn read_tags(body: &[u8]) -> Option<QueryAttempt<TagsResult>> {
    let response: TagResponse = serde_json::from_slice(body).ok()?;
    let code = response.code?;
    if code != 0 {
        return Some(provider_failure(code, false));
    }
    let total = response.total?;
    let items = response.tags?;
    if total < 0 || items.len() > 100 || items.len() as i64 > total {
        return None;
    }
    let mut tags = Vec::new();
    for item in items.into_iter().take(10) {
        let tag = item?;
        let id = tag.id?;
        let name = tag.name.unwrap_or_default();
        let group = tag.group.unwrap_or_default();
        if id.is_empty() || id.len() > 128 || name.len() > 512 || group.len() > 512 {
            return None;
        }
        tags.push(TagMetadata { id, name, group });
    }
    Some(QueryAttempt::branch(
        "listed",
        TagsResult { tags, total },
        Receipt::default(),
        None,
    ))
}
fn rejected(branch: &str, kind: FailureKind, message: &str) -> QueryAttempt<TagsResult> {
    QueryAttempt::branch(
        branch,
        TagsResult::default(),
        Receipt::default(),
        Some(Failure::new(kind, r"weiban", "listTags", message)),
    )
}

fn retry(kind: FailureKind, message: &str, delay: f64) -> QueryAttempt<TagsResult> {
    QueryAttempt::retry(Failure::new(kind, r"weiban", "listTags", message), delay)
}
