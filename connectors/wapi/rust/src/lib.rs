// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Read the Wapi vehicle brand catalog from a Dex Query Step.

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

/// The listVehicleBrands query takes an empty object.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct ListVehicleBrandsInput {}
/// Brands returned in provider order.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct ListVehicleBrandsOutput {
    /// Total reported by the provider.
    pub total: i64,
    /// Catalog entries in provider order.
    pub brands: Vec<VehicleBrandPrice>,
}
/// Metadata for one vehicle brand.
#[derive(Clone, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct VehicleBrandPrice {
    /// Vehicle brand identifier.
    pub id: String,
    /// Vehicle brand name.
    pub name: String,
    /// Price in fen.
    pub price_fen: i64,
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
#[derive(Clone)]
pub struct Credentials {
    /// Nonsecret Wapi application identifier.
    pub app_id: String,
    /// Secret used only to sign the request.
    pub app_secret: SecretString,
}
/// Bound vehicle brand query. Construct once and share the HTTP client across calls.
pub struct ListVehicleBrandsOperation {
    credentials: Arc<dyn CredentialProvider<Credentials>>,
    http: ProviderHttpClient,
    clock: Arc<dyn Fn() -> SystemTime + Send + Sync>,
    max_response_bytes: usize,
}

impl ListVehicleBrandsOperation {
    /// Bind a Dex Web project connection, including its API settings and per-call credential resolution.
    pub fn from_project(
        project: &sdk::projectconfig::LoadedProject,
        connection_name: &str,
    ) -> Result<Self, Box<dyn std::error::Error>> {
        let key = sdk::projectconfig::ConnectionKey::new("wapi", connection_name);
        let entry = project
            .connection_configuration(&key)
            .ok_or("Wapi connection is missing")?;
        if entry.provider != "wapi" {
            return Err("Wapi project connection provider differs".into());
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
                            .ok_or("invalid Wapi timeout setting")? as u64,
                    )
                }
                "maxResponseBytes" => {
                    config.max_response_bytes = value
                        .as_u64()
                        .and_then(|n| usize::try_from(n).ok())
                        .ok_or("invalid Wapi response size setting")?
                }
                _ => return Err("unknown Wapi setting".into()),
            }
        }
        let provider = sdk::projectconfig::ProjectCredentialProvider::new(
            project.connections().clone(),
            key,
            Arc::new(|fields| {
                if fields.len() != 2 {
                    return Err("Invalid Wapi credential fields".into());
                }
                let app_id = fields
                    .get("appID")
                    .and_then(serde_json::Value::as_str)
                    .ok_or("Missing Wapi application ID")?;
                let app_secret = fields
                    .get("appSecret")
                    .and_then(serde_json::Value::as_str)
                    .ok_or("Missing Wapi application secret")?;
                Ok(Credentials {
                    app_id: app_id.into(),
                    app_secret: SecretString::new(app_secret),
                })
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
        Self::with_http_client(
            credentials,
            http,
            config.max_response_bytes,
            Arc::new(SystemTime::now),
        )
    }
    /// Bind the transport, byte limit and clock used to sign each request.
    pub fn with_http_client(
        credentials: Arc<dyn CredentialProvider<Credentials>>,
        http: ProviderHttpClient,
        max_response_bytes: usize,
        clock: Arc<dyn Fn() -> SystemTime + Send + Sync>,
    ) -> Result<Self, ValidationError> {
        if max_response_bytes == 0 || max_response_bytes > 16 << 20 {
            return Err(ValidationError::new(
                "Wapi response size limit must be positive",
            ));
        }
        Ok(Self {
            clock,
            credentials,
            http,
            max_response_bytes,
        })
    }
}

impl Query for ListVehicleBrandsOperation {
    type Input = ListVehicleBrandsInput;
    type Output = ListVehicleBrandsOutput;

    fn definition(&self) -> QueryDefinition {
        generated_definitions::ListVehicleBrands::definition()
    }

    fn invoke(&self, call: &Call<'_>, _: &Self::Input) -> QueryAttempt<Self::Output> {
        let token = match self.credentials.resolve(call) {
            Ok(token) if valid_credentials(&token) => token,
            _ => {
                return rejected(
                    "defect",
                    FailureKind::Authentication,
                    "Invalid Wapi application credentials",
                );
            }
        };
        let request = ProviderHttpRequest::new("GET", signed_endpoint(&token, (self.clock)()))
            .header("Accept", "application/json");
        self.read_response(&request)
    }
}
impl ListVehicleBrandsOperation {
    fn read_response(
        &self,
        request: &ProviderHttpRequest,
    ) -> QueryAttempt<ListVehicleBrandsOutput> {
        let mut response = match self.http.send_traced(request) {
            Ok(response) => response,
            Err(_) => return retry(FailureKind::Availability, "Wapi request failed", 0.0),
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
                "Wapi temporarily unavailable",
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
                "Wapi rejected the request",
            );
        }
        let body = match read_bounded_body(&mut response, self.max_response_bytes) {
            Ok(body) => body,
            Err(ProviderHttpError::BodyTooLarge(_)) => {
                return rejected(
                    "invalidResponse",
                    FailureKind::ResponseTooLarge,
                    "Wapi response exceeded the configured limit",
                );
            }
            Err(_) => {
                return retry(FailureKind::Availability, "Wapi response interrupted", 0.0);
            }
        };
        decode_brands(&body).unwrap_or_else(invalid)
    }
}
/// Bind the vehicle brand operation to its branch targets and validate the Step configuration.
pub fn new_list_vehicle_brands_step<Input>(
    config: QueryStepConfig<Input, ListVehicleBrandsOperation>,
) -> Result<QueryStep<Input, ListVehicleBrandsOperation>, ValidationError> {
    if config.connection.provider != r"wapi" {
        return Err(ValidationError::new("Wapi connection provider is invalid"));
    }
    QueryStep::new(config)
}

