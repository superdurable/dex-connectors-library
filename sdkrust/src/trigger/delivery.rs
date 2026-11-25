// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Trigger delivery outcomes, contexts, records, and the retrying delivery loop.

use std::collections::BTreeMap;
use std::error::Error;
use std::fmt;
use std::sync::{Arc, Condvar, Mutex, PoisonError};
use std::time::{Duration, Instant};

use crate::timestamps::Rfc3339NanoTimestamp;

/// Prefix of every [`UndeliverableTriggerError`] message.
pub const UNDELIVERABLE_TRIGGER_MESSAGE: &str = "Trigger event is undeliverable";

/// Error type a Dex Worker reports for an RPC handler's undeliverable marker, shared with Python
/// and TypeScript.
pub const UNDELIVERABLE_TRIGGER_ERROR_TYPE: &str = "ConnectorsSDK.UndeliverableTriggerError";

const MAXIMUM_CAUSE_DEPTH: usize = 32;
const RETRY_INITIAL_DELAY: Duration = Duration::from_millis(250);
const RETRY_MAXIMUM_DELAY: Duration = Duration::from_secs(30);

/// Boxed error returned by Trigger targets, sources, and stores.
pub type TriggerError = Box<dyn Error + Send + Sync + 'static>;

/// Marks an event no retry can deliver; the source consumes it.
///
/// Durable inboxes, Trigger runners, and [`deliver_trigger`] consume the event instead of
/// retrying it. The message is the marker followed by the cause's text when it has any.
#[derive(Debug)]
pub struct UndeliverableTriggerError {
    message: String,
    cause: Option<TriggerError>,
}

impl UndeliverableTriggerError {
    /// Wraps an optional reason.
    pub fn new(cause: Option<TriggerError>) -> Self {
        let reason = cause.as_ref().map(ToString::to_string).unwrap_or_default();
        let message = if reason.is_empty() {
            UNDELIVERABLE_TRIGGER_MESSAGE.to_string()
        } else {
            format!("{UNDELIVERABLE_TRIGGER_MESSAGE}: {reason}")
        };
        Self { message, cause }
    }

    /// Wraps a reason given as text.
    pub fn with_reason(reason: impl Into<String>) -> Self {
        Self::new(Some(reason.into().into()))
    }
}

impl fmt::Display for UndeliverableTriggerError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str(&self.message)
    }
}

impl Error for UndeliverableTriggerError {
    fn source(&self) -> Option<&(dyn Error + 'static)> {
        self.cause
            .as_deref()
            .map(|cause| cause as &(dyn Error + 'static))
    }
}

/// Returns `error` when it already is the marker, otherwise a new marker caused by it.
pub fn mark_trigger_undeliverable(error: TriggerError) -> TriggerError {
    if error.is::<UndeliverableTriggerError>() {
        error
    } else {
        Box::new(UndeliverableTriggerError::new(Some(error)))
    }
}

/// Reports whether `error` or an error in its source chain, up to 32 links, is the marker.
pub fn is_trigger_undeliverable(error: &(dyn Error + 'static)) -> bool {
    has_in_source_chain::<UndeliverableTriggerError>(error)
}

/// Reports whether `error` or its source chain, up to 32 links, is an abort.
pub fn is_trigger_delivery_aborted(error: &(dyn Error + 'static)) -> bool {
    has_in_source_chain::<TriggerDeliveryAbortedError>(error)
}

fn has_in_source_chain<Marker: Error + 'static>(error: &(dyn Error + 'static)) -> bool {
    let mut current = Some(error);
    for _ in 0..MAXIMUM_CAUSE_DEPTH {
        let Some(candidate) = current else {
            return false;
        };
        if candidate.is::<Marker>() {
            return true;
        }
        current = candidate.source();
    }
    false
}

/// Returned when a Trigger context's cancellation ends delivery.
#[derive(Debug, Default)]
pub struct TriggerDeliveryAbortedError;

impl fmt::Display for TriggerDeliveryAbortedError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("Trigger delivery was aborted")
    }
}

impl Error for TriggerDeliveryAbortedError {}

type CancellationListener = Arc<dyn Fn() + Send + Sync>;

struct CancellationState {
    is_cancelled: bool,
    children: Vec<TriggerCancellation>,
    listeners: Vec<CancellationListener>,
}

