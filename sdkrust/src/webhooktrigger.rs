// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Webhook Trigger endpoints: verify, decode, and durably record provider callbacks in arrival
//! order.
//!
//! A [`WebhookEndpoint`] serves one connection and Trigger through the framework-neutral
//! [`WebhookEndpoint::handle_request`]; the host's HTTP server reads the complete body and
//! calls it on its request thread. Each Trigger binding runs one source from
//! [`WebhookEndpoint::new_source`]. [`WebhookEndpointRunner`] runs the bindings' Trigger
//! runners together.
//!
//! Responses are `text/plain; charset=utf-8` and end with a newline: 405 for a method other than
//! POST, 413 for a body over the limit, 503 when credentials cannot be resolved or verification
//! is temporarily unavailable, 400 for a failed verification or an undecodable event, 503 when
//! the event cannot be recorded within the arrival timeout, and 200 once every accepting binding
//! recorded it.

use std::collections::VecDeque;
use std::error::Error;
use std::fmt;
use std::sync::mpsc;
use std::sync::{Arc, Condvar, Mutex, PoisonError};
use std::thread;
use std::time::{Duration, Instant};

use sha2::{Digest, Sha256};
use uuid::Uuid;

use crate::credentials::{CredentialProvider, CredentialRefreshDriver, resolve_credential};
use crate::errors::ValidationError;
use crate::timestamps::Rfc3339NanoTimestamp;
use crate::trigger::{
    TriggerCancellation, TriggerContext, TriggerDeliveryAbortedError, TriggerError, TriggerEvent,
    TriggerFilter, TriggerLogLevel, TriggerLogger, TriggerRunner, TriggerSource, TriggerTarget,
    deliver_trigger, is_trigger_delivery_aborted, log_trigger_record, prepare_trigger_delivery,
    trigger_error_attributes, trigger_retry_delay, wait_for_trigger_delay,
};
use crate::types::{Call, CallId, ConnectionRef, OperationRef};

/// Events one binding's source queues before the endpoint answers 503.
pub const DEFAULT_SOURCE_QUEUE_CAPACITY: usize = 64;

/// How long a verified request may wait to be recorded, measured from its slot reservation.
pub const DEFAULT_ARRIVAL_TIMEOUT: Duration = Duration::from_secs(10);

const RUN_RESTART_RESET: Duration = Duration::from_secs(30);
const TEXT_CONTENT_TYPE: (&str, &str) = ("content-type", "text/plain; charset=utf-8");

/// Verification cannot run now, such as an unreadable secret; the endpoint answers 503.
#[derive(Debug, Default)]
pub struct WebhookVerificationUnavailableError {
    cause: Option<TriggerError>,
}

impl WebhookVerificationUnavailableError {
    /// Creates the error with an optional cause.
    pub fn new(cause: Option<TriggerError>) -> Self {
        Self { cause }
    }
}

impl fmt::Display for WebhookVerificationUnavailableError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("webhook verification is temporarily unavailable")
    }
}

impl Error for WebhookVerificationUnavailableError {
    fn source(&self) -> Option<&(dyn Error + 'static)> {
        self.cause
            .as_deref()
            .map(|cause| cause as &(dyn Error + 'static))
    }
}

/// One received webhook request with its complete raw body.
#[derive(Clone, PartialEq)]
pub struct WebhookRequest {
    /// The raw request target: path and query exactly as received.
    pub url: String,
    /// Header pairs in arrival order with lower-case names; duplicates are kept.
    pub headers: Vec<(String, String)>,
    /// The complete raw body, as signatures cover it.
    pub body: Vec<u8>,
    /// When the endpoint received the request.
    pub received_at: Rfc3339NanoTimestamp,
}

impl fmt::Debug for WebhookRequest {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("WebhookRequest")
            .field("url", &self.url)
            .field("headers", &self.headers)
            .field("body_bytes", &self.body.len())
            .field("received_at", &self.received_at)
            .finish()
    }
}

impl WebhookRequest {
    /// Returns the first value of a header, matched case-insensitively.
    pub fn header(&self, name: &str) -> Option<&str> {
        let wanted = name.to_ascii_lowercase();
        self.headers
            .iter()
            .find(|(key, _)| *key == wanted)
            .map(|(_, value)| value.as_str())
    }

