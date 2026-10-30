# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Instance-addressed async Dex Steps for typed connector operations."""

import re
from collections.abc import Callable, Mapping
from dataclasses import dataclass, field, replace
from datetime import timedelta
from typing import Generic, TypeVar, cast

import dex

from .configuration_ui import ConnectorConfigurationUI
from .errors import Failure, RetryError
from .operations import Mutation, Query, run_mutation, run_query
from .retry_policy import default_connector_step_execute_retry_policy
from .types import (
    Call,
    ConnectionRef,
    MutationResult,
    ProgressUpdate,
    QueryResult,
    StepDefaults,
    _ProgressReporter,
    derive_call_id,
)

StepInput = TypeVar("StepInput")
OperationInput = TypeVar("OperationInput")
Output = TypeVar("Output")
_GROUP_ID = re.compile(r"^[a-z][a-z0-9-]*$")


@dataclass(frozen=True, slots=True)
class StepAnnotations:
    """Stable group and explanation metadata for a Connector Step."""

    group_id: str
    group_label: str
    explanation: str

    def validate(self) -> None:
        """Require a stable group identifier and meaningful display text."""
        if (
            not _GROUP_ID.fullmatch(self.group_id)
            or not self.group_label.strip()
            or not self.explanation.strip()
        ):
            raise ValueError("Step group, group label, and explanation are required")


@dataclass(frozen=True, slots=True)
class QueryStepConfig(Generic[StepInput, OperationInput, Output]):
    """Bind one Query to a typed Dex Step and declared branch destinations.

    Register the constructed Step instance in a Flow. Its `step_input_type`
    selects the Dex codec when generic handler annotations contain TypeVars.
    An unwired optional branch fails the Flow if selected.
    """

    step_type: str
    step_input_type: object
    annotations: StepAnnotations
    operation: Query[OperationInput, Output]
    connection: ConnectionRef
    map_to_operation_input: Callable[[StepInput], OperationInput]
    branches: Mapping[str, dex.StepTarget[QueryResult[Output]]]
    configuration_ui: ConnectorConfigurationUI = field(
        default_factory=lambda: ConnectorConfigurationUI(())
    )
    result_attribute: dex.Attribute[QueryResult[Output]] | None = None
    progress_stream: dex.Stream[ProgressUpdate] | None = None
    text_stream: dex.Stream[str] | None = None
    text_flush_interval: timedelta = timedelta(seconds=1)
    text_max_buffered_bytes: int = 16 * 1024
    step_options_override: dex.StepOptions | None = None


@dataclass(frozen=True, slots=True)
class MutationStepConfig(Generic[StepInput, OperationInput, Output]):
    """Bind one Mutation to a typed Dex Step and declared branch destinations.

    Register the constructed Step instance in a Flow. The operation supplies
    a stable idempotency key for retries, and only declared terminal branches
    may route to another Step.
    """

    step_type: str
    step_input_type: object
    annotations: StepAnnotations
    operation: Mutation[OperationInput, Output]
    connection: ConnectionRef
    map_to_operation_input: Callable[[StepInput], OperationInput]
    branches: Mapping[str, dex.StepTarget[MutationResult[Output]]]
    configuration_ui: ConnectorConfigurationUI = field(
        default_factory=lambda: ConnectorConfigurationUI(())
    )
    result_attribute: dex.Attribute[MutationResult[Output]] | None = None
    progress_stream: dex.Stream[ProgressUpdate] | None = None
    text_stream: dex.Stream[str] | None = None
    text_flush_interval: timedelta = timedelta(seconds=1)
    text_max_buffered_bytes: int = 16 * 1024
    step_options_override: dex.StepOptions | None = None


