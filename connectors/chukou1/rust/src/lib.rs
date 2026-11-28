// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Read the Chukou1 warehouse catalog from a Dex Query Step.

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

const BASE_URL: &str = r"https://openapi.chukou1.cn";

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
    /// Provider country code.
    pub country_code: String,
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
    /// Provider token used only for request authentication.
    pub access_token: SecretString,
}

impl From<SecretString> for Credentials {
    fn from(credential: SecretString) -> Self {
        Self {
            access_token: credential,
        }
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

    /// Supply the transport used for each request and its response size limit in bytes.
    pub fn with_http_client(
        credentials: Arc<dyn CredentialProvider<Credentials>>,
        http: ProviderHttpClient,
        max_response_bytes: usize,
    ) -> Result<Self, ValidationError> {
        Self::with_base_url(credentials, http, max_response_bytes, BASE_URL)
    }

    /// Bind a configured provider base URL, explicit transport and positive response limit in bytes.
    pub fn with_base_url(
        credentials: Arc<dyn CredentialProvider<Credentials>>,
        http: ProviderHttpClient,
        max_response_bytes: usize,
        base_url: &str,
    ) -> Result<Self, ValidationError> {
        let base = if base_url.is_empty() {
            BASE_URL
        } else {
            base_url
        };
        let origin = sdk::providerhttp::validate_base_url(base)?;
        let endpoint = origin + r"/v1/warehouses";
        if max_response_bytes == 0 {
            return Err(ValidationError::new(
                "Chukou1 response size limit must be positive",
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
            Ok(credentials) if valid_key(credentials.access_token.reveal()) => credentials,
            _ => {
                return rejected(
                    "defect",
                    FailureKind::Authentication,
                    "Chukou1 token is unavailable or invalid",
                );
            }
        };
        let request = ProviderHttpRequest::new("GET", &self.endpoint)
            .header(
                "Authorization",
                format!("Bearer {}", credentials.access_token.reveal()),
            )
            .header("Accept", "application/json")
            .header("Content-Type", "application/json; charset=utf-8");
        let Ok(mut response) = self.http.send_traced(&request) else {
            return retry(
                FailureKind::Availability,
                "Chukou1 warehouse request failed",
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
                    "Chukou1 rate limited the warehouse request",
                    delay,
                ),
                _ => retry(
                    FailureKind::Availability,
                    "Chukou1 API is temporarily unavailable",
                    delay,
                ),
            };
        }
        match read_bounded_body(&mut response, self.max_response_bytes) {
            Ok(body) => decode_warehouses(&body, status),
            Err(ProviderHttpError::BodyTooLarge(_)) => rejected(
                "invalidResponse",
                FailureKind::ResponseTooLarge,
                "Chukou1 response exceeded the configured limit",
            ),
            Err(_) => retry(
                FailureKind::Availability,
                "Chukou1 response was interrupted",
                0.0,
            ),
        }
    }
}

/// Bind the warehouse operation to its branch targets and validate the Step configuration.
pub fn new_list_warehouses_step<Input>(
    config: QueryStepConfig<Input, ListWarehousesOperation>,
) -> Result<QueryStep<Input, ListWarehousesOperation>, ValidationError> {
    if config.connection.provider != r"chukou1" {
        return Err(ValidationError::new(
            "Chukou1 connection provider is invalid",
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
    let rows: Option<Vec<Option<WireFields>>> = serde_json::from_slice(body).ok()?;

    let warehouses = rows.map(|rows| {
        rows.into_iter()
            .map(|row| {
                let row = row.unwrap_or_default();
                Some(Warehouse {
                    id: row
                        .optional::<String>("warehouseid")
                        .ok()?
                        .unwrap_or_default(),
                    name: row
                        .optional::<String>("warehousename")
                        .ok()?
                        .unwrap_or_default(),
                    country: row.optional::<String>("country").ok()?.unwrap_or_default(),
                    country_code: row
                        .optional::<String>("countrycode")
                        .ok()?
                        .unwrap_or_default(),
                })
            })
            .collect::<Option<Vec<_>>>()
    });
    let warehouses = warehouses??;
    if warehouses.len() > 200
        || warehouses
            .iter()
            .any(|row| row.id.is_empty() || row.name.is_empty() || row.country_code.is_empty())
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
        Some(Failure::new(kind, r"chukou1", "listWarehouses", message)),
    )
}

fn retry(kind: FailureKind, message: &str, delay: f64) -> QueryAttempt<WarehousePage> {
    QueryAttempt::retry(
        Failure::new(kind, r"chukou1", "listWarehouses", message),
        delay,
    )
}
