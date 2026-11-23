// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Bounded, redirect-safe, blocking provider HTTP helpers.
//!
//! [`ProviderHttpClient`] never follows redirects, bounds every request by a timeout, and can
//! check every DNS answer against an [`AddressPolicy`] before dialing. Responses stream their
//! body; read it with [`read_bounded_body`]. Transport failures report whether the request
//! provably never left the process, which single-dispatch writes use to decide a retry.
//!
//! Unlike Python, Rust cannot cancel a send at a single-dispatch write deadline. Use
//! [`ProviderHttpClient::send_traced_before`] inside [`crate::singledispatch::Fence::dispatch`]
//! so the request's own timeout ends at the Fence write deadline.
//!
//! ```no_run
//! use std::time::Duration;
//! use dex_connectors_sdk::providerhttp::{
//!     AddressPolicy, ProviderHttpClient, ProviderHttpRequest, read_bounded_body,
//! };
//!
//! # fn main() -> Result<(), Box<dyn std::error::Error>> {
//! let client = ProviderHttpClient::new(Duration::from_secs(30), AddressPolicy::PublicHttps)?;
//! let request = ProviderHttpRequest::new("GET", "https://provider.example/v1/items")
//!     .header("Accept", "application/json");
//! let mut response = client.send_traced(&request)?;
//! let body = read_bounded_body(&mut response, 1 << 20)?;
//! assert!(response.status() < 600 && body.len() <= 1 << 20);
//! # Ok(())
//! # }
//! ```

mod address_policy;

use std::collections::BTreeMap;
use std::fmt;
use std::io::Read;
use std::sync::Arc;
use std::time::{Duration, Instant, SystemTime};

use serde_json::value::RawValue;

pub use address_policy::{AddressPolicy, AddressRefusal, address_refusal_reason};

use crate::errors::ValidationError;

/// Largest provider error body a connector should read for classification.
pub const MAX_ERROR_BODY_BYTES: usize = 64 << 10;
/// Largest `Retry-After` delay honored, in seconds.
pub const MAX_RETRY_AFTER_DELAY_SECONDS: u64 = 3600;

/// Case-insensitive response header list in received order.
#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct ResponseHeaders {
    entries: Vec<(String, String)>,
}

impl ResponseHeaders {
    /// Creates headers from name and value pairs.
    pub fn new(entries: Vec<(String, String)>) -> Self {
        Self { entries }
    }

    /// Returns the first value of `name`, compared case-insensitively.
    pub fn get(&self, name: &str) -> Option<&str> {
        self.entries
            .iter()
            .find(|(candidate, _)| candidate.eq_ignore_ascii_case(name))
            .map(|(_, value)| value.as_str())
    }

    /// Returns every name and value pair.
    pub fn entries(&self) -> &[(String, String)] {
        &self.entries
    }
}

/// One provider HTTP request; its body is fully buffered.
#[derive(Clone)]
pub struct ProviderHttpRequest {
    /// HTTP method, such as `POST`.
    pub method: String,
    /// Absolute URL.
    pub url: String,
    /// Request headers in order.
    pub headers: Vec<(String, String)>,
    /// Request body; empty sends no body.
    pub body: Vec<u8>,
}

impl ProviderHttpRequest {
    /// Creates a request without headers or body.
    pub fn new(method: impl Into<String>, url: impl Into<String>) -> Self {
        Self {
            method: method.into(),
            url: url.into(),
            headers: Vec::new(),
            body: Vec::new(),
        }
    }

    /// Returns the request with one more header.
    #[must_use]
    pub fn header(mut self, name: impl Into<String>, value: impl Into<String>) -> Self {
        self.headers.push((name.into(), value.into()));
        self
    }

    /// Returns the request with `body`.
    #[must_use]
    pub fn body(mut self, body: impl Into<Vec<u8>>) -> Self {
        self.body = body.into();
        self
    }
}

impl fmt::Debug for ProviderHttpRequest {
    // Headers and bodies may carry credentials, so only the method is rendered.
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("ProviderHttpRequest")
            .field("method", &self.method)
            .finish_non_exhaustive()
    }
}

/// Streaming provider response; read its body with [`read_bounded_body`].
pub struct ProviderHttpResponse {
    status: u16,
    headers: ResponseHeaders,
    body: Box<dyn Read + Send>,
}

impl ProviderHttpResponse {
    /// Creates a response, as a [`ProviderHttpTransport`] fake does.
    pub fn new(status: u16, headers: ResponseHeaders, body: Box<dyn Read + Send>) -> Self {
        Self {
            status,
            headers,
            body,
        }
    }

