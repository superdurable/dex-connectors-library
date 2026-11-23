// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! In-process state shared by every connector call of one Dex Step attempt.

use std::collections::{BTreeMap, BTreeSet, HashMap};
use std::sync::{Mutex, MutexGuard, PoisonError};

use dex_sdk::{BufferedTextStream, Context, HandlerResult, Stream};

use crate::errors::ValidationError;
use crate::singledispatch::markers::Marker;
use crate::types::{Call, CallId, ConnectionRef, OperationRef, Progress, ProgressUpdate};

/// One Dex Step attempt's connector state: Call occurrences, running Mutations, the
/// single-dispatch checkpoint, and optional progress and text Streams.
///
/// Create one per Execute invocation from the handler's `Context` and build every
/// [`Call`] of that attempt from it with [`StepAttempt::new_call`], so Call IDs stay stable
/// across retries when calls run in a deterministic order. Never store it beyond the
/// invocation.
///
/// ```no_run
/// use dex_connectors_sdk::{ConnectionRef, OperationRef, StepAttempt};
///
/// fn execute(context: &mut dex_sdk::Context) -> Result<(), Box<dyn std::error::Error>> {
///     let attempt = StepAttempt::new(context);
///     let call = attempt.new_call(
///         ConnectionRef::new("sample", "main"),
///         OperationRef::new("sample", "createItem"),
///     )?;
///     assert_eq!(call.max_sends, 1);
///     Ok(())
/// }
/// ```
pub struct StepAttempt<'context> {
    context: Mutex<&'context mut Context>,
    flow_id: String,
    step_execution_id: String,
    attempt: u32,
    occurrences: Mutex<HashMap<OperationRef, u32>>,
    running_mutations: Mutex<BTreeSet<OperationRef>>,
    pub(crate) checkpoint_markers: Mutex<Option<BTreeMap<String, Marker>>>,
    progress_stream: Option<&'context Stream<ProgressUpdate>>,
    progress_sequence: Mutex<u64>,
    text_stream: Option<BufferedTextStream>,
}

impl<'context> StepAttempt<'context> {
    /// Wraps the Execute `Context` of one Step attempt.
    pub fn new(context: &'context mut Context) -> Self {
        Self::with_streams(context, None, None)
    }

    /// Wraps the Execute `Context` and the optional progress and text Streams.
    ///
    /// Both Streams are best effort and belong in the consuming Flow persistence schema.
    pub fn with_streams(
        context: &'context mut Context,
        progress_stream: Option<&'context Stream<ProgressUpdate>>,
        text_stream: Option<BufferedTextStream>,
    ) -> Self {
        let flow_id = context.flow_id().to_string();
        let step_execution_id = context.step_execution_id().to_string();
        let attempt = context.attempt();
        Self {
            context: Mutex::new(context),
            flow_id,
            step_execution_id,
            attempt,
            occurrences: Mutex::new(HashMap::new()),
            running_mutations: Mutex::new(BTreeSet::new()),
            checkpoint_markers: Mutex::new(None),
            progress_stream,
            progress_sequence: Mutex::new(0),
            text_stream,
        }
    }

    /// Returns the Dex Flow ID.
    pub fn flow_id(&self) -> &str {
        &self.flow_id
    }

    /// Returns the Dex Step execution ID.
    pub fn step_execution_id(&self) -> &str {
        &self.step_execution_id
    }

    /// Returns the one-based Dex attempt number.
    pub fn attempt(&self) -> u32 {
        self.attempt
    }

    /// Derives the next Call ID of `operation` in this attempt.
    ///
    /// The n-th call of the same operation gets occurrence n, as in sdkgo.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for an invalid operation.
    pub fn derive_call_id(&self, operation: &OperationRef) -> Result<CallId, ValidationError> {
        operation.validate()?;
        let occurrence = {
            let mut occurrences = lock(&self.occurrences);
            let count = occurrences.entry(operation.clone()).or_insert(0);
            *count += 1;
            *count
        };
        crate::types::derive_call_id(
            &self.flow_id,
            &self.step_execution_id,
            operation,
            occurrence,
        )
    }

