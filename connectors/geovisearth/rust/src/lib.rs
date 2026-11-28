// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Get current weather from a Dex Query Step.

#[rustfmt::skip]
mod generated_definitions;

mod weather;
use dex_connectors_sdk as sdk;
use sdk::{FailureKind, QueryAttempt, Receipt, SecretString, ValidationError};
use serde::{Deserialize, Serialize};
use weather::rejected;
pub use weather::{GetCurrentWeatherOperation, new_get_current_weather_step};

type WeatherOutput = GetCurrentWeatherOutput;

const CONNECTOR_ID: &str = "geovisearth";
const PROVIDER_LABEL: &str = "GEOVIS Earth";
/// An official area code or a longitude,latitude coordinate.
#[derive(Clone, Default, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct GetCurrentWeatherInput {
    /// Official area code or longitude,latitude pair in decimal degrees.
    pub location: String,
}
/// Basic current weather observation.
#[derive(Clone, Default, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct GetCurrentWeatherOutput {
    /// AreaCode is the area code, blank for coordinate lookups.
    pub area_code: String,
    /// Weather is the Chinese weather condition description.
    pub weather: String,
    /// WeatherCode is the provider's weather phenomenon code.
    pub weather_code: String,
    /// TemperatureCelsius is air temperature in degrees Celsius.
    pub temperature_celsius: f64,
    /// RelativeHumidityPercent is relative humidity from 0 to 100 percent.
    pub relative_humidity_percent: i64,
    /// WindDirection is the human-readable wind direction.
    pub wind_direction: String,
    /// WindForce is the human-readable wind-force level.
    pub wind_force: String,
    /// SurfacePressureHpa is surface pressure in hectopascals.
    pub surface_pressure_hpa: f64,
    /// ObservedAt is the provider's local observation timestamp formatted yyyyMMddHHmmss.
    pub observed_at: String,
}
/// Request settings for the weather service.
pub struct Config {
    /// Documented GEOVIS Earth HTTPS origin.
    pub base_url: String,
    /// Timeout for one provider attempt.
    pub timeout: std::time::Duration,
    /// Maximum response body size in bytes.
    pub max_response_bytes: usize,
}

impl Default for Config {
    fn default() -> Self {
        Self {
            base_url: "https://api.open.geovisearth.com".into(),
            timeout: std::time::Duration::from_secs(10),
            max_response_bytes: 262144,
        }
    }
}
impl Config {
    fn endpoint(&self) -> Result<String, ValidationError> {
        let origin = sdk::providerhttp::validate_base_url(&self.base_url)?;
        if origin != "https://api.open.geovisearth.com" {
            return Err(ValidationError::new(
                "GEOVIS Earth requires its documented HTTPS origin",
            ));
        }
        Ok(format!("{origin}/v2/cn/area/basic"))
    }
}
/// Credential resolved from the logical connection for each request.
#[derive(Clone)]
pub struct Credentials {
    /// Provider-issued API key, never stored in the result.
    pub token: SecretString,
}

impl From<SecretString> for Credentials {
    fn from(credential: SecretString) -> Self {
        Self { token: credential }
    }
}

impl Credentials {
    fn is_valid(&self) -> bool {
        let value = self.token.reveal();
        !value.is_empty() && value.len() <= 512 && value.bytes().all(|byte| byte.is_ascii_graphic())
    }
}

impl GetCurrentWeatherInput {
    fn valid_location(&self) -> bool {
        if self
            .location
            .strip_prefix("WTX_CH")
            .is_some_and(|code| code.len() == 9 && code.bytes().all(|b| b.is_ascii_digit()))
        {
            return true;
        }
        self.location
            .split_once(',')
            .is_some_and(|(lon, lat)| valid_coordinate(lon, 180.0) && valid_coordinate(lat, 90.0))
    }
}
fn valid_coordinate(text: &str, maximum: f64) -> bool {
    static FORMAT: std::sync::LazyLock<regex::Regex> = std::sync::LazyLock::new(|| {
        regex::Regex::new(r"\A[+-]?(?:(?:[0-9](?:_?[0-9])*(?:\.(?:[0-9](?:_?[0-9])*)?)?|\.[0-9](?:_?[0-9])*)(?:[Ee][+-]?[0-9](?:_?[0-9])*)?|0[xX](?:_?[0-9A-Fa-f](?:_?[0-9A-Fa-f])*(?:\.(?:[0-9A-Fa-f](?:_?[0-9A-Fa-f])*)?)?|\.[0-9A-Fa-f](?:_?[0-9A-Fa-f])*)[pP][+-]?[0-9](?:_?[0-9])*)\z").expect("coordinate format")
    });
    if !FORMAT.is_match(text) {
        return false;
    }
    let literal = text.trim_start_matches(['-', '+']).replace('_', "");
    if let Some(hex) = literal
        .strip_prefix("0x")
        .or_else(|| literal.strip_prefix("0X"))
    {
        return hex_coordinate_in_range(hex, maximum);
    }
    literal
        .parse::<f64>()
        .is_ok_and(|coordinate| coordinate <= maximum)
}

