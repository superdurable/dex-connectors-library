// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Read the ZenTao product catalog from a Dex Query Step.

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

/// The listProducts query takes an empty object.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct ListProductsInput {}
/// Products returned in provider order.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct ProductList {
    /// Total reported by the provider.
    pub total: i64,
    /// Catalog entries in provider order.
    pub products: Vec<Product>,
}
/// Metadata for one product.
#[derive(Clone, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Product {
    /// Product identifier.
    pub id: i64,
    /// Product display name.
    pub name: String,
    /// Product code.
    pub code: String,
    /// Product type.
    #[serde(rename = "type")]
    pub product_type: String,
    /// Product access mode.
    pub access_control: String,
}
/// Settings for a single provider request.
pub struct Config {
    /// Provider API base URL. Required installation root, including any subpath.
    pub base_url: String,
    /// Timeout per attempt. Uses the manifest default.
    pub timeout: Duration,
    /// Maximum response size in bytes. Uses the manifest default.
    pub max_response_bytes: usize,
}

impl Default for Config {
    fn default() -> Self {
        Self {
            base_url: String::new(),
            timeout: Duration::from_secs(15),
            max_response_bytes: 1048576,
        }
    }
}

/// Credentials resolved from the logical connection for each request.
pub type Credentials = SecretString;
/// Bound product query. Construct once and share the HTTP client across calls.
pub struct ListProductsOperation {
    credentials: Arc<dyn CredentialProvider<Credentials>>,
    http: ProviderHttpClient,
    endpoint: String,
    max_response_bytes: usize,
}

