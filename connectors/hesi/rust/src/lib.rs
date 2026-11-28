// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Read the Hesi department catalog from a Dex Query Step.

#[rustfmt::skip]
mod generated_definitions;
mod provider_fields;
use provider_fields::BalanceFields as WireFields;

use std::sync::Arc;
use std::time::{Duration, SystemTime};

use dex_connectors_sdk as sdk;
use sdk::providerhttp::{
    AddressPolicy, ProviderHttpClient, ProviderHttpError, ProviderHttpRequest, parse_retry_after,
    read_bounded_body,
};
use sdk::{
    Call, CredentialProvider, Failure, FailureKind, Query, QueryAttempt, QueryDefinition,
    QueryStep, QueryStepConfig, Receipt, SecretString, ValidationError,
};
use serde::{Deserialize, Serialize};

const BASE_URL: &str = r"https://app.ekuaibao.com";

/// Window of the authorized department directory.
#[derive(Clone, Default, Deserialize, Serialize)]
#[serde(default)]
pub struct ListDepartmentsInput {
    /// Zero-based department offset, from 0 through 1,000,000.
    pub start: i64,
    /// Page size from 1 through 100. Zero selects 20.
    pub count: i64,
}
/// One page of the authorized department directory.
#[derive(Clone, Default, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ListDepartmentsOutput {
    /// Departments in provider order.
    pub departments: Vec<DepartmentSummary>,
    /// Total departments reported by Hesi.
    pub total: i64,
    /// Next offset, or zero on the last page.
    pub next_start: i64,
}
/// Public metadata for one department.
#[derive(Clone, Deserialize, Serialize)]
pub struct DepartmentSummary {
    /// Department identifier.
    pub id: String,
    /// Department display name.
    pub name: String,
    /// Provider department code, or empty when omitted.
    pub code: String,
    /// Whether the department is active.
    pub active: bool,
}
/// Settings for a single provider request.
pub struct Config {
    /// Base URL of the provider API. Defaults to the public provider endpoint.
    pub base_url: String,
    /// Timeout per attempt. Uses the manifest default.
    pub timeout: Duration,
    /// Maximum response size in bytes. Uses the manifest default.
    pub max_response_bytes: usize,
}

impl Default for Config {
    fn default() -> Self {
        Self {
            base_url: BASE_URL.to_owned(),
            timeout: Duration::from_secs(10),
            max_response_bytes: 2097152,
        }
    }
}

/// Credentials resolved from the logical connection for each request.
#[derive(Clone)]
pub struct Credentials {
    /// Provider-issued application key.
    pub app_key: SecretString,
    /// Secret paired with the application key.
    pub app_security: SecretString,
}
/// Bound department query. Construct once and share the HTTP client across calls.
pub struct ListDepartmentsOperation {
    credentials: Arc<dyn CredentialProvider<Credentials>>,
    http: ProviderHttpClient,
    endpoint: String,
    max_response_bytes: usize,
}

impl ListDepartmentsOperation {
    /// Construct a bounded, redirect-safe transport without contacting the provider.
    pub fn new(
        config: Config,
        credentials: Arc<dyn CredentialProvider<Credentials>>,
    ) -> Result<Self, ValidationError> {
        let policy = if config
            .base_url
            .get(..7)
            .is_some_and(|scheme| scheme.eq_ignore_ascii_case("http://"))
        {
            AddressPolicy::PrivateNetworkHttp
        } else {
            AddressPolicy::PublicHttps
        };
        let http = ProviderHttpClient::new(config.timeout, policy)?;
        Self::with_base_url(
            credentials,
            http,
            config.max_response_bytes,
            &config.base_url,
        )
    }

    /// Supply the transport used for each request and its response size limit in bytes.
    pub fn with_http_client(
        credentials: Arc<dyn CredentialProvider<Credentials>>,
        http: ProviderHttpClient,
        max_response_bytes: usize,
    ) -> Result<Self, ValidationError> {
        Self::with_base_url(credentials, http, max_response_bytes, BASE_URL)
    }

