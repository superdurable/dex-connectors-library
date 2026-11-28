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

type WeatherOutput = GetCurrentWeatherOutput;

fn is_rate_limited(status:u16)->bool { status==429 }
fn http_rejection_kind(status:u16)->FailureKind { match status {401=>FailureKind::Authentication,403=>FailureKind::Authorization,_=>FailureKind::ProviderRejection} }

const CONNECTOR_ID: &str = "newayz";
const PROVIDER_LABEL: &str = "WAYZ";
const REJECT_HTTP_BEFORE_BODY: bool = true;
/// Selected city.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct GetCurrentWeatherInput {
    /// CityCode is a six-digit administrative code such as 420100 for Wuhan.
    #[serde(rename = "cityCode")]
    pub city_code: String,
}
/// Current conditions reported by WAYZ.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct GetCurrentWeatherOutput {
    /// Province is the province name.
    pub province: String,
    /// City is the city name.
    pub city: String,
    /// CityCode is the administrative code returned by WAYZ.
    #[serde(rename = "cityCode")]
    pub city_code: String,
    /// Weather is the current weather description.
    pub weather: String,
    /// TemperatureCelsius is the observed Celsius temperature as a decimal string.
    #[serde(rename = "temperatureCelsius")]
    pub temperature_celsius: String,
    /// WindDirection is the current wind direction.
    #[serde(rename = "windDirection")]
    pub wind_direction: String,
    /// WindPower is the provider wind scale string.
    #[serde(rename = "windPower")]
    pub wind_power: String,
    /// HumidityPercent is the relative humidity percent as a decimal string.
    #[serde(rename = "humidityPercent")]
    pub humidity_percent: String,
    /// ReportTime is the provider observation timestamp string.
    #[serde(rename = "reportTime")]
    pub report_time: String,
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
            max_response_bytes: 262144,
        }
    }
}
impl Config {
    fn endpoint(&self) -> Result<String, ValidationError> {
        Ok("https://api.newayz.com/location/scenario/v1/weather".to_owned())
    }
    fn valid_limit(bytes: usize) -> bool {
        (1..=(16 << 20)).contains(&bytes)
    }
}
/// Credential resolved from the logical connection for each request.
#[derive(Clone)]
pub struct Credentials {
    /// Provider-issued API key, never stored in the result.
    pub access_key: SecretString,
}

impl From<SecretString> for Credentials {
    fn from(credential: SecretString) -> Self {
        Self {
            access_key: credential,
        }
    }
}

impl Credentials {
    fn is_valid(&self) -> bool {
        let value = self.access_key.reveal();
        !value.is_empty()
            && value.len() <= 1024
            && value.bytes().all(|byte| byte.is_ascii_graphic())
    }
}

impl GetCurrentWeatherInput {
    fn valid_city(&self) -> bool {
        self.city_code.len() == 6 && self.city_code.bytes().all(|byte| byte.is_ascii_digit())
    }
    fn request(&self, endpoint: &str, credentials: &Credentials) -> ProviderHttpRequest {
        let values = std::collections::BTreeMap::from([
            ("city".into(), vec![self.city_code.clone()]),
            (
                "access_key".into(),
                vec![credentials.access_key.reveal().to_owned()],
            ),
            ("extensions".into(), vec!["base".into()]),
        ]);
        ProviderHttpRequest::new(
            "GET",
            format!(
                "{endpoint}?{}",
                sdk::providerhttp::encode_query_values(&values)
            ),
        )
        .header("Accept", "application/json")
    }
}

#[derive(Deserialize)]
struct WeatherEnvelope {
    status: String,
    infocode: Option<String>,
    lives: Option<Vec<Option<Observation>>>,
}
#[derive(Deserialize)]
struct Observation {
    province: Option<String>,
    city: Option<String>,
    adcode: Option<String>,
    weather: Option<String>,
    temperature: Option<String>,
    winddirection: Option<String>,
    windpower: Option<String>,
    humidity: Option<String>,
    reporttime: Option<String>,
}
fn decode_weather(
    body: &[u8],
    input: &GetCurrentWeatherInput,
) -> QueryAttempt<GetCurrentWeatherOutput> {
    let invalid = || {
        rejected(
            "invalidResponse",
            FailureKind::Protocol,
            "WAYZ returned invalid weather data",
        )
    };
    let Ok(response) = weather::decode_fields::<WeatherEnvelope>(body) else {
        return invalid();
    };
    if response.status.is_empty() {
        return invalid();
    }
    let code = response.infocode.unwrap_or_default();
    if response.status != "1" || code != "10000" {
        let code = if !code.is_empty()
            && code.len() <= 16
            && code.bytes().all(|byte| byte.is_ascii_digit())
        {
            &code
        } else {
            "unknown"
        };
        return rejected(
            "providerRejected",
            FailureKind::ProviderRejection,
            &format!("WAYZ rejected weather query with code {code}"),
        );
    }
    let mut observations = response.lives.unwrap_or_default();
    if observations.len() != 1 {
        return invalid();
    }
    let Some(live) = observations.pop().flatten() else {
        return invalid();
    };
    let output = GetCurrentWeatherOutput {
        province: live.province.unwrap_or_default(),
        city: live.city.unwrap_or_default(),
        city_code: live.adcode.unwrap_or_default(),
        weather: live.weather.unwrap_or_default(),
        temperature_celsius: live.temperature.unwrap_or_default(),
        wind_direction: live.winddirection.unwrap_or_default(),
        wind_power: live.windpower.unwrap_or_default(),
        humidity_percent: live.humidity.unwrap_or_default(),
        report_time: live.reporttime.unwrap_or_default(),
    };
    if output.city_code != input.city_code
        || output.city.is_empty()
        || output.weather.is_empty()
        || output.temperature_celsius.is_empty()
    {
        return invalid();
    }
    QueryAttempt::branch("fetched", output, Receipt::default(), None)
}
