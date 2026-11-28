// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use crate::{
    CONNECTOR_ID, Config, Credentials, GetCurrentWeatherInput, PROVIDER_LABEL, WeatherOutput,
    generated_definitions,
};
use std::sync::Arc;
use std::time::SystemTime;

use dex_connectors_sdk as sdk;
use sdk::providerhttp::{
    AddressPolicy, ProviderHttpClient, ProviderHttpError, parse_retry_after, read_bounded_body,
};
use sdk::{
    Call, CredentialProvider, Failure, FailureKind, Query, QueryAttempt, QueryDefinition,
    QueryStep, QueryStepConfig, Receipt, ValidationError,
};
/// Bound current weather query. Construct once and share the HTTP client across calls.
pub struct GetCurrentWeatherOperation {
    credentials: Arc<dyn CredentialProvider<Credentials>>,
    http: ProviderHttpClient,
    max_response_bytes: usize,
    clock: Arc<dyn Fn() -> SystemTime + Send + Sync>,
    base_url: String,
    nonce: Arc<dyn Fn() -> std::io::Result<[u8; 16]> + Send + Sync>,
}

impl GetCurrentWeatherOperation {
    /// Construct a bounded, redirect-safe transport without contacting the provider.
    pub fn new(
        config: Config,
        credentials: Arc<dyn CredentialProvider<Credentials>>,
    ) -> Result<Self, ValidationError> {
        let base_url = sdk::providerhttp::validate_base_url(&config.base_url)?;
        let policy = if base_url.starts_with("https://") {
            AddressPolicy::PublicHttps
        } else {
            AddressPolicy::PrivateNetworkHttp
        };
        let http = ProviderHttpClient::new(config.timeout, policy)?;
        Self::with_signing_sources(
            credentials,
            http,
            &base_url,
            config.max_response_bytes,
            Arc::new(SystemTime::now),
            Arc::new(random_nonce),
        )
    }
    /// Use a caller-supplied transport with the public Caiyun origin and fresh signing nonces.
    pub fn with_http_client(
        credentials: Arc<dyn CredentialProvider<Credentials>>,
        http: ProviderHttpClient,
        max_response_bytes: usize,
    ) -> Result<Self, ValidationError> {
        Self::with_signing_sources(
            credentials,
            http,
            &Config::default().base_url,
            max_response_bytes,
            Arc::new(SystemTime::now),
            Arc::new(random_nonce),
        )
    }
    /// Bind a trusted transport, base URL, byte limit, signing clock and fresh 16-byte nonce source.
    pub fn with_signing_sources(
        credentials: Arc<dyn CredentialProvider<Credentials>>,
        http: ProviderHttpClient,
        base_url: &str,
        max_response_bytes: usize,
        clock: Arc<dyn Fn() -> SystemTime + Send + Sync>,
        nonce: Arc<dyn Fn() -> std::io::Result<[u8; 16]> + Send + Sync>,
    ) -> Result<Self, ValidationError> {
        if max_response_bytes == 0 {
            return Err(ValidationError::new(
                "Caiyun response size limit must be positive",
            ));
        }
        Ok(Self {
            credentials,
            http,
            base_url: sdk::providerhttp::validate_base_url(base_url)?,
            max_response_bytes,
            clock,
            nonce,
        })
    }
}
fn random_nonce() -> std::io::Result<[u8; 16]> {
    use ring::rand::SecureRandom;
    let mut nonce = [0u8; 16];
    ring::rand::SystemRandom::new()
        .fill(&mut nonce)
        .map_err(|_| std::io::Error::other("Caiyun nonce generation failed"))?;
    Ok(nonce)
}
fn unix_seconds(time: SystemTime) -> i128 {
    match time.duration_since(SystemTime::UNIX_EPOCH) {
        Ok(elapsed) => i128::from(elapsed.as_secs()),
        Err(before) => {
            -i128::from(before.duration().as_secs())
                - i128::from(before.duration().subsec_nanos() != 0)
        }
    }
}

impl Query for GetCurrentWeatherOperation {
    type Input = GetCurrentWeatherInput;
    type Output = WeatherOutput;

    fn definition(&self) -> QueryDefinition {
        generated_definitions::GetCurrentWeather::definition()
    }