impl ListProductsOperation {
    /// Bind a Dex Web project connection, including its API settings and per-call credential resolution.
    pub fn from_project(
        project: &sdk::projectconfig::LoadedProject,
        connection_name: &str,
    ) -> Result<Self, Box<dyn std::error::Error>> {
        let key = sdk::projectconfig::ConnectionKey::new("zentao", connection_name);
        let entry = project
            .connection_configuration(&key)
            .ok_or("ZenTao connection is missing")?;
        if entry.provider != "zentao" {
            return Err("ZenTao project connection provider differs".into());
        }
        let mut config = Config::default();
        for (name, value) in entry.configuration.to_json_object()? {
            match name.as_str() {
                "baseUrl" | "timeout" | "maxResponseBytes" if value.as_str() == Some("") => {}
                "baseUrl" => {
                    config.base_url = value
                        .as_str()
                        .ok_or("invalid ZenTao API URL setting")?
                        .into()
                }
                "timeout" => {
                    config.timeout = Duration::from_nanos(
                        value
                            .as_i64()
                            .filter(|n| *n > 0)
                            .ok_or("invalid ZenTao timeout setting")?
                            as u64,
                    )
                }
                "maxResponseBytes" => {
                    config.max_response_bytes = value
                        .as_u64()
                        .and_then(|n| usize::try_from(n).ok())
                        .ok_or("invalid ZenTao response size setting")?
                }
                _ => return Err("unknown ZenTao setting".into()),
            }
        }
        let provider = sdk::projectconfig::ProjectCredentialProvider::new(
            project.connections().clone(),
            key,
            Arc::new(|fields| {
                if fields.len() != 1 {
                    return Err("Invalid ZenTao credential fields".into());
                }
                fields
                    .get("token")
                    .and_then(serde_json::Value::as_str)
                    .filter(|value| !value.is_empty())
                    .map(Credentials::new)
                    .ok_or_else(|| "Missing ZenTao API token".into())
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

    /// Bind the deployment origin, a caller-supplied transport and a positive byte limit.
    pub fn with_base_url(
        credentials: Arc<dyn CredentialProvider<Credentials>>,
        http: ProviderHttpClient,
        max_response_bytes: usize,
        base_url: &str,
    ) -> Result<Self, ValidationError> {
        let endpoint = sdk::providerhttp::validate_base_url(base_url)?;
        if endpoint.ends_with("/api.php") || endpoint.contains("/api.php/") {
            return Err(ValidationError::new(
                "ZenTao requires the installation root without /api.php",
            ));
        }
        if max_response_bytes == 0 {
            return Err(ValidationError::new(
                "ZenTao response size limit must be positive",
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

impl Query for ListProductsOperation {
    type Input = ListProductsInput;
    type Output = ProductList;

    fn definition(&self) -> QueryDefinition {
        generated_definitions::ListProducts::definition()
    }

    fn invoke(&self, call: &Call<'_>, _: &Self::Input) -> QueryAttempt<Self::Output> {
        let token = match self.credentials.resolve(call) {
            Ok(token) if valid_key(token.reveal()) => token,
            _ => {
                return rejected(
                    "defect",
                    FailureKind::Authentication,
                    "Invalid ZenTao API token",
                );
            }
        };
        let request =
            ProviderHttpRequest::new("GET", format!("{}/api.php/v1/products", self.endpoint))
                .header("Accept", "application/json")
                .header("Token", token.reveal());
        self.read_response(&request)
    }
}
impl ListProductsOperation {
    fn read_response(&self, request: &ProviderHttpRequest) -> QueryAttempt<ProductList> {
        let mut response = match self.http.send_traced(request) {
            Ok(response) => response,
            Err(_) => return retry(FailureKind::Availability, "ZenTao request failed", 0.0),
        };
        let status = response.status();
        let delay = parse_retry_after(
            response.headers().get("Retry-After").unwrap_or(""),
            SystemTime::now(),
        );
        if matches!(status, 408 | 429 | 500..) {
            return retry(
                if status == 429 {
                    FailureKind::RateLimit
                } else {
                    FailureKind::Availability
                },
                "ZenTao temporarily unavailable",
                delay,
            );
        }
        let body = match read_bounded_body(&mut response, self.max_response_bytes) {
            Ok(body) => body,
            Err(ProviderHttpError::BodyTooLarge(_)) => {
                return rejected(
                    "invalidResponse",
                    FailureKind::ResponseTooLarge,
                    "ZenTao response exceeded the configured limit",
                );
            }
            Err(_) => {
                return retry(
                    FailureKind::Availability,
                    "ZenTao response interrupted",
                    0.0,
                );
            }
        };
        if status != 200 {
            return rejected(
                "providerRejected",
                match status {
                    401 => FailureKind::Authentication,
                    403 => FailureKind::Authorization,
                    400 | 404 => FailureKind::Validation,
                    _ => FailureKind::ProviderRejection,
                },
                "ZenTao rejected the request",
            );
        }
        decode_products(&body).unwrap_or_else(invalid)
    }
}
/// Bind the product operation to its branch targets and validate the Step configuration.
pub fn new_list_products_step<Input>(
    config: QueryStepConfig<Input, ListProductsOperation>,
) -> Result<QueryStep<Input, ListProductsOperation>, ValidationError> {
    if config.connection.provider != r"zentao" {
        return Err(ValidationError::new(
            "ZenTao connection provider is invalid",
        ));
    }
    QueryStep::new(config)
}

fn valid_key(key: &str) -> bool {
    !key.is_empty() && key.bytes().all(|c| c.is_ascii_graphic())
}
fn invalid() -> QueryAttempt<ProductList> {
    rejected(
        "invalidResponse",
        FailureKind::Protocol,
        "Invalid ZenTao response",
    )
}
#[derive(Deserialize)]
#[serde(try_from = "WireFields")]
struct WireItem {
    id: Option<i64>,
    name: Option<String>,
    code: Option<String>,
    product_type: Option<String>,
    access_control: Option<String>,
}
impl TryFrom<WireFields> for WireItem {
    type Error = serde_json::Error;
    fn try_from(fields: WireFields) -> Result<Self, Self::Error> {
        Ok(Self {
            id: fields.optional("id")?,
            name: fields.optional("name")?,
            code: fields.optional("code")?,
            product_type: fields.optional("type")?,
            access_control: fields.optional("acl")?,
        })
    }
}
#[derive(Deserialize)]
#[serde(try_from = "WireFields")]
struct DirectoryResponse {
    items: Option<Vec<Option<WireItem>>>,
    total: Option<i64>,
}
impl TryFrom<WireFields> for DirectoryResponse {
    type Error = serde_json::Error;
    fn try_from(fields: WireFields) -> Result<Self, Self::Error> {
        Ok(Self {
            items: fields.optional("products")?,
            total: fields.optional("total")?,
        })
    }
}
fn decode_products(body: &[u8]) -> Option<QueryAttempt<ProductList>> {
    let page: DirectoryResponse = serde_json::from_slice(body).ok()?;
    let items = page.items?;
    let total = page.total?;
    if total < 0 || items.len() > 1000 {
        return None;
    }
    let mut products = Vec::new();
    for item in items {
        let item = item?;
        let id = item.id?;
        let name = item.name?;
        let code = item.code.unwrap_or_default();
        let product_type = item.product_type?;
        let access_control = item.access_control?;
        if id <= 0
            || name.is_empty()
            || !matches!(product_type.as_str(), "normal" | "branch" | "platform")
            || !matches!(access_control.as_str(), "open" | "private")
        {
            return None;
        }
        products.push(Product {
            id,
            name,
            code,
            product_type,
            access_control,
        });
    }
    Some(QueryAttempt::branch(
        "listed",
        ProductList { products, total },
        Receipt::default(),
        None,
    ))
}
fn rejected(branch: &str, kind: FailureKind, message: &str) -> QueryAttempt<ProductList> {
    QueryAttempt::branch(
        branch,
        ProductList::default(),
        Receipt::default(),
        Some(Failure::new(kind, r"zentao", "listProducts", message)),
    )
}

fn retry(kind: FailureKind, message: &str, delay: f64) -> QueryAttempt<ProductList> {
    QueryAttempt::retry(
        Failure::new(kind, r"zentao", "listProducts", message),
        delay,
    )
}