    /// Bind a configured provider base URL, explicit transport and positive response limit in bytes.
    pub fn with_base_url(
        credentials: Arc<dyn CredentialProvider<Credentials>>,
        http: ProviderHttpClient,
        max_response_bytes: usize,
        base_url: &str,
    ) -> Result<Self, ValidationError> {
        let base = if base_url.is_empty() {
            BASE_URL
        } else {
            base_url
        };
        let origin = sdk::providerhttp::validate_base_url(base)?;
        if !matches!(
            origin.as_str(),
            "https://app.ekuaibao.com"
                | "https://dd2.hosecloud.com"
                | "https://wx2.ekuaibao.com"
                | "https://kdcloud2.ekuaibao.com"
                | "https://ddenterprise.ekuaibao.com"
                | "https://feishu.ekuaibao.com"
        ) {
            return Err(ValidationError::new(
                "Hesi requires a documented platform origin",
            ));
        }
        let endpoint = origin;
        if max_response_bytes == 0 {
            return Err(ValidationError::new(
                "Hesi response size limit must be positive",
            ));
        }
        Ok(Self {
            credentials,
            endpoint,
            http,
            max_response_bytes,
        })
    }
}

impl Query for ListDepartmentsOperation {
    type Input = ListDepartmentsInput;
    type Output = ListDepartmentsOutput;

    fn definition(&self) -> QueryDefinition {
        generated_definitions::ListDepartments::definition()
    }

    fn invoke(&self, call: &Call<'_>, input: &Self::Input) -> QueryAttempt<Self::Output> {
        let keys = match self.credentials.resolve(call) {
            Ok(keys)
                if valid_key(keys.app_key.reveal()) && valid_key(keys.app_security.reveal()) =>
            {
                keys
            }
            _ => {
                return rejected(
                    "defect",
                    FailureKind::Authentication,
                    "Hesi credentials are unavailable or invalid",
                );
            }
        };
        let count = if input.count == 0 { 20 } else { input.count };
        if !(0..=1000000).contains(&input.start) || !(1..=100).contains(&count) {
            return rejected(
                "defect",
                FailureKind::Validation,
                "Invalid Hesi department window",
            );
        }
        let body=serde_json::json!({"appKey":keys.app_key.reveal(),"appSecurity":keys.app_security.reveal()}).to_string();
        let request = ProviderHttpRequest::new(
            "POST",
            format!("{}/api/openapi/v1/auth/getAccessToken", self.endpoint),
        )
        .header("Accept", "application/json")
        .header("Content-Type", "application/json")
        .body(body);
        let auth = match self.send(&request) {
            Ok(bytes) => bytes,
            Err(attempt) => return *attempt,
        };
        let Some(token) = read_token(&auth) else {
            return invalid();
        };
        let query = form_urlencoded::Serializer::new(String::new())
            .append_pair("accessToken", &token)
            .append_pair("count", &count.to_string())
            .append_pair("start", &input.start.to_string())
            .finish();
        let request = ProviderHttpRequest::new(
            "GET",
            format!("{}/api/openapi/v1/departments?{}", self.endpoint, query),
        )
        .header("Accept", "application/json");
        let body = match self.send(&request) {
            Ok(bytes) => bytes,
            Err(attempt) => return *attempt,
        };
        match read_departments(&body, input.start, count) {
            Some(page) => QueryAttempt::branch("listed", page, Receipt::default(), None),
            None => invalid(),
        }
    }
}
impl ListDepartmentsOperation {
    fn send(
        &self,
        request: &ProviderHttpRequest,
    ) -> Result<Vec<u8>, Box<QueryAttempt<ListDepartmentsOutput>>> {
        let mut response = self
            .http
            .send_traced(request)
            .map_err(|_| Box::new(retry(FailureKind::Availability, "Hesi request failed", 0.0)))?;
        let status = response.status();
        if matches!(status, 408 | 429 | 500..) {
            let delay = parse_retry_after(
                response.headers().get("Retry-After").unwrap_or(""),
                SystemTime::now(),
            );
            let kind = if status == 429 {
                FailureKind::RateLimit
            } else {
                FailureKind::Availability
            };
            return Err(Box::new(retry(kind, "Hesi temporarily unavailable", delay)));
        }
        let body = read_bounded_body(&mut response, self.max_response_bytes).map_err(|error| {
            Box::new(match error {
                ProviderHttpError::BodyTooLarge(_) => rejected(
                    "invalidResponse",
                    FailureKind::ResponseTooLarge,
                    "Hesi response exceeded the configured limit",
                ),
                _ => retry(
                    FailureKind::Availability,
                    "Hesi response was interrupted",
                    0.0,
                ),
            })
        })?;
        if status != 200 {
            return Err(Box::new(rejected(
                "providerRejected",
                if matches!(status, 401 | 403) {
                    FailureKind::Authentication
                } else {
                    FailureKind::ProviderRejection
                },
                "Hesi rejected the request",
            )));
        }
        Ok(body)
    }
}
/// Bind the department operation to its branch targets and validate the Step configuration.
pub fn new_list_departments_step<Input>(
    config: QueryStepConfig<Input, ListDepartmentsOperation>,
) -> Result<QueryStep<Input, ListDepartmentsOperation>, ValidationError> {
    if config.connection.provider != r"hesi" {
        return Err(ValidationError::new("Hesi connection provider is invalid"));
    }
    QueryStep::new(config)
}

