// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Credential broker wire client and shared-connection cache.

use std::collections::HashMap;
use std::fmt;
use std::io::Read;
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant, SystemTime};

use serde_json::Value;

use super::errors::{ProjectConfigError, ProjectConfigResult};
use super::models::{
    ApplicationSecretRef, CredentialBrokerConfiguration, JsonObject, go_canonical_json_bytes,
    parse_url, require_object, validate_broker_endpoint,
};
use super::storage::lock;
use crate::relaysignature::{
    ParsedRelaySignature, RelayHeaders, matches_relay_signature, parse_relay_signature,
};
use crate::timestamps::Rfc3339NanoTimestamp;

/// Largest broker response body accepted.
pub const MAXIMUM_BROKER_RESPONSE_BYTES: usize = 1 << 20;
/// Largest encoded shared credential accepted.
pub const MAXIMUM_BROKER_CREDENTIAL_BYTES: usize = 64 << 10;
const MAXIMUM_RELAYED_TRIGGER_REGISTRATION_BYTES: usize = 4096;
const RELAY_TOKEN_RENEWAL_INTERVAL: Duration = Duration::from_secs(30);
const BROKER_EXCHANGE_TIMEOUT: Duration = Duration::from_secs(40);
const ISSUED_CALL_LIMIT: usize = 4096;

/// Status and bounded body of one broker HTTP exchange.
#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct BrokerHttpResponse {
    /// HTTP status.
    pub status: u16,
    /// Body bytes; read only for status 200, at most the response limit plus one byte.
    pub body: Vec<u8>,
}

/// Blocking HTTP transport of the broker client; it never follows redirects.
pub trait BrokerHttpClient: Send + Sync {
    /// Posts `body` as JSON with a bearer `token`.
    ///
    /// # Errors
    ///
    /// Returns [`ProjectConfigError::BrokerUnavailable`] for a transport failure.
    fn post_json(
        &self,
        url: &str,
        token: &str,
        body: &[u8],
        timeout: Duration,
    ) -> ProjectConfigResult<BrokerHttpResponse>;
}

/// [`BrokerHttpClient`] over `ureq` without redirects.
pub struct UreqBrokerHttpClient {
    agent: ureq::Agent,
}

impl UreqBrokerHttpClient {
    /// Creates a client with a 40-second exchange timeout and no redirects.
    pub fn new() -> Self {
        Self {
            agent: ureq::AgentBuilder::new()
                .redirects(0)
                .timeout(BROKER_EXCHANGE_TIMEOUT)
                .build(),
        }
    }
}

impl Default for UreqBrokerHttpClient {
    fn default() -> Self {
        Self::new()
    }
}

impl BrokerHttpClient for UreqBrokerHttpClient {
    fn post_json(
        &self,
        url: &str,
        token: &str,
        body: &[u8],
        timeout: Duration,
    ) -> ProjectConfigResult<BrokerHttpResponse> {
        let unavailable =
            || ProjectConfigError::BrokerUnavailable("credential broker is unavailable".into());
        let response = match self
            .agent
            .post(url)
            .timeout(timeout)
            .set("Authorization", &format!("Bearer {token}"))
            .set("Content-Type", "application/json")
            .set("Accept", "application/json")
            .send_bytes(body)
        {
            Ok(response) => response,
            Err(ureq::Error::Status(status, _)) => {
                return Ok(BrokerHttpResponse {
                    status,
                    body: Vec::new(),
                });
            }
            Err(ureq::Error::Transport(_)) => return Err(unavailable()),
        };
        let status = response.status();
        if status != 200 {
            return Ok(BrokerHttpResponse {
                status,
                body: Vec::new(),
            });
        }
        let mut body = Vec::new();
        response
            .into_reader()
            .take((MAXIMUM_BROKER_RESPONSE_BYTES + 1) as u64)
            .read_to_end(&mut body)
            .map_err(|_| unavailable())?;
        Ok(BrokerHttpResponse { status, body })
    }
}

/// Private credential body and broker revision metadata; `Debug` redacts the body.
#[derive(Clone)]
pub struct BrokerCredential {
    /// Provider credential members.
    pub credential: JsonObject,
    /// Positive broker revision.
    pub revision: u64,
    /// Issue time when declared.
    pub issued_at: Option<Rfc3339NanoTimestamp>,
    /// Expiry when declared.
    pub expires_at: Option<Rfc3339NanoTimestamp>,
    /// Local read time.
    pub read_at: Rfc3339NanoTimestamp,
}