    /// Returns every value of a header in arrival order, matched case-insensitively.
    pub fn header_values(&self, name: &str) -> Vec<&str> {
        let wanted = name.to_ascii_lowercase();
        self.headers
            .iter()
            .filter(|(key, _)| *key == wanted)
            .map(|(_, value)| value.as_str())
            .collect()
    }
}

/// The endpoint's answer to one request.
#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct WebhookResponse {
    /// HTTP status code.
    pub status: u16,
    /// Response header pairs.
    pub headers: Vec<(String, String)>,
    /// Response body.
    pub body: Vec<u8>,
}

impl WebhookResponse {
    /// Creates an empty response with `status`.
    pub fn new(status: u16) -> Self {
        Self {
            status,
            headers: Vec::new(),
            body: Vec::new(),
        }
    }

    fn text(status: u16, text: &str, extra_headers: &[(&str, &str)]) -> Self {
        let mut headers = vec![(
            TEXT_CONTENT_TYPE.0.to_string(),
            TEXT_CONTENT_TYPE.1.to_string(),
        )];
        headers.extend(
            extra_headers
                .iter()
                .map(|(name, value)| ((*name).to_string(), (*value).to_string())),
        );
        Self {
            status,
            headers,
            body: format!("{text}\n").into_bytes(),
        }
    }
}

/// Verifies a request with resolved credentials; an error carrying
/// [`WebhookVerificationUnavailableError`] answers 503, any other error 400.
///
/// The cancellation fires when the client disconnects.
pub type WebhookRequestVerifier<Credential> = Box<
    dyn Fn(&WebhookRequest, &Credential, &TriggerCancellation) -> Result<(), TriggerError>
        + Send
        + Sync,
>;

/// Decodes a verified request; `Ok(None)` is a valid event no binding handles (200) and an error
/// is 400.
pub type WebhookEventDecoder<Payload> = Box<
    dyn Fn(&WebhookRequest) -> Result<Option<TriggerEvent<Payload>>, TriggerError> + Send + Sync,
>;

/// Static configuration of one connection's webhook endpoint for one Trigger.
pub struct WebhookEndpointConfig<Credential, Payload> {
    /// Manifest connector ID.
    pub connector_id: String,
    /// Manifest Trigger name.
    pub trigger_name: String,
    /// Connection whose credentials verify requests.
    pub connection: ConnectionRef,
    /// Resolves the connection's credentials for each request.
    pub credentials: Arc<dyn CredentialProvider<Credential>>,
    /// Verifies a request.
    pub verify_request: WebhookRequestVerifier<Credential>,
    /// Decodes a verified request.
    pub decode_event: WebhookEventDecoder<Payload>,
    /// Largest accepted body in bytes; positive.
    pub maximum_body_bytes: usize,
    /// Optional refresh driver used to resolve credentials.
    pub credential_refresh: Option<Arc<dyn CredentialRefreshDriver<Credential>>>,
    /// Returns the receipt time; `None` uses the current UTC time.
    pub clock: Option<Box<dyn Fn() -> Rfc3339NanoTimestamp + Send + Sync>>,
    /// Events each source queues; at least 1.
    pub source_queue_capacity: usize,
    /// How long a verified request may wait to be recorded; positive.
    pub arrival_timeout: Duration,
    /// Receives the sources' delivery records; `None` uses the tracing logger.
    pub logger: Option<Arc<dyn TriggerLogger>>,
}

impl<Credential, Payload> WebhookEndpointConfig<Credential, Payload> {
    /// Creates a config with required fields and defaults for the rest.
    pub fn new(
        connector_id: impl Into<String>,
        trigger_name: impl Into<String>,
        connection: ConnectionRef,
        credentials: Arc<dyn CredentialProvider<Credential>>,
        verify_request: WebhookRequestVerifier<Credential>,
        decode_event: WebhookEventDecoder<Payload>,
        maximum_body_bytes: usize,
    ) -> Self {
        Self {
            connector_id: connector_id.into(),
            trigger_name: trigger_name.into(),
            connection,
            credentials,
            verify_request,
            decode_event,
            maximum_body_bytes,
            credential_refresh: None,
            clock: None,
            source_queue_capacity: DEFAULT_SOURCE_QUEUE_CAPACITY,
            arrival_timeout: DEFAULT_ARRIVAL_TIMEOUT,
            logger: None,
        }
    }
}

