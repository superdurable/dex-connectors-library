// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Look up an IPv4 or IPv6 location from a Dex Query Step.

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

const ADDRESS_POLICY: lookup::LookupAddressPolicy = lookup::LookupAddressPolicy::Any;
const DEFAULT_MAX_RESPONSE_BYTES: usize = 1 << 20;
const PROVIDER_LABEL: &str = "IPIP";
const CONNECTOR_ID: &str = "ipip";
const ENDPOINT: &str = r"https://ipapi.ipip.net/v2/query/";

/// Default country, region and city fields returned by IPIP.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct LookupIPLocationOutput {
    /// Country name.
    #[serde(rename = "countryName")]
    pub country_name: String,
    /// Province or state name.
    #[serde(rename = "regionName")]
    pub region_name: String,
    /// City name.
    #[serde(rename = "cityName")]
    pub city_name: String,
    /// Country code.
    #[serde(rename = "countryCode")]
    pub country_code: String,
}

/// Credential resolved from the logical connection for each request.
#[derive(Clone)]
pub struct Credentials {
    /// IPIP service token, never stored in the result.
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
    let parameters =
        std::collections::BTreeMap::from([("token".to_owned(), vec![token.to_owned()])]);
    let query = sdk::providerhttp::encode_query_values(&parameters);
    ProviderHttpRequest::new("GET", format!("{ENDPOINT}{ip}?{query}"))
        .header("Accept", "application/json")
}

#[derive(Deserialize)]
struct LookupResponse {
    ret: String,
    data: Option<LocationData>,
}
#[derive(Deserialize)]
struct LocationData {
    info: Option<LocationInfo>,
}
#[derive(Deserialize)]
struct LocationInfo {
    country_name: Option<String>,
    region_name: Option<String>,
    city_name: Option<String>,
    country_code: Option<String>,
}

fn decode_location(
    body: &[u8],
    _: f64,
    _: std::net::IpAddr,
    _: &Credentials,
) -> QueryAttempt<LookupIPLocationOutput> {
    let response = serde_json::from_slice::<serde_json::Value>(body).and_then(|mut wire| {
        lowercase_fields(&mut wire);
        if let Some(data) = wire.get_mut("data") {
            lowercase_fields(data);
            if let Some(info) = data.get_mut("info") {
                lowercase_fields(info);
            }
        }
        serde_json::from_value::<LookupResponse>(wire)
    });
    let invalid = || {
        rejected(
            "invalidResponse",
            FailureKind::Protocol,
            "IPIP returned invalid location data",
        )
    };
    let Ok(response) = response else {
        return invalid();
    };
    if response.ret == "err" {
        return rejected(
            "providerRejected",
            FailureKind::ProviderRejection,
            "IPIP rejected the location request",
        );
    }
    let Some(info) = response.data.and_then(|data| data.info) else {
        return invalid();
    };
    let (Some(country_name), Some(region_name), Some(city_name), Some(country_code)) = (
        info.country_name,
        info.region_name,
        info.city_name,
        info.country_code,
    ) else {
        return invalid();
    };
    if response.ret != "ok" || country_name.is_empty() || country_code.is_empty() {
        return invalid();
    }
    QueryAttempt::branch(
        "found",
        LookupIPLocationOutput {
            country_name,
            region_name,
            city_name,
            country_code,
        },
        Receipt::default(),
        None,
    )
}
