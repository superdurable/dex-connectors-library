// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Read the TianxiaGongchang account status from a Dex Query Step.

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

const ENDPOINT: &str = "https://open.tianxiagongchang.com/open/v1/account";

/// The account query takes no application input.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct GetAccountStatusInput {}

/// Account snapshot returned by the provider.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct GetAccountStatusOutput {
    /// Application identified by the key.
    #[serde(rename = "appId")]
    pub app_id: i64,
    /// Account-wide available provider credits.
    #[serde(rename = "creditsBalance")]
    pub credits_balance: i64,
    /// Credits frozen for this application.
    #[serde(rename = "frozenCredits")]
    pub frozen_credits: i64,
    /// Whether the provider identified the key as a sandbox key.
    #[serde(rename = "isSandbox")]
    pub is_sandbox: bool,
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
                "TianxiaGongchang response size limit must be positive",
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
            Ok(credentials) if valid_key(credentials.api_key.reveal()) => credentials,
            _ => {
                return rejected(
                    "defect",
                    FailureKind::Authentication,
                    "TianxiaGongchang API Key is unavailable or invalid",
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
                "TianxiaGongchang account request failed",
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
            Ok(body) => decode_account(&body, status, delay),
            Err(ProviderHttpError::BodyTooLarge(_)) => rejected(
                "invalidResponse",
                FailureKind::ResponseTooLarge,
                "TianxiaGongchang response exceeded the configured limit",
            ),
            Err(_) => retry(
                FailureKind::Availability,
                "TianxiaGongchang response was interrupted",
                0.0,
            ),
        }
    }
}

/// Bind the balance operation to its branch targets and validate the Step configuration.
pub fn new_get_account_status_step<Input>(
    config: QueryStepConfig<Input, GetAccountStatusOperation>,
) -> Result<QueryStep<Input, GetAccountStatusOperation>, ValidationError> {
    if config.connection.provider != r"tianxiagongchang" {
        return Err(ValidationError::new(
            "TianxiaGongchang connection provider is invalid",
        ));
    }
    QueryStep::new(config)
}

fn valid_key(key: &str) -> bool {
    !key.is_empty() && key.len() <= 512 && key.bytes().all(|byte| byte.is_ascii_graphic())
}

fn decode_account(body: &[u8], status: u16, delay: f64) -> QueryAttempt<GetAccountStatusOutput> {
    serde_json::from_slice::<BalanceFields>(body)
        .ok()
        .and_then(|fields| decode_fields(fields, status, delay))
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
) -> Option<QueryAttempt<GetAccountStatusOutput>> {
    let code = fields.optional::<i64>("code").ok()??;
    let data = fields
        .optional::<BalanceFields>("data")
        .ok()?
        .unwrap_or_default();
    let charged = fields.optional::<i64>("credits_charged").ok()?;
    let balance = fields.optional::<i64>("credits_balance").ok()?;
    let app = data.optional::<i64>("app_id").ok()?;
    let frozen = data.optional::<i64>("frozen_credits").ok()?;
    let sandbox = data.optional::<bool>("is_sandbox").ok()?;
    let attempt = match code {
        42900 => retry(
            FailureKind::RateLimit,
            "Provider rate limited the account query",
            delay,
        ),
        50000 | 50400 => retry(
            FailureKind::Availability,
            "Provider is temporarily unavailable",
            delay,
        ),
        40100 => rejected(
            "providerRejected",
            FailureKind::Authentication,
            "Provider rejected the API key",
        ),
        40300 | 42901 => rejected(
            "providerRejected",
            FailureKind::Authorization,
            "Provider denied account access",
        ),
        0 => {
            if status != 200 {
                return Some(invalid_response());
            }
            if charged != Some(0) {
                return Some(invalid_response());
            }
            QueryAttempt::branch(
                "retrieved",
                GetAccountStatusOutput {
                    app_id: app.filter(|v| *v > 0)?,
                    credits_balance: balance.filter(|v| *v >= 0)?,
                    frozen_credits: frozen.filter(|v| *v >= 0)?,
                    is_sandbox: sandbox?,
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
        Some(Failure::new(
            kind,
            r"tianxiagongchang",
            "getAccountStatus",
            message,
        )),
    )
}

fn retry(kind: FailureKind, message: &str, delay: f64) -> QueryAttempt<GetAccountStatusOutput> {
    QueryAttempt::retry(
        Failure::new(kind, r"tianxiagongchang", "getAccountStatus", message),
        delay,
    )
}