struct CancellationShared {
    state: Mutex<CancellationState>,
    changed: Condvar,
}

/// Cooperative cancellation shared by a Trigger run or one webhook arrival slot.
///
/// Cancelling stops new work, such as a retry wait or a late inbox write, without interrupting
/// work already in flight. A child is cancelled when its parent is. Clones share state.
#[derive(Clone)]
pub struct TriggerCancellation {
    shared: Arc<CancellationShared>,
}

impl Default for TriggerCancellation {
    fn default() -> Self {
        Self::new()
    }
}

impl fmt::Debug for TriggerCancellation {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("TriggerCancellation")
            .field("is_cancelled", &self.is_cancelled())
            .finish()
    }
}

impl TriggerCancellation {
    /// Creates a root cancellation.
    pub fn new() -> Self {
        Self {
            shared: Arc::new(CancellationShared {
                state: Mutex::new(CancellationState {
                    is_cancelled: false,
                    children: Vec::new(),
                    listeners: Vec::new(),
                }),
                changed: Condvar::new(),
            }),
        }
    }

    /// Creates a child, already cancelled when `parent` is.
    pub fn new_child(parent: &TriggerCancellation) -> Self {
        let child = Self::new();
        let mut parent_state = lock(&parent.shared.state);
        if parent_state.is_cancelled {
            lock(&child.shared.state).is_cancelled = true;
        } else {
            parent_state.children.push(child.clone());
        }
        child
    }

    /// Cancels this and every child; calling it again has no effect.
    pub fn cancel(&self) {
        let (children, listeners) = {
            let mut state = lock(&self.shared.state);
            if state.is_cancelled {
                return;
            }
            state.is_cancelled = true;
            (
                std::mem::take(&mut state.children),
                std::mem::take(&mut state.listeners),
            )
        };
        self.shared.changed.notify_all();
        for listener in listeners {
            listener();
        }
        for child in children {
            child.cancel();
        }
    }

    /// Reports whether cancellation was requested.
    pub fn is_cancelled(&self) -> bool {
        lock(&self.shared.state).is_cancelled
    }

    /// Blocks until cancellation was requested.
    pub fn wait(&self) {
        let mut state = lock(&self.shared.state);
        while !state.is_cancelled {
            state = self
                .shared
                .changed
                .wait(state)
                .unwrap_or_else(PoisonError::into_inner);
        }
    }

    /// Blocks up to `timeout`; returns whether cancellation was requested.
    pub fn wait_timeout(&self, timeout: Duration) -> bool {
        let deadline = Instant::now() + timeout;
        let mut state = lock(&self.shared.state);
        while !state.is_cancelled {
            let remaining = deadline.saturating_duration_since(Instant::now());
            if remaining.is_zero() {
                return false;
            }
            state = self
                .shared
                .changed
                .wait_timeout(state, remaining)
                .unwrap_or_else(PoisonError::into_inner)
                .0;
        }
        true
    }

    /// Returns [`TriggerDeliveryAbortedError`] when cancellation was requested.
    ///
    /// # Errors
    ///
    /// Returns the abort error once cancelled.
    pub fn check_cancelled(&self) -> Result<(), TriggerDeliveryAbortedError> {
        if self.is_cancelled() {
            Err(TriggerDeliveryAbortedError)
        } else {
            Ok(())
        }
    }

    /// Runs `listener` once on cancellation, immediately when already cancelled.
    ///
    /// Listeners wake waiters on other condition variables; they must not block.
    pub fn on_cancel(&self, listener: CancellationListener) {
        {
            let mut state = lock(&self.shared.state);
            if !state.is_cancelled {
                state.listeners.push(listener);
                return;
            }
        }
        listener();
    }
}

/// One safe record attribute value: an ID, count, enum value, or flag, never a payload.
#[derive(Clone, Debug, PartialEq)]
pub enum TriggerLogValue {
    /// Text such as an ID or enum value.
    Text(String),
    /// A signed count or code.
    Integer(i64),
    /// A floating-point measurement.
    Float(f64),
    /// A flag.
    Flag(bool),
}

impl fmt::Display for TriggerLogValue {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            TriggerLogValue::Text(value) => formatter.write_str(value),
            TriggerLogValue::Integer(value) => write!(formatter, "{value}"),
            TriggerLogValue::Float(value) => write!(formatter, "{value}"),
            TriggerLogValue::Flag(value) => write!(formatter, "{value}"),
        }
    }
}