    /// Creates the next Call of `operation` in this attempt with a send cap of 1.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for an invalid connection or operation.
    pub fn new_call(
        &self,
        connection: ConnectionRef,
        operation: OperationRef,
    ) -> Result<Call<'_>, ValidationError> {
        connection.validate()?;
        let id = self.derive_call_id(&operation)?;
        let mut call = Call::new(id, connection, operation)?;
        call.step_attempt = Some(self);
        Ok(call)
    }

    /// Runs `action` with exclusive access to the Dex `Context`.
    ///
    /// Do not call it re-entrantly from inside `action`.
    pub fn with_context<R>(&self, action: impl FnOnce(&mut Context) -> R) -> R {
        let mut context = lock(&self.context);
        action(&mut context)
    }

    pub(crate) fn has_progress_stream(&self) -> bool {
        self.progress_stream.is_some()
    }

    pub(crate) fn has_text_stream(&self) -> bool {
        self.text_stream.is_some()
    }

    pub(crate) fn report_progress(&self, call_id: &CallId, progress: Progress) {
        let Some(stream) = self.progress_stream else {
            return;
        };
        let mut sequence = lock(&self.progress_sequence);
        *sequence += 1;
        let update = ProgressUpdate {
            call_id: call_id.clone(),
            attempt: self.attempt,
            sequence: *sequence,
            phase: progress.phase,
            message: progress.message,
            percent: progress.percent,
            provider_sequence: progress.provider_sequence,
        };
        let written: HandlerResult<()> = self.with_context(|context| stream.write(context, update));
        if let Err(error) = written {
            tracing::warn!(error = %error, "connector progress Stream write failed");
        }
    }

    pub(crate) fn write_text(&self, value: &str) {
        if let Some(text_stream) = &self.text_stream
            && let Err(error) = text_stream.write(value)
        {
            tracing::warn!(error = %error, "connector text Stream write failed");
        }
    }

    /// Marks a Mutation as running; returns `false` while another call of it runs.
    pub(crate) fn begin_running_mutation(&self, operation: &OperationRef) -> bool {
        lock(&self.running_mutations).insert(operation.clone())
    }

    pub(crate) fn end_running_mutation(&self, operation: &OperationRef) {
        lock(&self.running_mutations).remove(operation);
    }
}

/// Lifetime-erased view of a [`StepAttempt`] held by a [`Call`].
///
/// `StepAttempt` is invariant in its Context lifetime, so a Call borrows it as a trait object.
pub(crate) trait StepAttemptAccess: Sync {
    fn attempt_number(&self) -> u32;
    fn has_progress_stream(&self) -> bool;
    fn has_text_stream(&self) -> bool;
    fn report_progress(&self, call_id: &CallId, progress: Progress);
    fn write_text(&self, value: &str);
    fn begin_running_mutation(&self, operation: &OperationRef) -> bool;
    fn end_running_mutation(&self, operation: &OperationRef);
    fn checkpoint_markers(&self) -> &Mutex<Option<BTreeMap<String, Marker>>>;
    /// Returns the previous attempt's heartbeat value, or `None` when absent or undecodable.
    fn last_heartbeat_value(&self) -> Option<serde_json::Value>;
    fn record_heartbeat_value(&self, value: serde_json::Value) -> Result<(), String>;
}

impl StepAttemptAccess for StepAttempt<'_> {
    fn attempt_number(&self) -> u32 {
        self.attempt
    }

    fn has_progress_stream(&self) -> bool {
        StepAttempt::has_progress_stream(self)
    }

    fn has_text_stream(&self) -> bool {
        StepAttempt::has_text_stream(self)
    }

    fn report_progress(&self, call_id: &CallId, progress: Progress) {
        StepAttempt::report_progress(self, call_id, progress);
    }

    fn write_text(&self, value: &str) {
        StepAttempt::write_text(self, value);
    }

    fn begin_running_mutation(&self, operation: &OperationRef) -> bool {
        StepAttempt::begin_running_mutation(self, operation)
    }

    fn end_running_mutation(&self, operation: &OperationRef) {
        StepAttempt::end_running_mutation(self, operation);
    }

    fn checkpoint_markers(&self) -> &Mutex<Option<BTreeMap<String, Marker>>> {
        &self.checkpoint_markers
    }

    fn last_heartbeat_value(&self) -> Option<serde_json::Value> {
        lock(&self.context)
            .last_heartbeat_value::<serde_json::Value>()
            .ok()
            .flatten()
    }

    fn record_heartbeat_value(&self, value: serde_json::Value) -> Result<(), String> {
        lock(&self.context)
            .record_heartbeat_value(value)
            .map_err(|error| error.to_string())
    }
}

pub(crate) fn lock<T>(mutex: &Mutex<T>) -> MutexGuard<'_, T> {
    // A panic in another call of the same attempt leaves plain data that stays consistent.
    mutex.lock().unwrap_or_else(PoisonError::into_inner)
}
