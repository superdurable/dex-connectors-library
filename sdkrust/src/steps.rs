// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Instance-addressed Dex Steps for typed connector operations.

use std::collections::{BTreeMap, BTreeSet};
use std::marker::PhantomData;
use std::time::Duration;

use dex_sdk::{
    Attribute, BufferedTextStreamOptions, Context, HandlerError, HandlerResult, Step, StepDecision,
    StepDurability, StepOptions, Stream, Value,
};

use crate::configuration_ui::ConnectorConfigurationUi;
use crate::errors::{Failure, RetryError, ValidationError};
use crate::operations::{Mutation, OperationOutcome, Query, run_query, run_step_mutation};
use crate::retry_policy::default_connector_step_execute_retry_policy;
use crate::singledispatch::{MINIMUM_MAX_SENDS, validate_max_sends};
use crate::step_attempt::StepAttempt;
use crate::types::{
    BranchDefinition, Call, ConnectionRef, MutationResult, ProgressUpdate, QueryResult,
    RepeatOnUnknown, StepDefaults,
};

/// Maps a Step input to an operation input.
pub type OperationInputMapper<StepInput, OperationInput> =
    Box<dyn Fn(&StepInput) -> OperationInput + Send + Sync>;

const DEFAULT_TEXT_FLUSH_INTERVAL: Duration = Duration::from_secs(1);
const DEFAULT_TEXT_MAX_BUFFERED_BYTES: usize = 16 * 1024;
const FAILURE_REASON_LIMIT_BYTES: usize = 1024;

/// Stable group and explanation metadata for a Connector Step.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct StepAnnotations {
    /// Lowercase kebab group ID, such as `gmail`.
    pub group_id: String,
    /// Group display label.
    pub group_label: String,
    /// Explanation of what the Step does.
    pub explanation: String,
}

impl StepAnnotations {
    /// Creates annotations.
    pub fn new(
        group_id: impl Into<String>,
        group_label: impl Into<String>,
        explanation: impl Into<String>,
    ) -> Self {
        Self {
            group_id: group_id.into(),
            group_label: group_label.into(),
            explanation: explanation.into(),
        }
    }

    /// Requires a stable group identifier and meaningful display text.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for an invalid group ID or blank text.
    pub fn validate(&self) -> Result<(), ValidationError> {
        let bytes = self.group_id.as_bytes();
        let is_group_id = !bytes.is_empty()
            && bytes[0].is_ascii_lowercase()
            && bytes
                .iter()
                .all(|byte| byte.is_ascii_lowercase() || byte.is_ascii_digit() || *byte == b'-');
        if !is_group_id || self.group_label.trim().is_empty() || self.explanation.trim().is_empty()
        {
            return Err(ValidationError::new(
                "Step group, group label, and explanation are required",
            ));
        }
        Ok(())
    }
}

/// Typed destination of one operation branch.
///
/// Dex routes by Step type, so a target records only the Step type of a Step whose input is
/// the operation result. Every target Step must be in the Flow's `steps()` list.
pub struct BranchTarget<Result> {
    step_type: &'static str,
    result: PhantomData<fn(Result)>,
}

impl<Result: Value> BranchTarget<Result> {
    /// Targets `step`, whose input is the operation result.
    pub fn to<TargetStep: Step<Input = Result>>(step: &TargetStep) -> Self {
        Self {
            step_type: step.step_type(),
            result: PhantomData,
        }
    }

    fn go_to(&self, result: Result) -> StepDecision {
        StepDecision::go_to(
            &StepTypeProxy::<Result> {
                step_type: self.step_type,
                input: PhantomData,
            },
            result,
        )
    }
}

impl<Result> Clone for BranchTarget<Result> {
    fn clone(&self) -> Self {
        Self {
            step_type: self.step_type,
            result: PhantomData,
        }
    }
}

/// Never registered; it names a registered Step type for [`StepDecision::go_to`].
struct StepTypeProxy<Input> {
    step_type: &'static str,
    input: PhantomData<fn(Input)>,
}