struct RegisteredSource<Payload> {
    source_id: u64,
    queue: Arc<SourceQueue<Payload>>,
    accepts_event: Arc<TriggerFilter<Payload>>,
    target: Arc<dyn TriggerTarget<Payload>>,
}

struct ArrivalState {
    next_ticket: u64,
    serving: u64,
}

struct EndpointShared<Payload> {
    sources: Mutex<Vec<RegisteredSource<Payload>>>,
    created_source_count: Mutex<usize>,
    next_source_id: Mutex<u64>,
    arrival: Mutex<ArrivalState>,
    arrival_turn: Condvar,
    source_queue_capacity: usize,
    logger: Option<Arc<dyn TriggerLogger>>,
}

/// Verifies, decodes, and records one connection's webhook requests for one Trigger.
///
/// Only a verified, decoded event takes an arrival slot, so unauthorized requests never hold up
/// valid ones. Events are recorded one at a time in arrival order: each running source that
/// accepts an event prepares it, such as a durable inbox write, and then queues it, before the
/// request is answered 200. A slot expires after `arrival_timeout` from its reservation or when
/// the client disconnects; the request then answers 503 even while it still waits behind an
/// earlier one. A write already in flight at expiry still completes and queues the event, so
/// delivery is at least once.
pub struct WebhookEndpoint<Credential, Payload> {
    config: WebhookEndpointConfig<Credential, Payload>,
    shared: Arc<EndpointShared<Payload>>,
}

enum TurnOutcome {
    Recorded(Result<(), TriggerError>),
    Cancelled,
}

