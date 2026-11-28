// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Read the Zilliz cluster catalog from a Dex Query Step.

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

/// The listClusters query takes an empty object.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct ListClustersInput {}
/// Authorized clusters in provider order.
#[derive(Clone, Default, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ListClustersOutput {
    /// Total clusters reported by the provider.
    pub total: i64,
    /// Clusters on the first page.
    pub clusters: Vec<ClusterSummary>,
}
/// Metadata for one space.
#[derive(Clone, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ClusterSummary {
    /// Cluster identifier.
    #[serde(rename = "clusterID")]
    pub cluster_id: String,
    /// Cluster display name.
    pub name: String,
    /// Deployment region identifier.
    #[serde(rename = "regionID")]
    pub region_id: String,
    /// Service plan.
    pub plan: String,
    /// Provider status.
    pub status: String,
}
/// Settings for a single provider request.
pub struct Config {
    /// Timeout per attempt. Uses the manifest default.
    pub timeout: Duration,
    /// Maximum response size in bytes. Uses the manifest default.
    pub max_response_bytes: usize,
}

impl Default for Config {
    fn default() -> Self {
        Self {
            timeout: Duration::from_secs(15),
            max_response_bytes: 1048576,
        }
    }
}

/// Credentials resolved from the logical connection for each request.
pub type Credentials = SecretString;
/// Bound cluster query. Construct once and share the HTTP client across calls.
pub struct ListClustersOperation {
    credentials: Arc<dyn CredentialProvider<Credentials>>,
    http: ProviderHttpClient,
    max_response_bytes: usize,
}

impl ListClustersOperation {
    /// Bind a Dex Web project connection, including its API settings and per-call credential resolution.
    pub fn from_project(
        project: &sdk::projectconfig::LoadedProject,
        connection_name: &str,
    ) -> Result<Self, Box<dyn std::error::Error>> {
        let key = sdk::projectconfig::ConnectionKey::new("zilliz", connection_name);
        let entry = project
            .connection_configuration(&key)
            .ok_or("Zilliz connection is missing")?;
        if entry.provider != "zilliz" {
            return Err("Zilliz project connection provider differs".into());
        }
        let mut config = Config::default();
        for (name, value) in entry.configuration.to_json_object()? {
            match name.as_str() {
                "timeout" | "maxResponseBytes" if value.as_str() == Some("") => {}
                "timeout" => {
                    config.timeout = Duration::from_nanos(
                        value
                            .as_i64()
                            .filter(|n| *n > 0)
                            .ok_or("invalid Zilliz timeout setting")?
                            as u64,
                    )
                }
                "maxResponseBytes" => {
                    config.max_response_bytes = value
                        .as_u64()
                        .and_then(|n| usize::try_from(n).ok())
                        .ok_or("invalid Zilliz response size setting")?
                }
                _ => return Err("unknown Zilliz setting".into()),
            }
        }
        let provider = sdk::projectconfig::ProjectCredentialProvider::new(
            project.connections().clone(),
            key,
            Arc::new(|fields| {
                if fields.len() != 1 {
                    return Err("Invalid Zilliz credential fields".into());
                }
                fields
                    .get("apiKey")
                    .and_then(serde_json::Value::as_str)
                    .filter(|value| !value.is_empty())
                    .map(Credentials::new)
                    .ok_or_else(|| "Missing Zilliz API token".into())
            }),
            None,
        )?;
        Ok(Self::new(config, Arc::new(provider))?)
    }

    /// Construct a bounded, redirect-safe transport without contacting the provider.
    pub fn new(
        config: Config,
        credentials: Arc<dyn CredentialProvider<Credentials>>,
    ) -> Result<Self, ValidationError> {
        let http = ProviderHttpClient::new(config.timeout, AddressPolicy::PublicHttps)?;
        Self::with_http_client(credentials, http, config.max_response_bytes)
    }
    /// Supply a caller-owned HTTP transport and a positive response limit in bytes.
    pub fn with_http_client(
        credentials: Arc<dyn CredentialProvider<Credentials>>,
        http: ProviderHttpClient,
        max_response_bytes: usize,
    ) -> Result<Self, ValidationError> {
        if max_response_bytes == 0 || max_response_bytes > 16 << 20 {
            return Err(ValidationError::new(
                "Zilliz response size limit must be positive",
            ));
        }
        Ok(Self {
            credentials,
            http,
            max_response_bytes,
        })
    }
}

impl Query for ListClustersOperation {
    type Input = ListClustersInput;
    type Output = ListClustersOutput;

    fn definition(&self) -> QueryDefinition {
        generated_definitions::ListClusters::definition()
    }

