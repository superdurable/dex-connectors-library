// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Read the Yanwen warehouse catalog from a Dex Query Step.

#[rustfmt::skip]
mod generated_definitions;
mod provider_fields;
use provider_fields::BalanceFields as WireFields;

use std::sync::Arc;
use std::time::{Duration, SystemTime};

use dex_connectors_sdk as sdk;
use md5::{Digest, Md5};
use sdk::providerhttp::{
    AddressPolicy, ProviderHttpClient, ProviderHttpError, ProviderHttpRequest, parse_retry_after,
    read_bounded_body,
};
use sdk::{
    Call, CredentialProvider, Failure, FailureKind, Query, QueryAttempt, QueryDefinition,
    QueryStep, QueryStepConfig, Receipt, SecretString, ValidationError,
};
use serde::{Deserialize, Serialize};

const BASE_URL: &str = r"https://open.yw56.com.cn";

/// The warehouse query takes no application input.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct ListWarehousesInput {}

/// Bounded catalog in provider order.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct WarehouseList {
    /// Authorized warehouses returned by the query.
    pub warehouses: Vec<Warehouse>,
}
/// Warehouse record returned by the provider.
#[derive(Clone, Default, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Warehouse {
    /// Warehouse code.
    pub code: String,
    /// Warehouse display name.
    pub name: String,
    /// Warehouse area.
    pub area: String,
}

/// Settings for a single provider request.
pub struct Config {
    /// Base URL of the provider API. Defaults to the public provider endpoint.
    pub base_url: String,
    /// Timeout per attempt. Uses the manifest default.
    pub timeout: Duration,
    /// Maximum response size in bytes. Uses the manifest default.
    pub max_response_bytes: usize,
}

impl Default for Config {
    fn default() -> Self {
        Self {
            base_url: BASE_URL.to_owned(),
            timeout: Duration::from_secs(15),
            max_response_bytes: 1048576,
        }
    }
}

/// Credentials resolved from the logical connection for each request.
#[derive(Clone)]
pub struct Credentials {
    /// Provider user id.
    pub user_id: String,
    /// Provider api token.
    pub api_token: SecretString,
}

/// Bound warehouse query. Construct once and share the HTTP client across calls.
pub struct ListWarehousesOperation {
    clock: Arc<dyn Fn() -> SystemTime + Send + Sync>,

    credentials: Arc<dyn CredentialProvider<Credentials>>,
    http: ProviderHttpClient,
    endpoint: String,
    max_response_bytes: usize,
}

impl ListWarehousesOperation {
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
        Self::with_transport(config, credentials, http, Arc::new(SystemTime::now))
    }
    /// Bind provider configuration, call-time credentials, HTTP transport and signing clock.
    pub fn with_transport(
        config: Config,
        credentials: Arc<dyn CredentialProvider<Credentials>>,
        http: ProviderHttpClient,
        clock: Arc<dyn Fn() -> SystemTime + Send + Sync>,
    ) -> Result<Self, ValidationError> {
        let max_response_bytes = config.max_response_bytes;
        let base_url = config.base_url.as_str();
        let base = if base_url.is_empty() {
            BASE_URL
        } else {
            base_url
        };
        let origin = sdk::providerhttp::validate_base_url(base)?;
        let endpoint = origin + r"/api/order";
        if max_response_bytes == 0 {
            return Err(ValidationError::new(
                "Yanwen response size limit must be positive",
            ));
        }
        Ok(Self {
            clock,
            credentials,
            endpoint,
            http,
            max_response_bytes,
        })
    }
}

impl Query for ListWarehousesOperation {
    type Input = ListWarehousesInput;
    type Output = WarehouseList;

    fn definition(&self) -> QueryDefinition {
        generated_definitions::ListWarehouses::definition()
    }