    fn invoke(&self, call: &Call<'_>, input: &Self::Input) -> QueryAttempt<Self::Output> {
        if !input.valid_coordinates() {
            return rejected(
                "defect",
                FailureKind::Validation,
                "Weather query location is invalid",
            );
        }
        let credentials = match self.credentials.resolve(call) {
            Ok(credentials) if credentials.is_valid() => credentials,
            _ => {
                return rejected(
                    "defect",
                    FailureKind::Authentication,
                    &format!("{PROVIDER_LABEL} credentials are unavailable or invalid"),
                );
            }
        };
        let Ok(nonce) = (self.nonce)() else {
            return retry(
                FailureKind::Availability,
                "Caiyun request nonce could not be generated",
                0.0,
            );
        };
        let request = input.signed_request(
            &self.base_url,
            &credentials,
            &nonce,
            unix_seconds((self.clock)()),
        );
        let Ok(mut response) = self.http.send_traced(&request) else {
            return retry(
                FailureKind::Availability,
                &format!("{PROVIDER_LABEL} weather request failed"),
                0.0,
            );
        };
        let status = response.status();
        let delay = parse_retry_after(
            response.headers().get("Retry-After").unwrap_or(""),
            (self.clock)(),
        );
        if status == 408 || status >= 500 {
            return retry(
                FailureKind::Availability,
                "Caiyun is temporarily unavailable",
                delay,
            );
        }
        if status == 429 {
            return retry(
                FailureKind::RateLimit,
                "Caiyun rate or quota limit reached",
                delay,
            );
        }
        if status != 200 {
            let kind = match status {
                400 | 401 => FailureKind::Authentication,
                403 => FailureKind::Authorization,
                422 => FailureKind::Validation,
                _ => FailureKind::ProviderRejection,
            };
            return rejected(
                "providerRejected",
                kind,
                "Caiyun rejected the weather request",
            );
        }
        match read_bounded_body(&mut response, self.max_response_bytes) {
            Ok(body) => crate::decode_weather(&body, &credentials),
            Err(ProviderHttpError::BodyTooLarge(_)) => rejected(
                "invalidResponse",
                FailureKind::ResponseTooLarge,
                &format!("{PROVIDER_LABEL} response exceeded the configured limit"),
            ),
            Err(_) => retry(
                FailureKind::Availability,
                &format!("{PROVIDER_LABEL} response was interrupted"),
                0.0,
            ),
        }
    }
}

/// Bind the weather operation to its branch targets and validate the Step configuration.
pub fn new_get_current_weather_step<Input>(
    config: QueryStepConfig<Input, GetCurrentWeatherOperation>,
) -> Result<QueryStep<Input, GetCurrentWeatherOperation>, ValidationError> {
    if config.connection.provider != CONNECTOR_ID {
        return Err(ValidationError::new(format!(
            "{PROVIDER_LABEL} connection provider is invalid"
        )));
    }
    QueryStep::new(config)
}

pub(super) fn rejected(
    branch: &str,
    kind: FailureKind,
    message: &str,
) -> QueryAttempt<WeatherOutput> {
    QueryAttempt::branch(
        branch,
        WeatherOutput::default(),
        Receipt::default(),
        Some(Failure::new(
            kind,
            CONNECTOR_ID,
            "getCurrentWeather",
            message,
        )),
    )
}

pub(super) fn retry(kind: FailureKind, message: &str, delay: f64) -> QueryAttempt<WeatherOutput> {
    QueryAttempt::retry(
        Failure::new(kind, CONNECTOR_ID, "getCurrentWeather", message),
        delay,
    )
}

pub(super) fn decode_fields<T: serde::de::DeserializeOwned>(
    body: &[u8],
) -> Result<T, serde_json::Error> {
    let mut response: serde_json::Value = serde_json::from_str(&String::from_utf8_lossy(body))?;
    lowercase_fields(&mut response);
    serde_json::from_value(response)
}

fn lowercase_fields(value: &mut serde_json::Value) {
    match value {
        serde_json::Value::Array(values) => values.iter_mut().for_each(lowercase_fields),
        serde_json::Value::Object(fields) => {
            *fields = std::mem::take(fields)
                .into_iter()
                .map(|(name, mut value)| {
                    lowercase_fields(&mut value);
                    (name.to_ascii_lowercase(), value)
                })
                .collect();
        }
        _ => (),
    }
}
