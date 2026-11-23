// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! In-process state shared by every connector call of one Dex Step attempt.

use std::collections::{BTreeMap, BTreeSet, HashMap};
use std::sync::{Mutex, MutexGuard, PoisonError};

use dex_sdk::{BufferedTextStream, Context, HandlerResult, Stream};

use crate::errors::ValidationError;
use crate::singledispatch::markers::{CheckpointHeartbeat, Marker};
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
///     assert_eq!(call.max_sends(), 1);
///     Ok(())
/// }
/// ```
pub struct StepAttempt<'context> {
    context: Mutex<&'context mut Context>,
    flow_id: String,
    step_execution_id: String,
    attempt: u32,
    call_admissions: Mutex<CallAdmissions>,
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
            call_admissions: Mutex::new(CallAdmissions::default()),
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
        let occurrence = lock(&self.call_admissions).next_occurrence(operation);
        crate::types::derive_call_id(
            &self.flow_id,
            &self.step_execution_id,
            operation,
            occurrence,
        )
    }

    /// Creates the next Call of `operation` in this attempt.
    ///
    /// For a Mutation prefer [`crate::run_step_mutation`], which assigns the occurrence and
    /// admits the call in one step, so concurrent calls cannot reorder Call IDs.
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

    /// Assigns the next occurrence of a Mutation Call and admits it in one critical section.
    ///
    /// The refused call still counts, so later Call IDs follow call order, as in sdkgo.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for an invalid connection or operation.
    pub(crate) fn new_admitted_mutation_call(
        &self,
        connection: ConnectionRef,
        operation: OperationRef,
    ) -> Result<(Call<'_>, Option<RunningMutation<'_>>), ValidationError> {
        connection.validate()?;
        operation.validate()?;
        let (occurrence, is_admitted) = lock(&self.call_admissions).admit_mutation(&operation);
        let id = crate::types::derive_call_id(
            &self.flow_id,
            &self.step_execution_id,
            &operation,
            occurrence,
        )?;
        let running = is_admitted.then(|| RunningMutation {
            step_attempt: self,
            operation: operation.clone(),
        });
        let mut call = Call::new(id, connection, operation)?;
        call.step_attempt = Some(self);
        Ok((call, running))
    }

    /// Marks a Mutation as running; returns `false` while another call of it runs.
    pub(crate) fn begin_running_mutation(&self, operation: &OperationRef) -> bool {
        lock(&self.call_admissions)
            .running
            .insert(operation.clone())
    }

    pub(crate) fn end_running_mutation(&self, operation: &OperationRef) {
        lock(&self.call_admissions).running.remove(operation);
    }
}

/// Call occurrences and running Mutations of one attempt, guarded together.
#[derive(Default)]
pub(crate) struct CallAdmissions {
    occurrences: HashMap<OperationRef, u32>,
    running: BTreeSet<OperationRef>,
}

impl CallAdmissions {
    fn next_occurrence(&mut self, operation: &OperationRef) -> u32 {
        let count = self.occurrences.entry(operation.clone()).or_insert(0);
        *count += 1;
        *count
    }

    /// Returns the next occurrence and whether the call is admitted, in one critical section.
    fn admit_mutation(&mut self, operation: &OperationRef) -> (u32, bool) {
        let occurrence = self.next_occurrence(operation);
        (occurrence, self.running.insert(operation.clone()))
    }
}

/// Admission of one running Mutation call; dropping it ends the call.
pub(crate) struct RunningMutation<'attempt> {
    step_attempt: &'attempt (dyn StepAttemptAccess + 'attempt),
    operation: OperationRef,
}

impl<'attempt> RunningMutation<'attempt> {
    /// Admits `operation` on `step_attempt`, or returns `None` while another call of it runs.
    pub(crate) fn begin(
        step_attempt: &'attempt (dyn StepAttemptAccess + 'attempt),
        operation: &OperationRef,
    ) -> Option<Self> {
        step_attempt
            .begin_running_mutation(operation)
            .then(|| Self {
                step_attempt,
                operation: operation.clone(),
            })
    }
}

impl Drop for RunningMutation<'_> {
    fn drop(&mut self) {
        self.step_attempt.end_running_mutation(&self.operation);
    }
}

/// Lifetime-erased view of a [`StepAttempt`] held by a [`Call`].
///
/// `StepAttempt` is invariant in its Context lifetime, so a Call borrows it as a trait object.
pub(crate) trait StepAttemptAccess: Sync {
    fn attempt_number(&self) -> u32;
    fn attempt_flow_id(&self) -> &str;
    fn has_progress_stream(&self) -> bool;
    fn has_text_stream(&self) -> bool;
    fn report_progress(&self, call_id: &CallId, progress: Progress);
    fn write_text(&self, value: &str);
    fn begin_running_mutation(&self, operation: &OperationRef) -> bool;
    fn end_running_mutation(&self, operation: &OperationRef);
    fn checkpoint_markers(&self) -> &Mutex<Option<BTreeMap<String, Marker>>>;
    /// Returns the previous attempt's heartbeat value, `None` when absent, or a read error.
    fn last_heartbeat_value(&self) -> Result<Option<serde_json::Value>, String>;
    fn record_heartbeat_value(&self, value: CheckpointHeartbeat) -> Result<(), String>;
}

impl StepAttemptAccess for StepAttempt<'_> {
    fn attempt_number(&self) -> u32 {
        self.attempt
    }

    fn attempt_flow_id(&self) -> &str {
        &self.flow_id
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

    fn last_heartbeat_value(&self) -> Result<Option<serde_json::Value>, String> {
        lock(&self.context)
            .last_heartbeat_value::<serde_json::Value>()
            .map_err(|error| error.to_string())
    }

    fn record_heartbeat_value(&self, value: CheckpointHeartbeat) -> Result<(), String> {
        lock(&self.context)
            .record_heartbeat_value(value)
            .map_err(|error| error.to_string())
    }
}

pub(crate) fn lock<T>(mutex: &Mutex<T>) -> MutexGuard<'_, T> {
    // A panic in another call of the same attempt leaves plain data that stays consistent.
    mutex.lock().unwrap_or_else(PoisonError::into_inner)
}

#[cfg(test)]
mod tests {
    use std::sync::{Arc, Barrier};

    use super::*;

    #[test]
    fn mutation_occurrence_and_admission_follow_one_order() {
        let admissions = Arc::new(Mutex::new(CallAdmissions::default()));
        let operation = OperationRef::new("sample", "createItem");
        let first_admitted = Arc::new(Barrier::new(2));
        let second_checked = Arc::new(Barrier::new(2));
        let first = {
            let admissions = Arc::clone(&admissions);
            let operation = operation.clone();
            let first_admitted = Arc::clone(&first_admitted);
            let second_checked = Arc::clone(&second_checked);
            std::thread::spawn(move || {
                let admitted = lock(&admissions).admit_mutation(&operation);
                first_admitted.wait();
                second_checked.wait();
                lock(&admissions).running.remove(&operation);
                admitted
            })
        };
        first_admitted.wait();
        let second = lock(&admissions).admit_mutation(&operation);
        second_checked.wait();
        let first = first.join().expect("first call thread");
        assert_eq!(
            first,
            (1, true),
            "the first call owns occurrence 1 and runs"
        );
        assert_eq!(
            second,
            (2, false),
            "the overlapping call counts and is refused"
        );
        assert_eq!(lock(&admissions).admit_mutation(&operation), (3, true));
    }
}