fn valid_credentials(credentials: &Credentials) -> bool {
    !credentials.app_id.is_empty()
        && credentials.app_id.len() <= 20
        && credentials.app_id.bytes().all(|b| b.is_ascii_digit())
        && credentials.app_secret.reveal().len() == 32
        && credentials
            .app_secret
            .reveal()
            .bytes()
            .all(|b| b.is_ascii_alphanumeric())
}
fn signed_endpoint(credentials: &Credentials, time: SystemTime) -> String {
    use md5::{Digest, Md5};
    let timestamp = match time.duration_since(SystemTime::UNIX_EPOCH) {
        Ok(elapsed) => i128::from(elapsed.as_secs()),
        Err(before) => {
            -i128::from(before.duration().as_secs())
                - i128::from(before.duration().subsec_nanos() > 0)
        }
    };
    let app_id = &credentials.app_id;
    let signature = Md5::new()
        .chain_update(format!("appid{app_id}formatjsontime{timestamp}"))
        .chain_update(credentials.app_secret.reveal())
        .finalize();
    let signature = u128::from_be_bytes(signature.into());
    format!(
        "https://uaqy.api.storeapi.net/pyi/170/324?appid={app_id}&format=json&sign={signature:032x}&time={timestamp}"
    )
}
fn invalid() -> QueryAttempt<ListVehicleBrandsOutput> {
    rejected(
        "invalidResponse",
        FailureKind::Protocol,
        "Invalid Wapi response",
    )
}
#[derive(Deserialize)]
#[serde(try_from = "WireFields")]
struct WireItem {
    id: Option<String>,
    name: Option<String>,
    price_fen: Option<i64>,
}
impl TryFrom<WireFields> for WireItem {
    type Error = serde_json::Error;
    fn try_from(fields: WireFields) -> Result<Self, Self::Error> {
        Ok(Self {
            id: fields.optional("ppid")?,
            name: fields.optional("pp_name")?,
            price_fen: fields.optional("pp_price")?,
        })
    }
}
#[derive(Deserialize)]
#[serde(try_from = "WireFields")]
struct DirectoryResponse {
    items: Option<Vec<Option<WireItem>>>,
    code: Option<String>,
}
impl TryFrom<WireFields> for DirectoryResponse {
    type Error = serde_json::Error;
    fn try_from(fields: WireFields) -> Result<Self, Self::Error> {
        Ok(Self {
            items: fields.optional("retdata")?,
            code: fields.optional("codeid")?,
        })
    }
}
fn decode_brands(body: &[u8]) -> Option<QueryAttempt<ListVehicleBrandsOutput>> {
    let page: DirectoryResponse = serde_json::from_slice(body).ok()?;
    match page.code?.as_str() {
        "" => return None,
        "10000" => {}
        "10021" | "10014" => {
            return Some(retry(
                FailureKind::Availability,
                "Wapi is temporarily unavailable",
                0.0,
            ));
        }
        _ => {
            return Some(rejected(
                "providerRejected",
                FailureKind::ProviderRejection,
                "Wapi rejected the query",
            ));
        }
    }
    let items = page.items?;
    if items.len() > 5000 {
        return None;
    }
    let total = items.len() as i64;
    let mut brands = Vec::new();
    for item in items {
        let item = item?;
        let id = item.id?;
        let name = item.name?;
        let price_fen = item.price_fen.unwrap_or_default();
        if id.is_empty() || id.len() > 50 || name.is_empty() || name.len() > 256 || price_fen < 0 {
            return None;
        }
        if brands.len() == 200 {
            continue;
        }
        brands.push(VehicleBrandPrice {
            id,
            name,
            price_fen,
        });
    }
    Some(QueryAttempt::branch(
        "found",
        ListVehicleBrandsOutput { brands, total },
        Receipt::default(),
        None,
    ))
}
fn rejected(
    branch: &str,
    kind: FailureKind,
    message: &str,
) -> QueryAttempt<ListVehicleBrandsOutput> {
    QueryAttempt::branch(
        branch,
        ListVehicleBrandsOutput::default(),
        Receipt::default(),
        Some(Failure::new(kind, r"wapi", "listVehicleBrands", message)),
    )
}

fn retry(kind: FailureKind, message: &str, delay: f64) -> QueryAttempt<ListVehicleBrandsOutput> {
    QueryAttempt::retry(
        Failure::new(kind, r"wapi", "listVehicleBrands", message),
        delay,
    )
}