impl<Credential, Payload> WebhookEndpoint<Credential, Payload>
where
    Credential: 'static,
    Payload: Clone + Send + Sync + 'static,
{
    /// Validates `config` and creates the endpoint.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for a blank identity or an invalid bound.
    pub fn new(
        config: WebhookEndpointConfig<Credential, Payload>,
    ) -> Result<Self, ValidationError> {
        if config.connector_id.trim().is_empty() || config.trigger_name.trim().is_empty() {
            return Err(ValidationError::new(
                "webhook endpoint connector ID and Trigger name are required",
            ));
        }
        if config.connection.name.trim().is_empty() {
            return Err(ValidationError::new(
                "webhook endpoint connection name is required",
            ));
        }
        if config.maximum_body_bytes == 0 {
            return Err(ValidationError::new(
                "webhook endpoint maximum_body_bytes must be a positive integer",
            ));
        }
        if config.source_queue_capacity == 0 {
            return Err(ValidationError::new(
                "webhook endpoint source_queue_capacity must be a positive integer",
            ));
        }
        if config.arrival_timeout.is_zero() {
            return Err(ValidationError::new(
                "webhook endpoint arrival_timeout must be positive",
            ));
        }
        let shared = Arc::new(EndpointShared {
            sources: Mutex::new(Vec::new()),
            created_source_count: Mutex::new(0),
            next_source_id: Mutex::new(0),
            arrival: Mutex::new(ArrivalState {
                next_ticket: 0,
                serving: 0,
            }),
            arrival_turn: Condvar::new(),
            source_queue_capacity: config.source_queue_capacity,
            logger: config.logger.clone(),
        });
        Ok(Self { config, shared })
    }

    /// Creates one binding's source; `accepts_event` selects its events and `None` accepts
    /// every event.
    ///
    /// The endpoint answers 503 to verified events until every created source is running, so a
    /// binding still replaying its inbox never misses an event another binding acknowledged.
    pub fn new_source(
        &self,
        accepts_event: Option<TriggerFilter<Payload>>,
    ) -> Arc<WebhookTriggerSource<Payload>> {
        *lock(&self.shared.created_source_count) += 1;
        let source_id = {
            let mut next = lock(&self.shared.next_source_id);
            *next += 1;
            *next
        };
        Arc::new(WebhookTriggerSource {
            endpoint: Arc::clone(&self.shared),
            source_id,
            accepts_event: Arc::new(accepts_event.unwrap_or_else(|| Box::new(|_| true))),
            queue: Arc::new(SourceQueue {
                events: Mutex::new(VecDeque::new()),
                changed: Condvar::new(),
            }),
            is_running: Mutex::new(false),
        })
    }

    /// Returns how many sources are running.
    pub fn running_source_count(&self) -> usize {
        lock(&self.shared.sources).len()
    }

    /// Answers one request with its complete body; HTTP server adapters call it.
    ///
    /// `url` is the raw request target and `cancellation` should fire when the client
    /// disconnects. Never pass a partial body: verification and decoding assume the complete
    /// bytes. The call blocks while the event waits for its arrival turn.
    pub fn handle_request(
        &self,
        method: &str,
        url: &str,
        headers: &[(String, String)],
        body: &[u8],
        cancellation: Option<&TriggerCancellation>,
    ) -> WebhookResponse {
        if !method.eq_ignore_ascii_case("POST") {
            return WebhookResponse::text(405, "method not allowed", &[("allow", "POST")]);
        }
        if body.len() > self.config.maximum_body_bytes {
            return WebhookResponse::text(
                413,
                "request body too large",
                &[("connection", "close")],
            );
        }
        let request_cancellation = cancellation.cloned().unwrap_or_default();
        let received_at = self
            .config
            .clock
            .as_ref()
            .map_or_else(Rfc3339NanoTimestamp::now_utc, |clock| clock());
        let request = WebhookRequest {
            url: url.to_string(),
            headers: headers
                .iter()
                .map(|(name, value)| (name.to_ascii_lowercase(), value.clone()))
                .collect(),
            body: body.to_vec(),
            received_at,
        };
        let Ok(credentials) = self.resolve_credentials(&request.body) else {
            return WebhookResponse::text(503, "webhook temporarily unavailable", &[]);
        };
        if let Err(error) =
            (self.config.verify_request)(&request, &credentials, &request_cancellation)
        {
            if has_verification_unavailable(error.as_ref()) {
                return WebhookResponse::text(503, "webhook temporarily unavailable", &[]);
            }
            return WebhookResponse::text(400, "invalid webhook signature", &[]);
        }
        let event = match (self.config.decode_event)(&request) {
            Ok(Some(event)) => event,
            Ok(None) => return WebhookResponse::new(200),
            Err(_) => return WebhookResponse::text(400, "invalid webhook event", &[]),
        };
        match self.record_in_arrival_order(event, &request_cancellation) {
            Ok(()) => WebhookResponse::new(200),
            Err(_) => WebhookResponse::text(503, "webhook delivery temporarily unavailable", &[]),
        }
    }

    fn resolve_credentials(&self, body: &[u8]) -> Result<Credential, TriggerError> {
        let call = Call::new(
            credential_call_id(&self.config.connector_id, &self.config.connection, body),
            self.config.connection.clone(),
            OperationRef::new(
                self.config.connector_id.clone(),
                self.config.trigger_name.clone(),
            ),
        )?;
        match &self.config.credential_refresh {
            Some(driver) => {
                resolve_credential(self.config.credentials.as_ref(), &call, driver.as_ref())
            }
            None => self.config.credentials.resolve(&call),
        }
    }

    fn record_in_arrival_order(
        &self,
        event: TriggerEvent<Payload>,
        request_cancellation: &TriggerCancellation,
    ) -> Result<(), TriggerError> {
        let deadline = Instant::now() + self.config.arrival_timeout;
        let slot = TriggerCancellation::new_child(request_cancellation);
        let ticket = {
            let mut arrival = lock(&self.shared.arrival);
            let ticket = arrival.next_ticket;
            arrival.next_ticket += 1;
            ticket
        };
        let (sender, receiver) = mpsc::channel::<TurnOutcome>();
        let cancel_sender = sender.clone();
        // A dropped receiver only means the request already answered.
        slot.on_cancel(Arc::new(move || {
            drop(cancel_sender.send(TurnOutcome::Cancelled))
        }));
        let shared = Arc::clone(&self.shared);
        let turn_slot = slot.clone();
        // The turn runs on its own thread so an expired request can answer while a write in
        // flight completes and queues the event.
        let spawned = thread::Builder::new()
            .name("dex-webhook-arrival".to_string())
            .spawn(move || {
                let outcome = run_arrival_turn(&shared, ticket, &turn_slot, &event);
                drop(sender.send(TurnOutcome::Recorded(outcome)));
            });
        if let Err(error) = spawned {
            self.shared.skip_ticket(ticket);
            return Err(Box::new(error));
        }
        let outcome = receiver.recv_timeout(deadline.saturating_duration_since(Instant::now()));
        match outcome {
            Ok(TurnOutcome::Recorded(result)) => result,
            Ok(TurnOutcome::Cancelled) | Err(_) => {
                slot.cancel();
                Err(Box::new(TriggerDeliveryAbortedError))
            }
        }
    }
}

