// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Provider-neutral OAuth token exchanges and deterministic refresh decisions.
//!
//! [`TokenEndpoint`] performs one bounded, redirect-free token exchange.
//! [`ManifestCredentialRefreshDriver`] runs the grants a connector manifest declares and
//! implements [`crate::CredentialRefreshDriver`].
//!
//! ```no_run
//! use std::time::Duration;
//! use dex_connectors_sdk::SecretString;
//! use dex_connectors_sdk::oauthtoken::{
//!     ClientAuthenticationMethod, ClientCredentials, TokenEndpoint,
//! };
//! use dex_connectors_sdk::providerhttp::{AddressPolicy, ProviderHttpClient};
//!
//! # fn main() -> Result<(), Box<dyn std::error::Error>> {
//! let client = ProviderHttpClient::new(Duration::from_secs(30), AddressPolicy::PublicHttps)?;
//! let mut endpoint = TokenEndpoint::new("Example", "https://oauth.example/token", client);
//! endpoint.terminal_error_codes = vec!["invalid_grant".to_string()];
//! let credentials = ClientCredentials::new(
//!     "client-id",
//!     SecretString::new("client-secret"),
//!     ClientAuthenticationMethod::ClientSecretPost,
//! );
//! let token = endpoint.exchange_refresh_token(&credentials, &SecretString::new("refresh"))?;
//! assert!(!token.access_token.reveal().is_empty());
//! # Ok(())
//! # }
//! ```

mod manifest_refresh;

use std::collections::{BTreeMap, BTreeSet};
use std::fmt;
use std::time::Duration;

use base64::Engine;
use base64::engine::general_purpose::{STANDARD, URL_SAFE_NO_PAD};
use serde_json::{Map as JsonMap, Value as JsonValue};

pub use manifest_refresh::{ManifestCredential, ManifestCredentialRefreshDriver};

use crate::credentials::{ReauthorizationRequiredError, SecretString};
use crate::errors::ValidationError;
use crate::providerhttp::{
    AddressRefusal, ProviderHttpClient, ProviderHttpError, ProviderHttpRequest, read_bounded_body,
};
use crate::timestamps::Rfc3339NanoTimestamp;

/// Largest token response body read.
pub const MAX_TOKEN_RESPONSE_BYTES: usize = 1 << 20;
/// Largest refresh skew: five minutes before expiry.
pub const REFRESH_SKEW: Duration = Duration::from_secs(5 * 60);

const STANDARD_RESPONSE_FIELDS: [&str; 10] = [
    "access_token",
    "refresh_token",
    "expires_in",
    "refresh_token_expires_in",
    "scope",
    "token_type",
    "error",
    "error_description",
    "error_uri",
    "id_token",
];
const GRANT_REQUEST_FIELDS: [&str; 7] = [
    "grant_type",
    "refresh_token",
    "client_id",
    "client_secret",
    "assertion",
    "client_assertion",
    "client_assertion_type",
];

/// Chooses whether a credential without expiry is refreshable or permanent.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum MissingExpiryRule {
    /// Refresh a token whose expiry is unknown.
    RefreshWhenExpiryMissing,
    /// Keep a token whose expiry is unknown.
    KeepWhenExpiryMissing,
}

fn lifetime_nanoseconds(
    issued_at: Option<&Rfc3339NanoTimestamp>,
    expires_at: Option<&Rfc3339NanoTimestamp>,
) -> Option<i128> {
    let lifetime = expires_at?.unix_nanoseconds() - issued_at?.unix_nanoseconds();
    (lifetime > 0).then_some(lifetime)
}

fn duration_from_nanoseconds(nanoseconds: i128) -> Duration {
    Duration::from_nanos(u64::try_from(nanoseconds.max(0)).unwrap_or(u64::MAX))
}

/// Returns the smaller of five minutes and one quarter of the token lifetime.
pub fn proportional_refresh_skew(
    issued_at: Option<&Rfc3339NanoTimestamp>,
    expires_at: Option<&Rfc3339NanoTimestamp>,
) -> Duration {
    lifetime_nanoseconds(issued_at, expires_at).map_or(REFRESH_SKEW, |lifetime| {
        REFRESH_SKEW.min(duration_from_nanoseconds(lifetime / 4))
    })
}