fn valid_key(key: &str) -> bool {
    !key.is_empty() && key.len() <= 2048 && key.bytes().all(|c| c.is_ascii_graphic())
}
fn invalid() -> QueryAttempt<ListDepartmentsOutput> {
    rejected(
        "invalidResponse",
        FailureKind::Protocol,
        "Invalid Hesi response",
    )
}
fn read_token(body: &[u8]) -> Option<String> {
    let envelope: WireFields = serde_json::from_slice(body).ok()?;
    let token = envelope
        .optional::<WireFields>("value")
        .ok()??
        .optional::<String>("accesstoken")
        .ok()??;
    valid_key(&token).then_some(token)
}
fn read_departments(body: &[u8], start: i64, count: i64) -> Option<ListDepartmentsOutput> {
    let envelope: WireFields = serde_json::from_slice(body).ok()?;
    let total = envelope.optional::<i64>("count").ok()?.unwrap_or_default();
    let items = envelope.optional::<Vec<WireFields>>("items").ok()??;
    if total < 0 || items.len() > count as usize {
        return None;
    }
    let mut departments = Vec::with_capacity(items.len());
    for item in items {
        let id = item.optional::<String>("id").ok()??;
        let name = item.optional::<String>("name").ok()??;
        let code = item.optional::<String>("code").ok()?.unwrap_or_default();
        let active = item.optional::<bool>("active").ok()??;
        if id.is_empty()
            || id.len() > 256
            || name.is_empty()
            || name.len() > 512
            || code.len() > 256
        {
            return None;
        }
        departments.push(DepartmentSummary {
            id,
            name,
            code,
            active,
        });
    }
    let end = start + departments.len() as i64;
    let next_start = if end < total {
        if departments.is_empty() || end > 1000000 {
            return None;
        }
        end
    } else {
        0
    };
    Some(ListDepartmentsOutput {
        departments,
        total,
        next_start,
    })
}
fn rejected(branch: &str, kind: FailureKind, message: &str) -> QueryAttempt<ListDepartmentsOutput> {
    QueryAttempt::branch(
        branch,
        ListDepartmentsOutput::default(),
        Receipt::default(),
        Some(Failure::new(kind, r"hesi", "listDepartments", message)),
    )
}

fn retry(kind: FailureKind, message: &str, delay: f64) -> QueryAttempt<ListDepartmentsOutput> {
    QueryAttempt::retry(
        Failure::new(kind, r"hesi", "listDepartments", message),
        delay,
    )
}