impl<Input: Value> Step for StepTypeProxy<Input> {
    type Input = Input;

    fn execute(&self, _context: &mut Context, _input: Input) -> HandlerResult<StepDecision> {
        Err(HandlerError::new(
            "dex_connectors_sdk::StepTypeProxy",
            "branch target proxies are never registered",
        ))
    }

    fn step_type(&self) -> &'static str {
        self.step_type
    }
}

/// Execute-only overrides of a Connector Step's manifest defaults.
///
/// Each `None` keeps the operation default. Connector Steps have no WaitFor behavior, so the
/// type exposes only Execute settings.
#[derive(Clone, Debug, Default)]
pub struct StepOptionsOverride {
    /// Execute method timeout.
    pub execute_method_timeout: Option<Duration>,
    /// Heartbeat timeout.
    pub heartbeat_timeout: Option<Duration>,
    /// Execute retry policy; it wins over the attempts a raised send cap adds.
    pub execute_retry: Option<dex_sdk::RetryPolicy>,
    /// Execute durability.
    pub execute_durability: Option<StepDurability>,
}

/// Binds one Query to a typed Dex Step and declared branch destinations.
///
/// An unwired optional branch fails the Flow when selected.
pub struct QueryStepConfig<StepInput, Operation: Query> {
    /// Stable Step type registered with Dex.
    pub step_type: &'static str,
    /// Authoring group and explanation.
    pub annotations: StepAnnotations,
    /// Provider operation.
    pub operation: Operation,
    /// Static logical connection.
    pub connection: ConnectionRef,
    /// Maps the Step input to the operation input.
    pub map_to_operation_input: OperationInputMapper<StepInput, Operation::Input>,
    /// Branch destinations keyed by declared branch ID.
    pub branches: BTreeMap<String, BranchTarget<QueryResult<Option<Operation::Output>>>>,
    /// Connector-owned configuration units.
    pub configuration_ui: ConnectorConfigurationUi,
    /// Attribute written with the result before routing.
    pub result_attribute: Option<&'static Attribute<QueryResult<Option<Operation::Output>>>>,
    /// Best-effort structured progress Stream.
    pub progress_stream: Option<&'static Stream<ProgressUpdate>>,
    /// Best-effort buffered text Stream.
    pub text_stream: Option<&'static Stream<String>>,
    /// Text flush interval; default one second.
    pub text_flush_interval: Duration,
    /// Text buffer size; default 16 KiB.
    pub text_max_buffered_bytes: usize,
    /// Execute-only overrides.
    pub step_options_override: Option<StepOptionsOverride>,
}

impl<StepInput, Operation: Query> QueryStepConfig<StepInput, Operation> {
    /// Creates a config with required fields and defaults for the rest.
    pub fn new(
        step_type: &'static str,
        annotations: StepAnnotations,
        operation: Operation,
        connection: ConnectionRef,
        map_to_operation_input: impl Fn(&StepInput) -> Operation::Input + Send + Sync + 'static,
        branches: BTreeMap<String, BranchTarget<QueryResult<Option<Operation::Output>>>>,
    ) -> Self {
        Self {
            step_type,
            annotations,
            operation,
            connection,
            map_to_operation_input: Box::new(map_to_operation_input),
            branches,
            configuration_ui: ConnectorConfigurationUi::default(),
            result_attribute: None,
            progress_stream: None,
            text_stream: None,
            text_flush_interval: DEFAULT_TEXT_FLUSH_INTERVAL,
            text_max_buffered_bytes: DEFAULT_TEXT_MAX_BUFFERED_BYTES,
            step_options_override: None,
        }
    }
}