/// Bounds repeated forced refreshes to one minute or a quarter lifetime.
pub fn forced_refresh_floor(
    issued_at: Option<&Rfc3339NanoTimestamp>,
    expires_at: Option<&Rfc3339NanoTimestamp>,
) -> Duration {
    let minute = Duration::from_secs(60);
    lifetime_nanoseconds(issued_at, expires_at).map_or(minute, |lifetime| {
        minute.min(duration_from_nanoseconds(lifetime / 4))
    })
}

/// Decides refresh from token presence, optional expiry, and the proportional skew.
pub fn is_refresh_required(
    has_access_token: bool,
    expires_at: Option<&Rfc3339NanoTimestamp>,
    now: &Rfc3339NanoTimestamp,
    rule: MissingExpiryRule,
    issued_at: Option<&Rfc3339NanoTimestamp>,
) -> bool {
    if !has_access_token {
        return true;
    }
    let Some(expires_at) = expires_at else {
        return rule != MissingExpiryRule::KeepWhenExpiryMissing;
    };
    let skew = i128::try_from(proportional_refresh_skew(issued_at, Some(expires_at)).as_nanos())
        .unwrap_or(i128::MAX);
    now.unix_nanoseconds() >= expires_at.unix_nanoseconds() - skew
}

fn scope_set(returned_scope: &str) -> BTreeSet<&str> {
    returned_scope
        .split([' ', ','])
        .filter(|scope| !scope.is_empty())
        .collect()
}

/// Accepts all required scopes in a space- or comma-separated OAuth value.
pub fn has_all_scopes(returned_scope: &str, required_scopes: &[&str]) -> bool {
    let granted = scope_set(returned_scope);
    required_scopes.iter().all(|scope| granted.contains(scope))
}

/// Requires equal scope sets, accepting spaces or commas as separators.
pub fn has_exact_scopes(returned_scope: &str, required_scopes: &[&str]) -> bool {
    scope_set(returned_scope) == required_scopes.iter().copied().collect()
}

/// OAuth token endpoint client authentication method.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ClientAuthenticationMethod {
    /// `client_secret_post`: client ID and secret in the request body.
    ClientSecretPost,
    /// `client_secret_basic`: HTTP Basic authentication.
    ClientSecretBasic,
    /// `none`: a public client sends only its ID.
    PublicClient,
}

impl ClientAuthenticationMethod {
    /// Parses the manifest name, such as `client_secret_basic`.
    pub fn from_wire_name(value: &str) -> Option<Self> {
        match value {
            "client_secret_post" => Some(ClientAuthenticationMethod::ClientSecretPost),
            "client_secret_basic" => Some(ClientAuthenticationMethod::ClientSecretBasic),
            "none" => Some(ClientAuthenticationMethod::PublicClient),
            _ => None,
        }
    }
}

/// OAuth client identity and redacted secret used for one token grant.
#[derive(Clone, Debug)]
pub struct ClientCredentials {
    /// Client ID.
    pub id: String,
    /// Client secret.
    pub secret: SecretString,
    /// Token endpoint authentication.
    pub authentication_method: ClientAuthenticationMethod,
}

impl ClientCredentials {
    /// Creates client credentials.
    pub fn new(
        id: impl Into<String>,
        secret: SecretString,
        authentication_method: ClientAuthenticationMethod,
    ) -> Self {
        Self {
            id: id.into(),
            secret,
            authentication_method,
        }
    }
}

/// Complete bounded OAuth token response with redacted token material.
#[derive(Clone)]
pub struct TokenResponse {
    /// Access token.
    pub access_token: SecretString,
    /// Rotated refresh token; empty when the provider returned none.
    pub refresh_token: SecretString,
    /// Access token lifetime; zero when the provider omitted `expires_in`.
    pub expires_in: Duration,
    /// Refresh token lifetime; zero when absent.
    pub refresh_token_expires_in: Duration,
    /// Returned scope text.
    pub scope: String,
    /// Returned token type.
    pub token_type: String,
    /// Non-standard response fields the endpoint was asked to retain.
    pub retained_fields: BTreeMap<String, JsonValue>,
    response_fields: BTreeMap<String, SecretString>,
}

impl TokenResponse {
    /// Uses rotated refresh material when present, otherwise retains `prior`.
    pub fn next_refresh_token(&self, prior: &SecretString) -> SecretString {
        if self.refresh_token.reveal().is_empty() {
            prior.clone()
        } else {
            self.refresh_token.clone()
        }
    }