impl From<&str> for TriggerLogValue {
    fn from(value: &str) -> Self {
        TriggerLogValue::Text(value.to_string())
    }
}

impl From<String> for TriggerLogValue {
    fn from(value: String) -> Self {
        TriggerLogValue::Text(value)
    }
}

impl From<i64> for TriggerLogValue {
    fn from(value: i64) -> Self {
        TriggerLogValue::Integer(value)
    }
}

impl From<bool> for TriggerLogValue {
    fn from(value: bool) -> Self {
        TriggerLogValue::Flag(value)
    }
}

/// Attributes of one Trigger record, sorted by key.
pub type TriggerLogAttributes = BTreeMap<String, TriggerLogValue>;

/// Severity of one Trigger delivery record.
#[derive(Clone, Copy, Debug, Eq, Hash, Ord, PartialEq, PartialOrd)]
pub enum TriggerLogLevel {
    /// Per-event delivery detail.
    Debug,
    /// Replay progress, filtered events, and recovered deliveries.
    Info,
    /// Retried failures and undeliverable events.
    Warn,
    /// Inbox read, write, and removal failures.
    Error,
}

/// Receives Trigger delivery records. Implementations must not panic.
pub trait TriggerLogger: Send + Sync {
    /// Reports whether records at `level` are written, so callers can skip building them.
    fn is_enabled_for(&self, level: TriggerLogLevel) -> bool;

    /// Writes one record.
    fn log(&self, level: TriggerLogLevel, message: &str, attributes: &TriggerLogAttributes);
}

/// The default Trigger logger: writes records through `tracing` with target
/// `dex_connectors_sdk::trigger`.
///
/// Each event's message is the Trigger message followed by its attributes as compact sorted
/// JSON-like `key=value` pairs.
#[derive(Clone, Copy, Debug, Default)]
pub struct TracingTriggerLogger;

impl TriggerLogger for TracingTriggerLogger {
    fn is_enabled_for(&self, level: TriggerLogLevel) -> bool {
        match level {
            TriggerLogLevel::Debug => {
                tracing::enabled!(target: "dex_connectors_sdk::trigger", tracing::Level::DEBUG)
            }
            TriggerLogLevel::Info => {
                tracing::enabled!(target: "dex_connectors_sdk::trigger", tracing::Level::INFO)
            }
            TriggerLogLevel::Warn => {
                tracing::enabled!(target: "dex_connectors_sdk::trigger", tracing::Level::WARN)
            }
            TriggerLogLevel::Error => {
                tracing::enabled!(target: "dex_connectors_sdk::trigger", tracing::Level::ERROR)
            }
        }
    }

    fn log(&self, level: TriggerLogLevel, message: &str, attributes: &TriggerLogAttributes) {
        let rendered = attributes
            .iter()
            .map(|(key, value)| format!("{key}={value}"))
            .collect::<Vec<_>>()
            .join(" ");
        match level {
            TriggerLogLevel::Debug => {
                tracing::debug!(target: "dex_connectors_sdk::trigger", attributes = %rendered, "{message}");
            }
            TriggerLogLevel::Info => {
                tracing::info!(target: "dex_connectors_sdk::trigger", attributes = %rendered, "{message}");
            }
            TriggerLogLevel::Warn => {
                tracing::warn!(target: "dex_connectors_sdk::trigger", attributes = %rendered, "{message}");
            }
            TriggerLogLevel::Error => {
                tracing::error!(target: "dex_connectors_sdk::trigger", attributes = %rendered, "{message}");
            }
        }
    }
}

struct AttributeBindingTriggerLogger {
    base: Arc<dyn TriggerLogger>,
    attributes: TriggerLogAttributes,
}

impl TriggerLogger for AttributeBindingTriggerLogger {
    fn is_enabled_for(&self, level: TriggerLogLevel) -> bool {
        self.base.is_enabled_for(level)
    }

    fn log(&self, level: TriggerLogLevel, message: &str, attributes: &TriggerLogAttributes) {
        let mut merged = self.attributes.clone();
        merged.extend(
            attributes
                .iter()
                .map(|(key, value)| (key.clone(), value.clone())),
        );
        self.base.log(level, message, &merged);
    }
}

