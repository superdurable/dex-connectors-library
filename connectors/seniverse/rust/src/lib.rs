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

const CONNECTOR_ID: &str = "seniverse";
const PROVIDER_LABEL: &str = "Seniverse";
/// Location to query.
#[derive(Clone, Default, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct GetCurrentWeatherInput {
    /// Location is a Seniverse city ID, Chinese city name, pinyin name, or latitude:longitude pair.
    pub location: String,
}
/// Current weather reported by Seniverse.
#[derive(Clone, Default, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct GetCurrentWeatherOutput {
    /// LocationId is Seniverse's city ID.
    pub location_id: String,
    /// LocationName is Seniverse's localized city name.
    pub location_name: String,
    /// Country is the two-letter country code.
    pub country: String,
    /// WeatherText is the localized current weather condition.
    pub weather_text: String,
    /// WeatherCode is Seniverse's weather condition code.
    pub weather_code: String,
    /// Temperature is the provider's numeric temperature string.
    pub temperature: String,
    /// TemperatureUnit is c for Celsius or f for Fahrenheit.
    pub temperature_unit: String,
    /// LastUpdate is the provider's RFC3339 local observation time.
    pub last_update: String,
}
/// Request settings for the weather service.
pub struct Config {
    /// Approved Seniverse origin or private test server.
    pub base_url: String,
    /// Supported locale for place names and weather descriptions.
    pub language: String,
    /// Temperature unit, c for Celsius or f for Fahrenheit.
    pub unit: String,
    /// Timeout for one provider attempt.
    pub timeout: std::time::Duration,
    /// Maximum response body size in bytes.
    pub max_response_bytes: usize,
}

impl Default for Config {
    fn default() -> Self {
        Self {
            base_url: "https://api.seniverse.com".into(),
            language: "zh-Hans".into(),
            unit: "c".into(),
            timeout: std::time::Duration::from_secs(10),
            max_response_bytes: 262144,
        }
    }
}
impl Config {
    fn endpoint(&self) -> Result<String, ValidationError> {
        if !matches!(
            self.language.as_str(),
            "zh-Hans"
                | "zh-Hant"
                | "en"
                | "ja"
                | "de"
                | "fr"
                | "es"
                | "pt"
                | "hi"
                | "id"
                | "ru"
                | "th"
                | "ar"
        ) || !matches!(self.unit.as_str(), "c" | "f")
        {
            return Err(ValidationError::new(
                "Seniverse language or unit is invalid",
            ));
        }
        Ok(format!(
            "{}/v3/weather/now.json",
            sdk::providerhttp::validate_base_url(&self.base_url)?
        ))
    }
}
/// Credential resolved from the logical connection for each request.
#[derive(Clone)]
pub struct Credentials {
    /// Provider-issued API key, never stored in the result.
    pub private_key: SecretString,
}

impl From<SecretString> for Credentials {
    fn from(credential: SecretString) -> Self {
        Self {
            private_key: credential,
        }
    }
}

impl Credentials {
    fn is_valid(&self) -> bool {
        let value = self.private_key.reveal();
        !value.is_empty() && value.len() <= 512 && value.bytes().all(|byte| byte.is_ascii_graphic())
    }
}

impl GetCurrentWeatherInput {
    fn valid_location(&self) -> bool {
        !self.location.is_empty()
            && self.location.len() <= 256
            && self.location.trim() == self.location
    }
}
#[derive(Deserialize)]
struct WeatherEnvelope {
    results: Vec<Observation>,
}
#[derive(Deserialize)]
struct Observation {
    location: Location,
    now: Conditions,
    last_update: String,
}
#[derive(Deserialize)]
struct Location {
    id: String,
    name: String,
    country: Option<String>,
}
#[derive(Deserialize)]
struct Conditions {
    text: String,
    code: String,
    temperature: String,
}