    /// Returns the HTTP status code.
    pub fn status(&self) -> u16 {
        self.status
    }

    /// Returns the response headers.
    pub fn headers(&self) -> &ResponseHeaders {
        &self.headers
    }
}

impl fmt::Debug for ProviderHttpResponse {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("ProviderHttpResponse")
            .field("status", &self.status)
            .finish_non_exhaustive()
    }
}

/// Provider body exceeded its limit; status and headers stay available for classification.
///
/// The message omits header values.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct BodyTooLargeError {
    /// HTTP status of the oversized response.
    pub status: u16,
    /// Response headers.
    pub headers: ResponseHeaders,
}

impl fmt::Display for BodyTooLargeError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("provider response body exceeds its size limit")
    }
}

impl std::error::Error for BodyTooLargeError {}

/// Secret-safe transport failure with dispatch evidence and available response metadata.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ProviderTransportError {
    /// Whether DNS, connect, or TLS failed before any HTTP byte was written.
    pub provably_not_sent: bool,
    /// Status of a response whose body read failed.
    pub status: Option<u16>,
    /// Headers of a response whose body read failed.
    pub headers: Option<ResponseHeaders>,
}

impl ProviderTransportError {
    /// Creates a transport failure without response metadata.
    pub fn new(provably_not_sent: bool) -> Self {
        Self {
            provably_not_sent,
            status: None,
            headers: None,
        }
    }
}

impl fmt::Display for ProviderTransportError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("provider request failed")
    }
}

impl std::error::Error for ProviderTransportError {}

/// Failure of a provider HTTP request or bounded body read.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum ProviderHttpError {
    /// The address policy refused the destination before dialing; nothing was sent.
    AddressRefusal(AddressRefusal),
    /// The request or body read failed.
    Transport(ProviderTransportError),
    /// The body exceeded its limit.
    BodyTooLarge(BodyTooLargeError),
}

impl ProviderHttpError {
    /// Reports whether the request provably never reached the provider.
    pub fn is_provably_not_sent(&self) -> bool {
        match self {
            ProviderHttpError::AddressRefusal(_) => true,
            ProviderHttpError::Transport(error) => error.provably_not_sent,
            ProviderHttpError::BodyTooLarge(_) => false,
        }
    }
}

impl fmt::Display for ProviderHttpError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            ProviderHttpError::AddressRefusal(error) => error.fmt(formatter),
            ProviderHttpError::Transport(error) => error.fmt(formatter),
            ProviderHttpError::BodyTooLarge(error) => error.fmt(formatter),
        }
    }
}

impl std::error::Error for ProviderHttpError {
    fn source(&self) -> Option<&(dyn std::error::Error + 'static)> {
        match self {
            ProviderHttpError::AddressRefusal(error) => Some(error),
            ProviderHttpError::Transport(error) => Some(error),
            ProviderHttpError::BodyTooLarge(error) => Some(error),
        }
    }
}

/// Sends one request without following redirects; implemented by the ureq client and fakes.
pub trait ProviderHttpTransport: Send + Sync {
    /// Sends `request` once, bounded by `timeout`, and returns the streaming response.
    ///
    /// Every HTTP status, including 3xx and errors, is a response.
    ///
    /// # Errors
    ///
    /// Returns [`ProviderHttpError`] for a refusal or transport failure.
    fn send(
        &self,
        request: &ProviderHttpRequest,
        timeout: Duration,
    ) -> Result<ProviderHttpResponse, ProviderHttpError>;
}

/// Redirect-safe blocking provider client with an optional address policy.
///
/// Clone it freely; clones share the connection pool.
#[derive(Clone)]
pub struct ProviderHttpClient {
    transport: Arc<dyn ProviderHttpTransport>,
    request_timeout: Duration,
    address_policy: Option<AddressPolicy>,
}

impl ProviderHttpClient {
    /// Creates a ureq-backed client.
    ///
    /// [`AddressPolicy::PublicHttps`] allows only HTTPS to public addresses;
    /// [`AddressPolicy::PrivateNetworkHttp`] also allows HTTP to private or loopback addresses.
    /// Restricted clients check every DNS answer and dial only checked IPs while keeping the
    /// hostname for Host and TLS verification. Environment proxies are never used.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for a zero timeout.
    pub fn new(request_timeout: Duration, policy: AddressPolicy) -> Result<Self, ValidationError> {
        let mut builder = ureq::AgentBuilder::new()
            .redirects(0)
            .timeout(request_timeout);
        if policy != AddressPolicy::Unrestricted {
            builder = builder.resolver(address_policy::CheckedResolver { policy });
        }
        let mut client = Self::with_transport(
            Arc::new(UreqTransport {
                agent: builder.build(),
                policy,
            }),
            request_timeout,
        )?;
        client.address_policy = Some(policy);
        Ok(client)
    }