    /// Reads a manifest-mapped string by dotted path, such as `authed_user.access_token`.
    pub fn mapped_response_string(&self, path: &str) -> Option<&str> {
        self.response_fields.get(path).map(SecretString::reveal)
    }
}

impl fmt::Debug for TokenResponse {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("TokenResponse")
            .field("expires_in", &self.expires_in)
            .field("scope", &self.scope)
            .field("token_type", &self.token_type)
            .finish_non_exhaustive()
    }
}

/// Failure of one token exchange.
#[derive(Debug)]
pub enum TokenExchangeError {
    /// Missing grant material or a declared terminal provider error.
    ReauthorizationRequired(ReauthorizationRequiredError),
    /// Invalid endpoint configuration; no request was sent.
    Configuration(ValidationError),
    /// The address policy refused the endpoint before dialing.
    AddressRefusal(AddressRefusal),
    /// Transport failure or an undeclared provider error; retryable.
    Unavailable(String),
    /// The provider response violated the token protocol; retryable.
    InvalidResponse(String),
}

impl fmt::Display for TokenExchangeError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            TokenExchangeError::ReauthorizationRequired(error) => error.fmt(formatter),
            TokenExchangeError::Configuration(error) => error.fmt(formatter),
            TokenExchangeError::AddressRefusal(error) => error.fmt(formatter),
            TokenExchangeError::Unavailable(message)
            | TokenExchangeError::InvalidResponse(message) => formatter.write_str(message),
        }
    }
}

impl std::error::Error for TokenExchangeError {
    fn source(&self) -> Option<&(dyn std::error::Error + 'static)> {
        match self {
            TokenExchangeError::ReauthorizationRequired(error) => Some(error),
            TokenExchangeError::Configuration(error) => Some(error),
            TokenExchangeError::AddressRefusal(error) => Some(error),
            TokenExchangeError::Unavailable(_) | TokenExchangeError::InvalidResponse(_) => None,
        }
    }
}

fn reauthorization() -> TokenExchangeError {
    TokenExchangeError::ReauthorizationRequired(ReauthorizationRequiredError::default())
}

/// OAuth token endpoint and connector-owned response rules.
///
/// Redirects are never followed, and declared terminal codes below HTTP 500 require
/// interactive reauthorization. Fields are public configuration set after
/// [`TokenEndpoint::new`].
#[derive(Clone, Debug)]
pub struct TokenEndpoint {
    /// Provider display name used in safe error messages.
    pub provider_name: String,
    /// Token endpoint URL; HTTPS unless `allows_plain_http`.
    pub url: String,
    /// HTTP client owned by the caller.
    pub http_client: ProviderHttpClient,
    /// Error codes that require reauthorization.
    pub terminal_error_codes: Vec<String>,
    /// Accepted `token_type` values, compared case-insensitively; empty means `Bearer`.
    pub accepted_token_types: Vec<String>,
    /// Whether a missing `token_type` is accepted.
    pub accepts_missing_token_type: bool,
    /// Whether a missing `expires_in` is accepted.
    pub accepts_missing_expires_in: bool,
    /// Non-standard response fields to retain.
    pub retained_response_fields: Vec<String>,
    /// Whether to send a JSON body instead of a form.
    pub uses_json_request_body: bool,
    /// Whether an `http://` endpoint is allowed.
    pub allows_plain_http: bool,
    /// Extra request parameters; grant parameters cannot be replaced.
    pub additional_request_parameters: BTreeMap<String, String>,
    /// Exchange timeout; `None` uses the client timeout.
    pub exchange_timeout: Option<Duration>,
}

impl TokenEndpoint {
    /// Creates an endpoint with default rules.
    pub fn new(
        provider_name: impl Into<String>,
        url: impl Into<String>,
        http_client: ProviderHttpClient,
    ) -> Self {
        Self {
            provider_name: provider_name.into(),
            url: url.into(),
            http_client,
            terminal_error_codes: Vec::new(),
            accepted_token_types: Vec::new(),
            accepts_missing_token_type: false,
            accepts_missing_expires_in: false,
            retained_response_fields: Vec::new(),
            uses_json_request_body: false,
            allows_plain_http: false,
            additional_request_parameters: BTreeMap::new(),
            exchange_timeout: None,
        }
    }