    fn invoke(&self, call: &Call<'_>, _: &Self::Input) -> QueryAttempt<Self::Output> {
        let credentials = match self.credentials.resolve(call) {
            Ok(credentials)
                if valid_key(&credentials.user_id)
                    && credentials.user_id.len() <= 64
                    && !credentials.api_token.reveal().is_empty()
                    && credentials.api_token.reveal().len() <= 2048
                    && credentials.api_token.reveal().trim() == credentials.api_token.reveal() =>
            {
                credentials
            }
            _ => {
                return rejected(
                    "defect",
                    FailureKind::Authentication,
                    "Yanwen token is unavailable or invalid",
                );
            }
        };
        let timestamp = match (self.clock)().duration_since(std::time::UNIX_EPOCH) {
            Ok(elapsed) => elapsed.as_millis().to_string(),
            Err(error) => format!("-{}", error.duration().as_nanos().div_ceil(1_000_000)),
        };
        let token = credentials.api_token.reveal();
        let signature = hex::encode(
            Md5::new()
                .chain_update(token)
                .chain_update(&credentials.user_id)
                .chain_update("{}jsoncommon.warehouse.getlist")
                .chain_update(&timestamp)
                .chain_update("V1.0")
                .chain_update(token)
                .finalize(),
        );
        let query = form_urlencoded::Serializer::new(String::new())
            .extend_pairs([
                ("format", "json"),
                ("method", "common.warehouse.getlist"),
                ("sign", &signature),
                ("timestamp", &timestamp),
                ("user_id", &credentials.user_id),
                ("version", "V1.0"),
            ])
            .finish();
        let request = ProviderHttpRequest::new("POST", format!("{}?{}", self.endpoint, query))
            .body("{}")
            .header("Content-Type", "application/json; charset=UTF-8")
            .header("Accept", "application/json");
        let Ok(mut response) = self.http.send_traced(&request) else {
            return retry(
                FailureKind::Availability,
                "Yanwen warehouse request failed",
                0.0,
            );
        };
        let status = response.status();
        let delay = parse_retry_after(
            response.headers().get("Retry-After").unwrap_or(""),
            SystemTime::now(),
        );
        if matches!(status, 408 | 429 | 500..) {
            return match status {
                429 => retry(
                    FailureKind::RateLimit,
                    "Yanwen rate limited the warehouse request",
                    delay,
                ),
                _ => retry(
                    FailureKind::Availability,
                    "Yanwen API is temporarily unavailable",
                    delay,
                ),
            };
        }
        match read_bounded_body(&mut response, self.max_response_bytes) {
            Ok(body) => decode_warehouses(&body, status, delay),
            Err(ProviderHttpError::BodyTooLarge(_)) => rejected(
                "invalidResponse",
                FailureKind::ResponseTooLarge,
                "Yanwen response exceeded the configured limit",
            ),
            Err(_) => retry(
                FailureKind::Availability,
                "Yanwen response was interrupted",
                0.0,
            ),
        }
    }
}

/// Bind the warehouse operation to its branch targets and validate the Step configuration.
pub fn new_list_warehouses_step<Input>(
    config: QueryStepConfig<Input, ListWarehousesOperation>,
) -> Result<QueryStep<Input, ListWarehousesOperation>, ValidationError> {
    if config.connection.provider != r"yanwen" {
        return Err(ValidationError::new(
            "Yanwen connection provider is invalid",
        ));
    }
    QueryStep::new(config)
}

fn valid_key(key: &str) -> bool {
    !key.is_empty() && key.len() <= 2048 && key.bytes().all(|c| c.is_ascii_graphic())
}
fn decode_warehouses(body: &[u8], status: u16, delay: f64) -> QueryAttempt<WarehouseList> {
    if status != 200 {
        let kind = match status {
            401 => FailureKind::Authentication,
            403 => FailureKind::Authorization,
            400 | 404 | 412 => FailureKind::Validation,
            _ => FailureKind::ProviderRejection,
        };
        return rejected("providerRejected", kind, "Warehouse query rejected");
    }
    read_warehouses(body, delay).unwrap_or_else(|| {
        rejected(
            "invalidResponse",
            FailureKind::Protocol,
            "Invalid warehouse response",
        )
    })
}
fn read_warehouses(body: &[u8], delay: f64) -> Option<QueryAttempt<WarehouseList>> {
    let envelope: WireFields = serde_json::from_slice(body).ok()?;
    let code = envelope.optional::<String>("code").ok()??;
    let success = envelope.optional::<bool>("success").ok()??;
    if code.is_empty() {
        return None;
    }
    let rows = envelope.optional::<Vec<Option<WireFields>>>("data").ok()?;
    let mut warehouses = Vec::new();
    if let Some(rows) = rows.as_ref() {
        for row in rows {
            let empty = WireFields::default();
            let fields = row.as_ref().unwrap_or(&empty);
            warehouses.push(Warehouse {
                code: fields.optional::<String>("code").ok()?.unwrap_or_default(),
                name: fields.optional::<String>("name").ok()?.unwrap_or_default(),
                area: fields.optional::<String>("area").ok()?.unwrap_or_default(),
            });
        }
    }
    if !success || code != "0" {
        if code.starts_with('-') {
            return Some(retry(
                FailureKind::Availability,
                "Provider temporarily unavailable",
                delay,
            ));
        }
        return Some(rejected(
            "providerRejected",
            FailureKind::ProviderRejection,
            "Warehouse query rejected",
        ));
    }
    if rows.is_none()
        || warehouses.len() > 500
        || warehouses
            .iter()
            .any(|row| row.code.is_empty() || row.name.is_empty() || row.area.is_empty())
    {
        return None;
    }
    Some(QueryAttempt::branch(
        "listed",
        WarehouseList { warehouses },
        Receipt::default(),
        None,
    ))
}
fn rejected(branch: &str, kind: FailureKind, message: &str) -> QueryAttempt<WarehouseList> {
    QueryAttempt::branch(
        branch,
        WarehouseList::default(),
        Receipt::default(),
        Some(Failure::new(kind, r"yanwen", "listWarehouses", message)),
    )
}

fn retry(kind: FailureKind, message: &str, delay: f64) -> QueryAttempt<WarehouseList> {
    QueryAttempt::retry(
        Failure::new(kind, r"yanwen", "listWarehouses", message),
        delay,
    )
}