impl<Payload> EndpointShared<Payload> {
    fn wait_for_turn(&self, ticket: u64) {
        let mut arrival = lock(&self.arrival);
        while arrival.serving != ticket {
            arrival = self
                .arrival_turn
                .wait(arrival)
                .unwrap_or_else(PoisonError::into_inner);
        }
    }

    fn finish_turn(&self, ticket: u64) {
        let mut arrival = lock(&self.arrival);
        if arrival.serving == ticket {
            arrival.serving += 1;
        }
        drop(arrival);
        self.arrival_turn.notify_all();
    }

    fn skip_ticket(&self, ticket: u64) {
        self.wait_for_turn(ticket);
        self.finish_turn(ticket);
    }
}

fn run_arrival_turn<Payload: Clone>(
    shared: &EndpointShared<Payload>,
    ticket: u64,
    slot: &TriggerCancellation,
    event: &TriggerEvent<Payload>,
) -> Result<(), TriggerError> {
    shared.wait_for_turn(ticket);
    let result = if slot.is_cancelled() {
        Err(Box::new(TriggerDeliveryAbortedError) as TriggerError)
    } else {
        record_event(shared, slot, event)
    };
    if let Err(error) = &result
        && slot.is_cancelled()
        && !is_trigger_delivery_aborted(error.as_ref())
    {
        log_trigger_record(
            shared.logger.as_deref(),
            &TriggerContext::new(),
            TriggerLogLevel::Debug,
            "webhook late record failed",
            trigger_error_attributes(error.as_ref()),
        );
    }
    shared.finish_turn(ticket);
    result
}

fn record_event<Payload: Clone>(
    shared: &EndpointShared<Payload>,
    slot: &TriggerCancellation,
    event: &TriggerEvent<Payload>,
) -> Result<(), TriggerError> {
    let running: Vec<RegisteredSource<Payload>> = lock(&shared.sources)
        .iter()
        .map(|source| RegisteredSource {
            source_id: source.source_id,
            queue: Arc::clone(&source.queue),
            accepts_event: Arc::clone(&source.accepts_event),
            target: Arc::clone(&source.target),
        })
        .collect();
    if running.is_empty() {
        return Err("no webhook Trigger source is running".into());
    }
    if running.len() < *lock(&shared.created_source_count) {
        return Err("a webhook Trigger source is not running yet".into());
    }
    let context = TriggerContext::with_cancellation(slot.clone());
    let mut accepting = Vec::new();
    for source in running {
        if !(source.accepts_event)(event) {
            continue;
        }
        prepare_trigger_delivery(&context, source.target.as_ref(), event)?;
        accepting.push(source.queue);
    }
    for queue in accepting {
        queue.enqueue(event.clone(), shared.source_queue_capacity)?;
    }
    Ok(())
}

