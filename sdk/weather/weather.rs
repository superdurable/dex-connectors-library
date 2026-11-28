// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use crate::{
    CONNECTOR_ID, Config, Credentials, GetCurrentWeatherInput, GetCurrentWeatherOutput,
    PROVIDER_LABEL, REJECT_HTTP_BEFORE_BODY, generated_definitions,
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
        Self::with_endpoint(
            credentials,
            http,
            config.max_response_bytes,
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
        Self::with_endpoint(
            credentials,
            http,
            max_response_bytes,
            Config::default().endpoint()?,
            clock,
        )
    }

    fn with_endpoint(
        credentials: Arc<dyn CredentialProvider<Credentials>>,
        http: ProviderHttpClient,
        max_response_bytes: usize,
        endpoint: String,
        clock: Arc<dyn Fn() -> SystemTime + Send + Sync>,
    ) -> Result<Self, ValidationError> {
        if !Config::valid_limit(max_response_bytes) {
            return Err(ValidationError::new(format!(
                "{PROVIDER_LABEL} response size limit is invalid"
            )));
        }
        Ok(Self {
            endpoint,
            credentials,
            http,
            max_response_bytes,
            clock,
        })
    }
}

impl Query for GetCurrentWeatherOperation {
    type Input = GetCurrentWeatherInput;
    type Output = GetCurrentWeatherOutput;

    fn definition(&self) -> QueryDefinition {
        generated_definitions::GetCurrentWeather::definition()
    }

    fn invoke(&self, call: &Call<'_>, input: &Self::Input) -> QueryAttempt<Self::Output> {
        if !input.valid_city() {
            return rejected(
                "defect",
                FailureKind::Validation,
                "Weather query city is invalid",
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
        let request = input.request(&self.endpoint, &credentials);
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
        if status == 429
            || status == 408
            || status >= 500
            || (REJECT_HTTP_BEFORE_BODY && status != 200)
        {
            return match status {
                429 => retry(
                    FailureKind::RateLimit,
                    &format!("{PROVIDER_LABEL} rate limited the weather request"),
                    delay,
                ),
                408 | 500.. => retry(
                    FailureKind::Availability,
                    &format!("{PROVIDER_LABEL} weather service is temporarily unavailable"),
                    delay,
                ),
                _ => rejected(
                    "providerRejected",
                    match status {
                        401 => FailureKind::Authentication,
                        403 => FailureKind::Authorization,
                        _ => FailureKind::ProviderRejection,
                    },
                    &format!("{PROVIDER_LABEL} rejected weather request with HTTP {status}"),
                ),
            };
        }
        match read_bounded_body(&mut response, self.max_response_bytes) {
            Ok(_) if status != 200 => rejected(
                "providerRejected",
                FailureKind::ProviderRejection,
                &format!("{PROVIDER_LABEL} rejected weather request with HTTP {status}"),
            ),
            Ok(body) => crate::decode_weather(&body, input),
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
) -> QueryAttempt<GetCurrentWeatherOutput> {
    QueryAttempt::branch(
        branch,
        GetCurrentWeatherOutput::default(),
        Receipt::default(),
        Some(Failure::new(
            kind,
            CONNECTOR_ID,
            "getCurrentWeather",
            message,
        )),
    )
}

pub(super) fn retry(
    kind: FailureKind,
    message: &str,
    delay: f64,
) -> QueryAttempt<GetCurrentWeatherOutput> {
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