/// Returns a logger that adds `attributes` to every record; a record keeps its own value for a
/// shared key. `None` binds the default [`TracingTriggerLogger`].
pub fn bind_trigger_log_attributes(
    logger: Option<Arc<dyn TriggerLogger>>,
    attributes: TriggerLogAttributes,
) -> Arc<dyn TriggerLogger> {
    Arc::new(AttributeBindingTriggerLogger {
        base: logger.unwrap_or_else(|| Arc::new(TracingTriggerLogger)),
        attributes,
    })
}

type SkipRecorder = Arc<dyn Fn() + Send + Sync>;

/// Per-delivery cancellation, log attributes, and skip recording, passed to every target call.
///
/// Create the root with [`TriggerContext::new`] and derive attempts with
/// [`start_trigger_attempt`]. Clones share the cancellation.
#[derive(Clone)]
pub struct TriggerContext {
    cancellation: TriggerCancellation,
    log_attributes: Arc<TriggerLogAttributes>,
    on_skip: Option<SkipRecorder>,
}

impl Default for TriggerContext {
    fn default() -> Self {
        Self::new()
    }
}

impl fmt::Debug for TriggerContext {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("TriggerContext")
            .field("cancellation", &self.cancellation)
            .field("log_attributes", &self.log_attributes)
            .finish()
    }
}

impl TriggerContext {
    /// Creates a root context that is never cancelled unless its cancellation is.
    pub fn new() -> Self {
        Self::with_cancellation(TriggerCancellation::new())
    }

    /// Creates a root context sharing `cancellation`.
    pub fn with_cancellation(cancellation: TriggerCancellation) -> Self {
        Self {
            cancellation,
            log_attributes: Arc::new(TriggerLogAttributes::new()),
            on_skip: None,
        }
    }

    pub(crate) fn derive(
        cancellation: TriggerCancellation,
        log_attributes: Arc<TriggerLogAttributes>,
        on_skip: Option<SkipRecorder>,
    ) -> Self {
        Self {
            cancellation,
            log_attributes,
            on_skip,
        }
    }

    /// Ends delivery: sources stop, retries stop waiting, and late inbox writes are refused.
    pub fn cancellation(&self) -> &TriggerCancellation {
        &self.cancellation
    }

    /// Event-scoped IDs, such as a channel ID, added to every delivery record.
    pub fn log_attributes(&self) -> &TriggerLogAttributes {
        &self.log_attributes
    }

    /// Returns a context whose records also carry `attributes`; never pass message text or
    /// payloads.
    pub fn with_log_attributes(&self, attributes: TriggerLogAttributes) -> TriggerContext {
        let mut merged = (*self.log_attributes).clone();
        merged.extend(attributes);
        let parent = self.clone();
        TriggerContext::derive(
            self.cancellation.clone(),
            Arc::new(merged),
            Some(Arc::new(move || parent.record_skip())),
        )
    }

    /// Records that a component in this attempt consumed the event as undeliverable and logged
    /// it.
    pub fn record_skip(&self) {
        if let Some(on_skip) = &self.on_skip {
            on_skip();
        }
    }
}

/// One delivery attempt started with [`start_trigger_attempt`].
#[derive(Clone)]
pub struct TriggerAttempt {
    context: TriggerContext,
    skipped: Arc<Mutex<bool>>,
}

impl TriggerAttempt {
    /// The context to pass to `handle_trigger` for this attempt.
    pub fn context(&self) -> &TriggerContext {
        &self.context
    }

    /// Reports whether a component inside the attempt consumed the event as undeliverable.
    pub fn was_skipped(&self) -> bool {
        *lock(&self.skipped)
    }
}

/// Starts one delivery attempt for a source that retries on its own schedule.
///
/// The attempt shares the context's cancellation and attributes; its `record_skip` also
/// reaches `context`.
pub fn start_trigger_attempt(context: &TriggerContext) -> TriggerAttempt {
    let skipped = Arc::new(Mutex::new(false));
    let recorded = Arc::clone(&skipped);
    let parent = context.clone();
    let attempt_context = TriggerContext::derive(
        context.cancellation.clone(),
        Arc::clone(&context.log_attributes),
        Some(Arc::new(move || {
            *lock(&recorded) = true;
            parent.record_skip();
        })),
    );
    TriggerAttempt {
        context: attempt_context,
        skipped,
    }
}