class QueryStep(dex.Step[StepInput], Generic[StepInput, OperationInput, Output]):
    """Invoke one Query per Dex Execute attempt and route its terminal branch."""

    def __init__(self, config: QueryStepConfig[StepInput, OperationInput, Output]) -> None:
        if config.operation is None:
            raise ValueError("query operation is required")
        definition = config.operation.definition()
        definition.validate()
        _validate_factory(config.step_type, config.step_input_type, config.annotations,
                          config.connection, config.map_to_operation_input)
        config.configuration_ui.validate()
        self._branches, self._optional_branches = _validate_branches(
            definition.branches or (), config.branches
        )
        self._config = config
        self._operation_ref = definition.operation
        self._options = _step_options(definition.step_defaults, config.step_options_override)

    def get_step_type(self) -> str:
        """Return the stable instance identity registered with Dex."""
        return self._config.step_type

    def get_input_type(self) -> object:
        """Return this instance's concrete Step input codec type."""
        return self._config.step_input_type

    def get_step_options(self) -> dex.StepOptions:
        """Return the Execute defaults merged with application overrides."""
        return self._options

    @property
    def annotations(self) -> StepAnnotations:
        """Return the authoring group and explanation for this Step."""
        return self._config.annotations

    @property
    def configuration_ui(self) -> ConnectorConfigurationUI:
        """Return the connector-owned configuration units for this Step."""
        return self._config.configuration_ui

    async def execute(  # type: ignore[override]
        self, context: dex.AsyncContext, input: StepInput
    ) -> dex.StepDecision:
        """Invoke the Query, persist its result if configured, and route its branch."""
        call = _call(
            context, self._operation_ref, self._config.connection,
            self._config.progress_stream, self._config.text_stream,
            self._config.text_flush_interval, self._config.text_max_buffered_bytes,
        )
        try:
            result = await run_query(
                self._config.operation, call, self._config.map_to_operation_input(input)
            )
        except RetryError as error:
            _raise_dex_retry(error)
        if self._config.result_attribute is not None:
            self._config.result_attribute.set(context, result)
        return _route_branch(
            self._branches, self._optional_branches, result.branch, result,
            result.failure, call, "query",
        )


class MutationStep(dex.Step[StepInput], Generic[StepInput, OperationInput, Output]):
    """Invoke one Mutation per Dex Execute attempt and route its terminal branch."""

    def __init__(self, config: MutationStepConfig[StepInput, OperationInput, Output]) -> None:
        if config.operation is None:
            raise ValueError("mutation operation is required")
        definition = config.operation.definition()
        definition.validate()
        _validate_factory(config.step_type, config.step_input_type, config.annotations,
                          config.connection, config.map_to_operation_input)
        config.configuration_ui.validate()
        self._branches, self._optional_branches = _validate_branches(
            definition.branches or (), config.branches
        )
        self._config = config
        self._operation_ref = definition.operation
        self._options = _step_options(definition.step_defaults, config.step_options_override)

    def get_step_type(self) -> str:
        """Return the stable instance identity registered with Dex."""
        return self._config.step_type

    def get_input_type(self) -> object:
        """Return this instance's concrete Step input codec type."""
        return self._config.step_input_type

    def get_step_options(self) -> dex.StepOptions:
        """Return the Execute defaults merged with application overrides."""
        return self._options

    @property
    def annotations(self) -> StepAnnotations:
        """Return the authoring group and explanation for this Step."""
        return self._config.annotations

    @property
    def configuration_ui(self) -> ConnectorConfigurationUI:
        """Return the connector-owned configuration units for this Step."""
        return self._config.configuration_ui

    async def execute(  # type: ignore[override]
        self, context: dex.AsyncContext, input: StepInput
    ) -> dex.StepDecision:
        """Invoke the Mutation, persist its result if configured, and route its branch."""
        call = _call(
            context, self._operation_ref, self._config.connection,
            self._config.progress_stream, self._config.text_stream,
            self._config.text_flush_interval, self._config.text_max_buffered_bytes,
        )
        try:
            result = await run_mutation(
                self._config.operation, call, self._config.map_to_operation_input(input)
            )
        except RetryError as error:
            _raise_dex_retry(error)
        if self._config.result_attribute is not None:
            self._config.result_attribute.set(context, result)
        return _route_branch(
            self._branches, self._optional_branches, result.branch, result,
            result.failure, call, "mutation",
        )


def _validate_factory(
    step_type: str, step_input_type: object, annotations: StepAnnotations,
    connection: ConnectionRef, mapper: object,
) -> None:
    if not step_type.strip():
        raise ValueError("stable Step type is required")
    if step_input_type is None:
        raise ValueError("Step input type is required")
    annotations.validate()
    connection.validate()
    if mapper is None:
        raise ValueError("map_to_operation_input is required")


