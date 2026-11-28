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
    endpoint: String,
    language: String,
    unit: String,
    http: ProviderHttpClient,
    max_response_bytes: usize,
    clock: Arc<dyn Fn() -> SystemTime + Send + Sync>,
}

impl GetCurrentWeatherOperation {
    /// Construct a bounded, redirect-safe transport without contacting the provider.
    pub fn new(
        config: Config,
        credentials: Arc<dyn CredentialProvider<Credentials>>,
    ) -> Result<Self, ValidationError> {
        let endpoint = config.endpoint()?;
        let policy = if endpoint.starts_with("https://") {
            AddressPolicy::PublicHttps
        } else {
            AddressPolicy::PrivateNetworkHttp
        };
        let http = ProviderHttpClient::new(config.timeout, policy)?;
        Self::with_config_and_clock(
            config,
            credentials,
            http,
            endpoint,
            Arc::new(SystemTime::now),
        )
    }

    /// Supply the transport used for each request and its response size limit in bytes.
    pub fn with_http_client(
        credentials: Arc<dyn CredentialProvider<Credentials>>,
        http: ProviderHttpClient,
        max_response_bytes: usize,
    ) -> Result<Self, ValidationError> {
        Self::with_http_client_and_clock(
            credentials,
            http,
            max_response_bytes,
            Arc::new(SystemTime::now),
        )
    }

    /// Use an injected clock for HTTP retry dates.
    pub fn with_http_client_and_clock(
        credentials: Arc<dyn CredentialProvider<Credentials>>,
        http: ProviderHttpClient,
        max_response_bytes: usize,
        clock: Arc<dyn Fn() -> SystemTime + Send + Sync>,
    ) -> Result<Self, ValidationError> {
        let config = Config {
            max_response_bytes,
            ..Config::default()
        };
        let endpoint = config.endpoint()?;
        Self::with_config_and_clock(config, credentials, http, endpoint, clock)
    }
    fn with_config_and_clock(
        config: Config,
        credentials: Arc<dyn CredentialProvider<Credentials>>,
        http: ProviderHttpClient,
        endpoint: String,
        clock: Arc<dyn Fn() -> SystemTime + Send + Sync>,
    ) -> Result<Self, ValidationError> {
        if config.max_response_bytes == 0 {
            return Err(ValidationError::new(
                "Seniverse response size limit must be positive",
            ));
        }
        Ok(Self {
            endpoint,
            credentials,
            http,
            max_response_bytes: config.max_response_bytes,
            language: config.language,
            unit: config.unit,
            clock,
        })
    }
}

impl Query for GetCurrentWeatherOperation {
    type Input = GetCurrentWeatherInput;
    type Output = WeatherOutput;

    fn definition(&self) -> QueryDefinition {
        generated_definitions::GetCurrentWeather::definition()
    }

    fn invoke(&self, call: &Call<'_>, input: &Self::Input) -> QueryAttempt<Self::Output> {
        if !input.valid_location() {
            return rejected(
                "defect",
                FailureKind::Validation,
                "Seniverse location is invalid",
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
        let parameters = std::collections::BTreeMap::from([
            ("location".into(), vec![input.location.clone()]),
            (
                "key".into(),
                vec![credentials.private_key.reveal().to_owned()],
            ),
            ("language".into(), vec![self.language.clone()]),
            ("unit".into(), vec![self.unit.clone()]),
        ]);
        let query = sdk::providerhttp::encode_query_values(&parameters);
        let request = sdk::providerhttp::ProviderHttpRequest::new(
            "GET",
            format!("{}?{query}", self.endpoint),
        )
        .header("Accept", "application/json");
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
        if status == 429 {
            return retry(
                FailureKind::RateLimit,
                "Seniverse rate limited the request",
                delay,
            );
        }
        if status == 408 || status >= 500 {
            return retry(
                FailureKind::Availability,
                "Seniverse is temporarily unavailable",
                delay,
            );
        }
        match read_bounded_body(&mut response, self.max_response_bytes) {
            Ok(body) if status != 200 => crate::classify_rejection(&body, status, delay),
            Ok(body) => crate::decode_weather(&body, &self.unit, credentials.private_key.reveal()),
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
    let mut response: serde_json::Value = serde_json::from_slice(body)?;
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