/// Binds one Mutation to a typed Dex Step and declared branch destinations.
///
/// `max_sends_override` replaces the send cap of a `limited` operation for this Step, within 1
/// to 10; 1 disables resend. Operations that are `safe`, `unsafe`, or undeclared reject it.
/// When the operation's retry policy limits attempts, the Step adds attempts for a raised cap
/// so it stays reachable; an explicit `step_options_override.execute_retry` still wins.
pub struct MutationStepConfig<StepInput, Operation: Mutation> {
    /// Stable Step type registered with Dex.
    pub step_type: &'static str,
    /// Authoring group and explanation.
    pub annotations: StepAnnotations,
    /// Provider operation.
    pub operation: Operation,
    /// Static logical connection.
    pub connection: ConnectionRef,
    /// Maps the Step input to the operation input.
    pub map_to_operation_input: OperationInputMapper<StepInput, Operation::Input>,
    /// Branch destinations keyed by declared branch ID.
    pub branches: BTreeMap<String, BranchTarget<MutationResult<Option<Operation::Output>>>>,
    /// Connector-owned configuration units.
    pub configuration_ui: ConnectorConfigurationUi,
    /// Attribute written with the result before routing.
    pub result_attribute: Option<&'static Attribute<MutationResult<Option<Operation::Output>>>>,
    /// Best-effort structured progress Stream.
    pub progress_stream: Option<&'static Stream<ProgressUpdate>>,
    /// Best-effort buffered text Stream.
    pub text_stream: Option<&'static Stream<String>>,
    /// Text flush interval; default one second.
    pub text_flush_interval: Duration,
    /// Text buffer size; default 16 KiB.
    pub text_max_buffered_bytes: usize,
    /// Execute-only overrides.
    pub step_options_override: Option<StepOptionsOverride>,
    /// Send cap override of a limited operation, within 1 to 10.
    pub max_sends_override: Option<u32>,
}

impl<StepInput, Operation: Mutation> MutationStepConfig<StepInput, Operation> {
    /// Creates a config with required fields and defaults for the rest.
    pub fn new(
        step_type: &'static str,
        annotations: StepAnnotations,
        operation: Operation,
        connection: ConnectionRef,
        map_to_operation_input: impl Fn(&StepInput) -> Operation::Input + Send + Sync + 'static,
        branches: BTreeMap<String, BranchTarget<MutationResult<Option<Operation::Output>>>>,
    ) -> Self {
        Self {
            step_type,
            annotations,
            operation,
            connection,
            map_to_operation_input: Box::new(map_to_operation_input),
            branches,
            configuration_ui: ConnectorConfigurationUi::default(),
            result_attribute: None,
            progress_stream: None,
            text_stream: None,
            text_flush_interval: DEFAULT_TEXT_FLUSH_INTERVAL,
            text_max_buffered_bytes: DEFAULT_TEXT_MAX_BUFFERED_BYTES,
            step_options_override: None,
            max_sends_override: None,
        }
    }
}

struct RouteTable<Result> {
    targets: BTreeMap<String, BranchTarget<Result>>,
    optional: BTreeSet<String>,
}

/// Invokes one Query per Dex Execute attempt and routes its terminal branch.
pub struct QueryStep<StepInput, Operation: Query> {
    config: QueryStepConfig<StepInput, Operation>,
    routes: RouteTable<QueryResult<Option<Operation::Output>>>,
    step_defaults: StepDefaults,
}

impl<StepInput, Operation> QueryStep<StepInput, Operation>
where
    Operation: Query,
{
    /// Validates the config and creates the Step; register it in a Flow's `steps()`.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for an invalid definition, annotation, connection, UI, or
    /// branch wiring.
    pub fn new(config: QueryStepConfig<StepInput, Operation>) -> Result<Self, ValidationError> {
        let definition = config.operation.definition();
        definition.validate()?;
        validate_factory(config.step_type, &config.annotations, &config.connection)?;
        config.configuration_ui.validate()?;
        let routes = validate_branches(
            definition.branches.as_deref().unwrap_or_default(),
            &config.branches,
        )?;
        Ok(Self {
            step_defaults: definition.step_defaults,
            config,
            routes,
        })
    }

    /// Returns the authoring group and explanation.
    pub fn annotations(&self) -> &StepAnnotations {
        &self.config.annotations
    }

    /// Returns the connector-owned configuration units.
    pub fn configuration_ui(&self) -> &ConnectorConfigurationUi {
        &self.config.configuration_ui
    }
}

