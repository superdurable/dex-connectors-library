// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Stable connector identities and wire-compatible result values.

use std::collections::BTreeMap;
use std::fmt;
use std::time::Duration;

use serde::{Deserialize, Serialize};
use uuid::Uuid;

use crate::errors::{Failure, ValidationError};
use crate::retry_policy::{RetryPolicy, duration_nanoseconds};
use crate::step_attempt::StepAttemptAccess;
use crate::timestamps::Rfc3339NanoTimestamp;

/// Branch selected for a local connector defect; every operation declares it.
pub const DEFECT_BRANCH_ID: &str = "defect";
/// Branch selected for an unconfirmable, possibly accepted write.
pub const UNCERTAIN_BRANCH_ID: &str = "uncertain";

/// UUIDv5 namespace of connector Call IDs:
/// UUIDv5(URL namespace, `https://superdurable.dev/dex-connectors/call-id/v1`).
pub fn call_id_namespace() -> Uuid {
    Uuid::new_v5(
        &Uuid::NAMESPACE_URL,
        b"https://superdurable.dev/dex-connectors/call-id/v1",
    )
}

/// Stable UUID identity of one provider call across Step retries.
#[derive(Clone, Debug, Default, Deserialize, Eq, Hash, Ord, PartialEq, PartialOrd, Serialize)]
#[serde(transparent)]
pub struct CallId(String);

impl CallId {
    /// Wraps a Call ID string without validating it.
    pub fn new(value: impl Into<String>) -> Self {
        Self(value.into())
    }

    /// Requires a UUID value.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] when the value is not a UUID.
    pub fn validate(&self) -> Result<(), ValidationError> {
        Uuid::parse_str(&self.0)
            .map(|_| ())
            .map_err(|_| ValidationError::new("call ID must be a UUID"))
    }

    /// Returns the UUID string.
    pub fn as_str(&self) -> &str {
        &self.0
    }

    /// Returns whether the ID is empty, as in a zero receipt.
    pub fn is_empty(&self) -> bool {
        self.0.is_empty()
    }
}

impl fmt::Display for CallId {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str(&self.0)
    }
}

/// Derives Go's length-prefixed SHA-1 UUID for one Dex Step execution.
///
/// `occurrence` is the one-based ordinal of this call among calls of the same operation in one
/// Step attempt. Occurrence 1 uses four fields: Flow ID, Step execution ID, connector ID, and
/// operation ID, each UTF-8 encoded after its four-byte big-endian byte length. From 2 the
/// decimal ordinal is a fifth field.
///
/// # Errors
///
/// Returns [`ValidationError`] for an empty Flow or Step execution ID, a zero occurrence, or
/// an invalid operation.
pub fn derive_call_id(
    flow_id: &str,
    step_execution_id: &str,
    operation: &OperationRef,
    occurrence: u32,
) -> Result<CallId, ValidationError> {
    if flow_id.is_empty() || step_execution_id.is_empty() {
        return Err(ValidationError::new(
            "Dex Flow and Step execution IDs are required",
        ));
    }
    if occurrence == 0 {
        return Err(ValidationError::new(
            "call occurrence must be a positive integer",
        ));
    }
    operation.validate()?;
    let occurrence_text = occurrence.to_string();
    let mut fields = vec![
        flow_id,
        step_execution_id,
        operation.connector_id.as_str(),
        operation.operation_id.as_str(),
    ];
    if occurrence > 1 {
        fields.push(&occurrence_text);
    }
    let mut payload = Vec::new();
    for field in fields {
        let length = u32::try_from(field.len())
            .map_err(|_| ValidationError::new("call identity field is too long"))?;
        payload.extend_from_slice(&length.to_be_bytes());
        payload.extend_from_slice(field.as_bytes());
    }
    Ok(CallId(
        Uuid::new_v5(&call_id_namespace(), &payload).to_string(),
    ))
}

/// Provider-safe representation of a stable call identity.
#[derive(Clone, Debug, Default, Deserialize, Eq, Hash, PartialEq, Serialize)]
#[serde(transparent)]
pub struct IdempotencyKey(String);

