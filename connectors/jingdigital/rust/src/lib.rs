// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Read the JINGdigital tag catalog from a Dex Query Step.

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

const BASE_URL: &str = "https://jingopenapi.jingsocial.com";
/// Select one tag page.
#[derive(Clone, Default, Deserialize, Serialize)]
#[serde(default, rename_all = "camelCase")]
pub struct ListTagsInput {
    /// One-based page, with zero selecting the first page.
    pub page: i64,
    /// Number of tags per page, with zero selecting 20.
    pub page_size: i64,
}
/// Tags and pagination metadata returned to Dex.
#[derive(Clone, Default, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ListTagsOutput {
    /// Tags in provider order.
    pub tags: Vec<TagSummary>,
    /// Total available tags.
    pub total: i64,
    /// Next page, or zero after the last page.
    pub next_page: i64,
}
/// Metadata for one tag visible to the connection.
#[derive(Clone, Deserialize, Serialize)]
pub struct TagSummary {
    /// Tag identifier.
    pub id: i64,
    /// Tag display name.
    pub name: String,
    /// Provider tag level.
    pub level: i64,
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
            timeout: Duration::from_secs(10),
            max_response_bytes: 262144,
        }
    }
}

/// Credentials resolved from the logical connection for each request.
#[derive(Clone)]
pub struct Credentials {
    /// Provider-issued client ID.
    pub app_id: SecretString,
    /// Secret paired with the client ID.
    pub app_secret: SecretString,
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
        let key = sdk::projectconfig::ConnectionKey::new("jingdigital", connection_name);
        let entry = project
            .connection_configuration(&key)
            .ok_or("JINGdigital connection is missing")?;
        if entry.provider != "jingdigital" {
            return Err("JINGdigital project connection provider differs".into());
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
                            .ok_or("invalid JINGdigital timeout setting")?
                            as u64,
                    )
                }
                "maxResponseBytes" => {
                    config.max_response_bytes = value
                        .as_u64()
                        .and_then(|n| usize::try_from(n).ok())
                        .ok_or("invalid JINGdigital response size setting")?
                }
                _ => return Err("unknown JINGdigital setting".into()),
            }
        }
        let provider = sdk::projectconfig::ProjectCredentialProvider::new(
            project.connections().clone(),
            key,
            Arc::new(|fields| {
                if fields.len() != 2 {
                    return Err("Invalid JINGdigital credential fields".into());
                }
                let field = |name: &str| {
                    fields
                        .get(name)
                        .and_then(serde_json::Value::as_str)
                        .filter(|v| !v.is_empty())
                        .map(SecretString::new)
                        .ok_or("Missing JINGdigital client credentials")
                };
                Ok(Credentials {
                    app_id: field("app_id")?,
                    app_secret: field("app_secret")?,
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
        if base_url != BASE_URL {
            return Err(ValidationError::new(
                "JINGdigital requires its production origin",
            ));
        }
        if max_response_bytes == 0 {
            return Err(ValidationError::new(
                "JINGdigital response limit must be positive",
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
    type Output = ListTagsOutput;

    fn definition(&self) -> QueryDefinition {
        generated_definitions::ListTags::definition()
    }

    fn invoke(&self, call: &Call<'_>, input: &Self::Input) -> QueryAttempt<Self::Output> {
        let keys = match self.credentials.resolve(call) {
            Ok(keys) if valid_key(keys.app_id.reveal()) && valid_key(keys.app_secret.reveal()) => {
                keys
            }
            _ => {
                return rejected(
                    "defect",
                    FailureKind::Authentication,
                    "JINGdigital credentials are unavailable or invalid",
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
                "Invalid tag page or page size",
            );
        }
        let body=serde_json::json!({"app_id":keys.app_id.reveal(),"app_secret":keys.app_secret.reveal()}).to_string();
        let request =
            ProviderHttpRequest::new("POST", format!("{}/v1/oauth2/token", self.base_url))
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
            Some(code) => return provider_failure(code),
        }
        let Some(token) = token
            .data
            .and_then(|data| data.token)
            .filter(|token| valid_key(token))
        else {
            return invalid();
        };
        let filter =
            serde_json::json!({"tag_ids":[],"level":1,"page":page,"perPage":page_size}).to_string();
        let request =
            ProviderHttpRequest::new("POST", format!("{}/v1/tag/select/list", self.base_url))
                .header("Accept", "application/json")
                .header("Content-Type", "application/json")
                .header("Access_token", token)
                .header("App_id", keys.app_id.reveal())
                .body(filter);
        let body = match self.send(&request) {
            Ok(bytes) => bytes,
            Err(attempt) => return *attempt,
        };
        read_tags(&body, page, page_size).unwrap_or_else(invalid)
    }
}
impl ListTagsOperation {
    fn send(
        &self,
        request: &ProviderHttpRequest,
    ) -> Result<Vec<u8>, Box<QueryAttempt<ListTagsOutput>>> {
        let mut response = self.http.send_traced(request).map_err(|_| {
            Box::new(retry(
                FailureKind::Availability,
                "JINGdigital request failed",
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
                "JINGdigital temporarily unavailable",
                delay,
            )));
        }
        let body = read_bounded_body(&mut response, self.max_response_bytes).map_err(|error| {
            Box::new(match error {
                ProviderHttpError::BodyTooLarge(_) => rejected(
                    "invalidResponse",
                    FailureKind::ResponseTooLarge,
                    "JINGdigital response exceeded the configured limit",
                ),
                _ => retry(
                    FailureKind::Availability,
                    "JINGdigital response was interrupted",
                    0.0,
                ),
            })
        })?;
        if status != 200 {
            return Err(Box::new(rejected(
                "providerRejected",
                match status {
                    401 => FailureKind::Authentication,
                    403 => FailureKind::Authentication,
                    _ => FailureKind::ProviderRejection,
                },
                "JINGdigital rejected the request",
            )));
        }
        Ok(body)
    }
}
/// Bind the tag operation to its branch targets and validate the Step configuration.
pub fn new_list_tags_step<Input>(
    config: QueryStepConfig<Input, ListTagsOperation>,
) -> Result<QueryStep<Input, ListTagsOperation>, ValidationError> {
    if config.connection.provider != r"jingdigital" {
        return Err(ValidationError::new(
            "JINGdigital connection provider is invalid",
        ));
    }
    QueryStep::new(config)
}

fn valid_key(key: &str) -> bool {
    !key.is_empty() && key.len() <= 2048 && key.bytes().all(|b| b.is_ascii_graphic())
}
fn invalid() -> QueryAttempt<ListTagsOutput> {
    rejected(
        "invalidResponse",
        FailureKind::Protocol,
        "Invalid JINGdigital response",
    )
}
fn provider_failure(code: i64) -> QueryAttempt<ListTagsOutput> {
    if matches!(code, 20000 | 40005) {
        return retry(
            FailureKind::Availability,
            "JINGdigital temporarily unavailable",
            0.0,
        );
    }
    if code == 20002 {
        return retry(
            FailureKind::RateLimit,
            "JINGdigital rate limited the request",
            0.0,
        );
    }
    let kind = if matches!(code, 20001 | 40006) {
        FailureKind::Authentication
    } else {
        FailureKind::ProviderRejection
    };
    rejected("providerRejected", kind, "JINGdigital rejected the request")
}
#[derive(Deserialize)]
#[serde(try_from = "WireFields")]
struct AuthResponse {
    code: Option<i64>,
    data: Option<TokenData>,
}
impl TryFrom<WireFields> for AuthResponse {
    type Error = serde_json::Error;
    fn try_from(fields: WireFields) -> Result<Self, Self::Error> {
        Ok(Self {
            code: fields.optional("code")?,
            data: fields.optional("data")?,
        })
    }
}
#[derive(Deserialize)]
#[serde(try_from = "WireFields")]
struct TokenData {
    token: Option<String>,
}
impl TryFrom<WireFields> for TokenData {
    type Error = serde_json::Error;
    fn try_from(fields: WireFields) -> Result<Self, Self::Error> {
        Ok(Self {
            token: fields.optional("access_token")?,
        })
    }
}
#[derive(Deserialize)]
#[serde(try_from = "WireFields")]
struct TagResponse {
    code: Option<i64>,
    data: Option<TagData>,
}
impl TryFrom<WireFields> for TagResponse {
    type Error = serde_json::Error;
    fn try_from(fields: WireFields) -> Result<Self, Self::Error> {
        Ok(Self {
            code: fields.optional("code")?,
            data: fields.optional("data")?,
        })
    }
}
#[derive(Deserialize)]
#[serde(try_from = "WireFields")]
struct TagData {
    items: Option<Vec<Option<WireTag>>>,
    meta: Option<Pagination>,
}
impl TryFrom<WireFields> for TagData {
    type Error = serde_json::Error;
    fn try_from(fields: WireFields) -> Result<Self, Self::Error> {
        Ok(Self {
            items: fields.optional("items")?,
            meta: fields.optional("_meta")?,
        })
    }
}
#[derive(Deserialize)]
#[serde(try_from = "WireFields")]
struct Pagination {
    total: Option<i64>,
    page: Option<i64>,
    per_page: Option<i64>,
}
impl TryFrom<WireFields> for Pagination {
    type Error = serde_json::Error;
    fn try_from(fields: WireFields) -> Result<Self, Self::Error> {
        Ok(Self {
            total: fields.optional("totalcount")?,
            page: fields.optional("currentpage")?,
            per_page: fields.optional("perpage")?,
        })
    }
}
#[derive(Deserialize)]
#[serde(try_from = "WireFields")]
struct WireTag {
    id: Option<i64>,
    name: Option<String>,
    level: Option<i64>,
}
impl TryFrom<WireFields> for WireTag {
    type Error = serde_json::Error;
    fn try_from(fields: WireFields) -> Result<Self, Self::Error> {
        Ok(Self {
            id: fields.optional("id")?,
            name: fields.optional("name")?,
            level: fields.optional("level")?,
        })
    }
}
fn read_tags(body: &[u8], page: i64, page_size: i64) -> Option<QueryAttempt<ListTagsOutput>> {
    let response: TagResponse = serde_json::from_slice(body).ok()?;
    let code = response.code?;
    if code != 0 {
        return Some(provider_failure(code));
    }
    let data = response.data?;
    let meta = data.meta?;
    let total = meta.total.unwrap_or_default();
    let items = data.items?;
    let per_page = meta.per_page?;
    if total < 0 || meta.page? != page || per_page < 1 || items.len() > page_size as usize {
        return None;
    }
    let mut tags = Vec::with_capacity(items.len());
    for item in items {
        let tag = item?;
        let id = tag.id?;
        let name = tag.name?;
        let level = tag.level.unwrap_or_default();
        if id <= 0 || name.is_empty() || name.len() > 512 || !(0..=10).contains(&level) {
            return None;
        }
        tags.push(TagSummary { id, name, level });
    }
    let next_page = if page.wrapping_mul(per_page) < total {
        if tags.is_empty() || page == 1000000 {
            return None;
        }
        page + 1
    } else {
        0
    };
    Some(QueryAttempt::branch(
        "listed",
        ListTagsOutput {
            tags,
            total,
            next_page,
        },
        Receipt::default(),
        None,
    ))
}
fn rejected(branch: &str, kind: FailureKind, message: &str) -> QueryAttempt<ListTagsOutput> {
    QueryAttempt::branch(
        branch,
        ListTagsOutput::default(),
        Receipt::default(),
        Some(Failure::new(kind, r"jingdigital", "listTags", message)),
    )
}

fn retry(kind: FailureKind, message: &str, delay: f64) -> QueryAttempt<ListTagsOutput> {
    QueryAttempt::retry(
        Failure::new(kind, r"jingdigital", "listTags", message),
        delay,
    )
}
