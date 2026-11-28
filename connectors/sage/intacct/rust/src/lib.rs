// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Sage Intacct Accounts Receivable queries for Dex Flows.
mod credentials;
#[rustfmt::skip]
mod generated_definitions;
mod invoices;
mod provider_fields;
pub use credentials::{Credentials, project_credentials};
pub use invoices::{GetInvoiceInput, InvoiceList, InvoiceSummary, ListInvoicesInput};

use dex_connectors_sdk as sdk;
use sdk::oauthtoken::ManifestCredentialRefreshDriver;
use sdk::providerhttp::{
    self, AddressPolicy, ProviderHttpClient, ProviderHttpError, ProviderHttpRequest,
};
use sdk::{
    Call, CredentialProvider, Failure, FailureKind, Query, QueryAttempt, QueryDefinition,
    QueryStep, QueryStepConfig, Receipt, ValidationError,
};
use std::sync::Arc;
use std::time::{Duration, SystemTime};

/// Endpoint, per-request timeout and maximum body size used by both queries.
pub struct Config {
    /// Sage Intacct REST base URL.
    pub base_url: String,
    /// Maximum duration per API read. Defaults to 15 seconds.
    pub timeout: Duration,
    /// Maximum bytes in a successful response. Defaults to 1 MiB.
    pub max_response_bytes: usize,
}
impl Default for Config {
    fn default() -> Self {
        Self {
            base_url: "https://api.intacct.com".to_owned(),
            timeout: Duration::from_secs(15),
            max_response_bytes: 1 << 20,
        }
    }
}
/// A validated invoices API binding, shared by its operations. Construction performs no network I/O.
pub struct Client {
    endpoint: String,
    max_response_bytes: usize,
    credentials: Arc<dyn CredentialProvider<Credentials>>,
    http: ProviderHttpClient,
    refresh: ManifestCredentialRefreshDriver<Credentials>,
}
impl Client {
    /// Bind a Dex Web project connection, including its API settings and coordinated token renewal.
    pub fn from_project(
        project: &sdk::projectconfig::LoadedProject,
        connection_name: &str,
    ) -> Result<Self, Box<dyn std::error::Error>> {
        let key = sdk::projectconfig::ConnectionKey::new("sage-intacct", connection_name);
        let entry = project
            .connection_configuration(&key)
            .ok_or("Sage Intacct connection is missing")?;
        if entry.provider != "sage-intacct" {
            return Err("Sage Intacct project connection provider differs".into());
        }
        let mut config = Config::default();
        let settings = entry.configuration.to_json_object()?;
        for (name, value) in settings {
            match name.as_str() {
                "baseUrl" | "timeout" | "maxResponseBytes" if value.as_str() == Some("") => {}
                "baseUrl" => {
                    config.base_url = value
                        .as_str()
                        .ok_or("invalid Sage Intacct API URL setting")?
                        .into()
                }
                "timeout" => {
                    config.timeout = Duration::from_nanos(
                        value
                            .as_i64()
                            .filter(|n| *n > 0)
                            .ok_or("invalid Sage Intacct timeout setting")?
                            as u64,
                    )
                }
                "maxResponseBytes" => {
                    config.max_response_bytes = value
                        .as_u64()
                        .and_then(|n| usize::try_from(n).ok())
                        .ok_or("invalid Sage Intacct response size setting")?
                }
                _ => return Err("unknown Sage Intacct setting".into()),
            }
        }
        Ok(Self::new(
            config,
            Arc::new(project_credentials(project, connection_name)?),
        )?)
    }
    /// Construct a client with a redirect-safe transport and renewable credentials.
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
        Self::with_http_client(
            &config.base_url,
            config.max_response_bytes,
            credentials,
            http,
        )
    }
    /// Supply the transport used for both token exchanges and bounded invoices API reads.
    pub fn with_http_client(
        base_url: &str,
        max_response_bytes: usize,
        credentials: Arc<dyn CredentialProvider<Credentials>>,
        http: ProviderHttpClient,
    ) -> Result<Self, ValidationError> {
        let endpoint = providerhttp::validate_base_url(base_url)?;
        if max_response_bytes == 0 {
            return Err(ValidationError::new(
                "Sage Intacct response size limit must be positive",
            ));
        }
        let declaration =
            serde_json::from_str(include_str!("generated_refresh_authorization.json"))
                .expect("generated Sage Intacct authorization");
        let refresh = ManifestCredentialRefreshDriver::new(
            "SageIntacct",
            &declaration,
            http.clone(),
            Default::default(),
        )?;
        Ok(Self {
            endpoint,
            max_response_bytes,
            credentials,
            http,
            refresh,
        })
    }
    /// Bind the single-invoice lookup.
    pub fn get_invoice(self: &Arc<Self>) -> GetInvoiceOperation {
        GetInvoiceOperation(self.clone())
    }
    /// Bind the first-page invoice query.
    pub fn list_invoices(self: &Arc<Self>) -> ListInvoicesOperation {
        ListInvoicesOperation(self.clone())
    }
    fn request<Output: Default>(
        &self,
        call: &Call<'_>,
        suffix: &str,
        operation: &str,
    ) -> Result<Vec<u8>, Box<QueryAttempt<Output>>> {
        let failure = |kind, message| Failure::new(kind, "sage-intacct", operation, message);
        let branch = |id, kind, message| {
            Box::new(QueryAttempt::branch(
                id,
                Output::default(),
                Receipt::default(),
                Some(failure(kind, message)),
            ))
        };
        let retry =
            |kind, message, delay| Box::new(QueryAttempt::retry(failure(kind, message), delay));
        let credentials = sdk::resolve_credential(&*self.credentials, call, &self.refresh)
            .map_err(|error| {
                if sdk::is_reauthorization_required(&*error) {
                    branch(
                        "defect",
                        FailureKind::Authentication,
                        "Sage Intacct connection needs authorization",
                    )
                } else {
                    retry(
                        FailureKind::Transport,
                        "Sage Intacct credential refresh failed",
                        0.0,
                    )
                }
            })?;
        let token = credentials.access_token.reveal();
        if !providerhttp::is_header_safe_credential(token) {
            return Err(branch(
                "defect",
                FailureKind::Authentication,
                "Sage Intacct access token is unavailable",
            ));
        }
        let request = ProviderHttpRequest::new(
            "GET",
            format!(
                "{}/ia/api/v1/objects/accounts-receivable/invoice{}",
                self.endpoint, suffix
            ),
        )
        .header("Accept", "application/json")
        .header("Authorization", format!("Bearer {token}"));
        let mut response = self
            .http
            .send_traced(&request)
            .map_err(|_| retry(FailureKind::Transport, "Sage Intacct request failed", 0.0))?;
        let status = response.status();
        let delay = providerhttp::parse_retry_after(
            response.headers().get("Retry-After").unwrap_or(""),
            SystemTime::now(),
        );
        let body = providerhttp::read_bounded_body(&mut response, self.max_response_bytes);
        if body
            .as_ref()
            .is_ok_and(|bytes| contains_access_token(bytes, token))
        {
            return Err(branch(
                "defect",
                FailureKind::Authentication,
                "Sage Intacct response contained a credential",
            ));
        }
        match status {
            429 => {
                return Err(retry(
                    FailureKind::RateLimit,
                    "Sage Intacct rate limited the request",
                    delay,
                ));
            }
            408 | 500.. => {
                return Err(retry(
                    FailureKind::Availability,
                    "Sage Intacct is temporarily unavailable",
                    delay,
                ));
            }
            404 if operation == "getInvoice" => {
                return Err(branch(
                    "notFound",
                    FailureKind::NotFound,
                    "Sage Intacct invoice was not found",
                ));
            }
            200 => {}
            _ => {
                return Err(Box::new(QueryAttempt::branch(
                    "providerRejected",
                    Output::default(),
                    Receipt::default(),
                    Some(Failure::new(
                        FailureKind::ProviderRejection,
                        "sage-intacct",
                        operation,
                        format!("Sage Intacct rejected HTTP {status}"),
                    )),
                )));
            }
        }
        body.map_err(|error| match error {
            ProviderHttpError::BodyTooLarge(_) => branch(
                "invalidResponse",
                FailureKind::ResponseTooLarge,
                "Sage Intacct response exceeded the configured limit",
            ),
            _ => retry(
                FailureKind::Transport,
                "Sage Intacct response was interrupted",
                0.0,
            ),
        })
    }
}
fn contains_access_token(mut body: &[u8], token: &str) -> bool {
    if body
        .windows(token.len())
        .any(|window| window == token.as_bytes())
    {
        return true;
    }
    if !body.contains(&b'\\') {
        return false;
    }
    while let Some(start) = body.iter().position(|&byte| byte == b'"') {
        let mut strings =
            serde_json::Deserializer::from_slice(&body[start..]).into_iter::<String>();
        let Some(Ok(value)) = strings.next() else {
            return false;
        };
        if value.contains(token) {
            return true;
        }
        body = &body[start + strings.byte_offset()..];
    }
    false
}

