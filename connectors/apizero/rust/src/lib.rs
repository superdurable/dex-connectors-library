// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Read the ApiZero account information from a Dex Query Step.

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

const ENDPOINT: &str = "https://v1.apizero.cn/api/user-info";

/// The account query takes no application input.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct GetAccountInfoInput {}

/// Account snapshot returned by the provider.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct GetAccountInfoOutput {
    /// Scope of the connected key.
    pub scope: String,
    /// Wallet balance in yuan for account scope.
    #[serde(rename = "balanceYuan", skip_serializing_if = "Option::is_none")]
    pub balance_yuan: Option<String>,
    /// Remaining prepaid calls for key scope.
    #[serde(rename = "keyQuotaRemaining", skip_serializing_if = "Option::is_none")]
    pub key_quota_remaining: Option<i64>,
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
            timeout: Duration::from_nanos(10000000000),
            max_response_bytes: 1048576,
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

/// Bound account query. Construct once and share the HTTP client across calls.
pub struct GetAccountInfoOperation {
    credentials: Arc<dyn CredentialProvider<Credentials>>,
    http: ProviderHttpClient,
    max_response_bytes: usize,
}

impl GetAccountInfoOperation {
    /// Construct a bounded, redirect-safe transport without contacting the provider.
    pub fn new(
        config: Config,
        credentials: Arc<dyn CredentialProvider<Credentials>>,
    ) -> Result<Self, ValidationError> {
        let http = ProviderHttpClient::new(config.timeout, AddressPolicy::PublicHttps)?;
        Self::with_http_client(credentials, http, config.max_response_bytes)
    }

    /// Supply the transport used for each request and its response size limit in bytes.
    pub fn with_http_client(
        credentials: Arc<dyn CredentialProvider<Credentials>>,
        http: ProviderHttpClient,
        max_response_bytes: usize,
    ) -> Result<Self, ValidationError> {
        if max_response_bytes == 0 {
            return Err(ValidationError::new(
                "ApiZero response size limit must be positive",
            ));
        }
        Ok(Self {
            credentials,
            http,
            max_response_bytes,
        })
    }
}

impl Query for GetAccountInfoOperation {
    type Input = GetAccountInfoInput;
    type Output = GetAccountInfoOutput;

    fn definition(&self) -> QueryDefinition {
        generated_definitions::GetAccountInfo::definition()
    }

    fn invoke(&self, call: &Call<'_>, _: &Self::Input) -> QueryAttempt<Self::Output> {
        let credentials = match self.credentials.resolve(call) {
            Ok(credentials) if valid_key(credentials.api_key.reveal()) => credentials,
            _ => {
                return rejected(
                    "defect",
                    FailureKind::Authentication,
                    "ApiZero API Key is unavailable or invalid",
                );
            }
        };
        let request = ProviderHttpRequest::new("GET", ENDPOINT)
            .header(
                "Authorization",
                format!("Bearer {}", credentials.api_key.reveal()),
            )
            .header("Accept", "application/json");
        let Ok(mut response) = self.http.send_traced(&request) else {
            return retry(
                FailureKind::Availability,
                "ApiZero account request failed",
                0.0,
            );
        };
        let status = response.status();
        let delay = parse_retry_after(
            response.headers().get("Retry-After").unwrap_or(""),
            SystemTime::now(),
        );
        if status == 429 {
            return retry(
                FailureKind::RateLimit,
                "Provider rate limited the account query",
                delay,
            );
        }
        match read_bounded_body(&mut response, self.max_response_bytes) {
            Ok(body) => decode_account(&body, status, delay),
            Err(ProviderHttpError::BodyTooLarge(_)) => rejected(
                "invalidResponse",
                FailureKind::ResponseTooLarge,
                "ApiZero response exceeded the configured limit",
            ),
            Err(_) => retry(
                FailureKind::Availability,
                "ApiZero response was interrupted",
                0.0,
            ),
        }
    }
}

/// Bind the balance operation to its branch targets and validate the Step configuration.
pub fn new_get_account_info_step<Input>(
    config: QueryStepConfig<Input, GetAccountInfoOperation>,
) -> Result<QueryStep<Input, GetAccountInfoOperation>, ValidationError> {
    if config.connection.provider != r"apizero" {
        return Err(ValidationError::new(
            "ApiZero connection provider is invalid",
        ));
    }
    QueryStep::new(config)
}

