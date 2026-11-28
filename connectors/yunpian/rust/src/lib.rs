// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Read the Yunpian account balance from a Dex Query Step.

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

const BASE_URL: &str = r"https://sms.yunpian.com";

/// The balance query takes no application input.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct GetAccountBalanceInput {}

/// Account snapshot returned by the provider.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct GetAccountBalanceOutput {
    /// Remaining balance in account currency units.
    pub balance: f64,
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
            max_response_bytes: 1048576,
        }
    }
}

/// API key resolved from the logical connection for each request.
#[derive(Clone)]
pub struct Credentials {
    /// Provider-issued form credential, never stored in the result.
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
        let endpoint = origin + r"/v2/user/get.json";
        if max_response_bytes == 0 {
            return Err(ValidationError::new(
                "Yunpian response size limit must be positive",
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
                    "Yunpian API Key is unavailable or invalid",
                );
            }
        };
        let form = form_urlencoded::Serializer::new(String::new())
            .append_pair("apikey", credentials.api_key.reveal())
            .finish();
        let request = ProviderHttpRequest::new("POST", &self.endpoint)
            .header(
                "Content-Type",
                "application/x-www-form-urlencoded; charset=utf-8",
            )
            .body(form)
            .header("Accept", "application/json");
        let Ok(mut response) = self.http.send_traced(&request) else {
            return retry(
                FailureKind::Transport,
                "Yunpian balance request failed",
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
                    "Yunpian rate limited the balance request",
                    delay,
                ),
                _ => retry(
                    FailureKind::Availability,
                    "Yunpian API is temporarily unavailable",
                    delay,
                ),
            };
        }
        match read_bounded_body(&mut response, self.max_response_bytes) {
            Ok(body) => decode_balance(&body, status, delay),
            Err(ProviderHttpError::BodyTooLarge(_)) => rejected(
                "invalidResponse",
                FailureKind::ResponseTooLarge,
                "Yunpian response exceeded the configured limit",
            ),
            Err(_) => retry(
                FailureKind::Transport,
                "Yunpian response was interrupted",
                0.0,
            ),
        }
    }
}

/// Bind the balance operation to its branch targets and validate the Step configuration.
pub fn new_get_account_balance_step<Input>(
    config: QueryStepConfig<Input, GetAccountBalanceOperation>,
) -> Result<QueryStep<Input, GetAccountBalanceOperation>, ValidationError> {
    if config.connection.provider != r"yunpian" {
        return Err(ValidationError::new(
            "Yunpian connection provider is invalid",
        ));
    }
    QueryStep::new(config)
}

fn valid_key(key: &str) -> bool {
    !key.is_empty() && key.trim() == key
}

fn decode_balance(body: &[u8], status: u16, delay: f64) -> QueryAttempt<GetAccountBalanceOutput> {
    let invalid = || {
        rejected(
            "invalidResponse",
            FailureKind::Protocol,
            "Invalid account balance response",
        )
    };
    let mut decoder = serde_json::Deserializer::from_slice(body);
    let Ok(fields) = Option::<BalanceFields>::deserialize(&mut decoder) else {
        return invalid();
    };
    let code = match &fields {
        Some(fields) => match fields.optional::<i64>("code") {
            Ok(value) => value.unwrap_or(0),
            Err(_) => return invalid(),
        },
        None => 0,
    };
    match code {
        -5 => {
            return retry(
                FailureKind::RateLimit,
                "Yunpian query frequency exceeded",
                delay,
            );
        }
        -50 | -51 => {
            return retry(
                FailureKind::Availability,
                "Yunpian reported a temporary failure",
                delay,
            );
        }
        0 if status == 200 => {}
        _ => {
            return rejected(
                "providerRejected",
                FailureKind::ProviderRejection,
                "Yunpian rejected the balance lookup",
            );
        }
    }
    let Some(balance) = fields.and_then(|f| f.optional::<f64>("balance").ok().flatten()) else {
        return invalid();
    };
    if !balance.is_finite() || balance < 0.0 {
        return invalid();
    }
    QueryAttempt::branch(
        "read",
        GetAccountBalanceOutput { balance },
        Receipt::default(),
        None,
    )
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
        Some(Failure::new(kind, r"yunpian", "getAccountBalance", message)),
    )
}

fn retry(kind: FailureKind, message: &str, delay: f64) -> QueryAttempt<GetAccountBalanceOutput> {
    QueryAttempt::retry(
        Failure::new(kind, r"yunpian", "getAccountBalance", message),
        delay,
    )
}