fn hex_coordinate_in_range(literal: &str, maximum: f64) -> bool {
    let (mantissa, power) = literal
        .split_once(['p', 'P'])
        .expect("validated hex coordinate");
    let magnitude = power
        .trim_start_matches(['+', '-'])
        .bytes()
        .fold(0_i64, |n, b| {
            (n * 10 + i64::from(b - b'0')).min(1_i64 << 50)
        });
    let exponent = if power.starts_with('-') {
        -magnitude
    } else {
        magnitude
    };
    let integer_length = mantissa.find('.').unwrap_or(mantissa.len());
    let digits = mantissa.replace('.', "");
    let significant = digits.trim_start_matches('0');
    if significant.is_empty() {
        return true;
    }
    let skipped = digits.len() - significant.len();
    let (prefix, tail) = significant.split_at(significant.len().min(16));
    let mut head = u64::from_str_radix(prefix, 16).expect("validated hex digits");
    // Keep a sticky bit so converting the prefix to f64 rounds ties correctly.
    if tail.bytes().any(|digit| digit != b'0') {
        head |= 1;
    }
    let scale = exponent + 4 * (integer_length as i64 - skipped as i64 - prefix.len() as i64);
    if scale < -64 {
        return true;
    }
    if scale > 8 {
        return false;
    }
    head as f64 * 2.0_f64.powi(scale as i32) <= maximum
}
#[derive(Deserialize)]
struct Envelope {
    status: i64,
    date: Option<ObservationDate>,
    location: Option<Location>,
    result: Option<Observation>,
}
#[derive(Deserialize)]
struct ObservationDate {
    time: Option<String>,
}
#[derive(Deserialize)]
struct Location {
    areacode: Option<String>,
}
#[derive(Default, Deserialize)]
struct Observation {
    wp: Option<String>,
    wp_code: Option<String>,
    tem: Option<f64>,
    rh: Option<i64>,
    wd_desc: Option<String>,
    ws_desc: Option<String>,
    prs: Option<f64>,
}
fn decode_weather(body: &[u8], token: &str) -> QueryAttempt<GetCurrentWeatherOutput> {
    let invalid = || {
        rejected(
            "invalidResponse",
            FailureKind::Protocol,
            "GEOVIS Earth returned invalid weather data",
        )
    };
    let Ok(data) = weather::decode_fields::<Envelope>(body) else {
        return invalid();
    };
    let result = data.result.unwrap_or_default();
    if [result.tem, result.prs]
        .into_iter()
        .flatten()
        .any(|value| !value.is_finite())
    {
        return invalid();
    }
    if data.status != 0 {
        return rejected(
            "providerRejected",
            FailureKind::ProviderRejection,
            "GEOVIS Earth rejected the weather request",
        );
    }

    let output = GetCurrentWeatherOutput {
        area_code: data
            .location
            .and_then(|loc| loc.areacode)
            .unwrap_or_default(),
        observed_at: data.date.and_then(|date| date.time).unwrap_or_default(),
        weather: result.wp.unwrap_or_default(),
        weather_code: result.wp_code.unwrap_or_default(),
        temperature_celsius: result.tem.unwrap_or_default(),
        relative_humidity_percent: result.rh.unwrap_or(-1),
        wind_direction: result.wd_desc.unwrap_or_default(),
        wind_force: result.ws_desc.unwrap_or_default(),
        surface_pressure_hpa: result.prs.unwrap_or_default(),
    };
    if output.weather.is_empty()
        || output.weather_code.is_empty()
        || output.wind_force.is_empty()
        || output.wind_direction.is_empty()
        || !output.temperature_celsius.is_finite()
        || !output.surface_pressure_hpa.is_finite()
        || output.surface_pressure_hpa <= 0.0
        || !(0..=100).contains(&output.relative_humidity_percent)
        || !valid_time(&output.observed_at)
        || [
            &output.area_code,
            &output.observed_at,
            &output.weather,
            &output.weather_code,
            &output.wind_direction,
            &output.wind_force,
        ]
        .iter()
        .any(|field| field.contains(token))
    {
        return invalid();
    }
    QueryAttempt::branch("fetched", output, Receipt::default(), None)
}
fn valid_time(text: &str) -> bool {
    if text.len() != 14 || !text.bytes().all(|b| b.is_ascii_digit()) {
        return false;
    }
    let number = |start: usize, end: usize| {
        text[start..end]
            .parse::<u32>()
            .expect("checked decimal digits")
    };
    let year = number(0, 4);
    let days = match number(4, 6) {
        1 | 3 | 5 | 7 | 8 | 10 | 12 => 31,
        4 | 6 | 9 | 11 => 30,
        2 => {
            28 + u32::from(
                year.is_multiple_of(4) && (!year.is_multiple_of(100) || year.is_multiple_of(400)),
            )
        }
        _ => return false,
    };
    (1..=days).contains(&number(6, 8))
        && number(8, 10) < 24
        && number(10, 12) < 60
        && number(12, 14) < 60
}