/// Logs one record with the context's attributes; record attributes win.
pub fn log_trigger_record(
    logger: Option<&dyn TriggerLogger>,
    context: &TriggerContext,
    level: TriggerLogLevel,
    message: &str,
    attributes: TriggerLogAttributes,
) {
    let default_logger = TracingTriggerLogger;
    let target: &dyn TriggerLogger = logger.unwrap_or(&default_logger);
    if !target.is_enabled_for(level) {
        return;
    }
    let mut merged = (*context.log_attributes).clone();
    merged.extend(attributes);
    target.log(level, message, &merged);
}

fn trigger_error_type_name(error: &(dyn Error + 'static)) -> String {
    if error.is::<UndeliverableTriggerError>() {
        return "UndeliverableTriggerError".to_string();
    }
    if error.is::<TriggerDeliveryAbortedError>() {
        return "TriggerDeliveryAbortedError".to_string();
    }
    if let Some(sdk_error) = error.downcast_ref::<dex_sdk::SdkError>() {
        return sdk_error_variant_name(sdk_error).to_string();
    }
    "Error".to_string()
}

fn sdk_error_variant_name(error: &dex_sdk::SdkError) -> &'static str {
    match error {
        dex_sdk::SdkError::FlowAlreadyStarted { .. } => "FlowAlreadyStarted",
        dex_sdk::SdkError::FlowClosing { .. } => "FlowClosing",
        dex_sdk::SdkError::FlowNotFound { .. } => "FlowNotFound",
        dex_sdk::SdkError::FlowNotActiveOrNotFound { .. } => "FlowNotActiveOrNotFound",
        dex_sdk::SdkError::WorkerInvocation { .. } => "WorkerInvocation",
        dex_sdk::SdkError::ValueMapping { .. } => "ValueMapping",
        dex_sdk::SdkError::InvalidArgument { .. } => "InvalidArgument",
        _ => "SdkError",
    }
}