impl IdempotencyKey {
    /// Wraps a provider-safe key.
    pub fn new(value: impl Into<String>) -> Self {
        Self(value.into())
    }

    /// Returns the key string.
    pub fn as_str(&self) -> &str {
        &self.0
    }

    /// Returns whether the key is empty.
    pub fn is_empty(&self) -> bool {
        self.0.is_empty()
    }
}

impl fmt::Display for IdempotencyKey {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str(&self.0)
    }
}

fn is_connector_id(value: &str) -> bool {
    let bytes = value.as_bytes();
    (2..=63).contains(&bytes.len())
        && bytes[0].is_ascii_lowercase()
        && bytes[1..]
            .iter()
            .all(|byte| byte.is_ascii_lowercase() || byte.is_ascii_digit() || *byte == b'-')
}

pub(crate) fn is_lower_camel_identifier(value: &str) -> bool {
    let bytes = value.as_bytes();
    bytes.len() >= 2
        && bytes[0].is_ascii_lowercase()
        && bytes[1..].iter().all(u8::is_ascii_alphanumeric)
}

/// Logical credential reference safe to persist in a Flow.
#[derive(Clone, Debug, Deserialize, Eq, Hash, Ord, PartialEq, PartialOrd, Serialize)]
pub struct ConnectionRef {
    /// Provider or connector ID that owns the connection.
    pub provider: String,
    /// Application-chosen static connection name.
    pub name: String,
}

impl ConnectionRef {
    /// Creates a connection reference.
    pub fn new(provider: impl Into<String>, name: impl Into<String>) -> Self {
        Self {
            provider: provider.into(),
            name: name.into(),
        }
    }

    /// Requires both nonblank fields.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] when either field is blank.
    pub fn validate(&self) -> Result<(), ValidationError> {
        if self.provider.trim().is_empty() || self.name.trim().is_empty() {
            return Err(ValidationError::new(
                "connection provider and name are required",
            ));
        }
        Ok(())
    }
}

/// Stable manifest identity of a connector operation.
#[derive(Clone, Debug, Deserialize, Eq, Hash, Ord, PartialEq, PartialOrd, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct OperationRef {
    /// DNS-like connector ID of 2 to 63 characters.
    pub connector_id: String,
    /// Lower-camel operation or Trigger ID.
    pub operation_id: String,
}

impl OperationRef {
    /// Creates an operation reference.
    pub fn new(connector_id: impl Into<String>, operation_id: impl Into<String>) -> Self {
        Self {
            connector_id: connector_id.into(),
            operation_id: operation_id.into(),
        }
    }

    /// Requires a DNS-like connector ID and lower-camel operation ID.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for either invalid identity.
    pub fn validate(&self) -> Result<(), ValidationError> {
        if !is_connector_id(&self.connector_id) {
            return Err(ValidationError::new(
                "connector ID must be DNS-like and 2-63 characters",
            ));
        }
        if !is_lower_camel_identifier(&self.operation_id) {
            return Err(ValidationError::new(
                "operation ID must be lower camel case",
            ));
        }
        Ok(())
    }
}

/// Provider-safe progress before Dex call and attempt identity is added.
#[derive(Clone, Debug, Default, PartialEq)]
pub struct Progress {
    /// Short phase name, such as `uploading`.
    pub phase: String,
    /// Optional human-readable message; empty is omitted.
    pub message: String,
    /// Optional completion fraction within 0 to 1.
    pub percent: Option<f64>,
    /// Optional provider-assigned sequence.
    pub provider_sequence: Option<i64>,
}

/// Structured best-effort Dex Stream message for one connector attempt.
#[derive(Clone, Debug, Deserialize, PartialEq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ProgressUpdate {
    /// Stable call identity.
    pub call_id: CallId,
    /// Dex attempt number of the Step.
    pub attempt: u32,
    /// Monotonic connector sequence within the attempt.
    pub sequence: u64,
    /// Phase copied from [`Progress`].
    pub phase: String,
    /// Message copied from [`Progress`]; omitted when empty.
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub message: String,
    /// Completion fraction; omitted when absent.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub percent: Option<f64>,
    /// Provider sequence; omitted when absent.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub provider_sequence: Option<i64>,
}