    /// Exchanges a refresh token; missing grant material requires reauthorization.
    ///
    /// # Errors
    ///
    /// Returns [`TokenExchangeError`] classified for reauthorization, configuration, address
    /// refusal, or retry.
    pub fn exchange_refresh_token(
        &self,
        client: &ClientCredentials,
        refresh_token: &SecretString,
    ) -> Result<TokenResponse, TokenExchangeError> {
        if client.id.is_empty() || refresh_token.reveal().is_empty() {
            return Err(reauthorization());
        }
        let mut parameters = BTreeMap::from([
            ("grant_type".to_string(), "refresh_token".to_string()),
            (
                "refresh_token".to_string(),
                refresh_token.reveal().to_string(),
            ),
        ]);
        let basic_client = apply_client_authentication(&mut parameters, client, true)?;
        self.exchange(parameters, basic_client)
    }

    /// Requests a client-credentials grant with optional space-separated scopes.
    ///
    /// # Errors
    ///
    /// Returns [`TokenExchangeError`] as for [`Self::exchange_refresh_token`].
    pub fn exchange_client_credentials(
        &self,
        client: &ClientCredentials,
        scopes: &[String],
    ) -> Result<TokenResponse, TokenExchangeError> {
        if client.id.is_empty() || client.secret.reveal().is_empty() {
            return Err(reauthorization());
        }
        let mut parameters =
            BTreeMap::from([("grant_type".to_string(), "client_credentials".to_string())]);
        if !scopes.is_empty() {
            parameters.insert("scope".to_string(), scopes.join(" "));
        }
        let basic_client = apply_client_authentication(&mut parameters, client, false)?;
        self.exchange(parameters, basic_client)
    }

    /// Exchanges a signed RFC 7523 JWT-bearer assertion without client material.
    ///
    /// # Errors
    ///
    /// Returns [`TokenExchangeError`] as for [`Self::exchange_refresh_token`].
    pub fn exchange_jwt_bearer_assertion(
        &self,
        assertion: &SecretString,
    ) -> Result<TokenResponse, TokenExchangeError> {
        if assertion.reveal().is_empty() {
            return Err(reauthorization());
        }
        self.exchange(
            BTreeMap::from([
                (
                    "grant_type".to_string(),
                    "urn:ietf:params:oauth:grant-type:jwt-bearer".to_string(),
                ),
                ("assertion".to_string(), assertion.reveal().to_string()),
            ]),
            None,
        )
    }

    fn validate_configuration(&self) -> Result<(), TokenExchangeError> {
        let invalid_url = || {
            TokenExchangeError::Configuration(ValidationError::new(
                "OAuth token endpoint URL is invalid",
            ))
        };
        let lowercase = self.url.to_ascii_lowercase();
        let is_https = lowercase.starts_with("https://");
        let is_http = self.allows_plain_http && lowercase.starts_with("http://");
        if !(is_https || is_http) {
            return Err(invalid_url());
        }
        let rest = &self.url[if is_https { 8 } else { 7 }..];
        let authority = rest.split(['/', '?', '#']).next().unwrap_or_default();
        let host = authority
            .rsplit_once(':')
            .map_or(authority, |(host, port)| {
                if port.bytes().all(|byte| byte.is_ascii_digit()) && !authority.ends_with(']') {
                    host
                } else {
                    authority
                }
            });
        if authority.contains('@')
            || host.is_empty()
            || host == "[]"
            || !self.url.bytes().all(|byte| (33..=126).contains(&byte))
        {
            return Err(invalid_url());
        }
        if self
            .retained_response_fields
            .iter()
            .any(|name| STANDARD_RESPONSE_FIELDS.contains(&name.as_str()))
        {
            return Err(TokenExchangeError::Configuration(ValidationError::new(
                "OAuth token endpoint cannot retain standard token fields",
            )));
        }
        if self
            .additional_request_parameters
            .keys()
            .any(|name| GRANT_REQUEST_FIELDS.contains(&name.as_str()))
        {
            return Err(TokenExchangeError::Configuration(ValidationError::new(
                "OAuth token endpoint cannot replace grant parameters",
            )));
        }
        Ok(())
    }

