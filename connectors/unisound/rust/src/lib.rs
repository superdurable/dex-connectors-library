// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Read the Unisound voice catalog from a Dex Query Step.

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

/// The listSystemVoices query takes an empty object.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct ListSystemVoicesInput {}
/// Authorized voices in provider order.
#[derive(Clone, Default, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ListSystemVoicesOutput {
    /// Visible voices returned by the provider.
    pub voices: Vec<SystemVoice>,
}
/// Metadata for one space.
#[derive(Clone, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct SystemVoice {
    /// System voice identifier.
    #[serde(rename = "voiceID")]
    pub voice_id: String,
    /// Voice display name.
    pub name: String,
    /// Language codes in provider order.
    pub languages: Vec<String>,
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
            timeout: Duration::from_secs(15),
            max_response_bytes: 1048576,
        }
    }
}

/// Credentials resolved from the logical connection for each request.
pub type Credentials = SecretString;
/// Bound voice query. Construct once and share the HTTP client across calls.
pub struct ListSystemVoicesOperation {
    credentials: Arc<dyn CredentialProvider<Credentials>>,
    http: ProviderHttpClient,
    max_response_bytes: usize,
}

impl ListSystemVoicesOperation {
    /// Bind a Dex Web project connection, including its API settings and per-call credential resolution.
    pub fn from_project(
        project: &sdk::projectconfig::LoadedProject,
        connection_name: &str,
    ) -> Result<Self, Box<dyn std::error::Error>> {
        let key = sdk::projectconfig::ConnectionKey::new("unisound", connection_name);
        let entry = project
            .connection_configuration(&key)
            .ok_or("Unisound connection is missing")?;
        if entry.provider != "unisound" {
            return Err("Unisound project connection provider differs".into());
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
                            .ok_or("invalid Unisound timeout setting")?
                            as u64,
                    )
                }
                "maxResponseBytes" => {
                    config.max_response_bytes = value
                        .as_u64()
                        .and_then(|n| usize::try_from(n).ok())
                        .ok_or("invalid Unisound response size setting")?
                }
                _ => return Err("unknown Unisound setting".into()),
            }
        }
        let provider = sdk::projectconfig::ProjectCredentialProvider::new(
            project.connections().clone(),
            key,
            Arc::new(|fields| {
                if fields.len() != 1 {
                    return Err("Invalid Unisound credential fields".into());
                }
                fields
                    .get("apiKey")
                    .and_then(serde_json::Value::as_str)
                    .filter(|value| !value.is_empty())
                    .map(Credentials::new)
                    .ok_or_else(|| "Missing Unisound API token".into())
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
                "Unisound response size limit must be positive",
            ));
        }
        Ok(Self {
            credentials,
            http,
            max_response_bytes,
        })
    }
}

impl Query for ListSystemVoicesOperation {
    type Input = ListSystemVoicesInput;
    type Output = ListSystemVoicesOutput;

    fn definition(&self) -> QueryDefinition {
        generated_definitions::ListSystemVoices::definition()
    }

    fn invoke(&self, call: &Call<'_>, _: &Self::Input) -> QueryAttempt<Self::Output> {
        let token = match self.credentials.resolve(call) {
            Ok(token) if valid_key(token.reveal()) => token,
            _ => {
                return rejected(
                    "defect",
                    FailureKind::Authentication,
                    "Invalid Unisound API token",
                );
            }
        };
        let request = ProviderHttpRequest::new(
            "POST",
            "https://maas-api.unisound.com/v1/audio/voices/query",
        )
        .header("Accept", "application/json")
        .header("Content-Type", "application/json")
        .body(br#"{"voice_type":"system"}"#.to_vec())
        .header("Authorization", format!("Bearer {}", token.reveal()));
        self.read_response(&request)
    }
}
impl ListSystemVoicesOperation {
    fn read_response(&self, request: &ProviderHttpRequest) -> QueryAttempt<ListSystemVoicesOutput> {
        let mut response = match self.http.send_traced(request) {
            Ok(response) => response,
            Err(_) => return retry(FailureKind::Availability, "Unisound request failed", 0.0),
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
                "Unisound temporarily unavailable",
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
                "Unisound rejected the request",
            );
        }
        let body = match read_bounded_body(&mut response, self.max_response_bytes) {
            Ok(body) => body,
            Err(ProviderHttpError::BodyTooLarge(_)) => {
                return rejected(
                    "invalidResponse",
                    FailureKind::ResponseTooLarge,
                    "Unisound response exceeded the configured limit",
                );
            }
            Err(_) => {
                return retry(
                    FailureKind::Availability,
                    "Unisound response interrupted",
                    0.0,
                );
            }
        };
        decode_voices(&body).unwrap_or_else(invalid)
    }
}
/// Bind the voice operation to its branch targets and validate the Step configuration.
pub fn new_list_system_voices_step<Input>(
    config: QueryStepConfig<Input, ListSystemVoicesOperation>,
) -> Result<QueryStep<Input, ListSystemVoicesOperation>, ValidationError> {
    if config.connection.provider != r"unisound" {
        return Err(ValidationError::new(
            "Unisound connection provider is invalid",
        ));
    }
    QueryStep::new(config)
}