/// Record attributes for an error: its type, its source's type, and a gRPC code.
///
/// Messages are never included, because handlers, providers, and credential stores may put
/// secrets or payload text in them.
pub fn trigger_error_attributes(error: &(dyn Error + 'static)) -> TriggerLogAttributes {
    let mut attributes = TriggerLogAttributes::new();
    attributes.insert(
        "error_type".to_string(),
        TriggerLogValue::Text(trigger_error_type_name(error)),
    );
    if let Some(source) = error.source() {
        attributes.insert(
            "error_cause_type".to_string(),
            TriggerLogValue::Text(trigger_error_type_name(source)),
        );
    }
    if let Some(service) = error
        .downcast_ref::<dex_sdk::SdkError>()
        .and_then(dex_sdk::SdkError::service_error)
    {
        attributes.insert(
            "error_code".to_string(),
            TriggerLogValue::Integer(i64::from(i32::from(service.code()))),
        );
    }
    attributes
}

/// Returns the retry delay before attempt `attempt + 1`: 250 ms doubling to 30 s.
pub fn trigger_retry_delay(attempt: u32) -> Duration {
    let mut delay = RETRY_INITIAL_DELAY;
    let mut completed = 1;
    while completed < attempt && delay < RETRY_MAXIMUM_DELAY {
        delay *= 2;
        completed += 1;
    }
    delay.min(RETRY_MAXIMUM_DELAY)
}

/// Waits `delay` unless cancelled first.
///
/// # Errors
///
/// Returns [`TriggerDeliveryAbortedError`] when cancelled before or during the wait.
pub fn wait_for_trigger_delay(
    cancellation: &TriggerCancellation,
    delay: Duration,
) -> Result<(), TriggerDeliveryAbortedError> {
    cancellation.check_cancelled()?;
    cancellation.wait_timeout(delay);
    cancellation.check_cancelled()
}

/// One provider event with a stable provider identity.
#[derive(Clone, Debug, PartialEq)]
pub struct TriggerEvent<Payload> {
    /// Stable provider identifier; also the Flow-start request ID.
    pub id: String,
    /// When the provider event occurred.
    pub occurred_at: Rfc3339NanoTimestamp,
    /// Connector-decoded provider event body.
    pub payload: Payload,
}

/// Handles one event after the provider transport acknowledges receipt.
///
/// `handle_trigger` returns `Ok` when it consumes the event, an error carrying
/// [`UndeliverableTriggerError`] when no retry can deliver it, or another error to keep the
/// event pending for a retry. A durable target also persists an event before acknowledgement
/// and replays persisted events; it overrides the remaining methods.
pub trait TriggerTarget<Payload>: Send + Sync {
    /// Consumes, retries, or marks one provider event undeliverable.
    ///
    /// # Errors
    ///
    /// Returns an undeliverable marker to consume the event, or another error to retry it.
    fn handle_trigger(
        &self,
        context: &TriggerContext,
        event: &TriggerEvent<Payload>,
    ) -> Result<(), TriggerError>;

    /// Reports whether [`Self::prepare_trigger`] persists events before acknowledgement.
    fn prepares_trigger_delivery(&self) -> bool {
        false
    }

    /// Persists an event before provider acknowledgement; the default does nothing.
    ///
    /// # Errors
    ///
    /// Returns an error when the event could not be persisted; the source must not acknowledge.
    fn prepare_trigger(
        &self,
        context: &TriggerContext,
        event: &TriggerEvent<Payload>,
    ) -> Result<(), TriggerError> {
        let _ = (context, event);
        Ok(())
    }

    /// Reports whether [`Self::replay_trigger_deliveries`] redelivers persisted events.
    fn replays_trigger_deliveries(&self) -> bool {
        false
    }

    /// Delivers every pending event persisted before a previous process stopped, in order.
    ///
    /// # Errors
    ///
    /// Returns an error when replay cannot read or deliver the pending events.
    fn replay_trigger_deliveries(&self, context: &TriggerContext) -> Result<(), TriggerError> {
        let _ = context;
        Ok(())
    }
}

impl<Payload, Target: TriggerTarget<Payload> + ?Sized> TriggerTarget<Payload> for Arc<Target> {
    fn handle_trigger(
        &self,
        context: &TriggerContext,
        event: &TriggerEvent<Payload>,
    ) -> Result<(), TriggerError> {
        (**self).handle_trigger(context, event)
    }

    fn prepares_trigger_delivery(&self) -> bool {
        (**self).prepares_trigger_delivery()
    }

    fn prepare_trigger(
        &self,
        context: &TriggerContext,
        event: &TriggerEvent<Payload>,
    ) -> Result<(), TriggerError> {
        (**self).prepare_trigger(context, event)
    }

    fn replays_trigger_deliveries(&self) -> bool {
        (**self).replays_trigger_deliveries()
    }

    fn replay_trigger_deliveries(&self, context: &TriggerContext) -> Result<(), TriggerError> {
        (**self).replay_trigger_deliveries(context)
    }
}

/// Asks a durable target to persist an event before acknowledgement; other targets need
/// nothing.
///
/// # Errors
///
/// Returns the target's persistence error.
pub fn prepare_trigger_delivery<Payload>(
    context: &TriggerContext,
    target: &dyn TriggerTarget<Payload>,
    event: &TriggerEvent<Payload>,
) -> Result<(), TriggerError> {
    if target.prepares_trigger_delivery() {
        target.prepare_trigger(context, event)
    } else {
        Ok(())
    }
}

/// Reports whether a target both records and replays events, as a durable inbox target does.
pub fn has_durable_trigger_inbox<Payload>(target: &dyn TriggerTarget<Payload>) -> bool {
    target.prepares_trigger_delivery() && target.replays_trigger_deliveries()
}

/// Hands one acknowledged event to `target` until it consumes the event.
///
/// A returned attempt or an undeliverable marker consumes the event. Every other error is
/// retried after a delay that starts at 250 milliseconds and doubles up to 30 seconds. It logs
/// WARN "trigger delivery failed; retrying" before every wait, INFO "trigger delivered after
/// retry" when a later attempt succeeds, and WARN "trigger event skipped: undeliverable" when
/// the target returns the marker.
///
/// # Errors
///
/// Returns [`TriggerDeliveryAbortedError`] as soon as the context is cancelled, including
/// during a wait.
pub fn deliver_trigger<Payload>(
    context: &TriggerContext,
    target: &dyn TriggerTarget<Payload>,
    event: &TriggerEvent<Payload>,
    logger: Option<&dyn TriggerLogger>,
) -> Result<(), TriggerDeliveryAbortedError> {
    let mut attempt_number: u32 = 1;
    loop {
        context.cancellation.check_cancelled()?;
        let attempt = start_trigger_attempt(context);
        match target.handle_trigger(&attempt.context, event) {
            Ok(()) => {
                if attempt_number > 1 && !attempt.was_skipped() {
                    let mut attributes = event_attributes(&event.id);
                    attributes.insert(
                        "attempts".to_string(),
                        TriggerLogValue::Integer(i64::from(attempt_number)),
                    );
                    log_trigger_record(
                        logger,
                        context,
                        TriggerLogLevel::Info,
                        "trigger delivered after retry",
                        attributes,
                    );
                }
                return Ok(());
            }
            Err(error) => {
                if is_trigger_undeliverable(error.as_ref()) {
                    let mut attributes = event_attributes(&event.id);
                    attributes.extend(trigger_error_attributes(error.as_ref()));
                    log_trigger_record(
                        logger,
                        context,
                        TriggerLogLevel::Warn,
                        "trigger event skipped: undeliverable",
                        attributes,
                    );
                    return Ok(());
                }
                // The attempt failed because delivery ended, so there is no retry to report.
                context.cancellation.check_cancelled()?;
                let delay = trigger_retry_delay(attempt_number);
                let mut attributes = event_attributes(&event.id);
                attributes.insert(
                    "attempt".to_string(),
                    TriggerLogValue::Integer(i64::from(attempt_number)),
                );
                attributes.insert(
                    "delay_ms".to_string(),
                    TriggerLogValue::Integer(i64::try_from(delay.as_millis()).unwrap_or(i64::MAX)),
                );
                attributes.extend(trigger_error_attributes(error.as_ref()));
                log_trigger_record(
                    logger,
                    context,
                    TriggerLogLevel::Warn,
                    "trigger delivery failed; retrying",
                    attributes,
                );
                wait_for_trigger_delay(&context.cancellation, delay)?;
                attempt_number += 1;
            }
        }
    }
}

pub(crate) fn event_attributes(event_id: &str) -> TriggerLogAttributes {
    TriggerLogAttributes::from([(
        "event_id".to_string(),
        TriggerLogValue::Text(event_id.to_string()),
    )])
}

pub(crate) fn lock<T>(mutex: &Mutex<T>) -> std::sync::MutexGuard<'_, T> {
    // A panic elsewhere leaves plain flags and queues that stay consistent.
    mutex.lock().unwrap_or_else(PoisonError::into_inner)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn retry_delay_doubles_to_thirty_seconds() {
        assert_eq!(trigger_retry_delay(1), Duration::from_millis(250));
        assert_eq!(trigger_retry_delay(2), Duration::from_millis(500));
        assert_eq!(trigger_retry_delay(8), Duration::from_secs(30));
        assert_eq!(trigger_retry_delay(100), Duration::from_secs(30));
    }

    #[test]
    fn undeliverable_is_found_through_source_chain() {
        let marker: TriggerError = Box::new(UndeliverableTriggerError::with_reason("bad"));
        assert!(is_trigger_undeliverable(marker.as_ref()));
        assert_eq!(marker.to_string(), "Trigger event is undeliverable: bad");
        #[derive(Debug)]
        struct Wrapper(TriggerError);
        impl fmt::Display for Wrapper {
            fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
                formatter.write_str("wrapper")
            }
        }
        impl Error for Wrapper {
            fn source(&self) -> Option<&(dyn Error + 'static)> {
                Some(self.0.as_ref())
            }
        }
        let wrapped = Wrapper(marker);
        assert!(is_trigger_undeliverable(&wrapped));
        let plain: TriggerError = "plain".into();
        assert!(!is_trigger_undeliverable(plain.as_ref()));
        let marked = mark_trigger_undeliverable(plain);
        assert!(is_trigger_undeliverable(marked.as_ref()));
    }

    #[test]
    fn cancelling_parent_cancels_children_and_listeners() {
        let parent = TriggerCancellation::new();
        let child = TriggerCancellation::new_child(&parent);
        let fired = Arc::new(Mutex::new(false));
        let observed = Arc::clone(&fired);
        child.on_cancel(Arc::new(move || *lock(&observed) = true));
        assert!(!child.wait_timeout(Duration::from_millis(1)));
        parent.cancel();
        assert!(child.is_cancelled());
        assert!(*lock(&fired));
        assert!(TriggerCancellation::new_child(&parent).is_cancelled());
    }
}