impl fmt::Debug for BrokerCredential {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            formatter,
            "BrokerCredential(revision={}, credential=[REDACTED])",
            self.revision
        )
    }
}

/// Reads the scope token at an exact secret reference.
pub type ScopeTokenReader =
    Arc<dyn Fn(&ApplicationSecretRef) -> ProjectConfigResult<String> + Send + Sync>;
/// Returns the current scope token reference from the latest configuration.
pub type ScopeTokenReferenceReader =
    Arc<dyn Fn() -> ProjectConfigResult<ApplicationSecretRef> + Send + Sync>;

/// Scope-token client for the shared credential read protocol.
///
/// A cached token is used without waiting for a reread. A 401 rereads the current token
/// reference once; a relay signature mismatch rereads at most once every 30 seconds.
pub struct CredentialBrokerClient {
    endpoint: String,
    read_token: ScopeTokenReader,
    current_token_reference: ScopeTokenReferenceReader,
    http: Arc<dyn BrokerHttpClient>,
    token_reference: Mutex<ApplicationSecretRef>,
    token: Mutex<String>,
    renewal: Mutex<Option<Instant>>,
}

impl CredentialBrokerClient {
    /// Binds the trusted endpoint, scope-token readers, and HTTP transport.
    ///
    /// # Errors
    ///
    /// Returns [`ProjectConfigError::Invalid`] for an invalid endpoint.
    pub fn new(
        configuration: &CredentialBrokerConfiguration,
        read_token: ScopeTokenReader,
        current_token_reference: ScopeTokenReferenceReader,
        http: Arc<dyn BrokerHttpClient>,
    ) -> ProjectConfigResult<Self> {
        validate_broker_endpoint(&configuration.endpoint)?;
        Ok(Self {
            endpoint: configuration.endpoint.clone(),
            read_token,
            current_token_reference,
            http,
            token_reference: Mutex::new(configuration.token_secret_ref.clone()),
            token: Mutex::new(String::new()),
            renewal: Mutex::new(None),
        })
    }

    /// Reads one shared credential; `rejected_revision` forces a newer revision when nonzero.
    ///
    /// # Errors
    ///
    /// Returns [`BrokerReadError::ReauthorizationRequired`] for a `REAUTHORIZATION_REQUIRED`
    /// answer, or a [`BrokerReadError::Project`] holding
    /// [`ProjectConfigError::BrokerUnavailable`] for a transient failure,
    /// [`ProjectConfigError::BrokerRejected`] for a terminal one, or
    /// [`ProjectConfigError::ConnectionNotSelected`].
    pub fn read(
        &self,
        shared_connection_id: &str,
        rejected_revision: u64,
    ) -> Result<BrokerCredential, BrokerReadError> {
        if shared_connection_id.is_empty() {
            return Err(ProjectConfigError::invalid("invalid shared credential read").into());
        }
        let mut request = JsonObject::new();
        request.insert(
            "sharedConnectionId".into(),
            Value::String(shared_connection_id.into()),
        );
        if rejected_revision > 0 {
            request.insert("rejectedRevision".into(), Value::from(rejected_revision));
        }
        let response = self.exchange(&self.endpoint, &Value::Object(request))?;
        decode_broker_response(&response)
    }

    /// Registers a shared connection's relayed Trigger route at `registration_path` on the
    /// broker origin.
    ///
    /// # Errors
    ///
    /// Returns [`ProjectConfigError::ConnectionNotSelected`],
    /// [`ProjectConfigError::RelayedTriggerNotRelayed`], `BrokerRejected` for another answer,
    /// `BrokerUnavailable` for a transient failure, and `Invalid` for a bad path or an
    /// oversized request.
    pub fn register_relayed_trigger(
        &self,
        shared_connection_id: &str,
        connection_name: &str,
        registration_path: &str,
    ) -> ProjectConfigResult<()> {
        let target = self.origin_url(registration_path)?;
        let mut request = JsonObject::new();
        request.insert(
            "sharedConnectionId".into(),
            Value::String(shared_connection_id.into()),
        );
        request.insert(
            "connectionName".into(),
            Value::String(connection_name.into()),
        );
        let request = Value::Object(request);
        if go_canonical_json_bytes(&request)?.len() > MAXIMUM_RELAYED_TRIGGER_REGISTRATION_BYTES {
            return Err(ProjectConfigError::invalid(
                "relayed Trigger registration request is too large",
            ));
        }
        let response = self.exchange(&target, &request)?;
        classify_status(response.status)?;
        let answer: Value = serde_json::from_slice(&response.body).map_err(|_| {
            ProjectConfigError::BrokerRejected("credential broker answer is invalid".into())
        })?;
        match answer.get("status").and_then(Value::as_str) {
            Some("REGISTERED") => Ok(()),
            Some("CONNECTION_NOT_SELECTED") => Err(ProjectConfigError::ConnectionNotSelected),
            Some("NOT_RELAYED") => Err(ProjectConfigError::RelayedTriggerNotRelayed),
            _ => Err(ProjectConfigError::BrokerRejected(
                "credential broker status is unknown".into(),
            )),
        }
    }

