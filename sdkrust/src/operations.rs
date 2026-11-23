// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Provider operation contracts independent of Dex Worker registration.

use crate::errors::{Failure, FailureKind, RetryError, ValidationError};
use crate::singledispatch::{MINIMUM_MAX_SENDS, validate_max_sends};
use crate::step_attempt::{RunningMutation, StepAttempt};
use crate::timestamps::Rfc3339NanoTimestamp;
use crate::types::{
    Call, CallId, ConnectionRef, DEFECT_BRANCH_ID, IdempotencyKey, MutationAttempt,
    MutationDefinition, MutationResult, OperationRef, QueryAttempt, QueryDefinition, QueryResult,
    Receipt, RepeatOnUnknown, UNCERTAIN_BRANCH_ID,
};

/// Read-only provider operation invoked once per Dex Execute attempt.
///
/// Implementations classify a definite business outcome as a declared branch and a
/// safe-to-repeat failure as retry. They must not retain credentials in results. Handlers are
/// synchronous, like Dex Rust Step handlers; perform blocking I/O directly.
pub trait Query: Send + Sync {
    /// Operation input.
    type Input;
    /// Operation value stored in [`QueryResult::value`]; it crosses Dex as a Step input.
    type Output: dex_sdk::Value + Clone;

    /// Returns the immutable operation identity, branches, and Step defaults.
    fn definition(&self) -> QueryDefinition;

    /// Performs one provider call and classifies its attempt.
    fn invoke(&self, call: &Call<'_>, input: &Self::Input) -> QueryAttempt<Self::Output>;
}

/// Provider write with stable deduplication across Dex Execute retries.
///
/// An uncertain attempt is valid only when a request may have reached the provider and
/// neither safe repetition nor read-back can resolve the outcome.
pub trait Mutation: Send + Sync {
    /// Operation input.
    type Input;
    /// Operation value stored in [`MutationResult::value`]; it crosses Dex as a Step input.
    type Output: dex_sdk::Value + Clone;

    /// Returns the immutable operation identity, branches, Step defaults, and resend tier.
    fn definition(&self) -> MutationDefinition;

    /// Returns a provider-safe key stable for the logical call and input.
    ///
    /// An empty key asks the SDK to use the Call ID.
    fn idempotency_key(&self, call_id: &CallId, input: &Self::Input) -> IdempotencyKey;