impl<StepInput, Operation> Step for QueryStep<StepInput, Operation>
where
    StepInput: Value,
    Operation: Query + 'static,
    Operation::Output: Value,
{
    type Input = StepInput;

    fn execute(&self, context: &mut Context, input: StepInput) -> HandlerResult<StepDecision> {
        let definition = self.config.operation.definition();
        let text_stream = open_text_stream(
            context,
            self.config.text_stream,
            self.config.text_flush_interval,
            self.config.text_max_buffered_bytes,
        )?;
        let outcome = {
            let attempt =
                StepAttempt::with_streams(context, self.config.progress_stream, text_stream);
            let call = attempt
                .new_call(self.config.connection.clone(), definition.operation.clone())
                .map_err(HandlerError::from_error)?;
            let operation_input = (self.config.map_to_operation_input)(&input);
            let outcome = run_query(&self.config.operation, &call, &operation_input);
            (outcome, failure_route_context(&call))
        };
        let (outcome, route_context) = outcome;
        let result = match outcome {
            OperationOutcome::Completed(result) => result,
            OperationOutcome::Retry(retry) => return Err(dex_retry_error(&retry)),
        };
        if let Some(attribute) = self.config.result_attribute {
            attribute.set(context, result.clone())?;
        }
        let branch = result.branch.clone();
        let failure = result.failure.clone();
        route_branch(
            &self.routes,
            &branch,
            result,
            failure.as_ref(),
            &route_context,
            "query",
        )
    }

    fn step_type(&self) -> &'static str {
        self.config.step_type
    }

    fn options(&self) -> StepOptions<StepInput> {
        step_options(
            &self.step_defaults,
            self.config.step_options_override.as_ref(),
            0,
        )
    }
}

/// Invokes one Mutation per Dex Execute attempt and routes its terminal branch.
pub struct MutationStep<StepInput, Operation: Mutation> {
    config: MutationStepConfig<StepInput, Operation>,
    routes: RouteTable<MutationResult<Option<Operation::Output>>>,
    step_defaults: StepDefaults,
    max_sends: u32,
    additional_execute_attempts: u32,
}

impl<StepInput, Operation> MutationStep<StepInput, Operation>
where
    Operation: Mutation,
{
    /// Validates the config and creates the Step; register it in a Flow's `steps()`.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for an invalid definition, annotation, connection, UI,
    /// branch wiring, or send cap override.
    pub fn new(config: MutationStepConfig<StepInput, Operation>) -> Result<Self, ValidationError> {
        let definition = config.operation.definition();
        definition.validate()?;
        validate_factory(config.step_type, &config.annotations, &config.connection)?;
        config.configuration_ui.validate()?;
        let routes = validate_branches(
            definition.branches.as_deref().unwrap_or_default(),
            &config.branches,
        )?;
        let is_limited = definition.repeat_on_unknown == Some(RepeatOnUnknown::Limited);
        let (max_sends, additional_execute_attempts) = match config.max_sends_override {
            None if is_limited => (definition.max_sends, 0),
            None => (MINIMUM_MAX_SENDS, 0),
            Some(override_value) => {
                validate_max_sends(override_value)?;
                if !is_limited {
                    return Err(ValidationError::new(
                        "max_sends_override requires a repeatOnUnknown limited operation",
                    ));
                }
                (
                    override_value,
                    override_value.saturating_sub(definition.max_sends),
                )
            }
        };
        Ok(Self {
            step_defaults: definition.step_defaults,
            config,
            routes,
            max_sends,
            additional_execute_attempts,
        })
    }

    /// Returns the single-dispatch send cap every attempt of this Step receives.
    pub fn max_sends(&self) -> u32 {
        self.max_sends
    }

    /// Returns the authoring group and explanation.
    pub fn annotations(&self) -> &StepAnnotations {
        &self.config.annotations
    }

    /// Returns the connector-owned configuration units.
    pub fn configuration_ui(&self) -> &ConnectorConfigurationUi {
        &self.config.configuration_ui
    }
}

