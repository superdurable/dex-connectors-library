// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Read the TokenFactory account balance from a Dex Query Step.

#[rustfmt::skip]
mod generated_definitions;
mod balance_fields;
use balance_fields::BalanceFields;

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

const BASE_URL: &str = r"https://api.tokenfactory.cn";

/// The balance query takes no application input.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct GetBalanceInput {}

/// Account snapshot returned by the provider.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct Balance {
    /// Available balance in Chinese yuan.
    #[serde(rename = "balanceYuan")]
    pub balance_yuan: f64,
    /// Whether the account is active.
    #[serde(rename = "isActive")]
    pub is_active: bool,
    /// Currency code returned by the provider.
    #[serde(rename = "unit")]
    pub unit: String,
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
            timeout: Duration::from_nanos(15000000000),
            max_response_bytes: 65536,
        }
    }
}

/// API key resolved from the logical connection for each request.
#[derive(Clone)]
pub struct Credentials {
    /// Provider-issued bearer credential, never stored in the result.
    pub api_key: SecretString,
}

impl From<SecretString> for Credentials {
    fn from(credential: SecretString) -> Self {
        Self {
            api_key: credential,
        }
    }
}

/// Bound balance query. Construct once and share the HTTP client across calls.
pub struct GetBalanceOperation {
    credentials: Arc<dyn CredentialProvider<Credentials>>,
    http: ProviderHttpClient,
    endpoint: String,
    max_response_bytes: usize,
}

impl GetBalanceOperation {
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
        let endpoint = sdk::providerhttp::validate_base_url(base)? + r"/v1/user/balance";
        if max_response_bytes == 0 {
            return Err(ValidationError::new(
                "TokenFactory response size limit must be positive",
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

impl Query for GetBalanceOperation {
    type Input = GetBalanceInput;
    type Output = Balance;

    fn definition(&self) -> QueryDefinition {
        generated_definitions::GetBalance::definition()
    }

    fn invoke(&self, call: &Call<'_>, _: &Self::Input) -> QueryAttempt<Self::Output> {
        let credentials = match self.credentials.resolve(call) {
            Ok(credentials) if valid_key(credentials.api_key.reveal()) => credentials,
            _ => {
                return rejected(
                    "defect",
                    FailureKind::Authentication,
                    "TokenFactory API Key is unavailable or invalid",
                );
            }
        };
        let request = ProviderHttpRequest::new("GET", &self.endpoint)
            .header(
                "Authorization",
                format!("Bearer {}", credentials.api_key.reveal()),
            )
            .header("Accept", "application/json");
        let Ok(mut response) = self.http.send_traced(&request) else {
            return retry(
                FailureKind::Availability,
                "TokenFactory balance request failed",
                0.0,
            );
        };
        let status = response.status();
        if matches!(status, 408 | 429 | 500..) {
            let delay = parse_retry_after(
                response.headers().get("Retry-After").unwrap_or(""),
                SystemTime::now(),
            );
            return match status {
                429 => retry(
                    FailureKind::RateLimit,
                    "TokenFactory rate limited the balance request",
                    delay,
                ),
                _ => retry(
                    FailureKind::Availability,
                    "TokenFactory API is temporarily unavailable",
                    delay,
                ),
            };
        }
        match read_bounded_body(&mut response, self.max_response_bytes) {
            Ok(_) if status != 200 => rejected(
                "providerRejected",
                match status {
                    401 => FailureKind::Authentication,
                    403 => FailureKind::Authorization,
                    400 | 404 | 405 => FailureKind::Validation,
                    _ => FailureKind::ProviderRejection,
                },
                &format!("TokenFactory rejected balance request with HTTP {status}"),
            ),
            Ok(body) => decode_balance(&body),
            Err(ProviderHttpError::BodyTooLarge(_)) => rejected(
                "invalidResponse",
                FailureKind::ResponseTooLarge,
                "TokenFactory response exceeded the configured limit",
            ),
            Err(_) => retry(
                FailureKind::Availability,
                "TokenFactory response was interrupted",
                0.0,
            ),
        }
    }
}

/// Bind the balance operation to its branch targets and validate the Step configuration.
pub fn new_get_balance_step<Input>(
    config: QueryStepConfig<Input, GetBalanceOperation>,
) -> Result<QueryStep<Input, GetBalanceOperation>, ValidationError> {
    if config.connection.provider != r"tokenfactory" {
        return Err(ValidationError::new(
            "TokenFactory connection provider is invalid",
        ));
    }
    QueryStep::new(config)
}

fn valid_key(key: &str) -> bool {
    !key.is_empty() && key.len() <= 2048 && key.bytes().all(|byte| byte.is_ascii_graphic())
}

fn decode_balance(body: &[u8]) -> QueryAttempt<Balance> {
    serde_json::from_slice::<BalanceFields>(body)
        .ok()
        .and_then(decode_fields)
        .unwrap_or_else(|| {
            rejected(
                "invalidResponse",
                FailureKind::Protocol,
                "Invalid balance response",
            )
        })
}
fn decode_fields(fields: BalanceFields) -> Option<QueryAttempt<Balance>> {
    let balance = fields.optional::<f64>("balance").ok()??;
    let active = fields.optional("is_active").ok()??;
    let unit = fields.optional::<String>("unit").ok()??;
    if !balance.is_finite() || unit != "CNY" {
        return None;
    }
    Some(QueryAttempt::branch(
        "retrieved",
        Balance {
            balance_yuan: balance,
            is_active: active,
            unit,
        },
        Receipt::default(),
        None,
    ))
}
fn rejected(branch: &str, kind: FailureKind, message: &str) -> QueryAttempt<Balance> {
    QueryAttempt::branch(
        branch,
        Balance::default(),
        Receipt::default(),
        Some(Failure::new(kind, r"tokenfactory", "getBalance", message)),
    )
}

fn retry(kind: FailureKind, message: &str, delay: f64) -> QueryAttempt<Balance> {
    QueryAttempt::retry(
        Failure::new(kind, r"tokenfactory", "getBalance", message),
        delay,
    )
}