    fn exchange(
        &self,
        mut parameters: BTreeMap<String, String>,
        basic_client: Option<&ClientCredentials>,
    ) -> Result<TokenResponse, TokenExchangeError> {
        self.validate_configuration()?;
        parameters.extend(self.additional_request_parameters.clone());
        let mut request =
            ProviderHttpRequest::new("POST", self.url.clone()).header("Accept", "application/json");
        if self.uses_json_request_body {
            let body = serde_json::to_vec(&parameters).map_err(|_| {
                TokenExchangeError::Configuration(ValidationError::new(
                    "OAuth token request cannot be encoded",
                ))
            })?;
            request = request
                .header("Content-Type", "application/json")
                .body(body);
        } else {
            request = request
                .header("Content-Type", "application/x-www-form-urlencoded")
                .body(form_urlencode(&parameters));
        }
        if let Some(client) = basic_client {
            let encoded = STANDARD.encode(format!("{}:{}", client.id, client.secret.reveal()));
            request = request.header("Authorization", format!("Basic {encoded}"));
        }
        let unavailable = || {
            TokenExchangeError::Unavailable(format!(
                "{} token endpoint is unavailable",
                self.provider_name
            ))
        };
        let sent = match self.exchange_timeout {
            Some(timeout) => self.http_client.send_traced_within(&request, timeout),
            None => self.http_client.send_traced(&request),
        };
        let mut response = match sent {
            Ok(response) => response,
            Err(ProviderHttpError::AddressRefusal(refusal)) => {
                return Err(TokenExchangeError::AddressRefusal(refusal));
            }
            Err(_) => return Err(unavailable()),
        };
        let status = response.status();
        let contents = read_bounded_body(&mut response, MAX_TOKEN_RESPONSE_BYTES)
            .map_err(|_| unavailable())?;
        let is_success = (200..300).contains(&status);
        let http_error = || {
            TokenExchangeError::Unavailable(format!(
                "{} token endpoint returned HTTP {status}",
                self.provider_name
            ))
        };
        if !is_success && contents.is_empty() {
            return Err(http_error());
        }
        let invalid = || {
            TokenExchangeError::InvalidResponse(format!(
                "{} token response is invalid",
                self.provider_name
            ))
        };
        let decoded: JsonMap<String, JsonValue> = match serde_json::from_slice(&contents) {
            Ok(JsonValue::Object(object)) => object,
            _ => return Err(invalid()),
        };
        let error_code = decoded
            .get("error")
            .and_then(JsonValue::as_str)
            .unwrap_or_default();
        if !is_success || !error_code.is_empty() {
            if status < 500
                && self
                    .terminal_error_codes
                    .iter()
                    .any(|code| code == error_code)
            {
                return Err(reauthorization());
            }
            return Err(http_error());
        }
        self.decode_response(&decoded)
    }

    fn decode_response(
        &self,
        decoded: &JsonMap<String, JsonValue>,
    ) -> Result<TokenResponse, TokenExchangeError> {
        let omitted = || {
            TokenExchangeError::InvalidResponse(format!(
                "{} token response omitted required fields",
                self.provider_name
            ))
        };
        let invalid = || {
            TokenExchangeError::InvalidResponse(format!(
                "{} token response is invalid",
                self.provider_name
            ))
        };
        let access_token = match decoded.get("access_token") {
            Some(JsonValue::String(token)) if !token.is_empty() => token.clone(),
            _ => return Err(omitted()),
        };
        let expires_in = match decoded.get("expires_in") {
            None => 0,
            Some(value) => json_integer(value).ok_or_else(omitted)?,
        };
        if expires_in < 0 || (expires_in == 0 && !self.accepts_missing_expires_in) {
            return Err(omitted());
        }
        let token_type = match decoded.get("token_type") {
            None => String::new(),
            Some(JsonValue::String(token_type)) => token_type.clone(),
            Some(_) => return Err(omitted()),
        };
        let default_types = ["Bearer".to_string()];
        let accepted_types: &[String] = if self.accepted_token_types.is_empty() {
            &default_types
        } else {
            &self.accepted_token_types
        };
        if (token_type.is_empty() && !self.accepts_missing_token_type)
            || (!token_type.is_empty()
                && !accepted_types
                    .iter()
                    .any(|accepted| accepted.to_lowercase() == token_type.to_lowercase()))
        {
            return Err(omitted());
        }
        let refresh_token = match decoded.get("refresh_token") {
            None => String::new(),
            Some(JsonValue::String(token)) => token.clone(),
            Some(_) => return Err(invalid()),
        };
        let refresh_expiry = match decoded.get("refresh_token_expires_in") {
            None => 0,
            Some(value) => json_integer(value).ok_or_else(invalid)?,
        };
        let scope = match decoded.get("scope") {
            None => String::new(),
            Some(JsonValue::String(scope)) => scope.clone(),
            Some(_) => return Err(invalid()),
        };
        Ok(TokenResponse {
            access_token: SecretString::new(access_token),
            refresh_token: SecretString::new(refresh_token),
            expires_in: Duration::from_secs(u64::try_from(expires_in).unwrap_or(0)),
            refresh_token_expires_in: Duration::from_secs(
                u64::try_from(refresh_expiry.max(0)).unwrap_or(0),
            ),
            scope,
            token_type,
            retained_fields: self
                .retained_response_fields
                .iter()
                .filter_map(|name| decoded.get(name).map(|value| (name.clone(), value.clone())))
                .collect(),
            response_fields: redact_response_strings(decoded),
        })
    }
}

