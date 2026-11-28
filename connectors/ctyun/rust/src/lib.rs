// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Read the CTYun knowledge base directory from a Dex Query Step.

#[rustfmt::skip]
mod generated_definitions;
mod provider_fields;

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

const BASE_URL: &str = r"https://kqa-global.ctapi.ctyun.cn";

/// The knowledge-base list query takes no application input.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct ListKnowledgeBasesInput {}

/// The ordered list of accessible knowledge bases.
#[derive(Clone, Default, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ListKnowledgeBasesOutput {
    /// Provider resource metadata.
    pub knowledge_bases: Vec<KnowledgeBaseSummary>,
}
/// One knowledge-base resource from the provider.
#[derive(Clone, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct KnowledgeBaseSummary {
    /// Provider resource identifier.
    pub id: i64,
    /// Provider resource name.
    pub name: String,
    /// Number of files in the knowledge base.
    pub file_count: i64,
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
            timeout: Duration::from_nanos(10000000000),
            max_response_bytes: 2097152,
        }
    }
}

/// CTYun credentials resolved from the logical connection for each request.
#[derive(Clone)]
pub struct Credentials {
    /// Access key identifier.
    pub access_key: SecretString,
    /// Security key paired with the access key.
    pub security_key: SecretString,
    /// Tenant for the knowledge-base service.
    pub tenant_id: SecretString,
}
/// Bound knowledge-base query. Construct once and share the HTTP client across calls.
pub struct ListKnowledgeBasesOperation {
    credentials: Arc<dyn CredentialProvider<Credentials>>,
    http: ProviderHttpClient,
    endpoint: String,
    clock: Arc<dyn Fn() -> SystemTime + Send + Sync>,
    request_id: Arc<dyn Fn() -> Option<uuid::Uuid> + Send + Sync>,
    max_response_bytes: usize,
}

impl ListKnowledgeBasesOperation {
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
        Self::with_signing_sources(
            credentials,
            http,
            max_response_bytes,
            base_url,
            Arc::new(SystemTime::now),
            Arc::new(new_request_id),
        )
    }
    /// Bind a transport, clock and UUID generator. A missing UUID retries before dispatch.
    pub fn with_signing_sources(
        credentials: Arc<dyn CredentialProvider<Credentials>>,
        http: ProviderHttpClient,
        max_response_bytes: usize,
        base_url: &str,
        clock: Arc<dyn Fn() -> SystemTime + Send + Sync>,
        request_id: Arc<dyn Fn() -> Option<uuid::Uuid> + Send + Sync>,
    ) -> Result<Self, ValidationError> {
        let base = if base_url.is_empty() {
            BASE_URL
        } else {
            base_url
        };
        let endpoint = sdk::providerhttp::validate_base_url(base)? + r"/openapi/v1/infobases/list";
        if max_response_bytes == 0 {
            return Err(ValidationError::new(
                "CTYun response size limit must be positive",
            ));
        }
        if endpoint != format!("{BASE_URL}/openapi/v1/infobases/list") {
            return Err(ValidationError::new(
                "CTYun requires its documented API origin",
            ));
        }
        Ok(Self {
            clock,
            request_id,
            credentials,
            endpoint,
            http,
            max_response_bytes,
        })
    }
}

impl Query for ListKnowledgeBasesOperation {
    type Input = ListKnowledgeBasesInput;
    type Output = ListKnowledgeBasesOutput;

    fn definition(&self) -> QueryDefinition {
        generated_definitions::ListKnowledgeBases::definition()
    }

    fn invoke(&self, call: &Call<'_>, _: &Self::Input) -> QueryAttempt<Self::Output> {
        let credentials = match self.credentials.resolve(call) {
            Ok(credentials)
                if [
                    &credentials.access_key,
                    &credentials.security_key,
                    &credentials.tenant_id,
                ]
                .iter()
                .all(|key| valid_key(key.reveal())) =>
            {
                credentials
            }
            _ => {
                return rejected(
                    "defect",
                    FailureKind::Authentication,
                    "CTYun credentials or tenant ID are unavailable or invalid",
                );
            }
        };
        let Some(id) = (self.request_id)() else {
            return retry(
                FailureKind::Availability,
                "CTYun request ID could not be generated",
                0.0,
            );
        };
        let instant = chrono::DateTime::<chrono::Utc>::from((self.clock)());
        let date = instant
            .with_timezone(&chrono::FixedOffset::east_opt(8 * 3600).expect("Beijing offset"))
            .format("%Y%m%dT%H%M%SZ")
            .to_string();
        let request = ProviderHttpRequest::new("GET", &self.endpoint)
            .header("Accept", "application/json")
            .header("Ctyun-Eop-Request-Id", id.to_string())
            .header("Eop-Date", date.clone())
            .header("Tenantid", credentials.tenant_id.reveal())
            .header("Eop-Authorization", sign(&credentials, &id, &date));
        let Ok(mut response) = self.http.send_traced(&request) else {
            return retry(
                FailureKind::Availability,
                "CTYun knowledge-base request failed",
                0.0,
            );
        };
        let status = response.status();
        if matches!(status, 408 | 429 | 500..) {
            let delay = parse_retry_after(
                response.headers().get("Retry-After").unwrap_or(""),
                SystemTime::now(),
            );
            return match status {
                429 => retry(
                    FailureKind::RateLimit,
                    "CTYun rate limited the knowledge-base request",
                    delay,
                ),
                _ => retry(
                    FailureKind::Availability,
                    "CTYun API is temporarily unavailable",
                    delay,
                ),
            };
        }
        match read_bounded_body(&mut response, self.max_response_bytes) {
            Ok(_) if status != 200 => rejected(
                "providerRejected",
                match status {
                    401 => FailureKind::Authentication,
                    403 => FailureKind::Authentication,
                    _ => FailureKind::ProviderRejection,
                },
                &format!("CTYun rejected knowledge-base request with HTTP {status}"),
            ),
            Ok(body) => decode_knowledge_bases(&body, &credentials),
            Err(ProviderHttpError::BodyTooLarge(_)) => rejected(
                "invalidResponse",
                FailureKind::ResponseTooLarge,
                "CTYun response exceeded the configured limit",
            ),
            Err(_) => retry(
                FailureKind::Availability,
                "CTYun response was interrupted",
                0.0,
            ),
        }
    }
}