    /// Verifies that the host signed a relayed request with the scope token.
    ///
    /// Only a well-formed, fresh signature that does not match may reread the token, at most
    /// once every 30 seconds.
    ///
    /// # Errors
    ///
    /// Returns [`ProjectConfigError::RelaySignatureInvalid`] for a refused request,
    /// [`ProjectConfigError::RelayTokenRenewalDeferred`] for a mismatch inside the reread
    /// window, and [`ProjectConfigError::BrokerUnavailable`] when the token cannot be read.
    pub fn verify_relayed_request(
        &self,
        headers: &RelayHeaders,
        body: &[u8],
        received_at: SystemTime,
    ) -> ProjectConfigResult<()> {
        let parsed = parse_relay_signature(headers, received_at)
            .map_err(|_| ProjectConfigError::RelaySignatureInvalid)?;
        let unavailable = || {
            ProjectConfigError::BrokerUnavailable(
                "credential broker scope token is unavailable".into(),
            )
        };
        let token = self.cached_or_read_token().map_err(|_| unavailable())?;
        if matches_relay_signature(&parsed, &token, body) {
            return Ok(());
        }
        match self.renew_token_after_mismatch(&parsed, &token, body) {
            Ok(true) => Ok(()),
            Ok(false) => Err(ProjectConfigError::RelaySignatureInvalid),
            Err(ProjectConfigError::RelayTokenRenewalDeferred) => {
                Err(ProjectConfigError::RelayTokenRenewalDeferred)
            }
            Err(_) => Err(unavailable()),
        }
    }

    fn renew_token_after_mismatch(
        &self,
        parsed: &ParsedRelaySignature,
        mismatched: &str,
        body: &[u8],
    ) -> ProjectConfigResult<bool> {
        let current = lock(&self.token).clone();
        if !current.is_empty() && current != mismatched {
            return Ok(matches_relay_signature(parsed, &current, body));
        }
        // The renewal lock makes concurrent mismatches share one reread.
        let mut last_renewal = lock(&self.renewal);
        let current = lock(&self.token).clone();
        if !current.is_empty() && current != mismatched {
            return Ok(matches_relay_signature(parsed, &current, body));
        }
        let now = Instant::now();
        if last_renewal.is_some_and(|last| now.duration_since(last) < RELAY_TOKEN_RENEWAL_INTERVAL)
        {
            return Err(ProjectConfigError::RelayTokenRenewalDeferred);
        }
        *last_renewal = Some(now);
        // A rotation is a new secret version, so the current reference is resolved again.
        let reference = (self.current_token_reference)()?;
        let token = (self.read_token)(&reference)?;
        if !is_header_safe_token(&token) {
            return Err(ProjectConfigError::BrokerRejected(
                "credential broker scope token is invalid".into(),
            ));
        }
        *lock(&self.token_reference) = reference;
        *lock(&self.token) = token.clone();
        Ok(token != mismatched && matches_relay_signature(parsed, &token, body))
    }

    fn origin_url(&self, path: &str) -> ProjectConfigResult<String> {
        if !path.starts_with('/') || path.starts_with("//") || path.contains(['?', '#', '\\']) {
            return Err(ProjectConfigError::invalid(
                "credential broker endpoint path must be an absolute path without a query or fragment",
            ));
        }
        let endpoint = parse_url(&self.endpoint)
            .ok_or_else(|| ProjectConfigError::invalid("credential broker endpoint is invalid"))?;
        Ok(format!(
            "{}://{}{path}",
            endpoint.scheme, endpoint.authority
        ))
    }

