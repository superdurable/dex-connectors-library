// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use crate::{
    CONNECTOR_ID, Credentials, LookupIPLocationOutput, PROVIDER_LABEL, decode_location,
    generated_definitions, lookup_request,
};
use std::sync::Arc;
use std::time::{Duration, SystemTime};

use dex_connectors_sdk as sdk;
use sdk::providerhttp::{
    AddressPolicy, ProviderHttpClient, ProviderHttpError, parse_retry_after, read_bounded_body,
};
use sdk::{
    Call, CredentialProvider, Failure, FailureKind, Query, QueryAttempt, QueryDefinition,
    QueryStep, QueryStepConfig, Receipt, ValidationError,
};
use serde::{Deserialize, Serialize};

/// IPv4 address whose location should be looked up.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct LookupIPLocationInput {
    /// A public IPv4 address in textual form.
    #[serde(rename = "ipAddress")]
    pub ip_address: String,
}

/// Settings for a single provider request.
pub struct Config {
    /// Timeout per attempt. Uses the manifest default.
    pub timeout: Duration,
    /// Maximum response size in bytes. Uses the manifest default, up to sixteen MiB.
    pub max_response_bytes: usize,
}

impl Default for Config {
    fn default() -> Self {
        Self {
            timeout: Duration::from_nanos(10000000000),
            max_response_bytes: 65536,
        }
    }
}

/// Bound IP location query. Construct once and share the HTTP client across calls.
pub struct LookupIPLocationOperation {
    credentials: Arc<dyn CredentialProvider<Credentials>>,
    http: ProviderHttpClient,
    max_response_bytes: usize,
}

impl LookupIPLocationOperation {
    /// Construct a bounded, redirect-safe transport without contacting the provider.
    pub fn new(
        config: Config,
        credentials: Arc<dyn CredentialProvider<Credentials>>,
    ) -> Result<Self, ValidationError> {
        let http = ProviderHttpClient::new(config.timeout, AddressPolicy::PublicHttps)?;
        Self::with_http_client(credentials, http, config.max_response_bytes)
    }

    /// Supply the transport used for each request and its response size limit in bytes.
    pub fn with_http_client(
        credentials: Arc<dyn CredentialProvider<Credentials>>,
        http: ProviderHttpClient,
        max_response_bytes: usize,
    ) -> Result<Self, ValidationError> {
        if !(1..=(16 << 20)).contains(&max_response_bytes) {
            return Err(ValidationError::new(format!(
                "{PROVIDER_LABEL} response size limit must be between one byte and sixteen MiB"
            )));
        }
        Ok(Self {
            credentials,
            http,
            max_response_bytes,
        })
    }
}

impl Query for LookupIPLocationOperation {
    type Input = LookupIPLocationInput;
    type Output = LookupIPLocationOutput;

    fn definition(&self) -> QueryDefinition {
        generated_definitions::LookupIPLocation::definition()
    }

    fn invoke(&self, call: &Call<'_>, input: &Self::Input) -> QueryAttempt<Self::Output> {
        let Some(ip) = public_ipv4(&input.ip_address) else {
            return rejected(
                "defect",
                FailureKind::Validation,
                &format!("{PROVIDER_LABEL} requires one public IPv4 address"),
            );
        };
        let credentials = match self.credentials.resolve(call) {
            Ok(credentials) if valid_key(credentials.value()) => credentials,
            _ => {
                return rejected(
                    "defect",
                    FailureKind::Authentication,
                    &format!("{PROVIDER_LABEL} credentials are unavailable or invalid"),
                );
            }
        };
        let request = lookup_request(ip, credentials.value());
        let Ok(mut response) = self.http.send_traced(&request) else {
            return retry(
                FailureKind::Availability,
                &format!("{PROVIDER_LABEL} lookup request failed"),
                0.0,
            );
        };
        let status = response.status();
        let delay = parse_retry_after(
            response.headers().get("Retry-After").unwrap_or(""),
            SystemTime::now(),
        );
        if status != 200 {
            return match status {
                429 => retry(
                    FailureKind::RateLimit,
                    &format!("{PROVIDER_LABEL} rate limited the lookup"),
                    delay,
                ),
                408 | 500.. => retry(
                    FailureKind::Availability,
                    &format!("{PROVIDER_LABEL} lookup service is temporarily unavailable"),
                    delay,
                ),
                _ => rejected(
                    "providerRejected",
                    match status {
                        401 => FailureKind::Authentication,
                        403 => FailureKind::Authorization,
                        _ => FailureKind::ProviderRejection,
                    },
                    &format!("{PROVIDER_LABEL} rejected lookup with HTTP {status}"),
                ),
            };
        }
        match read_bounded_body(&mut response, self.max_response_bytes) {
            Ok(body) => decode_location(&body, delay),
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

/// Bind the lookup operation to its branch targets and validate the Step configuration.
pub fn new_lookup_ip_location_step<Input>(
    config: QueryStepConfig<Input, LookupIPLocationOperation>,
) -> Result<QueryStep<Input, LookupIPLocationOperation>, ValidationError> {
    if config.connection.provider != CONNECTOR_ID {
        return Err(ValidationError::new(format!(
            "{PROVIDER_LABEL} connection provider is invalid"
        )));
    }
    QueryStep::new(config)
}

fn valid_key(key: &str) -> bool {
    !key.is_empty() && key.len() <= 1024 && key.bytes().all(|byte| byte.is_ascii_graphic())
}

fn public_ipv4(value: &str) -> Option<std::net::Ipv4Addr> {
    use std::net::IpAddr;
    let address = match value.parse::<IpAddr>().ok()? {
        IpAddr::V4(address) => address,
        IpAddr::V6(address) => address.to_ipv4_mapped()?,
    };
    if address.is_private()
        || address.is_loopback()
        || address.is_link_local()
        || address.is_unspecified()
        || address.is_multicast()
    {
        return None;
    }
    Some(address)
}

pub(super) fn rejected(
    branch: &str,
    kind: FailureKind,
    message: &str,
) -> QueryAttempt<LookupIPLocationOutput> {
    QueryAttempt::branch(
        branch,
        LookupIPLocationOutput::default(),
        Receipt::default(),
        Some(Failure::new(
            kind,
            CONNECTOR_ID,
            "lookupIPLocation",
            message,
        )),
    )
}

pub(super) fn retry(
    kind: FailureKind,
    message: &str,
    delay: f64,
) -> QueryAttempt<LookupIPLocationOutput> {
    QueryAttempt::retry(
        Failure::new(kind, CONNECTOR_ID, "lookupIPLocation", message),
        delay,
    )
}

pub(super) fn lowercase_fields(value: &mut serde_json::Value) {
    if let Some(fields) = value.as_object_mut() {
        *fields = std::mem::take(fields)
            .into_iter()
            .map(|(key, value)| (key.to_ascii_lowercase(), value))
            .collect();
    }
}