def _validate_branches(
    definitions: tuple[object, ...], targets: Mapping[str, dex.StepTarget[Output]],
) -> tuple[dict[str, dex.StepTarget[Output]], set[str]]:
    from .types import BranchDefinition

    declared = {branch.id: branch for branch in cast(tuple[BranchDefinition, ...], definitions)}
    if set(targets) - set(declared):
        raise ValueError("branch target is not declared by the operation")
    for branch_id, target in targets.items():
        if target is None:
            raise ValueError(f"branch target {branch_id!r} must name a Step")
    for branch_id, branch in declared.items():
        if not branch.optional and branch_id not in targets:
            raise ValueError(f"branch target {branch_id!r} is required")
    return dict(targets), {branch_id for branch_id, branch in declared.items() if branch.optional}


def _step_options(defaults: StepDefaults, override: dex.StepOptions | None) -> dex.StepOptions:
    if override is not None and (
        override.wait_for_method_timeout is not None
        or override.wait_for_retry is not None
        or override.wait_for_failure != dex.WaitForFailurePolicy.FAIL_FLOW
        or override.wait_for_durability != dex.StepDurability.DEFAULT
        or override.wait_for_lock_attributes
        or override.wait_for_load_attribute_maps
        or override.wait_for_load_attribute_map_instances
        or override.wait_for_load_channels
        or override.wait_for_load_channel_maps
        or override.wait_for_load_channel_map_instances
    ):
        raise ValueError("execute-only connector Step rejects wait_for options")
    policy = defaults.execute_retry or default_connector_step_execute_retry_policy()
    retry = dex.RetryPolicy(
        initial_interval=policy.initial_interval,
        backoff_coefficient=policy.backoff_coefficient,
        maximum_interval=policy.maximum_interval,
        maximum_attempts=policy.maximum_attempts,
        total_duration=policy.total_duration,
    )
    base = dex.StepOptions(
        execute_method_timeout=defaults.execute_method_timeout,
        heartbeat_timeout=defaults.heartbeat_timeout or None,
        execute_retry=retry,
        execute_durability=(
            dex.StepDurability.DEFAULT,
            dex.StepDurability.SYNC,
            dex.StepDurability.ASYNC,
        )[defaults.execute_durability],
    )
    if override is None:
        return base
    return replace(
        override,
        execute_method_timeout=override.execute_method_timeout or base.execute_method_timeout,
        heartbeat_timeout=override.heartbeat_timeout or base.heartbeat_timeout,
        execute_retry=override.execute_retry or base.execute_retry,
        execute_durability=(
            override.execute_durability
            if override.execute_durability != dex.StepDurability.DEFAULT
            else base.execute_durability
        ),
    )


def _call(
    context: dex.AsyncContext, operation: object, connection: ConnectionRef,
    progress_stream: dex.Stream[ProgressUpdate] | None,
    text_stream: dex.Stream[str] | None, text_flush_interval: timedelta,
    text_max_buffered_bytes: int,
) -> Call:
    from .types import OperationRef

    operation_ref = cast(OperationRef, operation)
    if not context.flow_id or not context.step_execution_id:
        raise ValueError("connector operations require a Dex Flow and Step execution")
    call_id = derive_call_id(context.flow_id, context.step_execution_id, operation_ref)
    progress_reporter = (
        _ProgressReporter(progress_stream, context, call_id)
        if progress_stream is not None else None
    )
    text_writer = (
        text_stream.buffered_text(
            context, flush_interval=text_flush_interval,
            max_buffered_bytes=text_max_buffered_bytes,
        )
        if text_stream is not None else None
    )
    return Call(
        call_id, connection, operation_ref, context=context,
        _progress_reporter=progress_reporter, _text_writer=text_writer,
    )


def _raise_dex_retry(error: RetryError) -> None:
    if error.retry_after_whole_seconds > 0:
        raise dex.retry_after(error.retry_after_whole_seconds, error) from error
    raise error


def _route_branch(
    branches: Mapping[str, dex.StepTarget[Output]], optional: set[str], branch: str,
    result: Output, failure: Failure | None, call: Call, kind: str,
) -> dex.StepDecision:
    target = branches.get(branch)
    if target is not None:
        return dex.go_to(target, result)
    if branch in optional:
        reason = (
            f"{call.operation.connector_id} {call.operation.operation_id} "
            f"(connection {call.connection.name}) selected branch {branch} with no target"
        )
        if failure is not None:
            message = failure.message.encode("utf-8")[:1024].decode("utf-8", errors="ignore")
            reason += f": {failure.kind}: {message}"
            if len(failure.message.encode("utf-8")) > 1024:
                reason += " (truncated)"
        return dex.force_fail(reason)
    raise ValueError(f"{kind} result selected unconfigured branch {branch!r}")