/// Provider-safe metadata for one invocation, plus the live Step attempt when in a Step.
///
/// The Step attempt is available only during a Dex Step handler. It never enters JSON or
/// Flow state; [`Call::identity`] is the persisted form.
pub struct Call<'attempt> {
    /// Stable call identity across Step retries.
    pub id: CallId,
    /// Logical connection used to resolve credentials.
    pub connection: ConnectionRef,
    /// Operation or Trigger identity.
    pub operation: OperationRef,
    /// Provider-safe deduplication key; set by [`crate::run_mutation`].
    pub idempotency_key: IdempotencyKey,
    max_sends: u32,
    max_sends_override: Option<u32>,
    pub(crate) step_attempt: Option<&'attempt (dyn StepAttemptAccess + 'attempt)>,
}

impl<'attempt> Call<'attempt> {
    /// Creates a Call outside a Dex Step, such as in tests, mocks, or Trigger credential
    /// resolution.
    ///
    /// The Call has no Step attempt, so progress and text writes are no-ops.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for an invalid reference or Call ID.
    pub fn new(
        id: CallId,
        connection: ConnectionRef,
        operation: OperationRef,
    ) -> Result<Self, ValidationError> {
        connection.validate()?;
        operation.validate()?;
        id.validate()?;
        Ok(Self {
            id,
            connection,
            operation,
            idempotency_key: IdempotencyKey::default(),
            max_sends: 1,
            max_sends_override: None,
            step_attempt: None,
        })
    }

    /// Returns the effective single-dispatch send cap, within 1 to 10.
    ///
    /// [`crate::run_mutation`] sets it from the definition: the manifest `maxSends` of a
    /// `limited` operation, or the requested override; otherwise 1. Pass it to
    /// [`crate::singledispatch::Fence::new`].
    pub fn max_sends(&self) -> u32 {
        self.max_sends
    }

    /// Requests a send cap for a `limited` Mutation, within 1 to 10; 1 disables resend.
    ///
    /// [`crate::run_mutation`] validates it and selects `defect` for an invalid cap or a
    /// `safe`, `unsafe`, or undeclared operation.
    pub fn with_max_sends_override(mut self, max_sends: u32) -> Self {
        self.max_sends_override = Some(max_sends);
        self
    }

    /// Returns the requested send cap override, if any.
    pub fn max_sends_override(&self) -> Option<u32> {
        self.max_sends_override
    }

    pub(crate) fn with_effective_max_sends(&self, max_sends: u32) -> Call<'attempt> {
        let mut call = self.with_idempotency_key(self.idempotency_key.clone());
        call.max_sends = max_sends;
        call
    }

    /// Reports whether this Call runs inside a Dex Step attempt.
    pub fn is_in_step(&self) -> bool {
        self.step_attempt.is_some()
    }

    /// Returns the Dex attempt number, or `None` outside a Step.
    pub fn attempt(&self) -> Option<u32> {
        self.step_attempt.map(StepAttemptAccess::attempt_number)
    }

    /// Returns the Dex Flow ID, or `None` outside a Step.
    pub fn flow_id(&self) -> Option<&str> {
        self.step_attempt.map(StepAttemptAccess::attempt_flow_id)
    }

    /// Returns a copy with another idempotency key, keeping the Step attempt.
    pub fn with_idempotency_key(&self, idempotency_key: IdempotencyKey) -> Call<'attempt> {
        Call {
            id: self.id.clone(),
            connection: self.connection.clone(),
            operation: self.operation.clone(),
            idempotency_key,
            max_sends: self.max_sends,
            max_sends_override: self.max_sends_override,
            step_attempt: self.step_attempt,
        }
    }

    /// Appends structured progress when the Step configured a progress Stream.
    ///
    /// Progress is best effort: a Stream write failure is logged, not returned.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] when `percent` is outside 0 to 1.
    pub fn report_progress(&self, progress: Progress) -> Result<(), ValidationError> {
        if let Some(percent) = progress.percent
            && !(0.0..=1.0).contains(&percent)
        {
            return Err(ValidationError::new(
                "connector progress percent must be between 0 and 1",
            ));
        }
        if let Some(step_attempt) = self.step_attempt {
            step_attempt.report_progress(&self.id, progress);
        }
        Ok(())
    }

    /// Appends ordered text when the Step configured a buffered text Stream.
    ///
    /// Text is best effort: a Stream write failure is logged, not returned.
    pub fn write_text(&self, value: &str) {
        if let Some(step_attempt) = self.step_attempt {
            step_attempt.write_text(value);
        }
    }

    /// Reports whether this invocation can emit structured progress.
    pub fn has_progress_stream(&self) -> bool {
        self.step_attempt
            .is_some_and(StepAttemptAccess::has_progress_stream)
    }

    /// Reports whether this invocation can emit buffered text.
    pub fn has_text_stream(&self) -> bool {
        self.step_attempt
            .is_some_and(StepAttemptAccess::has_text_stream)
    }

    /// Returns the persisted identity fields in sdkgo's `Call` JSON shape.
    pub fn identity(&self) -> CallIdentity {
        CallIdentity {
            id: self.id.clone(),
            idempotency_key: self.idempotency_key.clone(),
            connection: self.connection.clone(),
            operation: self.operation.clone(),
        }
    }
}