    fn exchange(&self, target_url: &str, body: &Value) -> ProjectConfigResult<BrokerHttpResponse> {
        let encoded = serde_json::to_vec(body)
            .map_err(|_| ProjectConfigError::invalid("broker request cannot be encoded"))?;
        let token = self.cached_or_read_token()?;
        let response = self.post(target_url, &token, &encoded)?;
        if response.status != 401 {
            return Ok(response);
        }
        let renewed = self.renew_token(&token)?;
        if renewed == token {
            return Err(ProjectConfigError::BrokerRejected(
                "credential broker refused scope token".into(),
            ));
        }
        self.post(target_url, &renewed, &encoded)
    }

    fn post(
        &self,
        target_url: &str,
        token: &str,
        body: &[u8],
    ) -> ProjectConfigResult<BrokerHttpResponse> {
        let response = self
            .http
            .post_json(target_url, token, body, BROKER_EXCHANGE_TIMEOUT)?;
        if response.body.len() > MAXIMUM_BROKER_RESPONSE_BYTES {
            return Err(ProjectConfigError::BrokerRejected(
                "credential broker response is too large".into(),
            ));
        }
        Ok(response)
    }

    fn cached_or_read_token(&self) -> ProjectConfigResult<String> {
        let cached = lock(&self.token).clone();
        if !cached.is_empty() {
            return Ok(cached);
        }
        let reference = lock(&self.token_reference).clone();
        let token = (self.read_token)(&reference)?;
        let mut current = lock(&self.token);
        if current.is_empty() {
            *current = token;
        }
        Ok(current.clone())
    }

    fn renew_token(&self, refused: &str) -> ProjectConfigResult<String> {
        let _renewal = lock(&self.renewal);
        let current = lock(&self.token).clone();
        if !current.is_empty() && current != refused {
            return Ok(current);
        }
        let reference = (self.current_token_reference)()?;
        let token = (self.read_token)(&reference)?;
        *lock(&self.token_reference) = reference;
        *lock(&self.token) = token.clone();
        Ok(token)
    }
}

/// Failure of [`CredentialBrokerClient::read`] or a shared credential resolution.
#[derive(Debug)]
pub enum BrokerReadError {
    /// The broker requires interactive authorization of the shared connection.
    ReauthorizationRequired,
    /// Any other broker or configuration failure.
    Project(ProjectConfigError),
}

impl fmt::Display for BrokerReadError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            BrokerReadError::ReauthorizationRequired => {
                formatter.write_str("connector reauthorization is required")
            }
            BrokerReadError::Project(error) => error.fmt(formatter),
        }
    }
}

impl std::error::Error for BrokerReadError {}

impl From<ProjectConfigError> for BrokerReadError {
    fn from(error: ProjectConfigError) -> Self {
        BrokerReadError::Project(error)
    }
}

impl BrokerReadError {
    /// Converts to the boxed credential error used by credential providers.
    pub fn into_credential_error(self) -> crate::CredentialError {
        match self {
            BrokerReadError::ReauthorizationRequired => {
                Box::new(crate::ReauthorizationRequiredError::new(None))
            }
            BrokerReadError::Project(error) => Box::new(error),
        }
    }
}

fn classify_status(status: u16) -> ProjectConfigResult<()> {
    if matches!(status, 408 | 425 | 429) || status >= 500 {
        return Err(ProjectConfigError::BrokerUnavailable(format!(
            "credential broker HTTP {status}"
        )));
    }
    if status != 200 {
        return Err(ProjectConfigError::BrokerRejected(format!(
            "credential broker HTTP {status}"
        )));
    }
    Ok(())
}

