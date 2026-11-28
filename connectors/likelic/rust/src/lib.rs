// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Read the Likelic room catalog from a Dex Query Step.

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

/// Select one page of rooms visible to the customer.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct ListRoomsInput {
    /// One-based page, from 1 through 1000000.
    #[serde(default)]
    pub page: i64,
}
/// Room page returned to Dex.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct ListRoomsOutput {
    /// Rooms in provider order.
    pub rooms: Vec<Room>,
    /// One-based page number.
    pub page: i64,
    /// Total reported by the provider.
    pub total: i64,
}
/// Metadata for one visible room.
#[derive(Clone, Deserialize, Serialize)]
pub struct Room {
    /// Provider messaging channel.
    pub channel: String,
    /// Provider instance identifier.
    #[serde(rename = "instanceID")]
    pub instance_id: i64,
    /// Provider room identifier.
    #[serde(rename = "roomID")]
    pub room_id: String,
    /// Provider push status.
    #[serde(rename = "pushState")]
    pub push_state: String,
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
#[derive(Clone)]
pub struct Credentials {
    /// Nonsecret Likelic customer code.
    pub customer_code: String,
    /// API key paired with the customer code.
    pub api_key: SecretString,
}
/// Bound room query. Construct once and share the HTTP client across calls.
pub struct ListRoomsOperation {
    credentials: Arc<dyn CredentialProvider<Credentials>>,
    http: ProviderHttpClient,
    max_response_bytes: usize,
}

impl ListRoomsOperation {
    /// Bind a Dex Web project connection, including its API settings and per-call credential resolution.
    pub fn from_project(
        project: &sdk::projectconfig::LoadedProject,
        connection_name: &str,
    ) -> Result<Self, Box<dyn std::error::Error>> {
        let key = sdk::projectconfig::ConnectionKey::new("likelic", connection_name);
        let entry = project
            .connection_configuration(&key)
            .ok_or("Likelic connection is missing")?;
        if entry.provider != "likelic" {
            return Err("Likelic project connection provider differs".into());
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
                            .ok_or("invalid Likelic timeout setting")?
                            as u64,
                    )
                }
                "maxResponseBytes" => {
                    config.max_response_bytes = value
                        .as_u64()
                        .and_then(|n| usize::try_from(n).ok())
                        .ok_or("invalid Likelic response size setting")?
                }
                _ => return Err("unknown Likelic setting".into()),
            }
        }
        let provider = sdk::projectconfig::ProjectCredentialProvider::new(
            project.connections().clone(),
            key,
            Arc::new(|fields| {
                if fields.len() != 2 {
                    return Err("Invalid Likelic credential fields".into());
                }
                let customer_code = fields
                    .get("customerCode")
                    .and_then(serde_json::Value::as_str)
                    .ok_or("Missing Likelic customer code")?;
                let api_key = fields
                    .get("apiKey")
                    .and_then(serde_json::Value::as_str)
                    .ok_or("Missing Likelic API key")?;
                Ok(Credentials {
                    customer_code: customer_code.into(),
                    api_key: SecretString::new(api_key),
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
    /// Bind the transport and maximum response size in bytes.
    pub fn with_http_client(
        credentials: Arc<dyn CredentialProvider<Credentials>>,
        http: ProviderHttpClient,
        max_response_bytes: usize,
    ) -> Result<Self, ValidationError> {
        if max_response_bytes == 0 || max_response_bytes > 16 << 20 {
            return Err(ValidationError::new(
                "Likelic response size limit must be positive",
            ));
        }
        Ok(Self {
            credentials,
            http,
            max_response_bytes,
        })
    }
}

impl Query for ListRoomsOperation {
    type Input = ListRoomsInput;
    type Output = ListRoomsOutput;

    fn definition(&self) -> QueryDefinition {
        generated_definitions::ListRooms::definition()
    }

    fn invoke(&self, call: &Call<'_>, input: &Self::Input) -> QueryAttempt<Self::Output> {
        if !(1..=1000000).contains(&input.page) {
            return rejected(
                "defect",
                FailureKind::Validation,
                "Room page must be from 1 through 1000000",
            );
        }
        let token = match self.credentials.resolve(call) {
            Ok(token) if valid_credentials(&token) => token,
            _ => {
                return rejected(
                    "defect",
                    FailureKind::Authentication,
                    "Invalid Likelic customer credentials",
                );
            }
        };
        let request = ProviderHttpRequest::new(
            "GET",
            format!(
                "https://agent.likelic.com/api/v1/rooms?page={}&limit=20",
                input.page
            ),
        )
        .header("Accept", "application/json")
        .header("X-Customer-Code", &token.customer_code)
        .header("X-Api-Key", token.api_key.reveal());
        self.read_response(&request, input.page)
    }
}
impl ListRoomsOperation {
    fn read_response(
        &self,
        request: &ProviderHttpRequest,
        page: i64,
    ) -> QueryAttempt<ListRoomsOutput> {
        let mut response = match self.http.send_traced(request) {
            Ok(response) => response,
            Err(_) => return retry(FailureKind::Availability, "Likelic request failed", 0.0),
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
                "Likelic temporarily unavailable",
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
                "Likelic rejected the request",
            );
        }
        let body = match read_bounded_body(&mut response, self.max_response_bytes) {
            Ok(body) => body,
            Err(ProviderHttpError::BodyTooLarge(_)) => {
                return rejected(
                    "invalidResponse",
                    FailureKind::ResponseTooLarge,
                    "Likelic response exceeded the configured limit",
                );
            }
            Err(_) => {
                return retry(
                    FailureKind::Availability,
                    "Likelic response interrupted",
                    0.0,
                );
            }
        };
        decode_rooms(&body, page).unwrap_or_else(invalid)
    }
}
/// Bind the room operation to its branch targets and validate the Step configuration.
pub fn new_list_rooms_step<Input>(
    config: QueryStepConfig<Input, ListRoomsOperation>,
) -> Result<QueryStep<Input, ListRoomsOperation>, ValidationError> {
    if config.connection.provider != r"likelic" {
        return Err(ValidationError::new(
            "Likelic connection provider is invalid",
        ));
    }
    QueryStep::new(config)
}

fn valid_credentials(keys: &Credentials) -> bool {
    !keys.customer_code.is_empty()
        && keys.customer_code.len() <= 50
        && keys
            .customer_code
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || matches!(b, b'_' | b'-'))
        && keys
            .api_key
            .reveal()
            .strip_prefix("sk-")
            .is_some_and(|key| key.len() == 48 && key.bytes().all(|b| b.is_ascii_hexdigit()))
}
fn invalid() -> QueryAttempt<ListRoomsOutput> {
    rejected(
        "invalidResponse",
        FailureKind::Protocol,
        "Invalid Likelic response",
    )
}
#[derive(Deserialize)]
#[serde(try_from = "WireFields")]
struct RoomResponse {
    success: Option<bool>,
    data: Option<Vec<Option<RoomItem>>>,
    meta: Option<PageMetadata>,
}
impl TryFrom<WireFields> for RoomResponse {
    type Error = serde_json::Error;
    fn try_from(fields: WireFields) -> Result<Self, Self::Error> {
        Ok(Self {
            success: fields.optional("success")?,
            data: fields.optional("data")?,
            meta: fields.optional("meta")?,
        })
    }
}
#[derive(Deserialize)]
#[serde(try_from = "WireFields")]
struct RoomItem {
    channel: Option<String>,
    instance_id: Option<i64>,
    room_id: Option<String>,
    push_state: Option<String>,
}
impl TryFrom<WireFields> for RoomItem {
    type Error = serde_json::Error;
    fn try_from(fields: WireFields) -> Result<Self, Self::Error> {
        Ok(Self {
            channel: fields.optional("channel")?,
            instance_id: fields.optional("instance_id")?,
            room_id: fields.optional("room_id")?,
            push_state: fields.optional("push_state")?,
        })
    }
}
#[derive(Deserialize)]
#[serde(try_from = "WireFields")]
struct PageMetadata {
    page: Option<i64>,
    limit: Option<i64>,
    total: Option<i64>,
}
impl TryFrom<WireFields> for PageMetadata {
    type Error = serde_json::Error;
    fn try_from(fields: WireFields) -> Result<Self, Self::Error> {
        Ok(Self {
            page: fields.optional("page")?,
            limit: fields.optional("limit")?,
            total: fields.optional("total")?,
        })
    }
}
fn decode_rooms(body: &[u8], page: i64) -> Option<QueryAttempt<ListRoomsOutput>> {
    let response: RoomResponse = serde_json::from_slice(body).ok()?;
    let meta = response.meta?;
    let total = meta.total.unwrap_or_default();
    let data = response.data?;
    if !response.success? || data.len() > 20 || meta.page? != page || meta.limit? != 20 || total < 0
    {
        return None;
    }
    let mut rooms = Vec::with_capacity(data.len());
    for item in data {
        let item = item?;
        let channel = item.channel?;
        let instance_id = item.instance_id?;
        let room_id = item.room_id?;
        let push_state = item.push_state?;
        if !matches!(channel.as_str(), "wework" | "qq")
            || instance_id <= 0
            || room_id.is_empty()
            || room_id.len() > 256
            || room_id.contains(['\r', '\n', '\0'])
            || !matches!(push_state.as_str(), "ready" | "blocked" | "unknown")
        {
            return None;
        }
        rooms.push(Room {
            channel,
            instance_id,
            room_id,
            push_state,
        });
    }
    Some(QueryAttempt::branch(
        "found",
        ListRoomsOutput { rooms, total, page },
        Receipt::default(),
        None,
    ))
}
fn rejected(branch: &str, kind: FailureKind, message: &str) -> QueryAttempt<ListRoomsOutput> {
    QueryAttempt::branch(
        branch,
        ListRoomsOutput::default(),
        Receipt::default(),
        Some(Failure::new(kind, r"likelic", "listRooms", message)),
    )
}

fn retry(kind: FailureKind, message: &str, delay: f64) -> QueryAttempt<ListRoomsOutput> {
    QueryAttempt::retry(Failure::new(kind, r"likelic", "listRooms", message), delay)
}
