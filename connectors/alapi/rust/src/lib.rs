// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Look up a public IPv4 location from a Dex Query Step.

#[rustfmt::skip]
mod generated_definitions;

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

const ENDPOINT: &str = r"https://v3.alapi.cn/api/ip";

/// IPv4 address whose location should be looked up.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct LookupIPLocationInput {
    /// A public IPv4 address in textual form.
    #[serde(rename = "ipAddress")]
    pub ip_address: String,
}

/// Coarse location fields returned by ALAPI. Missing optional fields are empty.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct LookupIPLocationOutput {
    /// Country name.
    pub country: String,
    /// Province name.
    pub province: String,
    /// City name.
    pub city: String,
    /// District name.
    pub district: String,
    /// Network operator.
    pub operator: String,
}

/// Settings for a single provider request.
pub struct Config {
    /// Timeout per attempt. Uses the manifest default.
    pub timeout: Duration,
    /// Maximum response size in bytes. Uses the manifest default, up to sixteen MiB.
    pub max_response_bytes: usize,
}

impl Default for Config {
    fn default() -> Self {
        Self {
            timeout: Duration::from_nanos(10000000000),
            max_response_bytes: 65536,
        }
    }
}

/// token resolved from the logical connection for each request.
#[derive(Clone)]
pub struct Credentials {
    /// Provider-issued token, never stored in the result.
    pub token: SecretString,
}

impl From<SecretString> for Credentials {
    fn from(credential: SecretString) -> Self {
        Self { token: credential }
    }
}

/// Bound IP location query. Construct once and share the HTTP client across calls.
pub struct LookupIPLocationOperation {
    credentials: Arc<dyn CredentialProvider<Credentials>>,
    http: ProviderHttpClient,
    max_response_bytes: usize,
}

impl LookupIPLocationOperation {
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
        if !(1..=(16 << 20)).contains(&max_response_bytes) {
            return Err(ValidationError::new(
                "ALAPI response size limit must be between one byte and sixteen MiB",
            ));
        }
        Ok(Self {
            credentials,
            http,
            max_response_bytes,
        })
    }
}

impl Query for LookupIPLocationOperation {
    type Input = LookupIPLocationInput;
    type Output = LookupIPLocationOutput;

    fn definition(&self) -> QueryDefinition {
        generated_definitions::LookupIPLocation::definition()
    }

