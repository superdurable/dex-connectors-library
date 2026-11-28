// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Get current weather from a Dex Query Step.

#[rustfmt::skip]
mod generated_definitions;

mod weather;
use dex_connectors_sdk as sdk;
use sdk::providerhttp::ProviderHttpRequest;
use sdk::{FailureKind, QueryAttempt, Receipt, SecretString, ValidationError};
use serde::{Deserialize, Serialize};
use weather::rejected;
pub use weather::{GetCurrentWeatherOperation, new_get_current_weather_step};

type WeatherOutput = CurrentWeather;

fn is_rate_limited(status: u16) -> bool {
    matches!(status, 413 | 429)
}
fn http_rejection_kind(status: u16) -> FailureKind {
    match status {
        401 => FailureKind::Authentication,
        403 | 416 => FailureKind::Authorization,
        400 | 404 => FailureKind::Validation,
        _ => FailureKind::ProviderRejection,
    }
}

const CONNECTOR_ID: &str = "apispace";
const PROVIDER_LABEL: &str = "APISpace";
const REJECT_HTTP_BEFORE_BODY: bool = false;
/// Selected city.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct GetCurrentWeatherInput {
    /// City ID from the APISpace weather product's area list.
    #[serde(rename = "areaCode")]
    pub area_code: String,
}
/// Current conditions reported by APISpace.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct CurrentWeather {
    /// Provider's city display name.
    pub city: String,
    /// Current weather description.
    pub condition: String,
    /// Current temperature in degrees Celsius.
    #[serde(rename = "temperatureCelsius")]
    pub temperature_celsius: f64,
    /// Relative humidity from 0 to 100 percent, inclusive.
    #[serde(rename = "humidityPercent")]
    pub humidity_percent: i64,
    /// Observation time in Beijing local time.
    #[serde(rename = "updatedAtBeijing")]
    pub updated_at_beijing: String,
}
/// Request settings for the weather service.
pub struct Config {
    /// Timeout for one provider attempt.
    pub timeout: std::time::Duration,
    /// Maximum response body size in bytes.
    pub max_response_bytes: usize,
    /// Approved weather API base URL. Defaults to https://eolink.o.apispace.com.
    pub base_url: String,
}

impl Default for Config {
    fn default() -> Self {
        Self {
            timeout: std::time::Duration::from_secs(15),
            max_response_bytes: 1048576,
            base_url: "https://eolink.o.apispace.com".into(),
        }
    }
}
impl Config {
    fn endpoint(&self) -> Result<String, ValidationError> {
        Ok(format!(
            "{}/456456/weather/v001/now",
            sdk::providerhttp::validate_base_url(&self.base_url)?
        ))
    }
    fn valid_limit(bytes: usize) -> bool {
        bytes > 0
    }
}
/// Credential resolved from the logical connection for each request.
#[derive(Clone)]
pub struct Credentials {
    /// Provider-issued API key, never stored in the result.
    pub api_token: SecretString,
}

impl From<SecretString> for Credentials {
    fn from(credential: SecretString) -> Self {
        Self {
            api_token: credential,
        }
    }
}

impl Credentials {
    fn is_valid(&self) -> bool {
        let value = self.api_token.reveal();
        !value.is_empty()
            && value.len() <= 2048
            && value.bytes().all(|byte| byte.is_ascii_graphic())
    }
}

impl GetCurrentWeatherInput {
    fn valid_city(&self) -> bool {
        !self.area_code.is_empty()
            && self.area_code.len() <= 32
            && self
                .area_code
                .bytes()
                .all(|byte| byte.is_ascii_alphanumeric())
    }
    fn request(&self, endpoint: &str, credentials: &Credentials) -> ProviderHttpRequest {
        ProviderHttpRequest::new("GET", format!("{endpoint}?areacode={}", self.area_code))
            .header("Accept", "application/json")
            .header("Content-Type", "application/json")
            .header("Authorization-Type", "apikey")
            .header("X-Apispace-Token", credentials.api_token.reveal())
    }
}

#[derive(Deserialize)]
struct WeatherEnvelope {
    status: i64,
    result: Option<WeatherResult>,
}
#[derive(Deserialize)]
struct WeatherResult {
    location: Option<Location>,
    realtime: Option<Observation>,
    last_update: Option<String>,
}
#[derive(Deserialize)]
struct Location {
    name: Option<String>,
}
#[derive(Deserialize)]
struct Observation {
    text: Option<String>,
    temp: Option<f64>,
    rh: Option<i64>,
}

fn decode_weather(body: &[u8], _: &GetCurrentWeatherInput) -> QueryAttempt<CurrentWeather> {
    let invalid = || {
        rejected(
            "invalidResponse",
            FailureKind::Protocol,
            "APISpace returned invalid weather data",
        )
    };
    let Ok(wire) = weather::decode_fields::<WeatherEnvelope>(body) else {
        return invalid();
    };
    if wire
        .result
        .as_ref()
        .and_then(|result| result.realtime.as_ref())
        .and_then(|observation| observation.temp)
        .is_some_and(|temperature| !temperature.is_finite())
    {
        return invalid();
    }
    if wire.status != 0 {
        return rejected(
            "providerRejected",
            FailureKind::ProviderRejection,
            &format!(
                "APISpace rejected weather query with status {}",
                wire.status
            ),
        );
    }
    let Some(result) = wire.result else {
        return invalid();
    };
    let (Some(city), Some(live), Some(updated_at_beijing)) = (
        result.location.and_then(|location| location.name),
        result.realtime,
        result.last_update,
    ) else {
        return invalid();
    };
    let (Some(condition), Some(temperature_celsius), Some(humidity_percent)) =
        (live.text, live.temp, live.rh)
    else {
        return invalid();
    };
    if city.is_empty()
        || condition.is_empty()
        || updated_at_beijing.is_empty()
        || !(0..=100).contains(&humidity_percent)
    {
        return invalid();
    }
    QueryAttempt::branch(
        "found",
        CurrentWeather {
            city,
            condition,
            temperature_celsius,
            humidity_percent,
            updated_at_beijing,
        },
        Receipt::default(),
        None,
    )
}