fn has_verification_unavailable(error: &(dyn Error + 'static)) -> bool {
    let mut current = Some(error);
    for _ in 0..32 {
        let Some(candidate) = current else {
            return false;
        };
        if candidate.is::<WebhookVerificationUnavailableError>() {
            return true;
        }
        current = candidate.source();
    }
    false
}

/// Derives the stable credential Call ID of one webhook body, matching Python and TypeScript.
fn credential_call_id(connector_id: &str, connection: &ConnectionRef, body: &[u8]) -> CallId {
    let mut digest = Sha256::new();
    for part in [
        connector_id,
        connection.provider.as_str(),
        connection.name.as_str(),
    ] {
        digest.update(part.as_bytes());
        digest.update([0]);
    }
    digest.update(body);
    let digest = digest.finalize();
    let mut identity = [0_u8; 16];
    identity.copy_from_slice(&digest[..16]);
    identity[6] = (identity[6] & 0x0F) | 0x50;
    identity[8] = (identity[8] & 0x3F) | 0x80;
    CallId::new(Uuid::from_bytes(identity).to_string())
}

struct SourceQueue<Payload> {
    events: Mutex<VecDeque<TriggerEvent<Payload>>>,
    changed: Condvar,
}

impl<Payload> SourceQueue<Payload> {
    fn enqueue(&self, event: TriggerEvent<Payload>, capacity: usize) -> Result<(), TriggerError> {
        let mut events = lock(&self.events);
        if events.len() >= capacity {
            return Err("webhook Trigger delivery queue is full".into());
        }
        events.push_back(event);
        drop(events);
        self.changed.notify_all();
        Ok(())
    }

    fn notify(&self) {
        let _guard = lock(&self.events);
        self.changed.notify_all();
    }
}

/// One binding's source of an endpoint's events; create it with
/// [`WebhookEndpoint::new_source`].
///
/// `run` registers the source with its endpoint, delivers queued events in order with
/// [`deliver_trigger`], and returns [`TriggerDeliveryAbortedError`] once the context is
/// cancelled. It runs at most once at a time.
pub struct WebhookTriggerSource<Payload> {
    endpoint: Arc<EndpointShared<Payload>>,
    source_id: u64,
    accepts_event: Arc<TriggerFilter<Payload>>,
    queue: Arc<SourceQueue<Payload>>,
    is_running: Mutex<bool>,
}

impl<Payload: Send + Sync + 'static> TriggerSource<Payload> for WebhookTriggerSource<Payload> {
    fn run(
        &self,
        context: &TriggerContext,
        target: Arc<dyn TriggerTarget<Payload>>,
    ) -> Result<(), TriggerError> {
        {
            let mut is_running = lock(&self.is_running);
            if *is_running {
                return Err("webhook Trigger source is already running".into());
            }
            *is_running = true;
        }
        lock(&self.endpoint.sources).push(RegisteredSource {
            source_id: self.source_id,
            queue: Arc::clone(&self.queue),
            accepts_event: Arc::clone(&self.accepts_event),
            target: Arc::clone(&target),
        });
        let wake_queue = Arc::clone(&self.queue);
        context
            .cancellation()
            .on_cancel(Arc::new(move || wake_queue.notify()));
        let result = self.deliver_until_cancelled(context, target.as_ref());
        lock(&self.endpoint.sources).retain(|source| source.source_id != self.source_id);
        *lock(&self.is_running) = false;
        result
    }
}

impl<Payload> WebhookTriggerSource<Payload> {
    fn deliver_until_cancelled(
        &self,
        context: &TriggerContext,
        target: &dyn TriggerTarget<Payload>,
    ) -> Result<(), TriggerError> {
        loop {
            context.cancellation().check_cancelled()?;
            let next = {
                let mut events = lock(&self.queue.events);
                while events.is_empty() && !context.cancellation().is_cancelled() {
                    events = self
                        .queue
                        .changed
                        .wait(events)
                        .unwrap_or_else(PoisonError::into_inner);
                }
                events.pop_front()
            };
            if let Some(event) = next {
                deliver_trigger(context, target, &event, self.endpoint.logger.as_deref())?;
            }
        }
    }
}

/// Why a relayed request failed verification.
#[derive(Debug)]
pub enum RelayedRequestVerificationError {
    /// Verification cannot run now, such as an unavailable credential broker; answers 503.
    Unavailable(TriggerError),
    /// The request is not validly signed; answers 400.
    Refused(TriggerError),
}