impl<StepInput, Operation> Step for MutationStep<StepInput, Operation>
where
    StepInput: Value,
    Operation: Mutation + 'static,
    Operation::Output: Value,
{
    type Input = StepInput;

    fn execute(&self, context: &mut Context, input: StepInput) -> HandlerResult<StepDecision> {
        let definition = self.config.operation.definition();
        let text_stream = open_text_stream(
            context,
            self.config.text_stream,
            self.config.text_flush_interval,
            self.config.text_max_buffered_bytes,
        )?;
        let (outcome, route_context) = {
            let attempt =
                StepAttempt::with_streams(context, self.config.progress_stream, text_stream);
            let operation_input = (self.config.map_to_operation_input)(&input);
            let outcome = run_step_mutation(
                &self.config.operation,
                &attempt,
                self.config.connection.clone(),
                &operation_input,
                self.config.max_sends_override,
            );
            let route_context = FailureRouteContext {
                connector_id: definition.operation.connector_id.clone(),
                operation_id: definition.operation.operation_id.clone(),
                connection_name: self.config.connection.name.clone(),
            };
            (outcome, route_context)
        };
        let result = match outcome {
            OperationOutcome::Completed(result) => result,
            OperationOutcome::Retry(retry) => return Err(dex_retry_error(&retry)),
        };
        if let Some(attribute) = self.config.result_attribute {
            attribute.set(context, result.clone())?;
        }
        let branch = result.branch.clone();
        let failure = result.failure.clone();
        route_branch(
            &self.routes,
            &branch,
            result,
            failure.as_ref(),
            &route_context,
            "mutation",
        )
    }

    fn step_type(&self) -> &'static str {
        self.config.step_type
    }

    fn options(&self) -> StepOptions<StepInput> {
        step_options(
            &self.step_defaults,
            self.config.step_options_override.as_ref(),
            self.additional_execute_attempts,
        )
    }
}

/// Maps a connector [`RetryError`] to the Dex retry signal.
///
/// A Step that calls [`run_query`] or [`crate::run_mutation`] directly returns this as its handler
/// error. A positive delay, truncated to whole seconds, becomes `HandlerError::retry_after`;
/// a delay under one second uses the Step retry policy's backoff. Retry exhaustion fails the
/// Flow.
pub fn dex_retry_error(retry: &RetryError) -> HandlerError {
    let error_type = format!("dex_connectors_sdk::RetryError::{}", retry.failure().kind);
    let seconds = retry.retry_after_whole_seconds();
    if seconds > 0 {
        HandlerError::retry_after(seconds, error_type, retry.to_string())
    } else {
        HandlerError::new(error_type, retry.to_string())
    }
}

fn open_text_stream(
    context: &mut Context,
    text_stream: Option<&'static Stream<String>>,
    flush_interval: Duration,
    max_buffered_bytes: usize,
) -> HandlerResult<Option<dex_sdk::BufferedTextStream>> {
    text_stream
        .map(|stream| {
            stream.buffered_text_with_options(
                context,
                BufferedTextStreamOptions::new(flush_interval, max_buffered_bytes),
            )
        })
        .transpose()
}

fn validate_factory(
    step_type: &str,
    annotations: &StepAnnotations,
    connection: &ConnectionRef,
) -> Result<(), ValidationError> {
    if step_type.trim().is_empty() {
        return Err(ValidationError::new("stable Step type is required"));
    }
    annotations.validate()?;
    connection.validate()
}

fn validate_branches<Result>(
    definitions: &[BranchDefinition],
    targets: &BTreeMap<String, BranchTarget<Result>>,
) -> std::result::Result<RouteTable<Result>, ValidationError> {
    for branch_id in targets.keys() {
        if !definitions.iter().any(|branch| &branch.id == branch_id) {
            return Err(ValidationError::new(
                "branch target is not declared by the operation",
            ));
        }
    }
    for branch in definitions {
        if !branch.optional && !targets.contains_key(&branch.id) {
            return Err(ValidationError::new(format!(
                "branch target {:?} is required",
                branch.id
            )));
        }
    }
    Ok(RouteTable {
        targets: targets.clone(),
        optional: definitions
            .iter()
            .filter(|branch| branch.optional)
            .map(|branch| branch.id.clone())
            .collect(),
    })
}

