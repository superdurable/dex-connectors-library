// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Get current weather from a Dex Query Step.

#[rustfmt::skip]
mod generated_definitions;

mod weather;
use dex_connectors_sdk as sdk;
use sdk::providerhttp::ProviderHttpRequest;
use sdk::{FailureKind, QueryAttempt, Receipt, SecretString};
use serde::{Deserialize, Serialize};
use weather::rejected;
pub use weather::{GetCurrentWeatherOperation, new_get_current_weather_step};

type WeatherOutput = GetCurrentWeatherOutput;

const CONNECTOR_ID: &str = "qweather";
const PROVIDER_LABEL: &str = "QWeather";
/// Location in decimal degrees.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct GetCurrentWeatherInput {
    /// Latitude, from -90 to 90.
    pub latitude: f64,
    /// Longitude, from -180 to 180.
    pub longitude: f64,
}
/// Current conditions, timestamp and required provider attribution.
#[derive(Clone, Default, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct GetCurrentWeatherOutput {
    /// Localized description of the weather.
    pub condition_text: String,
    /// Provider condition code.
    pub condition_code: String,
    /// Measured temperature in the reported unit.
    pub temperature_value: f64,
    /// Provider temperature unit.
    pub temperature_unit: String,
    /// Observation timestamp including its offset.
    pub valid_time: String,
    /// Provider attributions to display with the weather.
    pub attributions: Vec<String>,
}
/// Request settings for the weather service.
pub struct Config {
    /// Timeout for one provider attempt.
    pub timeout: std::time::Duration,
    /// Maximum response body size in bytes.
    pub max_response_bytes: usize,
}

impl Default for Config {
    fn default() -> Self {
        Self {
            timeout: std::time::Duration::from_secs(10),
            max_response_bytes: 1 << 20,
        }
    }
}
/// Account-specific host and API key resolved for each attempt.
#[derive(Clone)]
pub struct Credentials {
    /// Account API hostname without scheme, port or path.
    pub api_host: SecretString,
    /// Provider-issued API key for this account.
    pub api_key: SecretString,
}
impl Credentials {
    fn is_valid(&self) -> bool {
        let host = self.api_host.reveal();
        let key = self.api_key.reveal();
        host.len() <= 253
            && host.ends_with(".qweatherapi.com")
            && host.split('.').all(|label| {
                !label.is_empty()
                    && label.len() <= 63
                    && !label.starts_with('-')
                    && !label.ends_with('-')
                    && label
                        .bytes()
                        .all(|b| b.is_ascii_lowercase() || b.is_ascii_digit() || b == b'-')
            })
            && !key.is_empty()
            && key.len() <= 1024
            && key.bytes().all(|b| b.is_ascii_graphic())
    }
    fn contains_credential(&self, text: &str) -> bool {
        text.contains(self.api_host.reveal()) || text.contains(self.api_key.reveal())
    }
}
impl GetCurrentWeatherInput {
    fn valid_coordinates(&self) -> bool {
        (-90.0..=90.0).contains(&self.latitude) && (-180.0..=180.0).contains(&self.longitude)
    }
    fn request(&self, credentials: &Credentials) -> ProviderHttpRequest {
        let host = credentials.api_host.reveal();
        ProviderHttpRequest::new(
            "GET",
            format!(
                "https://{host}/weather/v1/current/{:.6}/{:.6}",
                self.latitude, self.longitude
            ),
        )
        .header("Accept", "application/json")
        .header("X-Qw-Api-Key", credentials.api_key.reveal())
    }
}
fn monthly_limit(body: &[u8]) -> bool {
    weather::decode_fields::<serde_json::Value>(body)
        .ok()
        .and_then(|value| {
            value
                .pointer("/error/type")
                .and_then(|t| t.as_str())
                .map(|t| t.to_ascii_lowercase().contains("over-monthly-limit"))
        })
        .unwrap_or(false)
}
fn valid_time(text: &str) -> bool {
    static FORMAT: std::sync::LazyLock<regex::Regex> = std::sync::LazyLock::new(|| {
        regex::Regex::new(r"\A([0-9]{4})-([0-9]{2})-([0-9]{2})T([0-9]{1,2}):([0-9]{2})(?::([0-9]{2})(?:[.,][0-9]+)?)?(?:Z|[+-]([0-9]{2}):([0-9]{2}))\z").expect("weather timestamp pattern")
    });
    let Some(parts) = FORMAT.captures(text) else {
        return false;
    };
    let number = |index: usize| {
        parts.get(index).map_or(0, |m| {
            m.as_str().parse::<u32>().expect("matched decimal digits")
        })
    };
    let year = number(1);
    let days = match number(2) {
        1 | 3 | 5 | 7 | 8 | 10 | 12 => 31,
        4 | 6 | 9 | 11 => 30,
        2 => {
            28 + u32::from(
                year.is_multiple_of(4) && (!year.is_multiple_of(100) || year.is_multiple_of(400)),
            )
        }
        _ => return false,
    };
    (1..=days).contains(&number(3))
        && number(4) < 24
        && number(5) < 60
        && number(6) < 60
        && number(7) <= 24
        && number(8) <= 60
}
#[derive(Deserialize)]
struct Observation {
    condition: Condition,
    temperature: Temperature,
    validtime: String,
    metadata: Metadata,
}
#[derive(Deserialize)]
struct Condition {
    text: String,
    code: String,
}
#[derive(Deserialize)]
struct Temperature {
    value: f64,
    unit: String,
}
#[derive(Deserialize)]
struct Metadata {
    attributions: Vec<String>,
}
fn decode_weather(body: &[u8], credentials: &Credentials) -> QueryAttempt<GetCurrentWeatherOutput> {
    let invalid = || {
        rejected(
            "invalidResponse",
            FailureKind::Protocol,
            "QWeather returned invalid weather data",
        )
    };
    let Ok(data) = weather::decode_fields::<Observation>(body) else {
        return invalid();
    };
    let attributions = data.metadata.attributions;
    if data.condition.text.is_empty()
        || data.condition.code.is_empty()
        || data.temperature.unit.is_empty()
        || !data.temperature.value.is_finite()
        || !valid_time(&data.validtime)
        || [
            &data.condition.text,
            &data.condition.code,
            &data.temperature.unit,
            &data.validtime,
        ]
        .iter()
        .any(|field| credentials.contains_credential(field))
        || !(1..=16).contains(&attributions.len())
        || attributions
            .iter()
            .any(|a| a.is_empty() || a.len() > 500 || credentials.contains_credential(a))
    {
        return invalid();
    }
    QueryAttempt::branch(
        "found",
        GetCurrentWeatherOutput {
            condition_text: data.condition.text,
            condition_code: data.condition.code,
            temperature_value: data.temperature.value,
            temperature_unit: data.temperature.unit,
            valid_time: data.validtime,
            attributions,
        },
        Receipt::default(),
        None,
    )
}
