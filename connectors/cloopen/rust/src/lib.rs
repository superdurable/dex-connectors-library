// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Read the Cloopen account balance from a Dex Query Step.

#[rustfmt::skip]
mod generated_definitions;
mod balance_fields;
use balance_fields::BalanceFields;

use base64::Engine;
use md5::{Digest, Md5};
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

const BASE_URL: &str = r"https://app.cloopen.com:8883";

/// The balance query takes no application input.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct GetBalanceInput {}

/// Account snapshot returned by the provider.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct GetBalanceOutput {
    /// Nonnegative decimal balance in provider units.
    pub balance: String,
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

/// Account SID and auth token resolved together for each request.
#[derive(Clone)]
pub struct Credentials {
    /// Main account SID used in the signed request.
    pub account_sid: SecretString,
    /// Private authentication token used to sign requests.
    pub auth_token: SecretString,
}
/// Bound balance query. Construct once and share the HTTP client across calls.
pub struct GetBalanceOperation {
    credentials: Arc<dyn CredentialProvider<Credentials>>,
    http: ProviderHttpClient,
    endpoint: String,
    clock: Arc<dyn Fn() -> SystemTime + Send + Sync>,
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
        Self::with_clock(
            credentials,
            http,
            max_response_bytes,
            base_url,
            Arc::new(SystemTime::now),
        )
    }

    /// Bind the request-signing clock, transport, origin and positive response limit.
    pub fn with_clock(
        credentials: Arc<dyn CredentialProvider<Credentials>>,
        http: ProviderHttpClient,
        max_response_bytes: usize,
        base_url: &str,
        clock: Arc<dyn Fn() -> SystemTime + Send + Sync>,
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
        let endpoint = origin + r"/2013-12-26/Accounts/";
        if max_response_bytes == 0 {
            return Err(ValidationError::new(
                "Cloopen response size limit must be positive",
            ));
        }
        Ok(Self {
            credentials,
            endpoint,
            clock,
            http,
            max_response_bytes,
        })
    }
}

impl Query for GetBalanceOperation {
    type Input = GetBalanceInput;
    type Output = GetBalanceOutput;

    fn definition(&self) -> QueryDefinition {
        generated_definitions::GetBalance::definition()
    }

    fn invoke(&self, call: &Call<'_>, _: &Self::Input) -> QueryAttempt<Self::Output> {
        let credentials = match self.credentials.resolve(call) {
            Ok(credentials)
                if valid_sid(credentials.account_sid.reveal())
                    && valid_token(credentials.auth_token.reveal()) =>
            {
                credentials
            }
            _ => {
                return rejected(
                    "defect",
                    FailureKind::Authentication,
                    "Cloopen account SID or auth token is unavailable or invalid",
                );
            }
        };
        let sid = credentials.account_sid.reveal();
        let now = chrono::DateTime::<chrono::Utc>::from((self.clock)());
        let timestamp = now.format("%Y%m%d%H%M%S").to_string();
        let hash = Md5::new()
            .chain_update(sid)
            .chain_update(credentials.auth_token.reveal())
            .chain_update(&timestamp)
            .finalize();
        let signature = hex::encode_upper(hash);
        let authorization =
            base64::engine::general_purpose::STANDARD.encode(format!("{sid}:{timestamp}"));
        let url = format!("{}{sid}/AccountInfo?sig={signature}", self.endpoint);
        let request = ProviderHttpRequest::new("GET", url)
            .header("Accept", "application/json")
            .header("Content-Type", "application/json;charset=utf-8")
            .header("Authorization", authorization);
        let Ok(mut response) = self.http.send_traced(&request) else {
            return retry(
                FailureKind::Availability,
                "Cloopen balance request failed",
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
                    "Cloopen rate limited the balance request",
                    delay,
                ),
                _ => retry(
                    FailureKind::Availability,
                    "Cloopen API is temporarily unavailable",
                    delay,
                ),
            };
        }
        match read_bounded_body(&mut response, self.max_response_bytes) {
            Ok(_) if status != 200 => rejected(
                "providerRejected",
                match status {
                    401 => FailureKind::Authentication,
                    403 => FailureKind::Authentication,
                    400 => FailureKind::Validation,
                    _ => FailureKind::ProviderRejection,
                },
                &format!("Cloopen rejected balance request with HTTP {status}"),
            ),
            Ok(body) => decode_balance(&body),
            Err(ProviderHttpError::BodyTooLarge(_)) => rejected(
                "invalidResponse",
                FailureKind::ResponseTooLarge,
                "Cloopen response exceeded the configured limit",
            ),
            Err(_) => retry(
                FailureKind::Availability,
                "Cloopen response was interrupted",
                0.0,
            ),
        }
    }
}

/// Bind the balance operation to its branch targets and validate the Step configuration.
pub fn new_get_balance_step<Input>(
    config: QueryStepConfig<Input, GetBalanceOperation>,
) -> Result<QueryStep<Input, GetBalanceOperation>, ValidationError> {
    if config.connection.provider != r"cloopen" {
        return Err(ValidationError::new(
            "Cloopen connection provider is invalid",
        ));
    }
    QueryStep::new(config)
}

fn valid_sid(sid: &str) -> bool {
    sid.len() == 32 && sid.bytes().all(|b| b.is_ascii_alphanumeric())
}
fn valid_token(token: &str) -> bool {
    token.len() == 32 && token.bytes().all(|b| b.is_ascii_graphic())
}

fn decode_balance(body: &[u8]) -> QueryAttempt<GetBalanceOutput> {
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
fn decode_fields(fields: BalanceFields) -> Option<QueryAttempt<GetBalanceOutput>> {
    let status = fields.optional::<String>("statuscode").ok()??;
    let account = fields.optional::<BalanceFields>("account").ok()?;
    let balance = match account {
        Some(account) => account.optional::<String>("balance").ok()?,
        None => None,
    };
    let attempt = match status.as_str() {
        "" => return None,
        "111111" => retry(
            FailureKind::RateLimit,
            "Cloopen concurrency limit reached",
            0.0,
        ),
        s if s.starts_with("1110") => retry(
            FailureKind::Availability,
            "Cloopen reported an internal failure",
            0.0,
        ),
        "000000" => {
            let value = balance?;
            let (integer, fraction) = value
                .split_once('.')
                .map_or((value.as_str(), None), |(i, f)| (i, Some(f)));
            if value.len() > 64
                || integer.is_empty()
                || (integer.len() > 1 && integer.starts_with('0'))
                || !integer.bytes().all(|b| b.is_ascii_digit())
                || fraction.is_some_and(|f| f.is_empty() || !f.bytes().all(|b| b.is_ascii_digit()))
            {
                return None;
            }
            QueryAttempt::branch(
                "fetched",
                GetBalanceOutput { balance: value },
                Receipt::default(),
                None,
            )
        }
        _ => rejected(
            "providerRejected",
            FailureKind::ProviderRejection,
            "Cloopen rejected the account lookup",
        ),
    };
    Some(attempt)
}
fn rejected(branch: &str, kind: FailureKind, message: &str) -> QueryAttempt<GetBalanceOutput> {
    QueryAttempt::branch(
        branch,
        GetBalanceOutput::default(),
        Receipt::default(),
        Some(Failure::new(kind, r"cloopen", "getBalance", message)),
    )
}

fn retry(kind: FailureKind, message: &str, delay: f64) -> QueryAttempt<GetBalanceOutput> {
    QueryAttempt::retry(Failure::new(kind, r"cloopen", "getBalance", message), delay)
}
