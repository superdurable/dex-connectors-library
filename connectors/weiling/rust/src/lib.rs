// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Read the Weiling department catalog from a Dex Query Step.

#[rustfmt::skip]
mod generated_definitions;
mod provider_fields;
use provider_fields::BalanceFields as WireFields;

use sha2::{Digest, Sha256};
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant, SystemTime};

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

const BASE_URL: &str = r"https://openapi.weiling.cn";

/// The accessible directory requires no caller-supplied identifiers.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct ListDepartmentsInput {}
/// Accessible department directory in provider order.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct ListDepartmentsOutput {
    /// Department identities, display names, and parents.
    pub departments: Vec<DepartmentSummary>,
}
/// Public metadata for one department.
#[derive(Clone, Deserialize, Serialize)]
pub struct DepartmentSummary {
    /// Department identifier.
    pub id: String,
    /// Department display name.
    pub name: String,
    /// Parent department ID, or empty for a top-level department.
    #[serde(rename = "parentID")]
    pub parent_id: String,
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
    pub app_id: SecretString,
    /// Secret paired with the application key.
    pub app_secret: SecretString,
}
struct CachedToken {
    credential_hash: [u8; 32],
    value: String,
    refresh_at: Instant,
}

/// Bound department query. Construct once and share the HTTP client across calls.
pub struct ListDepartmentsOperation {
    cached_token: Mutex<Option<CachedToken>>,
    monotonic_now: Arc<dyn Fn() -> Instant + Send + Sync>,
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
        Self::with_refresh_clock(
            credentials,
            http,
            max_response_bytes,
            base_url,
            Arc::new(Instant::now),
        )
    }

    fn with_refresh_clock(
        credentials: Arc<dyn CredentialProvider<Credentials>>,
        http: ProviderHttpClient,
        max_response_bytes: usize,
        base_url: &str,
        monotonic_now: Arc<dyn Fn() -> Instant + Send + Sync>,
    ) -> Result<Self, ValidationError> {
        let base = if base_url.is_empty() {
            BASE_URL
        } else {
            base_url
        };
        let origin = sdk::providerhttp::validate_base_url(base)?;
        if origin != BASE_URL {
            return Err(ValidationError::new(
                "Weiling requires its documented HTTPS origin",
            ));
        }
        let endpoint = origin;
        if max_response_bytes == 0 {
            return Err(ValidationError::new(
                "Weiling response size limit must be positive",
            ));
        }
        Ok(Self {
            cached_token: Mutex::new(None),
            monotonic_now,
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

    fn invoke(&self, call: &Call<'_>, _: &Self::Input) -> QueryAttempt<Self::Output> {
        let keys = match self.credentials.resolve(call) {
            Ok(keys) if valid_key(keys.app_id.reveal()) && valid_key(keys.app_secret.reveal()) => {
                keys
            }
            _ => {
                return rejected(
                    "defect",
                    FailureKind::Authentication,
                    "Weiling credentials are unavailable or invalid",
                );
            }
        };
        let token = match self.access_token(&keys) {
            Ok(token) => token,
            Err(attempt) => return *attempt,
        };
        let query = form_urlencoded::Serializer::new(String::new())
            .append_pair("access_token", &token)
            .finish();
        let request = ProviderHttpRequest::new(
            "GET",
            format!("{}/openapi/department/list?{}", self.endpoint, query),
        )
        .header("Accept", "application/json");
        let body = match self.send(&request) {
            Ok(bytes) => bytes,
            Err(attempt) => return *attempt,
        };
        read_departments(&body).unwrap_or_else(invalid)
    }
}
impl ListDepartmentsOperation {
    fn access_token(
        &self,
        keys: &Credentials,
    ) -> Result<String, Box<QueryAttempt<ListDepartmentsOutput>>> {
        let credential_hash: [u8; 32] = Sha256::new()
            .chain_update(keys.app_id.reveal())
            .chain_update([0])
            .chain_update(keys.app_secret.reveal())
            .finalize()
            .into();
        let mut cache = self
            .cached_token
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        if let Some(entry) = cache.as_ref()
            && credential_hash == entry.credential_hash
            && (self.monotonic_now)() < entry.refresh_at
        {
            return Ok(entry.value.clone());
        }
        let query = form_urlencoded::Serializer::new(String::new())
            .append_pair("app_id", keys.app_id.reveal())
            .append_pair("app_secret", keys.app_secret.reveal())
            .finish();
        let request = ProviderHttpRequest::new(
            "GET",
            format!("{}/openapi/auth/access_token/get?{}", self.endpoint, query),
        )
        .header("Accept", "application/json");
        let body = self.send(&request)?;
        let (token, expires) = read_token(&body).unwrap_or_else(|| Err(Box::new(invalid())))?;
        *cache = Some(CachedToken {
            credential_hash,
            value: token.clone(),
            refresh_at: (self.monotonic_now)() + Duration::from_secs(expires - 120),
        });
        Ok(token)
    }
    fn send(
        &self,
        request: &ProviderHttpRequest,
    ) -> Result<Vec<u8>, Box<QueryAttempt<ListDepartmentsOutput>>> {
        let mut response = self.http.send_traced(request).map_err(|_| {
            Box::new(retry(
                FailureKind::Availability,
                "Weiling request failed",
                0.0,
            ))
        })?;
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
            return Err(Box::new(retry(
                kind,
                "Weiling temporarily unavailable",
                delay,
            )));
        }
        let body = read_bounded_body(&mut response, self.max_response_bytes).map_err(|error| {
            Box::new(match error {
                ProviderHttpError::BodyTooLarge(_) => rejected(
                    "invalidResponse",
                    FailureKind::ResponseTooLarge,
                    "Weiling response exceeded the configured limit",
                ),
                _ => retry(
                    FailureKind::Availability,
                    "Weiling response was interrupted",
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
                "Weiling rejected the request",
            )));
        }
        Ok(body)
    }
}
/// Bind the department operation to its branch targets and validate the Step configuration.
pub fn new_list_departments_step<Input>(
    config: QueryStepConfig<Input, ListDepartmentsOperation>,
) -> Result<QueryStep<Input, ListDepartmentsOperation>, ValidationError> {
    if config.connection.provider != r"weiling" {
        return Err(ValidationError::new(
            "Weiling connection provider is invalid",
        ));
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
        "Invalid Weiling response",
    )
}
type TokenResult = Result<(String, u64), Box<QueryAttempt<ListDepartmentsOutput>>>;
fn read_token(body: &[u8]) -> Option<TokenResult> {
    let envelope: WireFields = serde_json::from_slice(body).ok()?;
    let code = envelope.optional::<i64>("code").ok()??;
    let data = envelope
        .optional::<WireFields>("data")
        .ok()?
        .unwrap_or_default();
    let token = data
        .optional::<String>("access_token")
        .ok()?
        .unwrap_or_default();
    let expires = data.optional::<i64>("expires_in").ok()?.unwrap_or_default();
    if code != 0 {
        return Some(Err(Box::new(provider_failure(code))));
    }
    if !valid_key(&token) || !(180..=86400).contains(&expires) {
        return None;
    }
    Some(Ok((token, expires as u64)))
}
fn read_departments(body: &[u8]) -> Option<QueryAttempt<ListDepartmentsOutput>> {
    let envelope: WireFields = serde_json::from_slice(body).ok()?;
    let code = envelope.optional::<i64>("code").ok()??;
    let data = envelope
        .optional::<WireFields>("data")
        .ok()?
        .unwrap_or_default();
    let rows = data.optional::<Vec<Option<WireFields>>>("data_list").ok()?;
    let mut departments = Vec::new();
    if let Some(rows) = rows.as_ref() {
        for item in rows {
            let empty = WireFields::default();
            let row = item.as_ref().unwrap_or(&empty);
            departments.push(DepartmentSummary {
                id: row.optional::<String>("dept_id").ok()?.unwrap_or_default(),
                name: row.optional::<String>("name").ok()?.unwrap_or_default(),
                parent_id: row
                    .optional::<String>("parent_id")
                    .ok()?
                    .unwrap_or_default(),
            });
        }
    }
    if code != 0 {
        return Some(provider_failure(code));
    }
    if rows.is_none()
        || departments.iter().any(|d| {
            d.id.is_empty()
                || d.id.len() > 256
                || d.name.is_empty()
                || d.name.len() > 512
                || d.parent_id.len() > 256
        })
    {
        return None;
    }
    Some(QueryAttempt::branch(
        "listed",
        ListDepartmentsOutput { departments },
        Receipt::default(),
        None,
    ))
}
fn provider_failure(code: i64) -> QueryAttempt<ListDepartmentsOutput> {
    if code == 1009 {
        return retry(
            FailureKind::RateLimit,
            "Weiling rate limited the request",
            0.0,
        );
    }
    rejected(
        "providerRejected",
        if matches!(code, 403 | 1007 | 1008) {
            FailureKind::Authentication
        } else {
            FailureKind::ProviderRejection
        },
        "Weiling rejected the request",
    )
}
fn rejected(branch: &str, kind: FailureKind, message: &str) -> QueryAttempt<ListDepartmentsOutput> {
    QueryAttempt::branch(
        branch,
        ListDepartmentsOutput::default(),
        Receipt::default(),
        Some(Failure::new(kind, r"weiling", "listDepartments", message)),
    )
}

fn retry(kind: FailureKind, message: &str, delay: f64) -> QueryAttempt<ListDepartmentsOutput> {
    QueryAttempt::retry(
        Failure::new(kind, r"weiling", "listDepartments", message),
        delay,
    )
}

#[cfg(test)]
mod token_cache_tests {
    use super::*;
    use sdk::providerhttp::{ProviderHttpResponse, ProviderHttpTransport, ResponseHeaders};
    use std::sync::atomic::{AtomicU64, AtomicUsize, Ordering};
    #[derive(Default)]
    struct TokenIssuer(AtomicUsize);
    impl ProviderHttpTransport for TokenIssuer {
        fn send(
            &self,
            request: &ProviderHttpRequest,
            _: Duration,
        ) -> Result<ProviderHttpResponse, ProviderHttpError> {
            assert!(request.url.contains("/auth/"));
            let sequence = self.0.fetch_add(1, Ordering::SeqCst) + 1;
            let payload = serde_json::json!({"code":0,"data":{"access_token":format!("token-{sequence}"),"expires_in":180}});
            Ok(ProviderHttpResponse::new(
                200,
                ResponseHeaders::new(vec![]),
                Box::new(std::io::Cursor::new(payload.to_string())),
            ))
        }
    }
    #[test]
    fn refreshes_at_the_safety_window_and_on_rotation() {
        let issuer = Arc::new(TokenIssuer::default());
        let transport =
            ProviderHttpClient::with_transport(issuer.clone(), Duration::from_secs(10)).unwrap();
        let provider = Arc::new(sdk::StaticCredentialProvider::new(Default::default()));
        let now = Instant::now();
        let elapsed = Arc::new(AtomicU64::new(0));
        let clock = elapsed.clone();
        let operation = ListDepartmentsOperation::with_refresh_clock(
            provider,
            transport,
            1024,
            BASE_URL,
            Arc::new(move || now + Duration::from_nanos(clock.load(Ordering::Relaxed))),
        )
        .unwrap();
        let mut keys = Credentials {
            app_id: SecretString::new("account"),
            app_secret: SecretString::new("first"),
        };
        let token = |keys: &Credentials| operation.access_token(keys).ok().expect("token");
        assert_eq!(token(&keys), "token-1");
        elapsed.store(60_000_000_000 - 1, Ordering::Relaxed);
        assert_eq!(token(&keys), "token-1");
        elapsed.fetch_add(1, Ordering::Relaxed);
        assert_eq!(token(&keys), "token-2");
        keys.app_secret = SecretString::new("rotated");
        assert_eq!(token(&keys), "token-3");
        assert_eq!(issuer.0.load(Ordering::SeqCst), 3);
    }
}
