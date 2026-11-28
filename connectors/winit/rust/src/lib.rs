// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Read the Winit warehouse catalog from a Dex Query Step.

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

const BASE_URL: &str = r"https://openapi.winit.com.cn";

/// The warehouse query takes no application input.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct ListWarehousesInput {}

/// Bounded catalog in provider order.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct WarehousePage {
    /// Authorized warehouses returned by the query.
    pub warehouses: Vec<Warehouse>,
}
/// Warehouse record returned by the provider.
#[derive(Clone, Default, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Warehouse {
    /// Warehouse identifier.
    pub id: i64,
    /// Warehouse code.
    pub code: String,
    /// Warehouse display name.
    pub name: String,
}

/// Settings for a single provider request.
pub struct Config {
    /// Required Winit platform identifier.
    pub platform: String,
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
            platform: String::new(),
            timeout: Duration::from_secs(15),
            max_response_bytes: 1048576,
        }
    }
}

/// Credentials resolved from the logical connection for each request.
#[derive(Clone)]
pub struct Credentials {
    /// Provider app key.
    pub app_key: SecretString,
    /// Provider token.
    pub token: SecretString,
    /// Provider client id.
    pub client_id: SecretString,
    /// Provider client secret.
    pub client_secret: SecretString,
}

/// Bound warehouse query. Construct once and share the HTTP client across calls.
pub struct ListWarehousesOperation {
    clock: Arc<dyn Fn() -> SystemTime + Send + Sync>,
    platform: String,
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
        if config.platform.is_empty()
            || config.platform.len() > 128
            || !config
                .platform
                .bytes()
                .all(|c| c.is_ascii_alphanumeric() || c == b'_' || c == b'-')
        {
            return Err(ValidationError::new("Invalid Winit platform"));
        }
        let base = if base_url.is_empty() {
            BASE_URL
        } else {
            base_url
        };
        let origin = sdk::providerhttp::validate_base_url(base)?;
        let endpoint = origin + r"/openapi/service";
        if max_response_bytes == 0 {
            return Err(ValidationError::new(
                "Winit response size limit must be positive",
            ));
        }
        Ok(Self {
            clock,
            platform: config.platform,
            credentials,
            endpoint,
            http,
            max_response_bytes,
        })
    }
}

impl Query for ListWarehousesOperation {
    type Input = ListWarehousesInput;
    type Output = WarehousePage;

    fn definition(&self) -> QueryDefinition {
        generated_definitions::ListWarehouses::definition()
    }

    fn invoke(&self, call: &Call<'_>, _: &Self::Input) -> QueryAttempt<Self::Output> {
        let credentials = match self.credentials.resolve(call) {
            Ok(credentials)
                if valid_key(credentials.app_key.reveal())
                    && valid_key(credentials.token.reveal())
                    && valid_key(credentials.client_id.reveal())
                    && valid_key(credentials.client_secret.reveal()) =>
            {
                credentials
            }
            _ => {
                return rejected(
                    "defect",
                    FailureKind::Authentication,
                    "Winit token is unavailable or invalid",
                );
            }
        };
        let instant: chrono::DateTime<chrono::Utc> = (self.clock)().into();
        let offset = chrono::FixedOffset::east_opt(8 * 3600).expect("UTC+8");
        let timestamp = instant
            .with_timezone(&offset)
            .format("%Y-%m-%d %H:%M:%S")
            .to_string();
        let mut fields = std::collections::BTreeMap::from([
            ("action", "queryWarehouse"),
            ("app_key", credentials.app_key.reveal()),
            ("format", "json"),
            ("platform", self.platform.as_str()),
            ("sign_method", "md5"),
            ("timestamp", &timestamp),
            ("version", "1.0"),
        ]);
        let mut message = String::new();
        for (name, value) in &fields {
            message.push_str(name);
            message.push_str(value);
        }
        let signature = |secret: &str| {
            hex::encode_upper(
                Md5::new()
                    .chain_update(secret)
                    .chain_update(&message)
                    .chain_update(secret)
                    .finalize(),
            )
        };
        let client_sign = signature(credentials.client_secret.reveal());
        let sign = signature(credentials.token.reveal());
        fields.insert("client_sign", &client_sign);
        fields.insert("sign", &sign);
        fields.insert("client_id", credentials.client_id.reveal());
        let Ok(body) = serde_json::to_vec(&fields) else {
            return rejected(
                "defect",
                FailureKind::Validation,
                "Warehouse request is invalid",
            );
        };
        let request = ProviderHttpRequest::new("POST", &self.endpoint)
            .body(body)
            .header("Content-Type", "application/json")
            .header("Accept", "application/json");
        let Ok(mut response) = self.http.send_traced(&request) else {
            return retry(
                FailureKind::Availability,
                "Winit warehouse request failed",
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
                    "Winit rate limited the warehouse request",
                    delay,
                ),
                _ => retry(
                    FailureKind::Availability,
                    "Winit API is temporarily unavailable",
                    delay,
                ),
            };
        }
        match read_bounded_body(&mut response, self.max_response_bytes) {
            Ok(body) => decode_warehouses(&body, status),
            Err(ProviderHttpError::BodyTooLarge(_)) => rejected(
                "invalidResponse",
                FailureKind::ResponseTooLarge,
                "Winit response exceeded the configured limit",
            ),
            Err(_) => retry(
                FailureKind::Availability,
                "Winit response was interrupted",
                0.0,
            ),
        }
    }
}

