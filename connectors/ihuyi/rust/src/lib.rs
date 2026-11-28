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

const PROVIDER_LABEL: &str = "Ihuyi";
const CONNECTOR_ID: &str = "ihuyi";
const ENDPOINT: &str = r"https://api.ihuyi.com/appdev/ip/Submit.json";

/// Coarse location fields returned by Ihuyi. Missing optional fields are empty.
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
    /// Provider-issued account identifier.
    pub api_id: String,
    /// Provider-issued API key, never stored in the result.
    pub api_key: SecretString,
}

impl Credentials {
    fn is_valid(&self) -> bool {
        lookup::valid_key(&self.api_id) && lookup::valid_key(self.api_key.reveal())
    }
}

fn lookup_request(
    ip: std::net::Ipv4Addr,
    credentials: &Credentials,
    clock: &dyn Fn() -> std::time::SystemTime,
) -> ProviderHttpRequest {
    use md5::{Digest, Md5};
    let seconds = sdk::Rfc3339NanoTimestamp::from_system_time_utc(clock())
        .unix_nanoseconds()
        .div_euclid(1_000_000_000)
        .to_string();
    let signature = Md5::new()
        .chain_update(credentials.api_id.as_bytes())
        .chain_update(credentials.api_key.reveal().as_bytes())
        .chain_update(seconds.as_bytes())
        .finalize();
    let fields = std::collections::BTreeMap::from([
        ("account".to_owned(), vec![credentials.api_id.clone()]),
        ("ip".to_owned(), vec![ip.to_string()]),
        ("password".to_owned(), vec![hex::encode(signature)]),
        ("time".to_owned(), vec![seconds]),
    ]);
    ProviderHttpRequest::new("POST", ENDPOINT)
        .header("Accept", "application/json")
        .header("Content-Type", "application/x-www-form-urlencoded")
        .body(sdk::providerhttp::encode_query_values(&fields).into_bytes())
}

#[derive(Default, Deserialize)]
struct LocationData {
    country: Option<String>,
    #[serde(rename = "porovince")]
    province: Option<String>,
    city: Option<String>,
    district: Option<String>,
    operator: Option<String>,
}

#[derive(Deserialize)]
struct LookupResponse {
    code: i64,
    result: Option<LocationData>,
}

fn decode_location(
    body: &[u8],
    _: f64,
    _: std::net::Ipv4Addr,
) -> QueryAttempt<LookupIPLocationOutput> {
    let response = serde_json::from_slice::<serde_json::Value>(body).and_then(|mut document| {
        lowercase_fields(&mut document);
        if let Some(result) = document.get_mut("result") {
            lowercase_fields(result);
        }
        serde_json::from_value::<LookupResponse>(document)
    });
    let Ok(response) = response else {
        return rejected(
            "invalidResponse",
            FailureKind::Protocol,
            "Ihuyi returned invalid lookup data",
        );
    };
    if response.code != 2 {
        let kind = match response.code {
            405 | 4050 | 40501 | 40502 => FailureKind::Authentication,
            400 | 4052 => FailureKind::Authorization,
            _ => FailureKind::ProviderRejection,
        };
        return rejected(
            "providerRejected",
            kind,
            &format!("Ihuyi rejected lookup with code {}", response.code),
        );
    }
    let data = response.result.unwrap_or_default();
    let output = LookupIPLocationOutput {
        country: data.country.unwrap_or_default(),
        province: data.province.unwrap_or_default(),
        city: data.city.unwrap_or_default(),
        district: data.district.unwrap_or_default(),
        operator: data.operator.unwrap_or_default(),
    };
    if output.province.trim().is_empty() || output.city.trim().is_empty() {
        return rejected(
            "invalidResponse",
            FailureKind::Protocol,
            "Ihuyi returned incomplete location data",
        );
    }
    QueryAttempt::branch("found", output, Receipt::default(), None)
}