// Python accepts only JSON integers, never floats or booleans.
fn json_integer(value: &JsonValue) -> Option<i64> {
    match value {
        JsonValue::Number(number) if number.is_i64() || number.is_u64() => {
            number.as_i64().or(Some(i64::MAX))
        }
        _ => None,
    }
}

fn apply_client_authentication<'client>(
    parameters: &mut BTreeMap<String, String>,
    client: &'client ClientCredentials,
    allows_public_client: bool,
) -> Result<Option<&'client ClientCredentials>, TokenExchangeError> {
    match client.authentication_method {
        ClientAuthenticationMethod::PublicClient if allows_public_client => {
            parameters.insert("client_id".to_string(), client.id.clone());
            Ok(None)
        }
        ClientAuthenticationMethod::PublicClient => Err(TokenExchangeError::Configuration(
            ValidationError::new("OAuth client authentication method is not supported"),
        )),
        _ if client.secret.reveal().is_empty() => Err(reauthorization()),
        ClientAuthenticationMethod::ClientSecretPost => {
            parameters.insert("client_id".to_string(), client.id.clone());
            parameters.insert(
                "client_secret".to_string(),
                client.secret.reveal().to_string(),
            );
            Ok(None)
        }
        ClientAuthenticationMethod::ClientSecretBasic => Ok(Some(client)),
    }
}

/// Python `urlencode(sorted(...))`: `quote_plus` escaping of keys and values.
fn form_urlencode(parameters: &BTreeMap<String, String>) -> Vec<u8> {
    let encode = |value: &str| {
        let mut encoded = String::with_capacity(value.len());
        for byte in value.bytes() {
            match byte {
                b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' => {
                    encoded.push(char::from(byte));
                }
                b' ' => encoded.push('+'),
                _ => encoded.push_str(&format!("%{byte:02X}")),
            }
        }
        encoded
    };
    parameters
        .iter()
        .map(|(key, value)| format!("{}={}", encode(key), encode(value)))
        .collect::<Vec<_>>()
        .join("&")
        .into_bytes()
}

fn redact_response_strings(
    response: &JsonMap<String, JsonValue>,
) -> BTreeMap<String, SecretString> {
    let mut redacted = BTreeMap::new();
    let mut pending: Vec<(String, &JsonValue)> = response
        .iter()
        .map(|(name, value)| (name.clone(), value))
        .collect();
    while let Some((path, value)) = pending.pop() {
        match value {
            JsonValue::String(text) => {
                redacted.insert(path, SecretString::new(text.clone()));
            }
            JsonValue::Object(object) => {
                for (member, nested) in object {
                    pending.push((format!("{path}.{member}"), nested));
                }
            }
            _ => {}
        }
    }
    redacted
}

/// RFC 7523 RS256 assertion claims for a service-account grant.
#[derive(Clone, Debug)]
pub struct JwtBearerAssertion {
    /// `iss` claim, such as a service account email.
    pub issuer: String,
    /// `aud` claim, usually the token endpoint.
    pub audience: String,
    /// Issue time.
    pub issued_at: Rfc3339NanoTimestamp,
    /// Assertion lifetime added to `iat` for `exp`.
    pub lifetime: Duration,
    /// Optional `sub` claim; empty is omitted.
    pub subject: String,
    /// Optional `scope` claim; empty is omitted.
    pub scope: String,
}

/// RSA private key parsed from PEM; it never renders key material.
pub struct RsaPrivateKey {
    key_pair: ring::rsa::KeyPair,
}

impl fmt::Debug for RsaPrivateKey {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("RsaPrivateKey([REDACTED])")
    }
}

