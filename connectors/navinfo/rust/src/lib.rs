// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Look up an IPv4 location from a Dex Query Step.

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

const ADDRESS_POLICY: lookup::LookupAddressPolicy = lookup::LookupAddressPolicy::Ipv4;
const DEFAULT_MAX_RESPONSE_BYTES: usize = 1 << 20;
const PROVIDER_LABEL: &str = "NavInfo";
const CONNECTOR_ID: &str = "navinfo";
const ENDPOINT: &str = r"https://lbs.navinfo.com/open-api/position/v1/ip";

/// Coarse location fields returned by NavInfo. Missing optional fields are empty.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct LookupIPLocationOutput {
    /// Province or municipality.
    pub province: String,
    /// City name.
    pub city: String,
    /// City administrative code.
    pub adcode: String,
}

/// Credential resolved from the logical connection for each request.
#[derive(Clone)]
pub struct Credentials {
    /// Provider-issued API key, never stored in the result.
    pub ak: SecretString,
}

impl From<SecretString> for Credentials {
    fn from(credential: SecretString) -> Self {
        Self { ak: credential }
    }
}

impl Credentials {
    fn is_valid(&self) -> bool {
        lookup::valid_key(self.ak.reveal())
    }
}

fn lookup_request(
    ip: std::net::IpAddr,
    credentials: &Credentials,
    _: &dyn Fn() -> std::time::SystemTime,
) -> ProviderHttpRequest {
    let token = credentials.ak.reveal();
    let parameters = std::collections::BTreeMap::from([
        ("ip".to_owned(), vec![ip.to_string()]),
        ("ak".to_owned(), vec![token.to_owned()]),
    ]);
    let query = sdk::providerhttp::encode_query_values(&parameters);
    ProviderHttpRequest::new("GET", format!("{ENDPOINT}?{query}"))
        .header("Accept", "application/json")
}

#[derive(Deserialize)]
struct LookupResponse {
    status: i64,
    data: Option<LocationData>,
}
#[derive(Deserialize)]
struct LocationData {
    prov: Option<String>,
    city: Option<String>,
    adcode: Option<String>,
}

fn decode_location(
    body: &[u8],
    _: f64,
    _: std::net::IpAddr,
    credentials: &Credentials,
) -> QueryAttempt<LookupIPLocationOutput> {
    let response = serde_json::from_slice::<serde_json::Value>(body).and_then(|mut wire| {
        lowercase_fields(&mut wire);
        if let Some(data) = wire.get_mut("data") {
            lowercase_fields(data);
        }
        serde_json::from_value::<LookupResponse>(wire)
    });
    let invalid = || {
        rejected(
            "invalidResponse",
            FailureKind::Protocol,
            "NavInfo returned invalid location data",
        )
    };
    let Ok(response) = response else {
        return invalid();
    };
    match response.status {
        1008 | 1010 => {
            return lookup::retry(FailureKind::RateLimit, "NavInfo is temporarily busy", 0.0);
        }
        200 => (),
        _ => {
            return rejected(
                "providerRejected",
                FailureKind::ProviderRejection,
                "NavInfo rejected the location request",
            );
        }
    }
    let Some(data) = response.data else {
        return invalid();
    };
    let output = LookupIPLocationOutput {
        province: data.prov.unwrap_or_default(),
        city: data.city.unwrap_or_default(),
        adcode: data.adcode.unwrap_or_default(),
    };
    let location = format!("{}{}{}", output.province, output.city, output.adcode);
    if output.province.len() > 128
        || output.city.len() > 128
        || output.adcode.len() > 16
        || location.contains(['\r', '\n', '\0'])
        || location.contains(credentials.ak.reveal())
    {
        return invalid();
    }
    QueryAttempt::branch("found", output, Receipt::default(), None)
}
