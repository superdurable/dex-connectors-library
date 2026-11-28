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

const PROVIDER_LABEL: &str = "HAIMOU";
const CONNECTOR_ID: &str = "haimou";
const ENDPOINT: &str = r"https://hmkj4399.com/v1/ip";

/// Coarse location fields returned by HAIMOU. Missing optional fields are empty.
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

/// Credential resolved from the logical connection for each request.
#[derive(Clone)]
pub struct Credentials {
    /// Provider-issued API key, never stored in the result.
    pub api_key: SecretString,
}

impl From<SecretString> for Credentials {
    fn from(credential: SecretString) -> Self {
        Self {
            api_key: credential,
        }
    }
}

impl Credentials {
    fn is_valid(&self) -> bool {
        lookup::valid_key(self.api_key.reveal())
    }
}

fn lookup_request(
    ip: std::net::Ipv4Addr,
    credentials: &Credentials,
    _: &dyn Fn() -> std::time::SystemTime,
) -> ProviderHttpRequest {
    let token = credentials.api_key.reveal();
    let parameters = std::collections::BTreeMap::from([
        ("ip".to_owned(), vec![ip.to_string()]),
        ("apikey".to_owned(), vec![token.to_owned()]),
    ]);
    let query = sdk::providerhttp::encode_query_values(&parameters);
    ProviderHttpRequest::new("GET", format!("{ENDPOINT}?{query}"))
        .header("Accept", "application/json")
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
    code: i64,
    data: Option<LocationData>,
}

fn decode_location(
    body: &[u8],
    delay: f64,
    _: std::net::Ipv4Addr,
) -> QueryAttempt<LookupIPLocationOutput> {
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
            "HAIMOU returned invalid lookup data",
        );
    };
    if response.code == 429 || response.code == 10006 {
        return retry(
            FailureKind::RateLimit,
            "HAIMOU rate limited the lookup",
            delay,
        );
    }
    if (500..=599).contains(&response.code) {
        return retry(
            FailureKind::Availability,
            "HAIMOU lookup service is temporarily unavailable",
            delay,
        );
    }
    if response.code != 200 {
        let kind = match response.code {
            10001 | 10002 => FailureKind::Authentication,
            10003 => FailureKind::Authorization,
            10005 => FailureKind::QuotaExhausted,
            _ => FailureKind::ProviderRejection,
        };
        return rejected(
            "providerRejected",
            kind,
            &format!("HAIMOU rejected lookup with code {}", response.code),
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
            "HAIMOU returned incomplete location data",
        );
    }
    QueryAttempt::branch("found", output, Receipt::default(), None)
}