fn step_options<Input: Value>(
    defaults: &StepDefaults,
    override_options: Option<&StepOptionsOverride>,
    additional_execute_attempts: u32,
) -> StepOptions<Input> {
    let policy = defaults
        .execute_retry
        .unwrap_or_else(default_connector_step_execute_retry_policy);
    let mut maximum_attempts = policy.maximum_attempts;
    if maximum_attempts > 0 {
        // Zero means unlimited attempts, which already reach any send cap.
        maximum_attempts += additional_execute_attempts;
    }
    let default_retry = dex_sdk::RetryPolicy::new()
        .initial_interval(policy.initial_interval)
        .backoff_coefficient(policy.backoff_coefficient)
        .maximum_interval(policy.maximum_interval)
        .maximum_attempts(maximum_attempts)
        .total_duration(policy.total_duration);
    let override_options = override_options.cloned().unwrap_or_default();
    let mut options = StepOptions::new()
        .execute_retry(override_options.execute_retry.unwrap_or(default_retry))
        .execute_durability(
            override_options
                .execute_durability
                .filter(|durability| *durability != StepDurability::Default)
                .unwrap_or_else(|| defaults.execute_durability.to_dex_step_durability()),
        );
    let execute_method_timeout = override_options
        .execute_method_timeout
        .filter(|timeout| !timeout.is_zero())
        .unwrap_or(defaults.execute_method_timeout);
    if !execute_method_timeout.is_zero() {
        options = options.execute_method_timeout(execute_method_timeout);
    }
    let heartbeat_timeout = override_options
        .heartbeat_timeout
        .filter(|timeout| !timeout.is_zero())
        .unwrap_or(defaults.heartbeat_timeout);
    if !heartbeat_timeout.is_zero() {
        options = options.heartbeat_timeout(heartbeat_timeout);
    }
    options
}

struct FailureRouteContext {
    connector_id: String,
    operation_id: String,
    connection_name: String,
}

fn failure_route_context(call: &Call<'_>) -> FailureRouteContext {
    FailureRouteContext {
        connector_id: call.operation.connector_id.clone(),
        operation_id: call.operation.operation_id.clone(),
        connection_name: call.connection.name.clone(),
    }
}

fn route_branch<Result: Value>(
    routes: &RouteTable<Result>,
    branch: &str,
    result: Result,
    failure: Option<&Failure>,
    route_context: &FailureRouteContext,
    operation_kind: &str,
) -> HandlerResult<StepDecision> {
    if let Some(target) = routes.targets.get(branch) {
        return Ok(target.go_to(result));
    }
    if routes.optional.contains(branch) {
        let mut reason = format!(
            "{} {} (connection {}) selected branch {} with no target",
            route_context.connector_id,
            route_context.operation_id,
            route_context.connection_name,
            branch
        );
        if let Some(failure) = failure {
            let message = truncate_utf8(&failure.message, FAILURE_REASON_LIMIT_BYTES);
            reason.push_str(&format!(": {}: {}", failure.kind, message));
            if failure.message.len() > FAILURE_REASON_LIMIT_BYTES {
                reason.push_str(" (truncated)");
            }
        }
        return Ok(StepDecision::force_fail(reason));
    }
    Err(HandlerError::new(
        "dex_connectors_sdk::UnconfiguredBranch",
        format!("{operation_kind} result selected unconfigured branch {branch:?}"),
    ))
}

fn truncate_utf8(value: &str, limit_bytes: usize) -> &str {
    if value.len() <= limit_bytes {
        return value;
    }
    let mut end = limit_bytes;
    while !value.is_char_boundary(end) {
        end -= 1;
    }
    &value[..end]
}