impl fmt::Display for RelayedRequestVerificationError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            RelayedRequestVerificationError::Unavailable(_) => {
                formatter.write_str("relayed request verification is unavailable")
            }
            RelayedRequestVerificationError::Refused(_) => {
                formatter.write_str("relayed request signature is invalid")
            }
        }
    }
}

impl Error for RelayedRequestVerificationError {
    fn source(&self) -> Option<&(dyn Error + 'static)> {
        match self {
            RelayedRequestVerificationError::Unavailable(cause)
            | RelayedRequestVerificationError::Refused(cause) => {
                Some(cause.as_ref() as &(dyn Error + 'static))
            }
        }
    }
}

/// Verifies that a Dex host signed a relayed request, as a loaded project does.
pub trait RelayedRequestVerifier: Send + Sync {
    /// Returns an error when the request is not validly signed or cannot be checked now.
    ///
    /// # Errors
    ///
    /// Returns [`RelayedRequestVerificationError`].
    fn verify_relayed_request(
        &self,
        headers: &[(String, String)],
        body: &[u8],
        received_at: &Rfc3339NanoTimestamp,
    ) -> Result<(), RelayedRequestVerificationError>;
}

impl RelayedRequestVerifier for crate::projectconfig::LoadedProject {
    fn verify_relayed_request(
        &self,
        headers: &[(String, String)],
        body: &[u8],
        received_at: &Rfc3339NanoTimestamp,
    ) -> Result<(), RelayedRequestVerificationError> {
        let nanoseconds = u64::try_from(received_at.unix_nanoseconds()).unwrap_or(0);
        let received_at = std::time::UNIX_EPOCH + Duration::from_nanos(nanoseconds);
        crate::projectconfig::LoadedProject::verify_relayed_request(
            self,
            headers,
            body,
            received_at,
        )
        .map_err(|error| {
            let is_unavailable = matches!(
                error,
                crate::projectconfig::ProjectConfigError::BrokerUnavailable(_)
                    | crate::projectconfig::ProjectConfigError::RelayTokenRenewalDeferred
            );
            if is_unavailable {
                RelayedRequestVerificationError::Unavailable(Box::new(error))
            } else {
                RelayedRequestVerificationError::Refused(Box::new(error))
            }
        })
    }
}

/// Returns a request verifier that checks the Dex relay signature with `verifier`.
///
/// An unavailable verification becomes [`WebhookVerificationUnavailableError`] (503); every
/// other error is a refusal (400).
pub fn new_relayed_request_verification<Credential>(
    verifier: Arc<dyn RelayedRequestVerifier>,
) -> WebhookRequestVerifier<Credential> {
    Box::new(move |request, _credentials, _cancellation| {
        match verifier.verify_relayed_request(&request.headers, &request.body, &request.received_at)
        {
            Ok(()) => Ok(()),
            Err(RelayedRequestVerificationError::Unavailable(cause)) => Err(Box::new(
                WebhookVerificationUnavailableError::new(Some(cause)),
            )),
            Err(refused) => Err(Box::new(refused)),
        }
    })
}

/// Serves one endpoint and runs the Trigger runners of its bindings together.
///
/// [`Self::run_with_restarts`] restarts the runners after a failure with a backoff of 250
/// milliseconds doubling to 30 seconds, answering 503 meanwhile; [`Self::run_failure`] reports
/// the latest failure for a readiness probe.
pub struct WebhookEndpointRunner<Credential, Payload> {
    endpoint: Arc<WebhookEndpoint<Credential, Payload>>,
    runners: Vec<Arc<dyn TriggerRunner>>,
    run_failure: Mutex<Option<String>>,
}

impl<Credential, Payload> WebhookEndpointRunner<Credential, Payload>
where
    Credential: 'static,
    Payload: Clone + Send + Sync + 'static,
{
    /// Binds the endpoint and at least one runner.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] without a runner.
    pub fn new(
        endpoint: Arc<WebhookEndpoint<Credential, Payload>>,
        runners: Vec<Arc<dyn TriggerRunner>>,
    ) -> Result<Self, ValidationError> {
        if runners.is_empty() {
            return Err(ValidationError::new(
                "webhook endpoint runner requires an endpoint and at least one Trigger runner",
            ));
        }
        Ok(Self {
            endpoint,
            runners,
            run_failure: Mutex::new(None),
        })
    }

    /// Returns the safe text of the failure that stopped the runners, or `None` while they run.
    pub fn run_failure(&self) -> Option<String> {
        lock(&self.run_failure).clone()
    }

    /// Answers one request through the endpoint, or 503 while the runners are failed.
    pub fn handle_request(
        &self,
        method: &str,
        url: &str,
        headers: &[(String, String)],
        body: &[u8],
        cancellation: Option<&TriggerCancellation>,
    ) -> WebhookResponse {
        if lock(&self.run_failure).is_some() {
            return WebhookResponse::text(503, "webhook delivery temporarily unavailable", &[]);
        }
        self.endpoint
            .handle_request(method, url, headers, body, cancellation)
    }

    /// Runs every Trigger runner until the context is cancelled or one of them stops.
    ///
    /// When the first runner stops, the others are cancelled and joined.
    ///
    /// # Errors
    ///
    /// Returns [`TriggerDeliveryAbortedError`] when `context` was cancelled, otherwise the
    /// first runner's failure unless it is an abort.
    pub fn run(&self, context: &TriggerContext) -> Result<(), TriggerError> {
        let child_cancellation = TriggerCancellation::new_child(context.cancellation());
        let child = TriggerContext::with_cancellation(child_cancellation.clone())
            .with_log_attributes(context.log_attributes().clone());
        let (sender, receiver) = mpsc::channel::<Result<(), TriggerError>>();
        let first_result = thread::scope(|scope| {
            for runner in &self.runners {
                let sender = sender.clone();
                let child = child.clone();
                scope.spawn(move || {
                    // The receiver outlives every runner inside this scope.
                    drop(sender.send(runner.run(&child)));
                });
            }
            drop(sender);
            let first = receiver.recv().unwrap_or(Ok(()));
            child_cancellation.cancel();
            first
        });
        if context.cancellation().is_cancelled() {
            return Err(Box::new(TriggerDeliveryAbortedError));
        }
        match first_result {
            Err(error) if !is_trigger_delivery_aborted(error.as_ref()) => Err(error),
            _ => Ok(()),
        }
    }

    /// Runs the runners, restarting them after a failure until `context` is cancelled.
    ///
    /// A run that outlived the longest backoff starts the backoff over.
    pub fn run_with_restarts(&self, context: &TriggerContext) {
        let mut failed_attempts: u32 = 0;
        loop {
            let started_at = Instant::now();
            let failure: TriggerError = match self.run(context) {
                Ok(()) => "webhook Trigger runners stopped".into(),
                Err(error) => {
                    if context.cancellation().is_cancelled() {
                        return;
                    }
                    if is_trigger_delivery_aborted(error.as_ref()) {
                        "webhook Trigger runners stopped".into()
                    } else {
                        error
                    }
                }
            };
            if context.cancellation().is_cancelled() {
                return;
            }
            *lock(&self.run_failure) = Some(failure.to_string());
            log_trigger_record(
                self.endpoint.config.logger.as_deref(),
                &TriggerContext::new(),
                TriggerLogLevel::Error,
                "webhook endpoint runner failed",
                trigger_error_attributes(failure.as_ref()),
            );
            if started_at.elapsed() >= RUN_RESTART_RESET {
                failed_attempts = 0;
            }
            failed_attempts += 1;
            if wait_for_trigger_delay(context.cancellation(), trigger_retry_delay(failed_attempts))
                .is_err()
            {
                return;
            }
            *lock(&self.run_failure) = None;
        }
    }
}

fn lock<T>(mutex: &Mutex<T>) -> std::sync::MutexGuard<'_, T> {
    // A panic elsewhere leaves plain queues and counters that stay consistent.
    mutex.lock().unwrap_or_else(PoisonError::into_inner)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn credential_call_id_is_a_version_five_uuid() {
        let call_id = credential_call_id("slack", &ConnectionRef::new("slack", "main"), b"{}");
        assert_eq!(call_id.as_str(), "ed4d8209-5d64-56a2-b908-de95fed8c0d0");
    }
}
