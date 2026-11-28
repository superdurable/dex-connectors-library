// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Read the IsTero account status from a Dex Query Step.

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

const ENDPOINT: &str = "https://api.istero.com/resource/v1/isas/mine/information";

/// The account query takes no application input.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct GetAccountStatusInput {}

/// Account snapshot returned by the provider.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct GetAccountStatusOutput {
    /// Numeric ISAS account identifier.
    pub isid: i64,
    /// Calls already used today.
    #[serde(rename = "todayUsed")]
    pub today_used: i64,
    /// Calls remaining today.
    #[serde(rename = "todayRemaining")]
    pub today_remaining: i64,
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
    /// Decimal ISAS account identifier paired with this key.
    pub isid: String,
}

/// Bound account query. Construct once and share the HTTP client across calls.
pub struct GetAccountStatusOperation {
    credentials: Arc<dyn CredentialProvider<Credentials>>,
    http: ProviderHttpClient,
    max_response_bytes: usize,
}

impl GetAccountStatusOperation {
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
                "IsTero response size limit must be positive",
            ));
        }
        Ok(Self {
            credentials,
            http,
            max_response_bytes,
        })
    }
}

impl Query for GetAccountStatusOperation {
    type Input = GetAccountStatusInput;
    type Output = GetAccountStatusOutput;

    fn definition(&self) -> QueryDefinition {
        generated_definitions::GetAccountStatus::definition()
    }

    fn invoke(&self, call: &Call<'_>, _: &Self::Input) -> QueryAttempt<Self::Output> {
        let credentials = match self.credentials.resolve(call) {
            Ok(credentials)
                if valid_key(credentials.api_key.reveal()) && valid_isid(&credentials.isid) =>
            {
                credentials
            }
            _ => {
                return rejected(
                    "defect",
                    FailureKind::Authentication,
                    "IsTero API Key is unavailable or invalid",
                );
            }
        };
        let request =
            ProviderHttpRequest::new("GET", format!("{ENDPOINT}?isid={}", credentials.isid))
                .header(
                    "Authorization",
                    format!("Bearer {}", credentials.api_key.reveal()),
                )
                .header("Accept", "application/json");
        let Ok(mut response) = self.http.send_traced(&request) else {
            return retry(
                FailureKind::Availability,
                "IsTero account request failed",
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
        if status == 408 || status >= 500 {
            return retry(
                FailureKind::Availability,
                "Provider is temporarily unavailable",
                delay,
            );
        }
        match read_bounded_body(&mut response, self.max_response_bytes) {
            Ok(body) => decode_account(&body, status, delay, &credentials.isid),
            Err(ProviderHttpError::BodyTooLarge(_)) => rejected(
                "invalidResponse",
                FailureKind::ResponseTooLarge,
                "IsTero response exceeded the configured limit",
            ),
            Err(_) => retry(
                FailureKind::Availability,
                "IsTero response was interrupted",
                0.0,
            ),
        }
    }
}

/// Bind the balance operation to its branch targets and validate the Step configuration.
pub fn new_get_account_status_step<Input>(
    config: QueryStepConfig<Input, GetAccountStatusOperation>,
) -> Result<QueryStep<Input, GetAccountStatusOperation>, ValidationError> {
    if config.connection.provider != r"istero" {
        return Err(ValidationError::new(
            "IsTero connection provider is invalid",
        ));
    }
    QueryStep::new(config)
}

fn valid_isid(isid: &str) -> bool {
    !isid.is_empty()
        && isid.len() <= 18
        && !isid.starts_with('0')
        && isid.bytes().all(|b| b.is_ascii_digit())
}
fn valid_key(key: &str) -> bool {
    !key.is_empty() && key.len() <= 512 && key.bytes().all(|byte| byte.is_ascii_graphic())
}

fn decode_account(
    body: &[u8],
    status: u16,
    delay: f64,
    isid: &str,
) -> QueryAttempt<GetAccountStatusOutput> {
    serde_json::from_slice::<BalanceFields>(body)
        .ok()
        .and_then(|fields| decode_fields(fields, status, delay, isid))
        .unwrap_or_else(invalid_response)
}
fn invalid_response() -> QueryAttempt<GetAccountStatusOutput> {
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
    isid: &str,
) -> Option<QueryAttempt<GetAccountStatusOutput>> {
    let code = fields.optional::<i64>("code").ok()??;
    let data = fields
        .optional::<BalanceFields>("data")
        .ok()?
        .unwrap_or_default();
    let account = data.optional::<i64>("isid").ok()?;
    let used = data.optional::<i64>("today_used").ok()?;
    let remaining = data.optional::<i64>("today_remaining").ok()?;
    let attempt = match code {
        429 => retry(
            FailureKind::RateLimit,
            "Provider rate limited the account query",
            delay,
        ),
        500 => retry(
            FailureKind::Availability,
            "Provider is temporarily unavailable",
            delay,
        ),
        403 => rejected(
            "providerRejected",
            FailureKind::Authentication,
            "Provider rejected the API key",
        ),
        502 => rejected(
            "providerRejected",
            FailureKind::Authorization,
            "Provider denied account access",
        ),
        200 => {
            if status != 200 {
                return Some(invalid_response());
            }
            let account = account.filter(|account| account.to_string() == isid)?;
            QueryAttempt::branch(
                "retrieved",
                GetAccountStatusOutput {
                    isid: account,
                    today_used: used.filter(|v| *v >= 0)?,
                    today_remaining: remaining.filter(|v| *v >= 0)?,
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
fn rejected(
    branch: &str,
    kind: FailureKind,
    message: &str,
) -> QueryAttempt<GetAccountStatusOutput> {
    QueryAttempt::branch(
        branch,
        GetAccountStatusOutput::default(),
        Receipt::default(),
        Some(Failure::new(kind, r"istero", "getAccountStatus", message)),
    )
}

fn retry(kind: FailureKind, message: &str, delay: f64) -> QueryAttempt<GetAccountStatusOutput> {
    QueryAttempt::retry(
        Failure::new(kind, r"istero", "getAccountStatus", message),
        delay,
    )
}
