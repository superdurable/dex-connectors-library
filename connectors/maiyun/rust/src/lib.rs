// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Look up a public IPv4 location from a Dex Query Step.

#[rustfmt::skip]
mod generated_definitions;

mod lookup;
pub use lookup::{
    Config, LookupIPLocationInput, LookupIPLocationOperation, new_lookup_ip_location_step,
};

use dex_connectors_sdk as sdk;
use lookup::{lowercase_fields, rejected, retry};
use sdk::providerhttp::ProviderHttpRequest;
use sdk::{FailureKind, QueryAttempt, Receipt, SecretString};
use serde::{Deserialize, Serialize};

const ADDRESS_POLICY: lookup::LookupAddressPolicy = lookup::LookupAddressPolicy::PublicIpv4;
const DEFAULT_MAX_RESPONSE_BYTES: usize = 65_536;
const PROVIDER_LABEL: &str = "MAIYUN";
const CONNECTOR_ID: &str = "maiyun";
const ENDPOINT: &str = r"https://lts.maiyun.net/api/service/ip";

/// Coarse location fields returned by MAIYUN. Missing optional fields are empty.
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
}

/// Credential resolved from the logical connection for each request.
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

impl Credentials {
    fn is_valid(&self) -> bool {
        lookup::valid_key(self.token.reveal())
    }
}

fn lookup_request(
    ip: std::net::IpAddr,
    credentials: &Credentials,
    _: &dyn Fn() -> std::time::SystemTime,
) -> ProviderHttpRequest {
    let token = credentials.token.reveal();
    ProviderHttpRequest::new("POST", ENDPOINT)
        .header("Accept", "application/json")
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {}", token))
        .body(format!(r#"{{"ip":"{ip}"}}"#).into_bytes())
}

#[derive(Default, Deserialize)]
struct LocationData {
    country: Option<String>,
    province: Option<String>,
    city: Option<String>,
    district: Option<String>,
}

#[derive(Deserialize)]
struct LookupResponse {
    result: i64,
    #[serde(flatten)]
    data: LocationData,
}

fn decode_location(
    body: &[u8],
    delay: f64,
    _: std::net::IpAddr,
    _: &Credentials,
) -> QueryAttempt<LookupIPLocationOutput> {
    let response = serde_json::from_slice::<serde_json::Value>(body).and_then(|mut document| {
        lowercase_fields(&mut document);
        serde_json::from_value::<LookupResponse>(document)
    });
    let Ok(response) = response else {
        return rejected(
            "invalidResponse",
            FailureKind::Protocol,
            "MAIYUN returned invalid lookup data",
        );
    };
    if response.result == -429 {
        return retry(
            FailureKind::RateLimit,
            "MAIYUN rate limited the lookup",
            delay,
        );
    }
    if response.result <= -500 {
        return retry(
            FailureKind::Availability,
            "MAIYUN lookup service is temporarily unavailable",
            delay,
        );
    }
    if response.result != 1 {
        let kind = match response.result {
            -401 | -402 => FailureKind::Authentication,
            -403 => FailureKind::Authorization,
            -10 => FailureKind::QuotaExhausted,
            _ => FailureKind::ProviderRejection,
        };
        return rejected(
            "providerRejected",
            kind,
            &format!("MAIYUN rejected lookup with code {}", response.result),
        );
    }
    let data = response.data;
    let output = LookupIPLocationOutput {
        country: data.country.unwrap_or_default(),
        province: data.province.unwrap_or_default(),
        city: data.city.unwrap_or_default(),
        district: data.district.unwrap_or_default(),
    };
    if output.country.trim().is_empty() || output.city.trim().is_empty() {
        return rejected(
            "invalidResponse",
            FailureKind::Protocol,
            "MAIYUN returned incomplete location data",
        );
    }
    QueryAttempt::branch("found", output, Receipt::default(), None)
}
