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
use lookup::{lowercase_fields, rejected};
use sdk::providerhttp::ProviderHttpRequest;
use sdk::{FailureKind, QueryAttempt, Receipt, SecretString};
use serde::{Deserialize, Serialize};

const ADDRESS_POLICY: lookup::LookupAddressPolicy = lookup::LookupAddressPolicy::PublicIpv4;
const DEFAULT_MAX_RESPONSE_BYTES: usize = 65_536;
const PROVIDER_LABEL: &str = "YikeAPI";
const CONNECTOR_ID: &str = "yikeapi";
const ENDPOINT: &str = r"https://apia.yikeapi.com/ip/";

/// Coarse location fields returned by YikeAPI. Missing optional fields are empty.
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
    /// Provider-issued application ID.
    pub app_id: SecretString,
    /// Secret issued for that application.
    pub app_secret: SecretString,
}

impl Credentials {
    fn is_valid(&self) -> bool {
        lookup::valid_key(self.app_id.reveal()) && lookup::valid_key(self.app_secret.reveal())
    }
}

fn lookup_request(
    ip: std::net::IpAddr,
    credentials: &Credentials,
    _: &dyn Fn() -> std::time::SystemTime,
) -> ProviderHttpRequest {
    let parameters = std::collections::BTreeMap::from([
        ("ip".to_owned(), vec![ip.to_string()]),
        (
            "appid".to_owned(),
            vec![credentials.app_id.reveal().to_owned()],
        ),
        (
            "appsecret".to_owned(),
            vec![credentials.app_secret.reveal().to_owned()],
        ),
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
}

#[derive(Deserialize)]
struct LookupResponse {
    errcode: i64,
    ip: Option<String>,
    #[serde(flatten)]
    location: LocationData,
}

fn decode_location(
    body: &[u8],
    _: f64,
    ip: std::net::IpAddr,
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
            "YikeAPI returned invalid lookup data",
        );
    };
    if response.errcode != 0 {
        return rejected(
            "providerRejected",
            FailureKind::ProviderRejection,
            &format!("YikeAPI rejected lookup with code {}", response.errcode),
        );
    }
    if response.ip.as_deref() != Some(&ip.to_string()) {
        return rejected(
            "invalidResponse",
            FailureKind::Protocol,
            "YikeAPI returned incomplete location data",
        );
    }
    let data = response.location;
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
            "YikeAPI returned incomplete location data",
        );
    }
    QueryAttempt::branch("found", output, Receipt::default(), None)
}