fn rejected<Output: Default>(
    operation: &str,
    branch: &str,
    kind: FailureKind,
    message: &str,
) -> QueryAttempt<Output> {
    QueryAttempt::branch(
        branch,
        Output::default(),
        Receipt::default(),
        Some(Failure::new(kind, "sage-intacct", operation, message)),
    )
}
/// Reads the system key and displayed document ID of one invoice.
pub struct GetInvoiceOperation(Arc<Client>);
impl Query for GetInvoiceOperation {
    type Input = GetInvoiceInput;
    type Output = InvoiceSummary;
    fn definition(&self) -> QueryDefinition {
        generated_definitions::GetInvoice::definition()
    }
    fn invoke(&self, call: &Call<'_>, input: &Self::Input) -> QueryAttempt<Self::Output> {
        if !invoices::valid_key(&input.invoice_key) {
            return rejected(
                "getInvoice",
                "defect",
                FailureKind::Validation,
                "Invalid Sage Intacct invoice key",
            );
        }
        let body = match self
            .0
            .request(call, &format!("/{}", input.invoice_key), "getInvoice")
        {
            Ok(body) => body,
            Err(attempt) => return *attempt,
        };
        match invoices::read_invoice(&body, &input.invoice_key) {
            Some(invoice) => QueryAttempt::branch("found", invoice, Receipt::default(), None),
            None => rejected(
                "getInvoice",
                "invalidResponse",
                FailureKind::Protocol,
                "Sage Intacct returned invalid invoice JSON",
            ),
        }
    }
}
/// Lists the first page of invoice identities in provider order.
pub struct ListInvoicesOperation(Arc<Client>);
impl Query for ListInvoicesOperation {
    type Input = ListInvoicesInput;
    type Output = InvoiceList;
    fn definition(&self) -> QueryDefinition {
        generated_definitions::ListInvoices::definition()
    }
    fn invoke(&self, call: &Call<'_>, _input: &Self::Input) -> QueryAttempt<Self::Output> {
        let body = match self.0.request(call, "", "listInvoices") {
            Ok(body) => body,
            Err(attempt) => return *attempt,
        };
        match invoices::read_page(&body) {
            Some(page) => QueryAttempt::branch("found", page, Receipt::default(), None),
            None => rejected(
                "listInvoices",
                "invalidResponse",
                FailureKind::Protocol,
                "Sage Intacct returned invalid invoice list JSON",
            ),
        }
    }
}
/// Bind the GetInvoice query to its configured Dex Step and branch targets.
pub fn new_get_invoice_step<Input>(
    config: QueryStepConfig<Input, GetInvoiceOperation>,
) -> Result<QueryStep<Input, GetInvoiceOperation>, ValidationError> {
    if config.connection.provider != "sage-intacct" {
        return Err(ValidationError::new(
            "Sage Intacct connection provider is invalid",
        ));
    }
    QueryStep::new(config)
}
/// Bind the ListInvoices query to its configured Dex Step and branch targets.
pub fn new_list_invoices_step<Input>(
    config: QueryStepConfig<Input, ListInvoicesOperation>,
) -> Result<QueryStep<Input, ListInvoicesOperation>, ValidationError> {
    if config.connection.provider != "sage-intacct" {
        return Err(ValidationError::new(
            "Sage Intacct connection provider is invalid",
        ));
    }
    QueryStep::new(config)
}
