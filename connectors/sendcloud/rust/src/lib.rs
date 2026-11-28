// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Read the SendCloud account balance from a Dex Query Step.

#[rustfmt::skip]
mod generated_definitions;
mod balance_fields;
mod decimal;
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

const BASE_URL: &str = r"https://api.sendcloud.net";

/// The balance query takes no application input.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct GetAccountBalanceInput {}

/// Account snapshot returned by the provider.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct GetAccountBalanceOutput {
    /// Available balance as the exact provider decimal.
    #[serde(rename = "availableBalance")]
    pub available_balance: String,
    /// Total balance as the exact provider decimal.
    pub balance: String,
    /// Request allowance for the current day.
    #[serde(rename = "dailyQuota")]
    pub daily_quota: i64,
    /// Requests used today.
    #[serde(rename = "todayUsedQuota")]
    pub today_used_quota: i64,
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
            max_response_bytes: 262144,
        }
    }
}

/// API user and API key resolved together for each request.
#[derive(Clone)]
pub struct Credentials {
    /// Private API user identifier.
    pub api_user: SecretString,
    /// Private API key paired with the API user.
    pub api_key: SecretString,
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
        let endpoint = origin + r"/apiv2/userinfo/get";
        if max_response_bytes == 0 {
            return Err(ValidationError::new(
                "SendCloud response size limit must be positive",
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
            Ok(credentials)
                if valid_key(credentials.api_user.reveal())
                    && valid_key(credentials.api_key.reveal()) =>
            {
                credentials
            }
            _ => {
                return rejected(
                    "defect",
                    FailureKind::Authentication,
                    "SendCloud API user or API key is unavailable or invalid",
                );
            }
        };
        let body = form_urlencoded::Serializer::new(String::new())
            .append_pair("apiKey", credentials.api_key.reveal())
            .append_pair("apiUser", credentials.api_user.reveal())
            .finish();
        let request = ProviderHttpRequest::new("POST", &self.endpoint)
            .header("Accept", "application/json")
            .header("Content-Type", "application/x-www-form-urlencoded")
            .body(body);
        let Ok(mut response) = self.http.send_traced(&request) else {
            return retry(
                FailureKind::Availability,
                "SendCloud balance request failed",
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
                    "SendCloud rate limited the balance request",
                    delay,
                ),
                _ => retry(
                    FailureKind::Availability,
                    "SendCloud API is temporarily unavailable",
                    delay,
                ),
            };
        }
        match read_bounded_body(&mut response, self.max_response_bytes) {
            Ok(body) => decode_balance(&body, status, delay),
            Err(ProviderHttpError::BodyTooLarge(_)) => rejected(
                "invalidResponse",
                FailureKind::ResponseTooLarge,
                "SendCloud response exceeded the configured limit",
            ),
            Err(_) => retry(
                FailureKind::Availability,
                "SendCloud response was interrupted",
                0.0,
            ),
        }
    }
}

/// Bind the balance operation to its branch targets and validate the Step configuration.
pub fn new_get_account_balance_step<Input>(
    config: QueryStepConfig<Input, GetAccountBalanceOperation>,
) -> Result<QueryStep<Input, GetAccountBalanceOperation>, ValidationError> {
    if config.connection.provider != r"sendcloud" {
        return Err(ValidationError::new(
            "SendCloud connection provider is invalid",
        ));
    }
    QueryStep::new(config)
}

fn valid_key(key: &str) -> bool {
    !key.is_empty() && key.len() <= 512 && key.bytes().all(|byte| byte.is_ascii_graphic())
}

fn decode_balance(body: &[u8], status: u16, delay: f64) -> QueryAttempt<GetAccountBalanceOutput> {
    serde_json::from_slice::<BalanceFields>(body)
        .ok()
        .and_then(|fields| decode_fields(fields, status, delay))
        .unwrap_or_else(|| {
            if status == 200 {
                rejected(
                    "invalidResponse",
                    FailureKind::Protocol,
                    "Invalid balance response",
                )
            } else {
                rejected(
                    "providerRejected",
                    FailureKind::ProviderRejection,
                    "SendCloud rejected the balance request",
                )
            }
        })
}

fn decode_fields(
    fields: BalanceFields,
    status: u16,
    delay: f64,
) -> Option<QueryAttempt<GetAccountBalanceOutput>> {
    let result = fields.optional::<bool>("result").ok()??;
    let code = fields.optional::<i64>("statuscode").ok()??;
    let info = fields.optional::<BalanceFields>("info").ok()?;
    let (mut available, mut balance, mut quota, mut used) = (None, None, None, None);
    if let Some(info) = info {
        let amount = |name| match info
            .optional::<Box<serde_json::value::RawValue>>(name)
            .ok()?
        {
            Some(raw) => Some(Some(decimal::amount(&raw)?)),
            None => Some(None),
        };
        available = amount("avaliablebalance")?;
        balance = amount("balance")?;
        quota = info.optional::<i64>("quota").ok()?;
        used = info.optional::<i64>("todayusedquota").ok()?;
    }
    if code == 50000 {
        return Some(retry(
            FailureKind::RateLimit,
            "SendCloud rate limited the request",
            delay,
        ));
    }
    if status != 200 || !result || code != 200 {
        let kind = match code {
            40005 => FailureKind::Authentication,
            40018 | 40025 | 40026 | 6001 => FailureKind::Authorization,
            _ => FailureKind::ProviderRejection,
        };
        return Some(rejected(
            "providerRejected",
            kind,
            "SendCloud rejected the account balance request",
        ));
    }
    let finite = |amount: &String| amount.parse::<f64>().is_ok_and(f64::is_finite);
    Some(QueryAttempt::branch(
        "retrieved",
        GetAccountBalanceOutput {
            available_balance: available.filter(finite)?,
            balance: balance.filter(finite)?,
            daily_quota: quota.filter(|count| *count >= 0)?,
            today_used_quota: used.filter(|count| *count >= 0)?,
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
            r"sendcloud",
            "getAccountBalance",
            message,
        )),
    )
}

fn retry(kind: FailureKind, message: &str, delay: f64) -> QueryAttempt<GetAccountBalanceOutput> {
    QueryAttempt::retry(
        Failure::new(kind, r"sendcloud", "getAccountBalance", message),
        delay,
    )
}