/// Bind the warehouse operation to its branch targets and validate the Step configuration.
pub fn new_list_warehouses_step<Input>(
    config: QueryStepConfig<Input, ListWarehousesOperation>,
) -> Result<QueryStep<Input, ListWarehousesOperation>, ValidationError> {
    if config.connection.provider != r"winit" {
        return Err(ValidationError::new("Winit connection provider is invalid"));
    }
    QueryStep::new(config)
}

fn valid_key(key: &str) -> bool {
    !key.is_empty() && key.len() <= 2048 && key.bytes().all(|c| c.is_ascii_graphic())
}
fn decode_warehouses(body: &[u8], status: u16) -> QueryAttempt<WarehousePage> {
    if status != 200 {
        let kind = match status {
            401 => FailureKind::Authentication,
            403 => FailureKind::Authorization,
            400 | 404 => FailureKind::Validation,
            _ => FailureKind::ProviderRejection,
        };
        return rejected("providerRejected", kind, "Warehouse query rejected");
    }
    read_warehouses(body).unwrap_or_else(|| {
        rejected(
            "invalidResponse",
            FailureKind::Protocol,
            "Invalid warehouse response",
        )
    })
}
fn read_warehouses(body: &[u8]) -> Option<QueryAttempt<WarehousePage>> {
    let envelope: WireFields = serde_json::from_slice(body).ok()?;
    let code = envelope.optional::<i64>("code").ok()??;
    let rows = envelope.optional::<Vec<Option<WireFields>>>("data").ok()?;
    let mut warehouses = Vec::new();
    if let Some(rows) = rows.as_ref() {
        for row in rows {
            let empty = WireFields::default();
            let fields = row.as_ref().unwrap_or(&empty);
            warehouses.push(Warehouse {
                id: fields
                    .optional::<i64>("warehouseid")
                    .ok()?
                    .unwrap_or_default(),
                code: fields
                    .optional::<String>("warehousecode")
                    .ok()?
                    .unwrap_or_default(),
                name: fields
                    .optional::<String>("warehousename")
                    .ok()?
                    .unwrap_or_default(),
            });
        }
    }
    if code != 0 {
        return Some(rejected(
            "providerRejected",
            FailureKind::ProviderRejection,
            "Warehouse query rejected",
        ));
    }
    if rows.is_none()
        || warehouses.len() > 200
        || warehouses
            .iter()
            .any(|row| row.id <= 0 || row.code.is_empty() || row.name.is_empty())
    {
        return None;
    }
    Some(QueryAttempt::branch(
        "listed",
        WarehousePage { warehouses },
        Receipt::default(),
        None,
    ))
}
fn rejected(branch: &str, kind: FailureKind, message: &str) -> QueryAttempt<WarehousePage> {
    QueryAttempt::branch(
        branch,
        WarehousePage::default(),
        Receipt::default(),
        Some(Failure::new(kind, r"winit", "listWarehouses", message)),
    )
}

fn retry(kind: FailureKind, message: &str, delay: f64) -> QueryAttempt<WarehousePage> {
    QueryAttempt::retry(
        Failure::new(kind, r"winit", "listWarehouses", message),
        delay,
    )
}