/// Bind the knowledge-base operation to its branch targets and validate the Step configuration.
pub fn new_list_knowledge_bases_step<Input>(
    config: QueryStepConfig<Input, ListKnowledgeBasesOperation>,
) -> Result<QueryStep<Input, ListKnowledgeBasesOperation>, ValidationError> {
    if config.connection.provider != r"ctyun" {
        return Err(ValidationError::new("CTYun connection provider is invalid"));
    }
    QueryStep::new(config)
}

fn valid_key(key: &str) -> bool {
    !key.is_empty() && key.len() <= 2048 && key.bytes().all(|byte| byte.is_ascii_graphic())
}

fn decode_knowledge_bases(
    body: &[u8],
    credential: &Credentials,
) -> QueryAttempt<ListKnowledgeBasesOutput> {
    let invalid = || {
        rejected(
            "invalidResponse",
            FailureKind::Protocol,
            "Invalid knowledge-base metadata",
        )
    };
    #[derive(Deserialize)]
    struct Envelope {
        #[serde(rename = "statuscode")]
        status_code: serde_json::Value,
        error: Option<String>,
        #[serde(rename = "returnobj")]
        return_obj: Option<Vec<Option<Row>>>,
    }
    #[derive(Deserialize, Default)]
    struct Row {
        id: Option<i64>,
        name: Option<String>,
        count: Option<i64>,
    }
    let Ok(envelope) = provider_fields::decode_provider_fields::<Envelope>(body) else {
        return invalid();
    };
    let status = match envelope.status_code {
        serde_json::Value::String(value) if !value.is_empty() => value,
        serde_json::Value::Number(value) if value.as_i64().is_some_and(|n| n > 0) => {
            value.to_string()
        }
        _ => return invalid(),
    };
    if status != "200" {
        if matches!(status.as_str(), "40002" | "40004") {
            return retry(
                FailureKind::Availability,
                "CTYun service reported a temporary error",
                0.0,
            );
        }
        let auth = matches!(status.as_str(), "40001" | "40003" | "40016")
            || envelope.error.unwrap_or_default().starts_with("CTAPI_100");
        return rejected(
            "providerRejected",
            if auth {
                FailureKind::Authentication
            } else {
                FailureKind::ProviderRejection
            },
            "CTYun rejected the knowledge-base request",
        );
    }
    let Some(rows) = envelope.return_obj else {
        return invalid();
    };
    let mut output = Vec::with_capacity(rows.len());
    for row in rows {
        let row = row.unwrap_or_default();
        let item = KnowledgeBaseSummary {
            id: row.id.unwrap_or_default(),
            name: row.name.unwrap_or_default(),
            file_count: row.count.unwrap_or_default(),
        };
        if item.id <= 0
            || item.name.is_empty()
            || item.name.len() > 1024
            || item.file_count < 0
            || [
                &credential.access_key,
                &credential.security_key,
                &credential.tenant_id,
            ]
            .iter()
            .any(|key| item.name.contains(key.reveal()))
        {
            return invalid();
        }
        output.push(item);
    }

    QueryAttempt::branch(
        "listed",
        ListKnowledgeBasesOutput {
            knowledge_bases: output,
        },
        Receipt::default(),
        None,
    )
}
fn rejected(
    branch: &str,
    kind: FailureKind,
    message: &str,
) -> QueryAttempt<ListKnowledgeBasesOutput> {
    QueryAttempt::branch(
        branch,
        ListKnowledgeBasesOutput::default(),
        Receipt::default(),
        Some(Failure::new(kind, r"ctyun", "listKnowledgeBases", message)),
    )
}

fn retry(kind: FailureKind, message: &str, delay: f64) -> QueryAttempt<ListKnowledgeBasesOutput> {
    QueryAttempt::retry(
        Failure::new(kind, r"ctyun", "listKnowledgeBases", message),
        delay,
    )
}

fn new_request_id() -> Option<uuid::Uuid> {
    let mut bytes = [0; 16];
    getrandom::fill(&mut bytes).ok()?;
    Some(uuid::Builder::from_random_bytes(bytes).into_uuid())
}

fn sign(keys: &Credentials, id: &uuid::Uuid, date: &str) -> String {
    use base64::Engine;
    use hmac::{Hmac, Mac};
    use sha2::Sha256;
    fn mac(key: &[u8], data: &str) -> Vec<u8> {
        let mut mac = Hmac::<Sha256>::new_from_slice(key).expect("HMAC accepts any key length");
        mac.update(data.as_bytes());
        mac.finalize().into_bytes().to_vec()
    }
    let canonical = format!(
        "ctyun-eop-request-id:{id}\neop-date:{date}\n\ne3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
    );
    let time_key = mac(keys.security_key.reveal().as_bytes(), date);
    let access_key = mac(&time_key, keys.access_key.reveal());
    let date_key = mac(&access_key, &date[..8]);
    format!(
        "{} headers=ctyun-eop-request-id;eop-date Signature={}",
        keys.access_key.reveal(),
        base64::engine::general_purpose::STANDARD.encode(mac(&date_key, &canonical))
    )
}