impl fmt::Debug for Call<'_> {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("Call")
            .field("id", &self.id)
            .field("connection", &self.connection)
            .field("operation", &self.operation)
            .field("idempotency_key", &self.idempotency_key)
            .field("max_sends", &self.max_sends)
            .field("in_step", &self.step_attempt.is_some())
            .finish()
    }
}

/// sdkgo `Call` JSON identity fields without the live Step attempt.
#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct CallIdentity {
    /// Stable call identity.
    pub id: CallId,
    /// Provider-safe deduplication key; omitted when empty.
    #[serde(default, skip_serializing_if = "IdempotencyKey::is_empty")]
    pub idempotency_key: IdempotencyKey,
    /// Logical connection.
    pub connection: ConnectionRef,
    /// Operation identity.
    pub operation: OperationRef,
}

/// Stable operation route; only the happy path is required.
#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
pub struct BranchDefinition {
    /// Lower-camel branch ID.
    pub id: String,
    /// Explanation shown to application authors.
    pub description: String,
    /// Whether the application may leave the branch unwired; omitted when false.
    #[serde(default, skip_serializing_if = "is_false")]
    pub optional: bool,
}

fn is_false(value: &bool) -> bool {
    !*value
}

impl BranchDefinition {
    /// Creates a branch definition.
    pub fn new(id: impl Into<String>, description: impl Into<String>, optional: bool) -> Self {
        Self {
            id: id.into(),
            description: description.into(),
            optional,
        }
    }

    /// Requires a lower-camel ID and a nonblank description.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for an invalid identity or description.
    pub fn validate(&self) -> Result<(), ValidationError> {
        if !is_lower_camel_identifier(&self.id) || self.description.trim().is_empty() {
            return Err(ValidationError::new(
                "invalid branch identity or description",
            ));
        }
        Ok(())
    }
}

/// Dex Go's `dex.StepDurability` ordinal used in operation Step defaults.
#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
pub enum ExecuteDurability {
    /// Uses the Flow or server default (`0`).
    #[default]
    Default,
    /// Waits for durable persistence (`1`).
    Sync,
    /// Persists asynchronously after accepting the result (`2`).
    Async,
}

impl ExecuteDurability {
    /// Converts to the Dex Rust SDK durability.
    pub fn to_dex_step_durability(self) -> dex_sdk::StepDurability {
        match self {
            ExecuteDurability::Default => dex_sdk::StepDurability::Default,
            ExecuteDurability::Sync => dex_sdk::StepDurability::Sync,
            ExecuteDurability::Async => dex_sdk::StepDurability::Async,
        }
    }
}

impl Serialize for ExecuteDurability {
    fn serialize<S: serde::Serializer>(&self, serializer: S) -> Result<S::Ok, S::Error> {
        serializer.serialize_u8(match self {
            ExecuteDurability::Default => 0,
            ExecuteDurability::Sync => 1,
            ExecuteDurability::Async => 2,
        })
    }
}