    fn invoke(&self, call: &Call<'_>, _: &Self::Input) -> QueryAttempt<Self::Output> {
        let token = match self.credentials.resolve(call) {
            Ok(token) if valid_key(token.reveal()) => token,
            _ => {
                return rejected(
                    "defect",
                    FailureKind::Authentication,
                    "Invalid Zilliz API token",
                );
            }
        };
        let request = ProviderHttpRequest::new(
            "GET",
            "https://api.cloud.zilliz.com/v2/clusters?pageSize=25&currentPage=1",
        )
        .header("Accept", "application/json")
        .header("Authorization", format!("Bearer {}", token.reveal()));
        self.read_response(&request)
    }
}
impl ListClustersOperation {
    fn read_response(&self, request: &ProviderHttpRequest) -> QueryAttempt<ListClustersOutput> {
        let mut response = match self.http.send_traced(request) {
            Ok(response) => response,
            Err(_) => return retry(FailureKind::Availability, "Zilliz request failed", 0.0),
        };
        let status = response.status();

        if matches!(status, 408 | 429 | 500..) {
            let delay = parse_retry_after(
                response.headers().get("Retry-After").unwrap_or(""),
                SystemTime::now(),
            );
            return retry(
                if status == 429 {
                    FailureKind::RateLimit
                } else {
                    FailureKind::Availability
                },
                "Zilliz temporarily unavailable",
                delay,
            );
        }
        if status != 200 {
            return rejected(
                "providerRejected",
                match status {
                    401 => FailureKind::Authentication,
                    403 => FailureKind::Authorization,
                    _ => FailureKind::ProviderRejection,
                },
                "Zilliz rejected the request",
            );
        }
        let body = match read_bounded_body(&mut response, self.max_response_bytes) {
            Ok(body) => body,
            Err(ProviderHttpError::BodyTooLarge(_)) => {
                return rejected(
                    "invalidResponse",
                    FailureKind::ResponseTooLarge,
                    "Zilliz response exceeded the configured limit",
                );
            }
            Err(_) => {
                return retry(
                    FailureKind::Availability,
                    "Zilliz response interrupted",
                    0.0,
                );
            }
        };
        decode_clusters(&body).unwrap_or_else(invalid)
    }
}
/// Bind the cluster operation to its branch targets and validate the Step configuration.
pub fn new_list_clusters_step<Input>(
    config: QueryStepConfig<Input, ListClustersOperation>,
) -> Result<QueryStep<Input, ListClustersOperation>, ValidationError> {
    if config.connection.provider != r"zilliz" {
        return Err(ValidationError::new(
            "Zilliz connection provider is invalid",
        ));
    }
    QueryStep::new(config)
}

fn valid_key(key: &str) -> bool {
    !key.is_empty() && key.len() <= 1024 && key.bytes().all(|c| c.is_ascii_graphic())
}
fn invalid() -> QueryAttempt<ListClustersOutput> {
    rejected(
        "invalidResponse",
        FailureKind::Protocol,
        "Invalid Zilliz response",
    )
}
#[derive(Deserialize)]
#[serde(try_from = "WireFields")]
struct WireItem {
    cluster_id: Option<String>,
    name: Option<String>,
    region_id: Option<String>,
    plan: Option<String>,
    status: Option<String>,
}
impl TryFrom<WireFields> for WireItem {
    type Error = serde_json::Error;
    fn try_from(fields: WireFields) -> Result<Self, Self::Error> {
        Ok(Self {
            cluster_id: fields.optional("clusterid")?,
            name: fields.optional("clustername")?,
            region_id: fields.optional("regionid")?,
            plan: fields.optional("plan")?,
            status: fields.optional("status")?,
        })
    }
}
#[derive(Deserialize)]
#[serde(try_from = "WireFields")]
struct DirectoryResponse {
    code: Option<i64>,
    data: Option<DirectoryData>,
}
impl TryFrom<WireFields> for DirectoryResponse {
    type Error = serde_json::Error;
    fn try_from(fields: WireFields) -> Result<Self, Self::Error> {
        Ok(Self {
            code: fields.optional("code")?,
            data: fields.optional("data")?,
        })
    }
}
#[derive(Deserialize)]
#[serde(try_from = "WireFields")]
struct DirectoryData {
    count: Option<i64>,
    items: Option<Vec<Option<WireItem>>>,
}
impl TryFrom<WireFields> for DirectoryData {
    type Error = serde_json::Error;
    fn try_from(fields: WireFields) -> Result<Self, Self::Error> {
        Ok(Self {
            count: fields.optional("count")?,
            items: fields.optional("clusters")?,
        })
    }
}
fn decode_clusters(body: &[u8]) -> Option<QueryAttempt<ListClustersOutput>> {
    let page: DirectoryResponse = serde_json::from_slice(body).ok()?;
    if page.code? != 0 {
        return Some(rejected(
            "providerRejected",
            FailureKind::ProviderRejection,
            "Zilliz rejected the query",
        ));
    }
    let data = page.data?;
    let total = data.count?;
    if total < 0 {
        return None;
    }
    let items = data.items?;
    if items.len() > 25 {
        return None;
    }
    let mut clusters = Vec::new();
    for item in items {
        let item = item?;
        let cluster_id = item.cluster_id?;
        if cluster_id.is_empty() || cluster_id.trim() != cluster_id || cluster_id.len() > 128 {
            return None;
        }
        let name = item.name.unwrap_or_default();
        if name.len() > 200 {
            return None;
        }
        let region_id = item.region_id.unwrap_or_default();
        if region_id.len() > 100 {
            return None;
        }
        let plan = item.plan.unwrap_or_default();
        if plan.len() > 100 {
            return None;
        }
        let status = item.status.unwrap_or_default();
        if status.len() > 100 {
            return None;
        }
        clusters.push(ClusterSummary {
            cluster_id,
            name,
            region_id,
            plan,
            status,
        });
    }
    Some(QueryAttempt::branch(
        "found",
        ListClustersOutput { clusters, total },
        Receipt::default(),
        None,
    ))
}
fn rejected(branch: &str, kind: FailureKind, message: &str) -> QueryAttempt<ListClustersOutput> {
    QueryAttempt::branch(
        branch,
        ListClustersOutput::default(),
        Receipt::default(),
        Some(Failure::new(kind, r"zilliz", "listClusters", message)),
    )
}

fn retry(kind: FailureKind, message: &str, delay: f64) -> QueryAttempt<ListClustersOutput> {
    QueryAttempt::retry(
        Failure::new(kind, r"zilliz", "listClusters", message),
        delay,
    )
}
