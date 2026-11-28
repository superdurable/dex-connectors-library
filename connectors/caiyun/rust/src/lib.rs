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

const CONNECTOR_ID: &str = "caiyun";
const PROVIDER_LABEL: &str = "Caiyun";
/// Observation coordinates in GCJ-02 for mainland China and WGS84 elsewhere.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct GetCurrentWeatherInput {
    /// Longitude in degrees east, from -180 to 180.
    pub longitude: f64,
    /// Latitude in degrees north, from -90 to 90.
    pub latitude: f64,
}
/// Bounded metric values from the realtime observation.
#[derive(Clone, Default, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct GetCurrentWeatherOutput {
    /// Air temperature at two meters, in Celsius.
    pub temperature_celsius: f64,
    /// Relative humidity as a fraction between zero and one.
    pub relative_humidity: f64,
    /// Provider weather condition code.
    pub sky_condition: String,
    /// Wind speed at ten meters, in meters per second.
    pub wind_speed_meters_per_second: f64,
}
/// Request settings. The default timeout is ten seconds and the response limit is 256 KiB.
pub struct Config {
    /// Caiyun origin or approved proxy base URL.
    pub base_url: String,
    /// Timeout for one HTTP attempt.
    pub timeout: std::time::Duration,
    /// Positive maximum response size in bytes.
    pub max_response_bytes: usize,
}
impl Default for Config {
    fn default() -> Self {
        Self {
            base_url: "https://api.caiyunapp.com".into(),
            timeout: std::time::Duration::from_secs(10),
            max_response_bytes: 262144,
        }
    }
}
/// Caiyun signing credentials resolved from the connection on each attempt.
#[derive(Clone)]
pub struct Credentials {
    /// Path-safe application key assigned by Caiyun.
    pub app_key: SecretString,
    /// Secret used to compute the HMAC signature.
    pub app_secret: SecretString,
}
impl Credentials {
    fn is_valid(&self) -> bool {
        let key = self.app_key.reveal();
        let secret = self.app_secret.reveal();
        !key.is_empty()
            && key.len() <= 256
            && key
                .bytes()
                .all(|b| b.is_ascii_alphanumeric() || b == b'-' || b == b'_')
            && !secret.is_empty()
            && secret.len() <= 512
            && secret.bytes().all(|b| b.is_ascii_graphic())
    }
}
impl GetCurrentWeatherInput {
    fn valid_coordinates(&self) -> bool {
        (-180.0..=180.0).contains(&self.longitude) && (-90.0..=90.0).contains(&self.latitude)
    }
    fn signed_request(
        &self,
        base_url: &str,
        credentials: &Credentials,
        nonce_bytes: &[u8; 16],
        timestamp: i128,
    ) -> ProviderHttpRequest {
        use base64::Engine;
        use std::fmt::Write;
        let key = credentials.app_key.reveal();
        let path = format!(
            "/v2.6/{key}/{},{}/realtime",
            coordinate(self.longitude),
            coordinate(self.latitude)
        );
        let mut nonce = String::with_capacity(32);
        for byte in nonce_bytes {
            write!(&mut nonce, "{byte:02x}").expect("writing to a String");
        }
        let canonical = format!("GET:{path}::{key}:{nonce}:{timestamp}");
        let hmac_key = ring::hmac::Key::new(
            ring::hmac::HMAC_SHA256,
            credentials.app_secret.reveal().as_bytes(),
        );
        let signature = base64::engine::general_purpose::URL_SAFE
            .encode(ring::hmac::sign(&hmac_key, canonical.as_bytes()).as_ref());
        ProviderHttpRequest::new("GET", format!("{base_url}{path}"))
            .header("Accept", "application/json")
            .header("X-Cy-Nonce", nonce)
            .header("X-Cy-Timestamp", timestamp.to_string())
            .header("X-Cy-Signature", signature)
    }
}
fn coordinate(value: f64) -> String {
    let mut buffer = ryu::Buffer::new();
    let shortest = buffer.format_finite(value.abs());
    let (mantissa, exponent) = shortest
        .split_once('e')
        .map_or((shortest, 0), |(m, power)| {
            (m, power.parse::<i32>().expect("Ryū exponent"))
        });
    let integer_digits = mantissa.find('.').unwrap_or(mantissa.len()) as i32;
    let digits = mantissa.replace('.', "");
    let point = integer_digits + exponent;
    let plain = if point <= 0 {
        format!("0.{}{}", "0".repeat((-point) as usize), digits)
    } else if point as usize >= digits.len() {
        format!("{digits}{}", "0".repeat(point as usize - digits.len()))
    } else {
        format!(
            "{}.{}",
            &digits[..point as usize],
            &digits[point as usize..]
        )
    };
    let plain = if plain.contains('.') {
        plain.trim_end_matches('0').trim_end_matches('.')
    } else {
        &plain
    };
    if value.is_sign_negative() {
        format!("-{plain}")
    } else {
        plain.into()
    }
}
#[derive(Deserialize)]
struct Envelope {
    status: String,
    result: WeatherResult,
}
#[derive(Deserialize)]
struct WeatherResult {
    realtime: Observation,
}
#[derive(Deserialize)]
struct Observation {
    status: String,
    temperature: f64,
    humidity: f64,
    skycon: String,
    wind: Wind,
}
#[derive(Deserialize)]
struct Wind {
    speed: f64,
}
fn decode_weather(body: &[u8], credentials: &Credentials) -> QueryAttempt<GetCurrentWeatherOutput> {
    let invalid = || {
        rejected(
            "invalidResponse",
            FailureKind::Protocol,
            "Caiyun returned invalid realtime weather data",
        )
    };
    let Ok(document) = weather::decode_fields::<Envelope>(body) else {
        return invalid();
    };
    let observation = document.result.realtime;
    if document.status != "ok"
        || observation.status != "ok"
        || observation.skycon.is_empty()
        || observation.skycon.contains(credentials.app_key.reveal())
        || observation.skycon.contains(credentials.app_secret.reveal())
        || !(-150.0..=150.0).contains(&observation.temperature)
        || !(0.0..=1.0).contains(&observation.humidity)
        || !(0.0..=200.0).contains(&observation.wind.speed)
    {
        return invalid();
    }
    QueryAttempt::branch(
        "fetched",
        GetCurrentWeatherOutput {
            temperature_celsius: observation.temperature,
            relative_humidity: observation.humidity,
            sky_condition: observation.skycon,
            wind_speed_meters_per_second: observation.wind.speed,
        },
        Receipt::default(),
        None,
    )
}
