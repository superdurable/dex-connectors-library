// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Read the Baichuan knowledge base directory from a Dex Query Step.

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

const BASE_URL: &str = r"https://api.baichuan-ai.com";

/// The knowledge-base list query takes no application input.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct ListKnowledgeBasesInput {}

/// The ordered list of accessible knowledge bases.
#[derive(Clone, Default, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ListKnowledgeBasesOutput {
    /// Provider resource metadata.
    pub knowledge_bases: Vec<KnowledgeBase>,
}
/// One knowledge-base resource from the provider.
#[derive(Clone, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct KnowledgeBase {
    /// Stable knowledge-base identifier.
    pub id: String,
    /// Provider resource name.
    pub name: String,
    /// Provider description.
    pub description: String,
    /// Provider processing status.
    pub status: String,
    /// Attached provider file identifiers.
    pub file_ids: Vec<String>,
    /// Creation time in Unix seconds.
    pub created_at: i64,
    /// Last update time in Unix seconds.
    pub updated_at: i64,
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
            max_response_bytes: 1048576,
        }
    }
}

/// API key resolved from the logical connection for each request.
#[derive(Clone)]
pub struct Credentials {
    /// Provider-issued bearer credential, never stored in the result.
    pub api_key: SecretString,
}

impl From<SecretString> for Credentials {
    fn from(credential: SecretString) -> Self {
        Self {
            api_key: credential,
        }
    }
}

/// Bound knowledge-base query. Construct once and share the HTTP client across calls.
pub struct ListKnowledgeBasesOperation {
    credentials: Arc<dyn CredentialProvider<Credentials>>,
    http: ProviderHttpClient,
    endpoint: String,
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
        let base = if base_url.is_empty() {
            BASE_URL
        } else {
            base_url
        };
        let endpoint = sdk::providerhttp::validate_base_url(base)? + r"/v1/kbs";
        if max_response_bytes == 0 {
            return Err(ValidationError::new(
                "Baichuan response size limit must be positive",
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

impl Query for ListKnowledgeBasesOperation {
    type Input = ListKnowledgeBasesInput;
    type Output = ListKnowledgeBasesOutput;

    fn definition(&self) -> QueryDefinition {
        generated_definitions::ListKnowledgeBases::definition()
    }

    fn invoke(&self, call: &Call<'_>, _: &Self::Input) -> QueryAttempt<Self::Output> {
        let credentials = match self.credentials.resolve(call) {
            Ok(credentials) if valid_key(credentials.api_key.reveal()) => credentials,
            _ => {
                return rejected(
                    "defect",
                    FailureKind::Authentication,
                    "Baichuan API Key is unavailable or invalid",
                );
            }
        };
        let request = ProviderHttpRequest::new("GET", &self.endpoint)
            .header(
                "Authorization",
                format!("Bearer {}", credentials.api_key.reveal()),
            )
            .header("Accept", "application/json");
        let Ok(mut response) = self.http.send_traced(&request) else {
            return retry(
                FailureKind::Availability,
                "Baichuan knowledge-base request failed",
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
                    "Baichuan rate limited the knowledge-base request",
                    delay,
                ),
                _ => retry(
                    FailureKind::Availability,
                    "Baichuan API is temporarily unavailable",
                    delay,
                ),
            };
        }
        if status != 200 {
            return rejected(
                "providerRejected",
                match status {
                    401 => FailureKind::Authentication,
                    403 => FailureKind::Authorization,
                    _ => FailureKind::ProviderRejection,
                },
                &format!("Baichuan rejected knowledge-base request with HTTP {status}"),
            );
        }
        match read_bounded_body(&mut response, self.max_response_bytes) {
            Ok(body) => decode_knowledge_bases(&body, &credentials.api_key),
            Err(ProviderHttpError::BodyTooLarge(_)) => rejected(
                "invalidResponse",
                FailureKind::ResponseTooLarge,
                "Baichuan response exceeded the configured limit",
            ),
            Err(_) => retry(
                FailureKind::Availability,
                "Baichuan response was interrupted",
                0.0,
            ),
        }
    }
}

/// Bind the knowledge-base operation to its branch targets and validate the Step configuration.
pub fn new_list_knowledge_bases_step<Input>(
    config: QueryStepConfig<Input, ListKnowledgeBasesOperation>,
) -> Result<QueryStep<Input, ListKnowledgeBasesOperation>, ValidationError> {
    if config.connection.provider != r"baichuan" {
        return Err(ValidationError::new(
            "Baichuan connection provider is invalid",
        ));
    }
    QueryStep::new(config)
}

fn valid_key(key: &str) -> bool {
    !key.is_empty() && key.len() <= 512 && key.bytes().all(|byte| byte.is_ascii_graphic())
}

fn decode_knowledge_bases(
    body: &[u8],
    credential: &SecretString,
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
        object: String,
        data: Vec<Row>,
    }
    #[derive(Deserialize)]
    struct Row {
        id: String,
        object: String,
        name: Option<String>,
        description: Option<String>,
        status: Option<String>,
        file_ids: Option<Vec<String>>,
        created_at: Option<i64>,
        updated_at: Option<i64>,
    }
    let Ok(envelope) = provider_fields::decode_provider_fields::<Envelope>(body) else {
        return invalid();
    };
    if envelope.object != "list" || envelope.data.len() > 10000 {
        return invalid();
    }
    let mut seen = std::collections::HashSet::new();
    let mut output = Vec::with_capacity(envelope.data.len());
    for row in envelope.data {
        let item = KnowledgeBase {
            id: row.id,
            name: row.name.unwrap_or_default(),
            description: row.description.unwrap_or_default(),
            status: row.status.unwrap_or_default(),
            file_ids: row.file_ids.unwrap_or_default(),
            created_at: row.created_at.unwrap_or_default(),
            updated_at: row.updated_at.unwrap_or_default(),
        };
        if row.object != "knowledge-base"
            || item.id.is_empty()
            || item.id.len() > 256
            || !seen.insert(item.id.clone())
            || item.name.len() > 256
            || item.description.len() > 4096
            || item.created_at < 0
            || item.updated_at < 0
            || item.file_ids.len() > 10000
            || item.file_ids.iter().any(|f| f.is_empty() || f.len() > 256)
            || [&item.id, &item.name, &item.description, &item.status]
                .into_iter()
                .chain(item.file_ids.iter())
                .any(|f| f.contains(credential.reveal()))
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
        Some(Failure::new(
            kind,
            r"baichuan",
            "listKnowledgeBases",
            message,
        )),
    )
}

fn retry(kind: FailureKind, message: &str, delay: f64) -> QueryAttempt<ListKnowledgeBasesOutput> {
    QueryAttempt::retry(
        Failure::new(kind, r"baichuan", "listKnowledgeBases", message),
        delay,
    )
}
