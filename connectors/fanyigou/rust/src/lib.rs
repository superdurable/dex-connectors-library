// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Read the Fanyigou account balance from a Dex Query Step.

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

const ENDPOINT: &str = "https://www.fanyigou.com/TranslateApi/api/getAccount";

/// The balance query takes no application input.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct GetAccountBalanceInput {}

/// Account snapshot returned by the provider.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct GetAccountBalanceOutput {
    /// Remaining document resource quantity.
    #[serde(rename = "balance")]
    pub balance: i64,
    /// Zero for points and one for pages.
    #[serde(rename = "deductType")]
    pub deduct_type: i64,
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
            timeout: Duration::from_nanos(15000000000),
            max_response_bytes: 1048576,
        }
    }
}

/// Application ID and private key resolved together for each request.
#[derive(Clone)]
pub struct Credentials {
    /// Translation application identifier.
    pub app_id: SecretString,
    /// Private key for this translation application.
    pub private_key: SecretString,
}
/// Bound balance query. Construct once and share the HTTP client across calls.
pub struct GetAccountBalanceOperation {
    credentials: Arc<dyn CredentialProvider<Credentials>>,
    http: ProviderHttpClient,
    nonce: Arc<dyn Fn() -> Option<[u8; 16]> + Send + Sync>,
    max_response_bytes: usize,
}

impl GetAccountBalanceOperation {
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
        Self::with_nonce_source(
            credentials,
            http,
            max_response_bytes,
            Arc::new(|| {
                let mut bytes = [0; 16];
                getrandom::fill(&mut bytes).ok()?;
                Some(bytes)
            }),
        )
    }
    /// Bind a per-attempt nonce source. An unavailable nonce retries before sending.
    pub fn with_nonce_source(
        credentials: Arc<dyn CredentialProvider<Credentials>>,
        http: ProviderHttpClient,
        max_response_bytes: usize,
        nonce: Arc<dyn Fn() -> Option<[u8; 16]> + Send + Sync>,
    ) -> Result<Self, ValidationError> {
        if max_response_bytes == 0 {
            return Err(ValidationError::new(
                "Fanyigou response size limit must be positive",
            ));
        }
        Ok(Self {
            credentials,
            nonce,
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
            Ok(credentials)
                if valid_key(credentials.app_id.reveal())
                    && valid_key(credentials.private_key.reveal()) =>
            {
                credentials
            }
            _ => {
                return rejected(
                    "defect",
                    FailureKind::Authentication,
                    "Fanyigou application ID or private key is unavailable or invalid",
                );
            }
        };
        let Some(bytes) = (self.nonce)() else {
            return retry(
                FailureKind::Availability,
                "Fanyigou request nonce is unavailable",
                0.0,
            );
        };
        let nonce = hex::encode(bytes);
        let form = form_urlencoded::Serializer::new(String::new())
            .append_pair("appid", credentials.app_id.reveal())
            .append_pair("nonce_str", &nonce)
            .append_pair("type", "1")
            .finish();
        let request = ProviderHttpRequest::new("POST", ENDPOINT)
            .header("Accept", "application/json")
            .header(
                "Authorization",
                format!("Bearer {}", credentials.private_key.reveal()),
            )
            .header(
                "Content-Type",
                "application/x-www-form-urlencoded; charset=UTF-8",
            )
            .body(form.into_bytes());
        let Ok(mut response) = self.http.send_traced(&request) else {
            return retry(
                FailureKind::Availability,
                "Fanyigou balance request failed",
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
                    "Fanyigou rate limited the balance request",
                    delay,
                ),
                _ => retry(
                    FailureKind::Availability,
                    "Fanyigou API is temporarily unavailable",
                    delay,
                ),
            };
        }
        if status != 200 {
            return rejected(
                "providerRejected",
                FailureKind::ProviderRejection,
                "Fanyigou rejected the balance request",
            );
        }
        match read_bounded_body(&mut response, self.max_response_bytes) {
            Ok(body) => decode_balance(&body, delay),
            Err(ProviderHttpError::BodyTooLarge(_)) => rejected(
                "invalidResponse",
                FailureKind::ResponseTooLarge,
                "Fanyigou response exceeded the configured limit",
            ),
            Err(_) => retry(
                FailureKind::Availability,
                "Fanyigou response was interrupted",
                0.0,
            ),
        }
    }
}

/// Bind the balance operation to its branch targets and validate the Step configuration.
pub fn new_get_account_balance_step<Input>(
    config: QueryStepConfig<Input, GetAccountBalanceOperation>,
) -> Result<QueryStep<Input, GetAccountBalanceOperation>, ValidationError> {
    if config.connection.provider != r"fanyigou" {
        return Err(ValidationError::new(
            "Fanyigou connection provider is invalid",
        ));
    }
    QueryStep::new(config)
}

fn valid_key(key: &str) -> bool {
    !key.is_empty()
        && key.len() <= 1024
        && key.trim() == key
        && key.bytes().all(|b| b >= 32 && b != 127)
}

fn decode_balance(body: &[u8], delay: f64) -> QueryAttempt<GetAccountBalanceOutput> {
    serde_json::from_slice::<BalanceFields>(body)
        .ok()
        .and_then(|fields| decode_fields(fields, delay))
        .unwrap_or_else(|| {
            rejected(
                "invalidResponse",
                FailureKind::Protocol,
                "Invalid balance response",
            )
        })
}
fn decode_fields(
    fields: BalanceFields,
    delay: f64,
) -> Option<QueryAttempt<GetAccountBalanceOutput>> {
    let code = fields.optional::<i64>("code").ok()??;
    let data = fields.optional::<BalanceFields>("data").ok()?;
    let (units, balance) = if let Some(data) = data {
        (
            data.optional::<i64>("deducttype").ok()?,
            data.optional::<i64>("balance").ok()?,
        )
    } else {
        (None, None)
    };
    match code {
        102 | 105 => {
            return Some(retry(
                FailureKind::Availability,
                "Fanyigou account service is unavailable",
                delay,
            ));
        }
        108 => {
            return Some(retry(
                FailureKind::RateLimit,
                "Fanyigou account request was rate limited",
                delay,
            ));
        }
        100 => {}
        _ => {
            return Some(rejected(
                "providerRejected",
                FailureKind::ProviderRejection,
                "Fanyigou rejected the balance request",
            ));
        }
    }
    let deduct_type = units?;
    let balance = balance?;
    if !matches!(deduct_type, 0 | 1) || balance < 0 {
        return None;
    }
    Some(QueryAttempt::branch(
        "found",
        GetAccountBalanceOutput {
            deduct_type,
            balance,
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
            r"fanyigou",
            "getAccountBalance",
            message,
        )),
    )
}

fn retry(kind: FailureKind, message: &str, delay: f64) -> QueryAttempt<GetAccountBalanceOutput> {
    QueryAttempt::retry(
        Failure::new(kind, r"fanyigou", "getAccountBalance", message),
        delay,
    )
}