/// Parses an RSA PKCS #1 or PKCS #8 PEM key without echoing key material.
///
/// Keys shorter than 2048 bits are rejected by the signing backend.
///
/// # Errors
///
/// Returns [`ValidationError`] for an invalid, encrypted, or non-RSA key.
pub fn parse_rsa_private_key_pem(pem_text: &str) -> Result<RsaPrivateKey, ValidationError> {
    let invalid = || ValidationError::new("private key is invalid");
    let trimmed = pem_text.trim();
    let (label, rest) = trimmed
        .strip_prefix("-----BEGIN ")
        .and_then(|rest| rest.split_once("-----"))
        .ok_or_else(invalid)?;
    let footer = format!("-----END {label}-----");
    let body = rest
        .trim()
        .strip_suffix(footer.as_str())
        .ok_or_else(invalid)?;
    let encoded: String = body
        .chars()
        .filter(|character| !character.is_whitespace())
        .collect();
    let der = STANDARD.decode(encoded).map_err(|_| invalid())?;
    let key_pair = match label {
        "PRIVATE KEY" => ring::rsa::KeyPair::from_pkcs8(&der)
            .map_err(|_| ValidationError::new("private key must use RSA"))?,
        "RSA PRIVATE KEY" => ring::rsa::KeyPair::from_der(&der).map_err(|_| invalid())?,
        _ => return Err(invalid()),
    };
    Ok(RsaPrivateKey { key_pair })
}

/// Signs a compact RS256 assertion for [`TokenEndpoint::exchange_jwt_bearer_assertion`].
///
/// Claims are serialized with sorted keys: `aud`, `exp`, `iat`, `iss`, then optional `scope`
/// and `sub`.
///
/// # Errors
///
/// Returns [`ValidationError`] for incomplete claims or a signing failure.
pub fn sign_jwt_bearer_assertion(
    assertion: &JwtBearerAssertion,
    private_key: &RsaPrivateKey,
) -> Result<SecretString, ValidationError> {
    if assertion.issuer.is_empty() || assertion.audience.is_empty() || assertion.lifetime.is_zero()
    {
        return Err(ValidationError::new(
            "JWT-bearer assertion claims are incomplete",
        ));
    }
    let issued_at_seconds = assertion
        .issued_at
        .unix_nanoseconds()
        .div_euclid(1_000_000_000);
    let expires_at_seconds = (assertion.issued_at.unix_nanoseconds()
        + i128::try_from(assertion.lifetime.as_nanos()).unwrap_or(i128::MAX))
    .div_euclid(1_000_000_000);
    let to_i64 = |value: i128| {
        i64::try_from(value).map_err(|_| ValidationError::new("JWT-bearer time is out of range"))
    };
    let mut claims = BTreeMap::from([
        (
            "aud".to_string(),
            JsonValue::from(assertion.audience.clone()),
        ),
        (
            "exp".to_string(),
            JsonValue::from(to_i64(expires_at_seconds)?),
        ),
        (
            "iat".to_string(),
            JsonValue::from(to_i64(issued_at_seconds)?),
        ),
        ("iss".to_string(), JsonValue::from(assertion.issuer.clone())),
    ]);
    if !assertion.subject.is_empty() {
        claims.insert(
            "sub".to_string(),
            JsonValue::from(assertion.subject.clone()),
        );
    }
    if !assertion.scope.is_empty() {
        claims.insert(
            "scope".to_string(),
            JsonValue::from(assertion.scope.clone()),
        );
    }
    let encode = |value: &BTreeMap<String, JsonValue>| -> Result<String, ValidationError> {
        let serialized = serde_json::to_vec(value)
            .map_err(|_| ValidationError::new("JWT-bearer claims cannot be encoded"))?;
        Ok(URL_SAFE_NO_PAD.encode(serialized))
    };
    let header = BTreeMap::from([
        ("alg".to_string(), JsonValue::from("RS256")),
        ("typ".to_string(), JsonValue::from("JWT")),
    ]);
    let unsigned = format!("{}.{}", encode(&header)?, encode(&claims)?);
    let mut signature = vec![0; private_key.key_pair.public().modulus_len()];
    private_key
        .key_pair
        .sign(
            &ring::signature::RSA_PKCS1_SHA256,
            &ring::rand::SystemRandom::new(),
            unsigned.as_bytes(),
            &mut signature,
        )
        .map_err(|_| ValidationError::new("JWT-bearer assertion cannot be signed"))?;
    Ok(SecretString::new(format!(
        "{unsigned}.{}",
        URL_SAFE_NO_PAD.encode(signature)
    )))
}