    /// Returns the address policy of a ureq-backed client, or `None` for a custom transport.
    pub fn address_policy(&self) -> Option<AddressPolicy> {
        self.address_policy
    }

    /// Creates a client over a caller-supplied transport, such as a test fake.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for a zero timeout.
    pub fn with_transport(
        transport: Arc<dyn ProviderHttpTransport>,
        request_timeout: Duration,
    ) -> Result<Self, ValidationError> {
        if request_timeout.is_zero() {
            return Err(ValidationError::new(
                "provider request timeout must be positive",
            ));
        }
        Ok(Self {
            transport,
            request_timeout,
            address_policy: None,
        })
    }

    /// Returns the default per-request timeout.
    pub fn request_timeout(&self) -> Duration {
        self.request_timeout
    }

    /// Sends once within the client timeout; the caller reads the streaming body.
    ///
    /// # Errors
    ///
    /// Returns [`ProviderHttpError`] for a refusal or transport failure.
    pub fn send_traced(
        &self,
        request: &ProviderHttpRequest,
    ) -> Result<ProviderHttpResponse, ProviderHttpError> {
        self.transport.send(request, self.request_timeout)
    }

    /// Sends once within `timeout`, capped by the client timeout.
    ///
    /// # Errors
    ///
    /// Returns [`ProviderHttpError`] for a refusal or transport failure.
    pub fn send_traced_within(
        &self,
        request: &ProviderHttpRequest,
        timeout: Duration,
    ) -> Result<ProviderHttpResponse, ProviderHttpError> {
        let timeout = timeout.min(self.request_timeout);
        if timeout.is_zero() {
            return Err(ProviderHttpError::Transport(ProviderTransportError::new(
                true,
            )));
        }
        self.transport.send(request, timeout)
    }

    /// Sends once so the request ends by `deadline`, such as a Fence write deadline.
    ///
    /// A deadline already passed fails as provably not sent without dialing.
    ///
    /// # Errors
    ///
    /// Returns [`ProviderHttpError`] for a refusal, transport failure, or expired deadline.
    pub fn send_traced_before(
        &self,
        request: &ProviderHttpRequest,
        deadline: Instant,
    ) -> Result<ProviderHttpResponse, ProviderHttpError> {
        self.send_traced_within(request, deadline.saturating_duration_since(Instant::now()))
    }
}

impl fmt::Debug for ProviderHttpClient {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("ProviderHttpClient")
            .field("request_timeout", &self.request_timeout)
            .finish_non_exhaustive()
    }
}

struct UreqTransport {
    agent: ureq::Agent,
    policy: AddressPolicy,
}

impl ProviderHttpTransport for UreqTransport {
    fn send(
        &self,
        request: &ProviderHttpRequest,
        timeout: Duration,
    ) -> Result<ProviderHttpResponse, ProviderHttpError> {
        let scheme = request
            .url
            .split_once("://")
            .map(|(scheme, _)| scheme.to_ascii_lowercase())
            .unwrap_or_default();
        if self.policy != AddressPolicy::Unrestricted && scheme != "http" && scheme != "https" {
            return Err(ProviderHttpError::AddressRefusal(AddressRefusal::new(
                "URL scheme is not allowed",
            )));
        }
        let mut builder = self
            .agent
            .request(&request.method, &request.url)
            .timeout(timeout);
        for (name, value) in &request.headers {
            builder = builder.set(name, value);
        }
        address_policy::begin_checked_request(&scheme);
        let result = if request.body.is_empty() {
            builder.call()
        } else {
            builder.send_bytes(&request.body)
        };
        let refusal = address_policy::end_checked_request();
        match result {
            Ok(response) | Err(ureq::Error::Status(_, response)) => Ok(convert_response(response)),
            Err(ureq::Error::Transport(transport)) => {
                if let Some(refusal) = refusal {
                    return Err(ProviderHttpError::AddressRefusal(refusal));
                }
                Err(ProviderHttpError::Transport(ProviderTransportError::new(
                    is_pre_dispatch_failure(transport.kind()),
                )))
            }
        }
    }
}

