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

const CONNECTOR_ID: &str = "juhe";
const PROVIDER_LABEL: &str = "Juhe";
const REJECT_HTTP_BEFORE_BODY: bool = false;
/// Selected city.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct GetCurrentWeatherInput {
    /// City is the exact city name or ID from Juhe API 73's supported-city list.
    pub city: String,
}
/// Current conditions reported by Juhe.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct GetCurrentWeatherOutput {
    /// City is the provider's returned city name.
    pub city: String,
    /// WeatherText is the current condition description.
    #[serde(rename = "weatherText")]
    pub weather_text: String,
    /// WeatherCode is the condition ID from Juhe's weather-type list.
    #[serde(rename = "weatherCode")]
    pub weather_code: String,
    /// TemperatureCelsius is the optional current temperature in Celsius.
    #[serde(rename = "temperatureCelsius")]
    pub temperature_celsius: String,
    /// HumidityPercent is the optional relative humidity percentage.
    #[serde(rename = "humidityPercent")]
    pub humidity_percent: String,
    /// WindDirection is the optional wind direction text.
    #[serde(rename = "windDirection")]
    pub wind_direction: String,
    /// WindPower is the optional wind power text.
    #[serde(rename = "windPower")]
    pub wind_power: String,
    /// AirQualityIndex is the optional AQI text.
    #[serde(rename = "airQualityIndex")]
    pub air_quality_index: String,
}
/// Request settings for the weather service.
pub struct Config {
    /// Timeout for one provider attempt.
    pub timeout: std::time::Duration,
    /// Maximum response body size in bytes.
    pub max_response_bytes: usize,
    /// Approved weather API base URL. Defaults to https://apis.juhe.cn.
    pub base_url: String,
}

impl Default for Config {
    fn default() -> Self {
        Self {
            timeout: std::time::Duration::from_secs(10),
            max_response_bytes: 524288,
            base_url: "https://apis.juhe.cn".into(),
        }
    }
}
impl Config {
    fn endpoint(&self) -> Result<String, ValidationError> {
        Ok(format!(
            "{}/simpleWeather/query",
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
        let value = self.api_key.reveal();
        !value.is_empty() && value.len() <= 512 && value.bytes().all(|byte| byte.is_ascii_graphic())
    }
}

impl GetCurrentWeatherInput {
    fn valid_city(&self) -> bool {
        !self.city.is_empty() && self.city.len() <= 128 && self.city.trim() == self.city
    }
    fn request(&self, endpoint: &str, credentials: &Credentials) -> ProviderHttpRequest {
        let values = std::collections::BTreeMap::from([
            ("city".into(), vec![self.city.clone()]),
            ("key".into(), vec![credentials.api_key.reveal().to_owned()]),
        ]);
        ProviderHttpRequest::new("POST", endpoint)
            .header("Accept", "application/json")
            .header("Content-Type", "application/x-www-form-urlencoded")
            .body(sdk::providerhttp::encode_query_values(&values).into_bytes())
    }
}

#[derive(Deserialize)]
struct WeatherEnvelope {
    error_code: i64,
    result: Option<WeatherResult>,
}
#[derive(Deserialize)]
struct WeatherResult {
    city: Option<String>,
    realtime: Option<Realtime>,
}
#[derive(Deserialize, Default)]
struct Realtime {
    info: Option<String>,
    wid: Option<String>,
    temperature: Option<String>,
    humidity: Option<String>,
    direct: Option<String>,
    power: Option<String>,
    aqi: Option<String>,
}
fn decode_weather(
    body: &[u8],
    _: &GetCurrentWeatherInput,
) -> QueryAttempt<GetCurrentWeatherOutput> {
    let invalid = || {
        rejected(
            "invalidResponse",
            FailureKind::Protocol,
            "Juhe returned invalid weather data",
        )
    };
    let Ok(response) = weather::decode_fields::<WeatherEnvelope>(body) else {
        return invalid();
    };
    if response.error_code != 0 {
        let kind = match response.error_code {
            207303 | 10014 | 10020 => {
                return weather::retry(
                    FailureKind::Availability,
                    "Juhe weather service is temporarily unavailable",
                    0.0,
                );
            }
            10011 => {
                return weather::retry(FailureKind::RateLimit, "Juhe limited the caller IP", 0.0);
            }
            10001 | 10003 | 10009 => FailureKind::Authentication,
            10002 | 10005 | 10007 | 10008 => FailureKind::Authorization,
            10012 | 10013 => FailureKind::QuotaExhausted,
            207301 | 207302 => FailureKind::Validation,
            _ => FailureKind::ProviderRejection,
        };
        return rejected(
            "providerRejected",
            kind,
            "Juhe rejected the weather request",
        );
    }
    let Some(result) = response.result else {
        return invalid();
    };
    let live = result.realtime.unwrap_or_default();
    let output = GetCurrentWeatherOutput {
        city: result.city.unwrap_or_default(),
        weather_text: live.info.unwrap_or_default(),
        weather_code: live.wid.unwrap_or_default(),
        temperature_celsius: live.temperature.unwrap_or_default(),
        humidity_percent: live.humidity.unwrap_or_default(),
        wind_direction: live.direct.unwrap_or_default(),
        wind_power: live.power.unwrap_or_default(),
        air_quality_index: live.aqi.unwrap_or_default(),
    };
    if output.city.is_empty() || output.weather_text.is_empty() || output.weather_code.is_empty() {
        return invalid();
    }
    QueryAttempt::branch("fetched", output, Receipt::default(), None)
}
