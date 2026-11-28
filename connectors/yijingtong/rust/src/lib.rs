// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Read the Yijingtong warehouse catalog from a Dex Query Step.

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
    /// Provider warehouse identifier.
    pub id: String,
    /// Warehouse display name.
    pub name: String,
    /// Country display name, or an empty string when absent.
    pub country: String,
}

/// Settings for a single provider request.
pub struct Config {
    /// Base URL of the provider API. Required tenant URL.
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
            timeout: Duration::from_secs(15),
            max_response_bytes: 1048576,
        }
    }
}

/// Credentials resolved from the logical connection for each request.
#[derive(Clone)]
pub struct Credentials {
    /// Provider token used only for request authentication.
    pub token: SecretString,
}

impl From<SecretString> for Credentials {
    fn from(credential: SecretString) -> Self {
        Self { token: credential }
    }
}

/// Bound warehouse query. Construct once and share the HTTP client across calls.
pub struct ListWarehousesOperation {
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
        Self::with_base_url(
            credentials,
            http,
            config.max_response_bytes,
            &config.base_url,
        )
    }

    /// Bind a configured provider base URL, explicit transport and positive response limit in bytes.
    pub fn with_base_url(
        credentials: Arc<dyn CredentialProvider<Credentials>>,
        http: ProviderHttpClient,
        max_response_bytes: usize,
        base_url: &str,
    ) -> Result<Self, ValidationError> {
        let origin = sdk::providerhttp::validate_base_url(base_url)?;
        if origin
            .split_once("://")
            .is_some_and(|(_, host)| host.contains('/'))
        {
            return Err(ValidationError::new(
                "Yijingtong tenant URL must be an origin",
            ));
        }
        let endpoint = origin + r"/hwc_api/hwc_storage_list.php";
        if max_response_bytes == 0 {
            return Err(ValidationError::new(
                "Yijingtong response size limit must be positive",
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

impl Query for ListWarehousesOperation {
    type Input = ListWarehousesInput;
    type Output = WarehousePage;

    fn definition(&self) -> QueryDefinition {
        generated_definitions::ListWarehouses::definition()
    }

    fn invoke(&self, call: &Call<'_>, _: &Self::Input) -> QueryAttempt<Self::Output> {
        let credentials = match self.credentials.resolve(call) {
            Ok(credentials) if valid_key(credentials.token.reveal()) => credentials,
            _ => {
                return rejected(
                    "defect",
                    FailureKind::Authentication,
                    "Yijingtong token is unavailable or invalid",
                );
            }
        };
        let Ok(body) = serde_json::to_vec(&serde_json::json!({"token":credentials.token.reveal()}))
        else {
            return rejected(
                "defect",
                FailureKind::Validation,
                "Warehouse request is invalid",
            );
        };
        let request = ProviderHttpRequest::new("POST", &self.endpoint)
            .body(body)
            .header("Accept", "application/json")
            .header("Content-Type", "application/json; charset=utf-8");
        let Ok(mut response) = self.http.send_traced(&request) else {
            return retry(
                FailureKind::Availability,
                "Yijingtong warehouse request failed",
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
                    "Yijingtong rate limited the warehouse request",
                    delay,
                ),
                _ => retry(
                    FailureKind::Availability,
                    "Yijingtong API is temporarily unavailable",
                    delay,
                ),
            };
        }
        match read_bounded_body(&mut response, self.max_response_bytes) {
            Ok(body) => decode_warehouses(&body, status),
            Err(ProviderHttpError::BodyTooLarge(_)) => rejected(
                "invalidResponse",
                FailureKind::ResponseTooLarge,
                "Yijingtong response exceeded the configured limit",
            ),
            Err(_) => retry(
                FailureKind::Availability,
                "Yijingtong response was interrupted",
                0.0,
            ),
        }
    }
}

/// Bind the warehouse operation to its branch targets and validate the Step configuration.
pub fn new_list_warehouses_step<Input>(
    config: QueryStepConfig<Input, ListWarehousesOperation>,
) -> Result<QueryStep<Input, ListWarehousesOperation>, ValidationError> {
    if config.connection.provider != r"yijingtong" {
        return Err(ValidationError::new(
            "Yijingtong connection provider is invalid",
        ));
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
            400 | 404 | 412 => FailureKind::Validation,
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
    let error = envelope.optional::<String>("error").ok()??;
    let rows = envelope.optional::<Vec<Option<WireFields>>>("data").ok()?;

    let warehouses = rows.map(|rows| {
        rows.into_iter()
            .map(|row| {
                let row = row.unwrap_or_default();
                Some(Warehouse {
                    id: row.optional::<String>("nums").ok()?.unwrap_or_default(),
                    name: row.optional::<String>("pname").ok()?.unwrap_or_default(),
                    country: row.optional::<String>("country").ok()?.unwrap_or_default(),
                })
            })
            .collect::<Option<Vec<_>>>()
    });
    let warehouses = match warehouses {
        Some(None) => return None,
        other => other.flatten(),
    };
    match error.as_str() {
        "F" => {
            return Some(rejected(
                "providerRejected",
                FailureKind::ProviderRejection,
                "Warehouse query rejected",
            ));
        }
        "T" => {}
        _ => return None,
    }
    let warehouses = warehouses?;
    if warehouses.len() > 200
        || warehouses
            .iter()
            .any(|row| row.id.trim().is_empty() || row.name.trim().is_empty())
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
        Some(Failure::new(kind, r"yijingtong", "listWarehouses", message)),
    )
}

fn retry(kind: FailureKind, message: &str, delay: f64) -> QueryAttempt<WarehousePage> {
    QueryAttempt::retry(
        Failure::new(kind, r"yijingtong", "listWarehouses", message),
        delay,
    )
}