fn is_pre_dispatch_failure(kind: ureq::ErrorKind) -> bool {
    // ureq completes DNS, TCP connect, and the TLS handshake before writing request bytes.
    matches!(
        kind,
        ureq::ErrorKind::Dns
            | ureq::ErrorKind::ConnectionFailed
            | ureq::ErrorKind::InvalidUrl
            | ureq::ErrorKind::UnknownScheme
            | ureq::ErrorKind::InsecureRequestHttpsOnly
            | ureq::ErrorKind::InvalidProxyUrl
            | ureq::ErrorKind::ProxyConnect
            | ureq::ErrorKind::ProxyUnauthorized
    )
}

fn convert_response(response: ureq::Response) -> ProviderHttpResponse {
    let mut entries = Vec::new();
    for name in response.headers_names() {
        for value in response.all(&name) {
            entries.push((name.clone(), value.to_string()));
        }
    }
    ProviderHttpResponse {
        status: response.status(),
        headers: ResponseHeaders::new(entries),
        body: response.into_reader(),
    }
}

/// Reads at most `maximum_bytes` of the body and releases the stream on every path.
///
/// # Errors
///
/// Returns [`ProviderHttpError::BodyTooLarge`] past the limit, or
/// [`ProviderHttpError::Transport`] with the status and headers for a read failure.
pub fn read_bounded_body(
    response: &mut ProviderHttpResponse,
    maximum_bytes: usize,
) -> Result<Vec<u8>, ProviderHttpError> {
    let mut body = std::mem::replace(&mut response.body, Box::new(std::io::empty()));
    let mut contents = Vec::new();
    let limit = u64::try_from(maximum_bytes)
        .unwrap_or(u64::MAX)
        .saturating_add(1);
    if (&mut body).take(limit).read_to_end(&mut contents).is_err() {
        return Err(ProviderHttpError::Transport(ProviderTransportError {
            provably_not_sent: false,
            status: Some(response.status),
            headers: Some(response.headers.clone()),
        }));
    }
    if contents.len() > maximum_bytes {
        return Err(ProviderHttpError::BodyTooLarge(BodyTooLargeError {
            status: response.status,
            headers: response.headers.clone(),
        }));
    }
    Ok(contents)
}

/// Encodes a string multimap like Go `net/url.Values.Encode`, sorting keys by bytes.
///
/// Keys with an empty value list are omitted.
pub fn encode_query_values(values: &BTreeMap<String, Vec<String>>) -> String {
    let mut encoded = Vec::new();
    for (key, entries) in values {
        for value in entries {
            encoded.push(format!(
                "{}={}",
                go_query_escape(key),
                go_query_escape(value)
            ));
        }
    }
    encoded.join("&")
}

fn go_query_escape(value: &str) -> String {
    let mut escaped = String::with_capacity(value.len());
    for byte in value.bytes() {
        match byte {
            b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' => {
                escaped.push(char::from(byte));
            }
            b' ' => escaped.push('+'),
            _ => escaped.push_str(&format!("%{byte:02X}")),
        }
    }
    escaped
}

fn is_error_token(value: &str) -> bool {
    (1..=64).contains(&value.len())
        && value
            .bytes()
            .all(|byte| byte.is_ascii_alphanumeric() || matches!(byte, b'_' | b'.' | b':' | b'-'))
}

/// Reads safe scalar tokens at RFC 6901 pointers from a bounded JSON error body.
///
/// A string or the literal text of a number qualifies when it is 1 to 64 characters of
/// `[A-Za-z0-9_.:-]`; error prose and invalid JSON yield nothing.
pub fn read_error_tokens(body: &[u8], pointers: &[&str]) -> Vec<String> {
    let Ok(document) = serde_json::from_slice::<&RawValue>(body) else {
        return Vec::new();
    };
    let mut tokens = Vec::new();
    for pointer in pointers {
        if !pointer.is_empty() && !pointer.starts_with('/') {
            continue;
        }
        let mut current = document;
        let mut is_found = true;
        let segments: Vec<&str> = if pointer.is_empty() {
            Vec::new()
        } else {
            pointer[1..].split('/').collect()
        };
        for escaped in segments {
            let segment = escaped.replace("~1", "/").replace("~0", "~");
            let text = current.get().trim_start();
            let next = if text.starts_with('{') {
                serde_json::from_str::<BTreeMap<String, &RawValue>>(current.get())
                    .ok()
                    .and_then(|object| object.get(&segment).copied())
            } else if text.starts_with('[') {
                let is_canonical_index = !segment.is_empty()
                    && segment.bytes().all(|byte| byte.is_ascii_digit())
                    && (segment == "0" || !segment.starts_with('0'));
                if is_canonical_index {
                    serde_json::from_str::<Vec<&RawValue>>(current.get())
                        .ok()
                        .and_then(|items| {
                            segment
                                .parse::<usize>()
                                .ok()
                                .and_then(|index| items.get(index).copied())
                        })
                } else {
                    None
                }
            } else {
                None
            };
            match next {
                Some(value) => current = value,
                None => {
                    is_found = false;
                    break;
                }
            }
        }
        if !is_found {
            continue;
        }
        let text = current.get().trim();
        let token = if text.starts_with('"') {
            serde_json::from_str::<String>(text).ok()
        } else if text.starts_with('-') || text.starts_with(|first: char| first.is_ascii_digit()) {
            Some(text.to_string())
        } else {
            None
        };
        if let Some(token) = token.filter(|token| is_error_token(token)) {
            tokens.push(token);
        }
    }
    tokens
}