    /// Performs one provider write and classifies its attempt.
    fn invoke(&self, call: &Call<'_>, input: &Self::Input) -> MutationAttempt<Self::Output>;
}

/// Terminal result or retry request from [`run_query`] and [`run_mutation`].
#[derive(Clone, Debug, PartialEq)]
pub enum OperationOutcome<R> {
    /// A validated terminal result on a declared branch.
    Completed(R),
    /// A safe-to-repeat failure; a Dex-facing Step maps it with [`crate::dex_retry_error`].
    Retry(RetryError),
}

impl<R> OperationOutcome<R> {
    /// Converts a retry into `Err`, for `?` in application Steps.
    ///
    /// # Errors
    ///
    /// Returns the [`RetryError`] for a retry outcome.
    pub fn into_result(self) -> Result<R, RetryError> {
        match self {
            OperationOutcome::Completed(result) => Ok(result),
            OperationOutcome::Retry(retry) => Err(retry),
        }
    }
}

/// Invokes a Query once and validates its declared terminal branch and receipt.
///
/// A retry returns [`OperationOutcome::Retry`] for the Dex-facing Step to schedule. Invalid
/// operation output selects `defect` with a safe local failure and a `None` value.
pub fn run_query<Operation: Query + ?Sized>(
    operation: &Operation,
    call: &Call<'_>,
    input: &Operation::Input,
) -> OperationOutcome<QueryResult<Option<Operation::Output>>> {
    let definition = operation.definition();
    if let Err(error) = definition
        .validate()
        .and_then(|()| validate_call(call, &definition.operation))
    {
        return OperationOutcome::Completed(query_defect(call, error.message()));
    }
    match operation.invoke(call, input) {
        QueryAttempt::Retry {
            failure,
            retry_after_seconds,
        } => retry_outcome(call, failure, retry_after_seconds, query_defect),
        QueryAttempt::Branch {
            branch,
            value,
            receipt,
            failure,
        } => {
            if branch == UNCERTAIN_BRANCH_ID
                || failure.as_ref().is_some_and(|f| f.validate().is_err())
            {
                return OperationOutcome::Completed(query_defect(
                    call,
                    "query returned an invalid attempt",
                ));
            }
            if !definition.declares_branch(&branch) {
                return OperationOutcome::Completed(query_defect(
                    call,
                    "query returned an unknown branch",
                ));
            }
            match complete_receipt(receipt, call) {
                Ok(receipt) => OperationOutcome::Completed(QueryResult {
                    branch,
                    value: Some(value),
                    receipt,
                    failure,
                }),
                Err(error) => OperationOutcome::Completed(query_defect(call, error.message())),
            }
        }
    }
}

/// Invokes a Mutation once with stable idempotency and validates its outcome.
///
/// The Call ID is the fallback idempotency key. A retry returns
/// [`OperationOutcome::Retry`]; only a declared `uncertain` route may represent an
/// unconfirmable, non-repeatable write. A call that starts while an earlier call of the same
/// operation in the same Step attempt is still running selects `defect` without invoking the
/// operation, so it never sends.
///
/// The effective send cap comes from the definition: `maxSends` of a `limited` operation, or
/// the Call's [`Call::with_max_sends_override`]; otherwise 1. An override on any other tier
/// selects `defect`. Inside a Dex Step use [`run_step_mutation`], which assigns the Call's
/// occurrence and admission atomically; a Call from [`StepAttempt::new_call`] selects `defect`
/// here without invoking the operation, because its occurrence was assigned before admission.
pub fn run_mutation<Operation: Mutation + ?Sized>(
    operation: &Operation,
    call: &Call<'_>,
    input: &Operation::Input,
) -> OperationOutcome<MutationResult<Option<Operation::Output>>> {
    if call.has_unadmitted_occurrence {
        return OperationOutcome::Completed(mutation_defect(
            call,
            "a Mutation Call from StepAttempt::new_call is not admitted; run it with run_step_mutation, so its occurrence and admission are assigned together",
        ));
    }
    let definition = operation.definition();
    if let Err(error) = definition
        .validate()
        .and_then(|()| validate_call(call, &definition.operation))
    {
        return OperationOutcome::Completed(mutation_defect(call, error.message()));
    }
    let Some(step_attempt) = call.step_attempt else {
        return run_validated_mutation(operation, &definition, call, input);
    };
    let Some(_running) = RunningMutation::begin(step_attempt, &definition.operation) else {
        return OperationOutcome::Completed(concurrent_mutation_defect(call, &definition));
    };
    run_validated_mutation(operation, &definition, call, input)
}

/// Creates the next Call of a Mutation in `attempt` and runs it once.
///
/// The occurrence that derives the Call ID and the concurrency admission are taken in one
/// critical section, as in sdkgo, so Call IDs follow call order. `max_sends_override`
/// requests a send cap for a `limited` operation, within 1 to 10. Otherwise it behaves as
/// [`run_mutation`].
pub fn run_step_mutation<Operation: Mutation + ?Sized>(
    operation: &Operation,
    attempt: &StepAttempt<'_>,
    connection: ConnectionRef,
    input: &Operation::Input,
    max_sends_override: Option<u32>,
) -> OperationOutcome<MutationResult<Option<Operation::Output>>> {
    let definition = operation.definition();
    let (call, running) =
        match attempt.new_admitted_mutation_call(connection, definition.operation.clone()) {
            Ok(admitted) => admitted,
            Err(error) => return OperationOutcome::Completed(unscoped_defect(&definition, &error)),
        };
    let call = match max_sends_override {
        Some(max_sends) => call.with_max_sends_override(max_sends),
        None => call,
    };
    if let Err(error) = definition.validate() {
        return OperationOutcome::Completed(mutation_defect(&call, error.message()));
    }
    let Some(_running) = running else {
        return OperationOutcome::Completed(concurrent_mutation_defect(&call, &definition));
    };
    run_validated_mutation(operation, &definition, &call, input)
}

/// Resolves the send cap a Mutation Call receives from its definition and override.
fn effective_max_sends(
    definition: &MutationDefinition,
    max_sends_override: Option<u32>,
) -> Result<u32, ValidationError> {
    let is_limited = definition.repeat_on_unknown == Some(RepeatOnUnknown::Limited);
    match max_sends_override {
        None if is_limited => Ok(definition.max_sends),
        None => Ok(MINIMUM_MAX_SENDS),
        Some(max_sends) => {
            validate_max_sends(max_sends)?;
            if !is_limited {
                return Err(ValidationError::new(
                    "max sends override requires a repeatOnUnknown limited operation",
                ));
            }
            Ok(max_sends)
        }
    }
}

fn concurrent_mutation_defect<T>(
    call: &Call<'_>,
    definition: &MutationDefinition,
) -> MutationResult<Option<T>> {
    mutation_defect(
        call,
        &format!(
            "concurrent calls of connector operation {} {} in one Step are not supported; an earlier call of it is still running, so this call was not sent",
            definition.operation.connector_id, definition.operation.operation_id
        ),
    )
}

fn unscoped_defect<T>(
    definition: &MutationDefinition,
    error: &ValidationError,
) -> MutationResult<Option<T>> {
    let connector_id = if definition.operation.connector_id.is_empty() {
        "connector".to_string()
    } else {
        definition.operation.connector_id.clone()
    };
    let operation_id = if definition.operation.operation_id.is_empty() {
        "operation".to_string()
    } else {
        definition.operation.operation_id.clone()
    };
    MutationResult {
        branch: DEFECT_BRANCH_ID.to_string(),
        value: None,
        receipt: Receipt {
            provider: connector_id.clone(),
            observed_at: Rfc3339NanoTimestamp::now_utc(),
            ..Receipt::default()
        },
        failure: Some(Failure::new(
            FailureKind::LocalDefect,
            connector_id,
            operation_id,
            error.message(),
        )),
    }
}

fn run_validated_mutation<Operation: Mutation + ?Sized>(
    operation: &Operation,
    definition: &MutationDefinition,
    call: &Call<'_>,
    input: &Operation::Input,
) -> OperationOutcome<MutationResult<Option<Operation::Output>>> {
    let call = match effective_max_sends(definition, call.max_sends_override()) {
        Ok(max_sends) => call.with_effective_max_sends(max_sends),
        Err(error) => return OperationOutcome::Completed(mutation_defect(call, error.message())),
    };
    let call = &call;
    let mut idempotency_key = operation.idempotency_key(&call.id, input);
    if idempotency_key.is_empty() {
        idempotency_key = IdempotencyKey::new(call.id.as_str());
    }
    if idempotency_key.as_str().trim().is_empty() {
        return OperationOutcome::Completed(mutation_defect(
            call,
            "mutation returned an invalid idempotency key",
        ));
    }
    let call = call.with_idempotency_key(idempotency_key);
    let (branch, value, receipt, failure) = match operation.invoke(&call, input) {
        MutationAttempt::Retry {
            failure,
            retry_after_seconds,
        } => return retry_outcome(&call, failure, retry_after_seconds, mutation_defect),
        MutationAttempt::Branch {
            branch,
            value,
            receipt,
            failure,
        } => {
            if branch == UNCERTAIN_BRANCH_ID {
                return OperationOutcome::Completed(mutation_defect(
                    &call,
                    "mutation returned an invalid attempt",
                ));
            }
            (branch, value, receipt, failure)
        }
        MutationAttempt::Uncertain {
            value,
            failure,
            receipt,
        } => (
            UNCERTAIN_BRANCH_ID.to_string(),
            value,
            receipt,
            Some(failure),
        ),
    };
    if failure.as_ref().is_some_and(|f| f.validate().is_err()) {
        return OperationOutcome::Completed(mutation_defect(
            &call,
            "mutation returned an invalid attempt",
        ));
    }
    if !definition.declares_branch(&branch) {
        return OperationOutcome::Completed(mutation_defect(
            &call,
            "mutation returned an unknown branch",
        ));
    }
    match complete_receipt(receipt, &call) {
        Ok(receipt) => OperationOutcome::Completed(MutationResult {
            branch,
            value: Some(value),
            receipt,
            failure,
        }),
        Err(error) => OperationOutcome::Completed(mutation_defect(&call, error.message())),
    }
}

fn retry_outcome<R>(
    call: &Call<'_>,
    failure: Failure,
    retry_after_seconds: f64,
    defect: fn(&Call<'_>, &str) -> R,
) -> OperationOutcome<R> {
    match RetryError::new(failure, retry_after_seconds) {
        Ok(retry) => OperationOutcome::Retry(retry),
        Err(_) => {
            OperationOutcome::Completed(defect(call, "operation returned an invalid attempt"))
        }
    }
}

fn validate_call(call: &Call<'_>, operation: &OperationRef) -> Result<(), ValidationError> {
    call.id.validate()?;
    call.connection.validate()?;
    if &call.operation != operation {
        return Err(ValidationError::new(
            "call operation does not match the definition",
        ));
    }
    Ok(())
}

/// Fills the Call ID, provider, idempotency key, and zero observation time of a receipt.
pub(crate) fn complete_receipt(
    mut receipt: Receipt,
    call: &Call<'_>,
) -> Result<Receipt, ValidationError> {
    if !receipt.call_id.is_empty() && receipt.call_id != call.id {
        return Err(ValidationError::new(
            "operation returned a receipt for another call",
        ));
    }
    if !receipt.idempotency_key.is_empty() && receipt.idempotency_key != call.idempotency_key {
        return Err(ValidationError::new(
            "operation returned a receipt with another idempotency key",
        ));
    }
    receipt.call_id = call.id.clone();
    if receipt.provider.is_empty() {
        receipt.provider = call.operation.connector_id.clone();
    }
    receipt.idempotency_key = call.idempotency_key.clone();
    if receipt.observed_at.is_zero() {
        receipt.observed_at = Rfc3339NanoTimestamp::now_utc();
    }
    Ok(receipt)
}

fn local_defect_failure(call: &Call<'_>, message: &str) -> Failure {
    Failure::new(
        FailureKind::LocalDefect,
        call.operation.connector_id.clone(),
        call.operation.operation_id.clone(),
        message,
    )
}

fn defect_receipt(call: &Call<'_>) -> Receipt {
    Receipt {
        call_id: call.id.clone(),
        idempotency_key: call.idempotency_key.clone(),
        provider: call.operation.connector_id.clone(),
        observed_at: Rfc3339NanoTimestamp::now_utc(),
        ..Receipt::default()
    }
}

fn query_defect<T>(call: &Call<'_>, message: &str) -> QueryResult<Option<T>> {
    QueryResult {
        branch: DEFECT_BRANCH_ID.to_string(),
        value: None,
        receipt: defect_receipt(call),
        failure: Some(local_defect_failure(call, message)),
    }
}

fn mutation_defect<T>(call: &Call<'_>, message: &str) -> MutationResult<Option<T>> {
    MutationResult {
        branch: DEFECT_BRANCH_ID.to_string(),
        value: None,
        receipt: defect_receipt(call),
        failure: Some(local_defect_failure(call, message)),
    }
}
