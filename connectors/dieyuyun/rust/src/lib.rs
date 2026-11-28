// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Read the Dieyuyun account balance from a Dex Query Step.

#[rustfmt::skip]
mod generated_definitions;
mod balance_fields;
use balance_fields::BalanceFields;
mod decimal;

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

const BASE_URL: &str = r"https://open.dieyuyun.com";

/// The balance query takes no application input.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct GetAccountBalanceInput {}

/// Account snapshot returned by the provider.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct GetAccountBalanceOutput {
    /// Available yuan as the exact provider decimal.
    #[serde(rename = "availableCNY")]
    pub available_cny: String,
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
            timeout: Duration::from_nanos(10000000000),
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
pub struct GetAccountBalanceOperation {
    credentials: Arc<dyn CredentialProvider<Credentials>>,
    http: ProviderHttpClient,
    endpoint: String,
    max_response_bytes: usize,
}

impl GetAccountBalanceOperation {
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
        if origin
            .split_once("://")
            .is_some_and(|(_, host)| host.contains('/'))
        {
            return Err(ValidationError::new(
                "Balance API base URL must be a host root",
            ));
        }
        let endpoint = origin + r"/v1/balance";
        if max_response_bytes == 0 {
            return Err(ValidationError::new(
                "Dieyuyun response size limit must be positive",
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

impl Query for GetAccountBalanceOperation {
    type Input = GetAccountBalanceInput;
    type Output = GetAccountBalanceOutput;

    fn definition(&self) -> QueryDefinition {
        generated_definitions::GetAccountBalance::definition()
    }

    fn invoke(&self, call: &Call<'_>, _: &Self::Input) -> QueryAttempt<Self::Output> {
        let credentials = match self.credentials.resolve(call) {
            Ok(credentials) if valid_key(credentials.api_key.reveal()) => credentials,
            _ => {
                return rejected(
                    "defect",
                    FailureKind::Authentication,
                    "Dieyuyun API Key is unavailable or invalid",
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
                "Dieyuyun balance request failed",
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
                    "Dieyuyun rate limited the balance request",
                    delay,
                ),
                _ => retry(
                    FailureKind::Availability,
                    "Dieyuyun API is temporarily unavailable",
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
                    _ => FailureKind::ProviderRejection,
                },
                &format!("Dieyuyun rejected balance request with HTTP {status}"),
            ),
            Ok(body) => decode_balance(&body),
            Err(ProviderHttpError::BodyTooLarge(_)) => rejected(
                "invalidResponse",
                FailureKind::ResponseTooLarge,
                "Dieyuyun response exceeded the configured limit",
            ),
            Err(_) => retry(
                FailureKind::Availability,
                "Dieyuyun response was interrupted",
                0.0,
            ),
        }
    }
}

/// Bind the balance operation to its branch targets and validate the Step configuration.
pub fn new_get_account_balance_step<Input>(
    config: QueryStepConfig<Input, GetAccountBalanceOperation>,
) -> Result<QueryStep<Input, GetAccountBalanceOperation>, ValidationError> {
    if config.connection.provider != r"dieyuyun" {
        return Err(ValidationError::new(
            "Dieyuyun connection provider is invalid",
        ));
    }
    QueryStep::new(config)
}

fn valid_key(key: &str) -> bool {
    !key.is_empty() && key.len() <= 512 && key.bytes().all(|byte| byte.is_ascii_graphic())
}

fn decode_balance(body: &[u8]) -> QueryAttempt<GetAccountBalanceOutput> {
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
fn decode_fields(fields: BalanceFields) -> Option<QueryAttempt<GetAccountBalanceOutput>> {
    let object = fields.optional::<String>("object").ok()??;
    let currency = fields.optional::<String>("currency").ok()??;
    let raw = fields
        .optional::<Box<serde_json::value::RawValue>>("balance")
        .ok()??;
    let amount = decimal::amount(&raw)?;
    if object != "balance" || currency != "CNY" || amount.len() > 64 {
        return None;
    }
    Some(QueryAttempt::branch(
        "fetched",
        GetAccountBalanceOutput {
            available_cny: amount,
        },
        Receipt::default(),
        None,
    ))
}
fn rejected(
    branch: &str,
    kind: FailureKind,
    message: &str,
) -> QueryAttempt<GetAccountBalanceOutput> {
    QueryAttempt::branch(
        branch,
        GetAccountBalanceOutput::default(),
        Receipt::default(),
        Some(Failure::new(
            kind,
            r"dieyuyun",
            "getAccountBalance",
            message,
        )),
    )
}

fn retry(kind: FailureKind, message: &str, delay: f64) -> QueryAttempt<GetAccountBalanceOutput> {
    QueryAttempt::retry(
        Failure::new(kind, r"dieyuyun", "getAccountBalance", message),
        delay,
    )
}