/// Parses delta-seconds or an HTTP date, capped at one hour; invalid or past values give 0.
pub fn parse_retry_after(value: &str, now: SystemTime) -> f64 {
    let value = value.trim();
    if !value.is_empty() && value.bytes().all(|byte| byte.is_ascii_digit()) {
        let seconds = value.parse::<u64>().unwrap_or(u64::MAX);
        return f64::from(u32::try_from(seconds.min(MAX_RETRY_AFTER_DELAY_SECONDS)).unwrap_or(0));
    }
    let Ok(retry_at) = httpdate::parse_http_date(value) else {
        return 0.0;
    };
    match retry_at.duration_since(now) {
        Ok(delay) => delay.as_secs_f64().min(f64::from(
            u32::try_from(MAX_RETRY_AFTER_DELAY_SECONDS).unwrap_or(0),
        )),
        Err(_) => 0.0,
    }
}

/// Requires printable HTTPS, allowing HTTP only on `localhost` or a loopback IP.
///
/// Returns the URL without a trailing slash. User information, query, fragment, and a zero
/// or invalid port are rejected.
///
/// # Errors
///
/// Returns [`ValidationError`] for an invalid base URL.
pub fn validate_base_url(value: &str) -> Result<String, ValidationError> {
    if value.is_empty() || !value.bytes().all(|byte| (33..=126).contains(&byte)) {
        return Err(ValidationError::new(
            "base URL must be printable ASCII without spaces",
        ));
    }
    let absolute = || {
        ValidationError::new(
            "base URL must be absolute without user information, query, or fragment",
        )
    };
    if value.contains('?') || value.contains('#') {
        return Err(absolute());
    }
    let (scheme, rest) = value.split_once("://").ok_or_else(absolute)?;
    let scheme = scheme.to_ascii_lowercase();
    let (netloc, path) = match rest.find('/') {
        Some(index) => (&rest[..index], &rest[index..]),
        None => (rest, ""),
    };
    if netloc.contains('@') {
        return Err(absolute());
    }
    let (host, port) = split_host_port(netloc).ok_or_else(absolute)?;
    if host.is_empty() {
        return Err(absolute());
    }
    if let Some(port) = port {
        let is_valid_port = !port.is_empty()
            && port.bytes().all(|byte| byte.is_ascii_digit())
            && port
                .parse::<u32>()
                .is_ok_and(|number| (1..=65535).contains(&number));
        if !is_valid_port && !port.is_empty() {
            return Err(ValidationError::new("base URL port is invalid"));
        }
    }
    let is_loopback = host.eq_ignore_ascii_case("localhost")
        || host
            .parse::<std::net::IpAddr>()
            .is_ok_and(|address| address.is_loopback());
    if scheme != "https" && !(scheme == "http" && is_loopback) {
        return Err(ValidationError::new(
            "base URL must use HTTPS unless its host is loopback",
        ));
    }
    Ok(format!("{scheme}://{netloc}{}", path.trim_end_matches('/')))
}

fn split_host_port(netloc: &str) -> Option<(&str, Option<&str>)> {
    if let Some(bracketed) = netloc.strip_prefix('[') {
        let (host, after) = bracketed.split_once(']')?;
        return match after {
            "" => Some((host, None)),
            _ => Some((host, Some(after.strip_prefix(':')?))),
        };
    }
    match netloc.split_once(':') {
        Some((host, port)) => Some((host, Some(port))),
        None => Some((netloc, None)),
    }
}

/// Requires a nonempty printable ASCII value without spaces or controls.
pub fn is_header_safe_credential(value: &str) -> bool {
    !value.is_empty() && value.bytes().all(|byte| (33..=126).contains(&byte))
}