impl<'de> Deserialize<'de> for ExecuteDurability {
    fn deserialize<D: serde::Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        match u8::deserialize(deserializer)? {
            0 => Ok(ExecuteDurability::Default),
            1 => Ok(ExecuteDurability::Sync),
            2 => Ok(ExecuteDurability::Async),
            _ => Err(serde::de::Error::custom(
                "Step Execute durability is invalid",
            )),
        }
    }
}

/// Execute-only Dex defaults owned by an operation definition.
#[derive(Clone, Debug, Default, Deserialize, PartialEq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct StepDefaults {
    /// Maximum Execute duration; zero uses the Dex default.
    #[serde(with = "duration_nanoseconds")]
    pub execute_method_timeout: Duration,
    /// Heartbeat timeout; zero uses the Dex default and is omitted.
    #[serde(
        default,
        with = "duration_nanoseconds",
        skip_serializing_if = "Duration::is_zero"
    )]
    pub heartbeat_timeout: Duration,
    /// Execute retry policy; `None` uses the default connector policy.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub execute_retry: Option<RetryPolicy>,
    /// Execute durability.
    pub execute_durability: ExecuteDurability,
}

impl StepDefaults {
    /// Validates the retry policy when present.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for an invalid retry policy.
    pub fn validate(&self) -> Result<(), ValidationError> {
        if let Some(retry) = &self.execute_retry {
            retry.validate()?;
        }
        Ok(())
    }
}

/// Query identity, stable routes, and Execute defaults.
#[derive(Clone, Debug, Deserialize, PartialEq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct QueryDefinition {
    /// Operation identity.
    pub operation: OperationRef,
    /// Declared branches; `None` encodes JSON `null` as Go's nil slice.
    pub branches: Option<Vec<BranchDefinition>>,
    /// Execute defaults.
    pub step_defaults: StepDefaults,
}

impl QueryDefinition {
    /// Requires distinct routes, a defect branch, and no uncertainty route.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for an invalid identity, route set, or default.
    pub fn validate(&self) -> Result<(), ValidationError> {
        self.operation.validate()?;
        if let Some(branches) = self
            .branches
            .as_deref()
            .filter(|branches| !branches.is_empty())
        {
            validate_branch_set(branches, false)
                .map_err(|_| ValidationError::new("invalid Query branches"))?;
        }
        self.step_defaults.validate()
    }

    /// Returns whether `branch` is declared.
    pub fn declares_branch(&self, branch: &str) -> bool {
        self.branches
            .as_deref()
            .is_some_and(|branches| branches.iter().any(|candidate| candidate.id == branch))
    }
}

fn validate_branch_set(
    branches: &[BranchDefinition],
    allows_uncertain: bool,
) -> Result<(), ValidationError> {
    let mut seen = std::collections::BTreeSet::new();
    for branch in branches {
        branch.validate()?;
        if !seen.insert(branch.id.as_str()) {
            return Err(ValidationError::new("duplicate branch"));
        }
    }
    if !seen.contains(DEFECT_BRANCH_ID) || (!allows_uncertain && seen.contains(UNCERTAIN_BRANCH_ID))
    {
        return Err(ValidationError::new("invalid branch set"));
    }
    Ok(())
}

/// Manifest `execution.repeatOnUnknown` tier for an unknown provider outcome.
#[derive(Clone, Copy, Debug, Deserialize, Eq, Hash, PartialEq, Serialize)]
#[serde(rename_all = "lowercase")]
pub enum RepeatOnUnknown {
    /// Repeating is harmless; retry within the Step retry policy.
    Safe,
    /// A duplicate is acceptable; resend while possible sends stay below `maxSends`.
    Limited,
    /// A duplicate does real harm; read back only and never resend.
    Unsafe,
}