fn valid_key(key: &str) -> bool {
    !key.is_empty() && key.len() <= 1024 && key.bytes().all(|c| c.is_ascii_graphic())
}
fn invalid() -> QueryAttempt<ListSystemVoicesOutput> {
    rejected(
        "invalidResponse",
        FailureKind::Protocol,
        "Invalid Unisound response",
    )
}
#[derive(Deserialize)]
#[serde(try_from = "WireFields")]
struct WireItem {
    voice_id: Option<String>,
    name: Option<String>,
    languages: Option<Vec<Option<String>>>,
}
impl TryFrom<WireFields> for WireItem {
    type Error = serde_json::Error;
    fn try_from(fields: WireFields) -> Result<Self, Self::Error> {
        Ok(Self {
            voice_id: fields.optional("voice_id")?,
            name: fields.optional("voice_name")?,
            languages: fields.optional("language")?,
        })
    }
}
#[derive(Deserialize)]
#[serde(try_from = "WireFields")]
struct DirectoryResponse {
    base: Option<DirectoryData>,
    items: Option<Vec<Option<WireItem>>>,
}
impl TryFrom<WireFields> for DirectoryResponse {
    type Error = serde_json::Error;
    fn try_from(fields: WireFields) -> Result<Self, Self::Error> {
        Ok(Self {
            base: fields.optional("base_resp")?,
            items: fields.optional("system_voice")?,
        })
    }
}
#[derive(Deserialize)]
#[serde(try_from = "WireFields")]
struct DirectoryData {
    code: Option<i64>,
}
impl TryFrom<WireFields> for DirectoryData {
    type Error = serde_json::Error;
    fn try_from(fields: WireFields) -> Result<Self, Self::Error> {
        Ok(Self {
            code: fields.optional("status_code")?,
        })
    }
}
fn decode_voices(body: &[u8]) -> Option<QueryAttempt<ListSystemVoicesOutput>> {
    let page: DirectoryResponse = serde_json::from_slice(body).ok()?;
    match page.base?.code? {
        0 => {}
        100501 => {
            return Some(retry(
                FailureKind::RateLimit,
                "Unisound rate limited the query",
                0.0,
            ));
        }
        100999 => {
            return Some(retry(
                FailureKind::Availability,
                "Unisound is temporarily unavailable",
                0.0,
            ));
        }
        _ => {
            return Some(rejected(
                "providerRejected",
                FailureKind::ProviderRejection,
                "Unisound rejected the query",
            ));
        }
    }
    let items = page.items?;
    if items.len() > 1000 {
        return None;
    }
    let mut voices = Vec::new();
    for item in items.into_iter().take(25) {
        let item = item?;
        let voice_id = item.voice_id?;
        if voice_id.is_empty() || voice_id.trim() != voice_id || voice_id.len() > 128 {
            return None;
        }
        let name = item.name.unwrap_or_default();
        if name.len() > 200 {
            return None;
        }
        let languages = item.languages?;
        if languages.len() > 20 {
            return None;
        }
        let languages = languages
            .into_iter()
            .map(Option::unwrap_or_default)
            .collect();
        voices.push(SystemVoice {
            voice_id,
            name,
            languages,
        });
    }
    Some(QueryAttempt::branch(
        "found",
        ListSystemVoicesOutput { voices },
        Receipt::default(),
        None,
    ))
}
fn rejected(
    branch: &str,
    kind: FailureKind,
    message: &str,
) -> QueryAttempt<ListSystemVoicesOutput> {
    QueryAttempt::branch(
        branch,
        ListSystemVoicesOutput::default(),
        Receipt::default(),
        Some(Failure::new(kind, r"unisound", "listSystemVoices", message)),
    )
}

fn retry(kind: FailureKind, message: &str, delay: f64) -> QueryAttempt<ListSystemVoicesOutput> {
    QueryAttempt::retry(
        Failure::new(kind, r"unisound", "listSystemVoices", message),
        delay,
    )
}