fn valid_key(key: &str) -> bool {
    key.starts_with("sk_live_")
        && key.len() <= 1024
        && key.bytes().all(|byte| byte.is_ascii_graphic())
}

fn decode_account(body: &[u8], status: u16, delay: f64) -> QueryAttempt<GetAccountInfoOutput> {
    serde_json::from_slice::<BalanceFields>(body)
        .ok()
        .and_then(|fields| decode_fields(fields, status, delay))
        .unwrap_or_else(|| {
            if status == 408 || status >= 500 {
                return retry(
                    FailureKind::Availability,
                    "Provider is temporarily unavailable",
                    delay,
                );
            }
            if status != 200 {
                let kind = match status {
                    401 => FailureKind::Authentication,
                    403 => FailureKind::Authorization,
                    _ => FailureKind::ProviderRejection,
                };
                return rejected(
                    "providerRejected",
                    kind,
                    "Provider rejected the account query",
                );
            }
            invalid_response()
        })
}
fn invalid_response() -> QueryAttempt<GetAccountInfoOutput> {
    rejected(
        "invalidResponse",
        FailureKind::Protocol,
        "Invalid account response",
    )
}
fn decode_fields(
    fields: BalanceFields,
    status: u16,
    delay: f64,
) -> Option<QueryAttempt<GetAccountInfoOutput>> {
    let code = fields.optional::<i64>("code").ok()??;
    let data = fields
        .optional::<BalanceFields>("data")
        .ok()?
        .unwrap_or_default();
    let scope = data.optional::<String>("scope").ok()?.unwrap_or_default();
    let balance = data
        .optional::<BalanceFields>("balance")
        .ok()?
        .unwrap_or_default()
        .optional::<String>("yuan")
        .ok()?;
    let quota = data
        .optional::<BalanceFields>("key")
        .ok()?
        .unwrap_or_default()
        .optional::<i64>("quota_remaining")
        .ok()?;
    let attempt = match code {
        4029 => retry(
            FailureKind::RateLimit,
            "Provider rate limited the account query",
            delay,
        ),
        5000 | 5020 | 5021 | 5030 => retry(
            FailureKind::Availability,
            "Provider is temporarily unavailable",
            delay,
        ),
        4011 | 4015 => rejected(
            "providerRejected",
            FailureKind::Authentication,
            "Provider rejected the API key",
        ),
        4013 | 4014 => rejected(
            "providerRejected",
            FailureKind::Authorization,
            "Provider denied account access",
        ),
        0 => {
            if status != 200 {
                return Some(invalid_response());
            }
            let (balance_yuan, key_quota_remaining) = match scope.as_str() {
                "account" if balance.as_deref().is_some_and(valid_yuan) => (balance, None),
                "key" if quota.is_some_and(|n| n >= 0) => (None, quota),
                _ => return Some(invalid_response()),
            };
            QueryAttempt::branch(
                "found",
                GetAccountInfoOutput {
                    scope,
                    balance_yuan,
                    key_quota_remaining,
                },
                Receipt::default(),
                None,
            )
        }
        _ => rejected(
            "providerRejected",
            FailureKind::ProviderRejection,
            "Provider rejected the account query",
        ),
    };
    Some(attempt)
}
fn valid_yuan(value: &str) -> bool {
    let digits = |part: &str| !part.is_empty() && part.bytes().all(|b| b.is_ascii_digit());
    value.len() <= 32
        && value.split_once('.').map_or_else(
            || digits(value),
            |(whole, fraction)| digits(whole) && digits(fraction),
        )
}
fn rejected(branch: &str, kind: FailureKind, message: &str) -> QueryAttempt<GetAccountInfoOutput> {
    QueryAttempt::branch(
        branch,
        GetAccountInfoOutput::default(),
        Receipt::default(),
        Some(Failure::new(kind, r"apizero", "getAccountInfo", message)),
    )
}

fn retry(kind: FailureKind, message: &str, delay: f64) -> QueryAttempt<GetAccountInfoOutput> {
    QueryAttempt::retry(
        Failure::new(kind, r"apizero", "getAccountInfo", message),
        delay,
    )
}