/// Mutation identity, stable routes, Execute defaults, and resend tier.
///
/// `repeat_on_unknown` and `max_sends` carry the manifest `execution.repeatOnUnknown` and
/// `execution.maxSends`; neither is part of the wire encoding. `None` means the definition
/// predates these fields: a Step then rejects a send cap override and passes a cap of 1.
/// `max_sends` is 2 to 10 for [`RepeatOnUnknown::Limited`] and 0 otherwise.
#[derive(Clone, Debug, Deserialize, PartialEq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct MutationDefinition {
    /// Operation identity.
    pub operation: OperationRef,
    /// Declared branches; `None` encodes JSON `null`.
    pub branches: Option<Vec<BranchDefinition>>,
    /// Execute defaults.
    pub step_defaults: StepDefaults,
    /// Resend tier; not serialized.
    #[serde(skip)]
    pub repeat_on_unknown: Option<RepeatOnUnknown>,
    /// Send cap of a limited Mutation; not serialized.
    #[serde(skip)]
    pub max_sends: u32,
}

impl MutationDefinition {
    /// Requires distinct routes, a defect branch, and a consistent resend tier.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for an invalid identity, route set, tier, or default.
    pub fn validate(&self) -> Result<(), ValidationError> {
        self.operation.validate()?;
        if self.repeat_on_unknown == Some(RepeatOnUnknown::Limited) {
            if !(2..=10).contains(&self.max_sends) {
                return Err(ValidationError::new(
                    "limited Mutation maxSends must be an integer within 2..10",
                ));
            }
        } else if self.max_sends != 0 {
            return Err(ValidationError::new(
                "Mutation maxSends requires repeatOnUnknown limited",
            ));
        }
        if let Some(branches) = self
            .branches
            .as_deref()
            .filter(|branches| !branches.is_empty())
        {
            validate_branch_set(branches, true)
                .map_err(|_| ValidationError::new("invalid Mutation branches"))?;
        }
        self.step_defaults.validate()
    }

    /// Returns whether `branch` is declared.
    pub fn declares_branch(&self, branch: &str) -> bool {
        self.branches
            .as_deref()
            .is_some_and(|branches| branches.iter().any(|candidate| candidate.id == branch))
    }
}

/// Provider correlation data; zero values are completed by the operation runner.
///
/// Callers must exclude secrets. The all-default value matches Go's `sdkgo.Receipt{}` for a
/// local or uncorrelated provider outcome.
#[derive(Clone, Debug, Default, Deserialize, Eq, PartialEq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Receipt {
    /// Stable call identity; the runner fills it.
    pub call_id: CallId,
    /// Deduplication key; omitted when empty.
    #[serde(default, skip_serializing_if = "IdempotencyKey::is_empty")]
    pub idempotency_key: IdempotencyKey,
    /// Provider name; the runner fills it with the connector ID when empty.
    pub provider: String,
    /// Safe provider object ID; omitted when empty.
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub provider_object_id: String,
    /// Safe provider request ID; omitted when empty.
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub provider_request_id: String,
    /// Observation time; the runner fills the zero time with the current UTC time.
    pub observed_at: Rfc3339NanoTimestamp,
    /// Safe string metadata; omitted when empty.
    #[serde(default, skip_serializing_if = "BTreeMap::is_empty")]
    pub metadata: BTreeMap<String, String>,
}

/// Validated terminal Query route, typed value, receipt, and optional failure.
#[derive(Clone, Debug, Deserialize, PartialEq, Serialize)]
pub struct QueryResult<T> {
    /// Selected declared branch.
    pub branch: String,
    /// Operation-specific value.
    pub value: T,
    /// Completed receipt.
    pub receipt: Receipt,
    /// Safe failure context; omitted when absent.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub failure: Option<Failure>,
}

impl<T> QueryResult<T> {
    /// Rejects a blank or uncertain branch and an invalid failure.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for an invalid route or failure.
    pub fn validate(&self) -> Result<(), ValidationError> {
        if self.branch.is_empty() || self.branch == UNCERTAIN_BRANCH_ID {
            return Err(ValidationError::new("Query result branch is invalid"));
        }
        self.failure.as_ref().map_or(Ok(()), Failure::validate)
    }
}

