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

const PROVIDER_LABEL: &str = "ALAPI";
const CONNECTOR_ID: &str = "alapi";
const ENDPOINT: &str = r"https://v3.alapi.cn/api/ip";

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
    fn value(&self) -> &str {
        self.token.reveal()
    }
}

fn lookup_request(ip: std::net::Ipv4Addr, token: &str) -> ProviderHttpRequest {
    ProviderHttpRequest::new("POST", ENDPOINT)
        .header("Accept", "application/json")
        .header("Content-Type", "application/json")
        .header("Token", token)
        .body(format!(r#"{{"ip":"{ip}"}}"#).into_bytes())
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