/// Decodes one broker read answer.
///
/// # Errors
///
/// Returns the classified broker failure.
pub fn decode_broker_response(
    response: &BrokerHttpResponse,
) -> Result<BrokerCredential, BrokerReadError> {
    classify_status(response.status)?;
    if response.body.len() > MAXIMUM_BROKER_RESPONSE_BYTES {
        return Err(ProjectConfigError::BrokerRejected(
            "credential broker response is too large".into(),
        )
        .into());
    }
    let rejected = |message: &str| {
        BrokerReadError::Project(ProjectConfigError::BrokerRejected(message.into()))
    };
    let answer: Value = serde_json::from_slice(&response.body)
        .map_err(|_| rejected("credential broker answer is invalid"))?;
    let answer = require_object(Some(&answer), "broker answer")
        .map_err(|_| rejected("credential broker answer is invalid"))?;
    match answer.get("status").and_then(Value::as_str) {
        Some("REAUTHORIZATION_REQUIRED") => return Err(BrokerReadError::ReauthorizationRequired),
        Some("CONNECTION_NOT_SELECTED") => {
            return Err(ProjectConfigError::ConnectionNotSelected.into());
        }
        Some("READY") => {}
        _ => return Err(rejected("credential broker status is unknown")),
    }
    let credential = answer.get("credential").and_then(Value::as_object);
    let revision = answer
        .get("revision")
        .and_then(Value::as_u64)
        .filter(|revision| *revision > 0);
    let (Some(credential), Some(revision)) = (credential, revision) else {
        return Err(rejected("credential broker READY answer is invalid"));
    };
    let encoded_length = serde_json::to_vec(credential).map_or(usize::MAX, |bytes| bytes.len());
    if encoded_length > MAXIMUM_BROKER_CREDENTIAL_BYTES {
        return Err(rejected("credential broker READY answer is invalid"));
    }
    let timestamp = |name: &str| -> Result<Option<Rfc3339NanoTimestamp>, BrokerReadError> {
        match answer.get(name) {
            None | Some(Value::Null) => Ok(None),
            Some(Value::String(text)) if text.is_empty() => Ok(None),
            Some(Value::String(text)) => Rfc3339NanoTimestamp::from_wire(text)
                .map(Some)
                .map_err(|_| rejected("credential broker timestamps are invalid")),
            Some(_) => Err(rejected("credential broker timestamps are invalid")),
        }
    };
    Ok(BrokerCredential {
        credential: credential.clone(),
        revision,
        issued_at: timestamp("issuedAt")?,
        expires_at: timestamp("expiresAt")?,
        read_at: Rfc3339NanoTimestamp::now_utc(),
    })
}

fn is_header_safe_token(token: &str) -> bool {
    !token.is_empty() && token.bytes().all(|byte| (0x21..=0x7e).contains(&byte))
}

const NANOSECONDS_PER_MINUTE: i128 = 60 * 1_000_000_000;

struct SharedConnectionState {
    credential: Option<BrokerCredential>,
    issued_by_call: HashMap<String, (u64, i128)>,
    next_read_after: i128,
}

/// Revision-aware broker cache for one immutable shared connection selection.
pub struct SharedConnection {
    shared_connection_id: String,
    broker: Arc<CredentialBrokerClient>,
    state: Mutex<SharedConnectionState>,
    refresh: Mutex<()>,
}

impl SharedConnection {
    /// Binds `shared_connection_id` to the broker.
    ///
    /// # Errors
    ///
    /// Returns [`ProjectConfigError::Invalid`] for an empty ID.
    pub fn new(
        shared_connection_id: &str,
        broker: Arc<CredentialBrokerClient>,
    ) -> ProjectConfigResult<Self> {
        if shared_connection_id.is_empty() {
            return Err(ProjectConfigError::invalid(
                "shared connection ID is required",
            ));
        }
        Ok(Self {
            shared_connection_id: shared_connection_id.into(),
            broker,
            state: Mutex::new(SharedConnectionState {
                credential: None,
                issued_by_call: HashMap::new(),
                next_read_after: i128::MIN,
            }),
            refresh: Mutex::new(()),
        })
    }

    /// Returns the shared connection ID.
    pub fn shared_connection_id(&self) -> &str {
        &self.shared_connection_id
    }