/// Validated terminal Mutation route with the same wire shape as [`QueryResult`].
#[derive(Clone, Debug, Deserialize, PartialEq, Serialize)]
pub struct MutationResult<T> {
    /// Selected declared branch.
    pub branch: String,
    /// Operation-specific value.
    pub value: T,
    /// Completed receipt.
    pub receipt: Receipt,
    /// Safe failure context; required for `uncertain`, omitted when absent.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub failure: Option<Failure>,
}

impl<T> MutationResult<T> {
    /// Requires a branch, and failure context for an uncertain write route.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for an invalid route or failure.
    pub fn validate(&self) -> Result<(), ValidationError> {
        if self.branch.is_empty() || (self.branch == UNCERTAIN_BRANCH_ID && self.failure.is_none())
        {
            return Err(ValidationError::new("Mutation result branch is invalid"));
        }
        self.failure.as_ref().map_or(Ok(()), Failure::validate)
    }
}

/// One provider Query outcome before Step factory validation.
#[derive(Clone, Debug, PartialEq)]
#[expect(
    clippy::large_enum_variant,
    reason = "attempts are short-lived return values; boxing would burden every connector"
)]
pub enum QueryAttempt<T> {
    /// A definite business outcome on a declared branch.
    Branch {
        /// Declared branch ID.
        branch: String,
        /// Operation value.
        value: T,
        /// Receipt; zero fields are completed by the runner.
        receipt: Receipt,
        /// Optional safe failure context.
        failure: Option<Failure>,
    },
    /// A safe-to-repeat failure; Dex retries under the Step policy.
    Retry {
        /// Safe failure context.
        failure: Failure,
        /// Nonnegative delay hint in seconds; zero means none.
        retry_after_seconds: f64,
    },
}

impl<T> QueryAttempt<T> {
    /// Constructs a definite business outcome.
    pub fn branch(
        branch: impl Into<String>,
        value: T,
        receipt: Receipt,
        failure: Option<Failure>,
    ) -> Self {
        QueryAttempt::Branch {
            branch: branch.into(),
            value,
            receipt,
            failure,
        }
    }

    /// Constructs a retryable failure.
    pub fn retry(failure: Failure, retry_after_seconds: f64) -> Self {
        QueryAttempt::Retry {
            failure,
            retry_after_seconds,
        }
    }
}

/// One provider Mutation outcome, including narrowly justified uncertainty.
#[derive(Clone, Debug, PartialEq)]
pub enum MutationAttempt<T> {
    /// A definite business outcome on a declared branch other than `uncertain`.
    Branch {
        /// Declared branch ID.
        branch: String,
        /// Operation value.
        value: T,
        /// Receipt; zero fields are completed by the runner.
        receipt: Receipt,
        /// Optional safe failure context.
        failure: Option<Failure>,
    },
    /// A safe-to-repeat failure; Dex retries under the Step policy.
    Retry {
        /// Safe failure context.
        failure: Failure,
        /// Nonnegative delay hint in seconds; zero means none.
        retry_after_seconds: f64,
    },
    /// A possibly accepted, non-repeatable write that read-back cannot confirm.
    Uncertain {
        /// Operation value.
        value: T,
        /// Safe failure context.
        failure: Failure,
        /// Receipt; zero fields are completed by the runner.
        receipt: Receipt,
    },
}

impl<T> MutationAttempt<T> {
    /// Constructs a definite Mutation business outcome.
    pub fn branch(
        branch: impl Into<String>,
        value: T,
        receipt: Receipt,
        failure: Option<Failure>,
    ) -> Self {
        MutationAttempt::Branch {
            branch: branch.into(),
            value,
            receipt,
            failure,
        }
    }

    /// Constructs a retryable Mutation failure.
    pub fn retry(failure: Failure, retry_after_seconds: f64) -> Self {
        MutationAttempt::Retry {
            failure,
            retry_after_seconds,
        }
    }

    /// Records an unconfirmable, possibly accepted, non-repeatable write.
    pub fn uncertain(value: T, failure: Failure, receipt: Receipt) -> Self {
        MutationAttempt::Uncertain {
            value,
            failure,
            receipt,
        }
    }
}