fn decode_weather(body: &[u8], unit: &str, key: &str) -> QueryAttempt<GetCurrentWeatherOutput> {
    let invalid = || {
        rejected(
            "invalidResponse",
            FailureKind::Protocol,
            "Seniverse returned an invalid observation",
        )
    };
    let Ok(envelope) = weather::decode_fields::<WeatherEnvelope>(body) else {
        return invalid();
    };
    let Ok([result]) = <[Observation; 1]>::try_from(envelope.results) else {
        return invalid();
    };
    if result.location.id.is_empty()
        || result.location.name.is_empty()
        || result.now.text.is_empty()
        || result.now.code.is_empty()
        || !valid_temperature(&result.now.temperature)
        || !valid_time(&result.last_update)
    {
        return invalid();
    }
    let country = result.location.country.unwrap_or_default();
    if [
        &result.location.id,
        &result.location.name,
        &country,
        &result.now.text,
        &result.now.code,
        &result.now.temperature,
        &result.last_update,
    ]
    .iter()
    .any(|field| field.contains(key))
    {
        return invalid();
    }
    QueryAttempt::branch(
        "fetched",
        GetCurrentWeatherOutput {
            location_id: result.location.id,
            location_name: result.location.name,
            country,
            weather_text: result.now.text,
            weather_code: result.now.code,
            temperature: result.now.temperature,
            temperature_unit: unit.to_owned(),
            last_update: result.last_update,
        },
        Receipt::default(),
        None,
    )
}
fn classify_rejection(
    body: &[u8],
    status: u16,
    delay: f64,
) -> QueryAttempt<GetCurrentWeatherOutput> {
    #[derive(Deserialize)]
    struct ProviderError {
        status_code: String,
    }
    let code = weather::decode_fields::<ProviderError>(body)
        .map_or(String::new(), |error| error.status_code);
    let kind = match code.as_str() {
        "AP010014" => {
            return weather::retry(
                FailureKind::RateLimit,
                "Seniverse rate limited the request",
                delay,
            );
        }
        "AP010003" | "AP010004" => FailureKind::Authentication,
        "AP010002" | "AP010006" | "AP010008" | "AP010009" => FailureKind::Authorization,
        "AP010012" | "AP010013" => FailureKind::QuotaExhausted,
        "AP010001" | "AP010010" | "AP010011" | "AP010017" => FailureKind::Validation,
        _ => FailureKind::ProviderRejection,
    };
    rejected(
        "providerRejected",
        kind,
        &format!("Seniverse rejected the request with HTTP {status}"),
    )
}
fn valid_time(text: &str) -> bool {
    static FORMAT: std::sync::LazyLock<regex::Regex> = std::sync::LazyLock::new(|| {
        regex::Regex::new(r"\A([0-9]{4})-([0-9]{2})-([0-9]{2})T([0-9]{1,2}):([0-9]{2}):([0-9]{2})(?:[.,][0-9]+)?(?:Z|[+-]([0-9]{2}):([0-9]{2}))\z").expect("weather timestamp pattern")
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

fn valid_temperature(text: &str) -> bool {
    static FORMAT: std::sync::LazyLock<regex::Regex> = std::sync::LazyLock::new(|| {
        regex::Regex::new(r"\A[+-]?(?:(?:[0-9](?:_?[0-9])*(?:\.(?:[0-9](?:_?[0-9])*)?)?|\.[0-9](?:_?[0-9])*)(?:[Ee][+-]?[0-9](?:_?[0-9])*)?|0[xX](?:_?[0-9A-Fa-f](?:_?[0-9A-Fa-f])*(?:\.(?:[0-9A-Fa-f](?:_?[0-9A-Fa-f])*)?)?|\.[0-9A-Fa-f](?:_?[0-9A-Fa-f])*)[pP][+-]?[0-9](?:_?[0-9])*)\z").expect("temperature format")
    });
    if !FORMAT.is_match(text) {
        return false;
    }
    let literal = text.trim_start_matches(['-', '+']).replace('_', "");
    if let Some(hex) = literal
        .strip_prefix("0x")
        .or_else(|| literal.strip_prefix("0X"))
    {
        return finite_hex_temperature(hex);
    }
    literal.parse::<f64>().is_ok_and(f64::is_finite)
}
fn finite_hex_temperature(literal: &str) -> bool {
    let (mantissa, power) = literal
        .split_once(['p', 'P'])
        .expect("validated hex temperature");
    let exponent = power
        .trim_start_matches(['+', '-'])
        .bytes()
        .fold(0_i64, |n, b| (n * 10 + i64::from(b - b'0')).min(1i64 << 50));
    let exponent = if power.starts_with('-') {
        -exponent
    } else {
        exponent
    };
    let integer_length = mantissa.find('.').unwrap_or(mantissa.len());
    let digits = mantissa.replace('.', "");
    let significant = digits.trim_start_matches('0');
    if significant.is_empty() {
        return true;
    }
    let skipped = digits.len() - significant.len();
    let prefix = &significant[..16.min(significant.len())];
    let head = u64::from_str_radix(prefix, 16).expect("validated hex digits");
    let bits = 64 - head.leading_zeros();
    let top_exponent = exponent
        + 4 * (integer_length as i64 - skipped as i64 - prefix.len() as i64)
        + i64::from(bits)
        - 1;
    // A 54-bit leading run of ones at exponent 1023 rounds to infinity.
    top_exponent < 1023
        || (top_exponent == 1023 && (bits < 54 || head >> (bits - 54) != (1 << 54) - 1))
}