    /// Returns an unexpired credential, refreshing stale material in one caller.
    ///
    /// # Errors
    ///
    /// Returns the broker failure, or `BrokerUnavailable` when only expired material remains.
    pub fn resolve(&self, call_id: &str) -> Result<JsonObject, BrokerReadError> {
        {
            let mut state = lock(&self.state);
            let now = now_unix_nanoseconds();
            if let Some(current) = state.credential.clone() {
                let is_usable_before_backoff =
                    !is_expired(&current, now) && now < state.next_read_after;
                if is_usable_before_backoff || !is_stale(&current, now) {
                    record_issued(&mut state, call_id, current.revision, now);
                    return Ok(current.credential);
                }
            }
        }
        let _refresh = lock(&self.refresh);
        let now = now_unix_nanoseconds();
        let (mut current, next_read_after) = {
            let state = lock(&self.state);
            (state.credential.clone(), state.next_read_after)
        };
        let should_read = current
            .as_ref()
            .is_none_or(|credential| is_stale(credential, now) && now >= next_read_after);
        if should_read {
            match self.broker.read(&self.shared_connection_id, 0) {
                Ok(credential) => {
                    lock(&self.state).credential = Some(credential.clone());
                    current = Some(credential);
                }
                Err(BrokerReadError::Project(ProjectConfigError::BrokerUnavailable(message))) => {
                    lock(&self.state).next_read_after = now + 10 * 1_000_000_000;
                    if current
                        .as_ref()
                        .is_none_or(|credential| is_expired(credential, now))
                    {
                        return Err(ProjectConfigError::BrokerUnavailable(message).into());
                    }
                }
                Err(error) => return Err(error),
            }
        }
        let current = match current {
            Some(credential) if !is_expired(&credential, now) => credential,
            _ => {
                return Err(ProjectConfigError::BrokerUnavailable(
                    "shared credential is expired".into(),
                )
                .into());
            }
        };
        record_issued(&mut lock(&self.state), call_id, current.revision, now);
        Ok(current.credential)
    }

    /// Forces a broker refresh past the revision this provider call received.
    ///
    /// # Errors
    ///
    /// Returns [`ProjectConfigError::RejectedCredentialNotRefreshed`] when the broker returns
    /// the same revision, or the broker failure.
    pub fn resolve_after_rejection(&self, call_id: &str) -> Result<JsonObject, BrokerReadError> {
        let _refresh = lock(&self.refresh);
        let now = now_unix_nanoseconds();
        let (cached, rejected_revision) = {
            let state = lock(&self.state);
            let rejected = state
                .issued_by_call
                .get(call_id)
                .map(|(revision, _)| *revision)
                .or_else(|| {
                    state
                        .credential
                        .as_ref()
                        .map(|credential| credential.revision)
                })
                .unwrap_or(0);
            (state.credential.clone(), rejected)
        };
        let current = match cached {
            Some(credential) if credential.revision != rejected_revision => credential,
            _ => {
                let credential = self
                    .broker
                    .read(&self.shared_connection_id, rejected_revision)?;
                lock(&self.state).credential = Some(credential.clone());
                credential
            }
        };
        if current.revision == rejected_revision {
            return Err(ProjectConfigError::RejectedCredentialNotRefreshed(
                "the rejected credential was not refreshed".into(),
            )
            .into());
        }
        record_issued(&mut lock(&self.state), call_id, current.revision, now);
        Ok(current.credential)
    }
}

fn now_unix_nanoseconds() -> i128 {
    Rfc3339NanoTimestamp::now_utc().unix_nanoseconds()
}

fn record_issued(state: &mut SharedConnectionState, call_id: &str, revision: u64, now: i128) {
    if call_id.is_empty() {
        return;
    }
    state
        .issued_by_call
        .insert(call_id.to_string(), (revision, now));
    if state.issued_by_call.len() > ISSUED_CALL_LIMIT {
        let cutoff = now - 10 * NANOSECONDS_PER_MINUTE;
        state
            .issued_by_call
            .retain(|_, (_, issued)| *issued > cutoff);
        if state.issued_by_call.len() > ISSUED_CALL_LIMIT
            && let Some(oldest) = state
                .issued_by_call
                .iter()
                .min_by_key(|(_, (_, issued))| *issued)
                .map(|(key, _)| key.clone())
        {
            state.issued_by_call.remove(&oldest);
        }
    }
}

fn is_expired(credential: &BrokerCredential, now: i128) -> bool {
    credential
        .expires_at
        .as_ref()
        .is_some_and(|expires_at| expires_at.unix_nanoseconds() <= now)
}

fn is_stale(credential: &BrokerCredential, now: i128) -> bool {
    let read_at = credential.read_at.unix_nanoseconds();
    let Some(expires_at) = credential
        .expires_at
        .as_ref()
        .map(Rfc3339NanoTimestamp::unix_nanoseconds)
    else {
        return now - read_at >= 15 * NANOSECONDS_PER_MINUTE;
    };
    let issued_at = credential
        .issued_at
        .as_ref()
        .map_or(read_at, Rfc3339NanoTimestamp::unix_nanoseconds);
    let lifetime = expires_at - issued_at;
    let skew = if lifetime > 0 {
        (5 * NANOSECONDS_PER_MINUTE).min(lifetime / 4)
    } else {
        0
    };
    now >= expires_at - skew
}