    fn invoke(&self, call: &Call<'_>, input: &Self::Input) -> QueryAttempt<Self::Output> {
        let Some(ip) = public_ipv4(&input.ip_address) else {
            return rejected(
                "defect",
                FailureKind::Validation,
                "ALAPI requires one public IPv4 address",
            );
        };
        let credentials = match self.credentials.resolve(call) {
            Ok(credentials) if valid_key(credentials.token.reveal()) => credentials,
            _ => {
                return rejected(
                    "defect",
                    FailureKind::Authentication,
                    "ALAPI credentials are unavailable or invalid",
                );
            }
        };
        let request = ProviderHttpRequest::new("POST", ENDPOINT)
            .header("Accept", "application/json")
            .header("Content-Type", "application/json")
            .header("Token", credentials.token.reveal())
            .body(format!(r#"{{"ip":"{ip}"}}"#).into_bytes());
        let Ok(mut response) = self.http.send_traced(&request) else {
            return retry(
                FailureKind::Availability,
                "ALAPI lookup request failed",
                0.0,
            );
        };
        let status = response.status();
        let delay = parse_retry_after(
            response.headers().get("Retry-After").unwrap_or(""),
            SystemTime::now(),
        );
        if status != 200 {
            return match status {
                429 => retry(
                    FailureKind::RateLimit,
                    "ALAPI rate limited the lookup",
                    delay,
                ),
                408 | 500.. => retry(
                    FailureKind::Availability,
                    "ALAPI lookup service is temporarily unavailable",
                    delay,
                ),
                _ => rejected(
                    "providerRejected",
                    match status {
                        401 => FailureKind::Authentication,
                        403 => FailureKind::Authorization,
                        _ => FailureKind::ProviderRejection,
                    },
                    &format!("ALAPI rejected lookup with HTTP {status}"),
                ),
            };
        }
        match read_bounded_body(&mut response, self.max_response_bytes) {
            Ok(body) => decode_location(&body, delay),
            Err(ProviderHttpError::BodyTooLarge(_)) => rejected(
                "invalidResponse",
                FailureKind::ResponseTooLarge,
                "ALAPI response exceeded the configured limit",
            ),
            Err(_) => retry(
                FailureKind::Availability,
                "ALAPI response was interrupted",
                0.0,
            ),
        }
    }
}

/// Bind the lookup operation to its branch targets and validate the Step configuration.
pub fn new_lookup_ip_location_step<Input>(
    config: QueryStepConfig<Input, LookupIPLocationOperation>,
) -> Result<QueryStep<Input, LookupIPLocationOperation>, ValidationError> {
    if config.connection.provider != r"alapi" {
        return Err(ValidationError::new("ALAPI connection provider is invalid"));
    }
    QueryStep::new(config)
}

fn valid_key(key: &str) -> bool {
    !key.is_empty() && key.len() <= 1024 && key.bytes().all(|byte| byte.is_ascii_graphic())
}

fn public_ipv4(value: &str) -> Option<std::net::Ipv4Addr> {
    use std::net::IpAddr;
    let address = match value.parse::<IpAddr>().ok()? {
        IpAddr::V4(address) => address,
        IpAddr::V6(address) => address.to_ipv4_mapped()?,
    };
    if address.is_private()
        || address.is_loopback()
        || address.is_link_local()
        || address.is_unspecified()
        || address.is_multicast()
    {
        return None;
    }
    Some(address)
}

#[derive(Default, Deserialize)]
struct LocationData {
    country: Option<String>,
    province: Option<String>,
    city: Option<String>,
    district: Option<String>,
    isp: Option<String>,
}

#[derive(Deserialize)]
struct LookupResponse {
    success: bool,
    code: i64,
    data: Option<LocationData>,
}

fn decode_location(body: &[u8], delay: f64) -> QueryAttempt<LookupIPLocationOutput> {
    let response = serde_json::from_slice::<serde_json::Value>(body).and_then(|mut document| {
        lowercase_fields(&mut document);
        if let Some(data) = document.get_mut("data") {
            lowercase_fields(data);
        }
        serde_json::from_value::<LookupResponse>(document)
    });
    let Ok(response) = response else {
        return rejected(
            "invalidResponse",
            FailureKind::Protocol,
            "ALAPI returned invalid lookup data",
        );
    };
    if response.code == 429 {
        return retry(
            FailureKind::RateLimit,
            "ALAPI rate limited the lookup",
            delay,
        );
    }
    if response.code != 200 || !response.success {
        let kind = match response.code {
            10001 | 10002 | 10008 => FailureKind::Authentication,
            10004 | 10009 => FailureKind::Authorization,
            10005 | 10010 => FailureKind::QuotaExhausted,
            _ => FailureKind::ProviderRejection,
        };
        return rejected(
            "providerRejected",
            kind,
            &format!("ALAPI rejected lookup with code {}", response.code),
        );
    }
    let data = response.data.unwrap_or_default();
    let output = LookupIPLocationOutput {
        country: data.country.unwrap_or_default(),
        province: data.province.unwrap_or_default(),
        city: data.city.unwrap_or_default(),
        district: data.district.unwrap_or_default(),
        operator: data.isp.unwrap_or_default(),
    };
    if output.country.trim().is_empty() || output.city.trim().is_empty() {
        return rejected(
            "invalidResponse",
            FailureKind::Protocol,
            "ALAPI returned incomplete location data",
        );
    }
    QueryAttempt::branch("found", output, Receipt::default(), None)
}

fn rejected(
    branch: &str,
    kind: FailureKind,
    message: &str,
) -> QueryAttempt<LookupIPLocationOutput> {
    QueryAttempt::branch(
        branch,
        LookupIPLocationOutput::default(),
        Receipt::default(),
        Some(Failure::new(kind, r"alapi", "lookupIPLocation", message)),
    )
}

fn retry(kind: FailureKind, message: &str, delay: f64) -> QueryAttempt<LookupIPLocationOutput> {
    QueryAttempt::retry(
        Failure::new(kind, r"alapi", "lookupIPLocation", message),
        delay,
    )
}

fn lowercase_fields(value: &mut serde_json::Value) {
    if let Some(fields) = value.as_object_mut() {
        *fields = std::mem::take(fields)
            .into_iter()
            .map(|(key, value)| (key.to_ascii_lowercase(), value))
            .collect();
    }
}
