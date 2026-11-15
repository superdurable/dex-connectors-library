# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Dex registration and connector Step factory validation."""

from datetime import timedelta
from types import SimpleNamespace
from typing import cast

import dex
import pytest

import dex_connectors.sdk
import dex_connectors.sdk.steps
from dex_connectors.sdk import (
    BranchDefinition,
    Call,
    CallID,
    ConnectionRef,
    Failure,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
    MutationResult,
    MutationStep,
    MutationStepConfig,
    OperationRef,
    QueryAttempt,
    QueryDefinition,
    QueryResult,
    QueryStep,
    QueryStepConfig,
    Receipt,
    RepeatOnUnknown,
    RetryError,
    RetryPolicy,
    StepAnnotations,
    StepDefaults,
    raise_dex_retry,
)

OPERATION = OperationRef("sample", "createItem")
CONNECTION = ConnectionRef("sample", "main")
BRANCHES = (BranchDefinition("success", "Done"), BranchDefinition("defect", "Defect", True))
ANNOTATIONS = StepAnnotations("sample", "Sample", "Create one sample item.")


class ExampleQuery:
    """Small async Query for Step registration."""

    def definition(self) -> QueryDefinition:
        """Declare a happy path and optional local defect route."""
        return QueryDefinition(OPERATION, BRANCHES, StepDefaults(timedelta(seconds=10), 2))

    async def invoke(self, call: Call, input: str) -> QueryAttempt[str]:
        """Return a terminal result without provider I/O."""
        return QueryAttempt.branch_result("success", input, Receipt())


class ExampleMutation:
    """Small async Mutation for Step registration."""

    def definition(self) -> MutationDefinition:
        """Declare a happy path and optional local defect route."""
        return MutationDefinition(OPERATION, BRANCHES, StepDefaults(timedelta(seconds=10), 2))

    def idempotency_key(self, call_id: CallID, input: str) -> IdempotencyKey:
        """Use the call ID fallback."""
        return IdempotencyKey("")

    async def invoke(self, call: Call, input: str) -> MutationAttempt[str]:
        """Return a terminal result without provider I/O."""
        return MutationAttempt.branch_result("success", input, Receipt())


class QuerySink(dex.Step[QueryResult[str]]):
    """Consume the typed Query result."""

    async def execute(  # type: ignore[override]
        self, context: dex.AsyncContext, input: QueryResult[str]
    ) -> dex.StepDecision:
        """Complete after a successful query."""
        return dex.graceful_complete()


class MutationSink(dex.Step[MutationResult[str]]):
    """Consume the typed Mutation result."""

    async def execute(  # type: ignore[override]
        self, context: dex.AsyncContext, input: MutationResult[str]
    ) -> dex.StepDecision:
        """Complete after a successful mutation."""
        return dex.graceful_complete()


class QueryFlow(dex.Flow[str]):
    """Register a generic connector Step by its instance identity."""

    def __init__(self, step: QueryStep[str, str, str], sink: QuerySink) -> None:
        self.step = step
        self.sink = sink

    def get_steps(self) -> dex.StepList[str]:
        """Use the connector Step as the start."""
        return dex.StepList.start_step(self.step).other_steps(self.sink)


class MutationFlow(dex.Flow[str]):
    """Register a generic Mutation Step by its instance identity."""

    def __init__(self, step: MutationStep[str, str, str], sink: MutationSink) -> None:
        self.step = step
        self.sink = sink

    def get_steps(self) -> dex.StepList[str]:
        """Use the connector Step as the start."""
        return dex.StepList.start_step(self.step).other_steps(self.sink)


def test_query_and_mutation_steps_register_with_async_worker_codec() -> None:
    """R2 instance identity and R3 generic result codecs register together."""
    query_sink = QuerySink()
    query = QueryStep(QueryStepConfig(
        "QueryItem", str, ANNOTATIONS, ExampleQuery(), CONNECTION,
        lambda value: value, {"success": query_sink},
    ))
    mutation_sink = MutationSink()
    mutation = MutationStep(MutationStepConfig(
        "MutationItem", str, ANNOTATIONS, ExampleMutation(), CONNECTION,
        lambda value: value, {"success": mutation_sink},
    ))
    dex.Registry([QueryFlow(query, query_sink), MutationFlow(mutation, mutation_sink)],
                 allow_async_handlers=True)
    assert query.get_step_type() == "QueryItem"
    assert query.get_input_type() is str
    assert query.get_step_options().execute_retry is not None
    assert mutation.get_step_options().execute_durability == dex.StepDurability.ASYNC


def test_step_factory_rejects_missing_required_branch_and_wait_options() -> None:
    """Catch invalid static application wiring before Dex Worker startup."""
    with pytest.raises(ValueError, match="required"):
        QueryStep(QueryStepConfig(
            "QueryItem", str, ANNOTATIONS, ExampleQuery(), CONNECTION,
            lambda value: value, {},
        ))
    with pytest.raises(ValueError, match="wait_for"):
        MutationStep(MutationStepConfig(
            "MutationItem", str, ANNOTATIONS, ExampleMutation(), CONNECTION,
            lambda value: value, {"success": MutationSink()},
            step_options_override=dex.StepOptions(wait_for_method_timeout=timedelta(seconds=1)),
        ))


@pytest.mark.parametrize(
    ("delay", "expected_whole_seconds"),
    [(0.5, 0), (1.9, 1), (2.0, 2)],
)
def test_step_factory_truncates_retry_after_to_dex_seconds(
    delay: float, expected_whole_seconds: int
) -> None:
    """Dex receives a positive whole-second hint only after truncation."""
    failure = Failure(FailureKind.AVAILABILITY, "sample", "createItem", "unavailable")
    retry = RetryError(failure, delay)
    if expected_whole_seconds == 0:
        with pytest.raises(RetryError) as raised_retry:
            raise_dex_retry(retry)
        assert raised_retry.value is retry
    else:
        with pytest.raises(dex.RetryAfterError) as raised_after:
            raise_dex_retry(retry)
        assert raised_after.value.after_seconds == expected_whole_seconds
        assert raised_after.value.cause is retry


class TieredMutation(ExampleMutation):
    """Mutation declaring a repeat-on-unknown tier and recording each Call's send cap."""

    def __init__(
        self, repeat_on_unknown: RepeatOnUnknown | None, max_sends: int = 0,
        execute_retry: RetryPolicy | None = None,
    ) -> None:
        self.repeat_on_unknown = repeat_on_unknown
        self.max_sends = max_sends
        self.execute_retry = execute_retry
        self.call_max_sends: list[int] = []

    def definition(self) -> MutationDefinition:
        """Declare the tier, manifest send cap, and optional retry policy."""
        return MutationDefinition(
            OPERATION, BRANCHES,
            StepDefaults(timedelta(seconds=10), 1, execute_retry=self.execute_retry),
            self.repeat_on_unknown, self.max_sends,
        )

    async def invoke(self, call: Call, input: str) -> MutationAttempt[str]:
        """Record the cap this attempt would pass to its Fence."""
        self.call_max_sends.append(call.max_sends)
        return MutationAttempt.branch_result("success", input, Receipt())


def _tiered_step(
    mutation: TieredMutation, max_sends_override: int | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[str, str, str]:
    return MutationStep(MutationStepConfig(
        "MutationItem", str, ANNOTATIONS, mutation, CONNECTION,
        lambda value: value, {"success": MutationSink()},
        step_options_override=step_options_override,
        max_sends_override=max_sends_override,
    ))


def _limited_retry(maximum_attempts: int) -> RetryPolicy:
    return RetryPolicy(
        timedelta(seconds=1), 2.0, timedelta(minutes=5), maximum_attempts, timedelta(minutes=10)
    )


def test_mutation_definition_validates_repeat_on_unknown_tier() -> None:
    """Only a limited Mutation carries a 2..10 send cap."""
    MutationDefinition(OPERATION, BRANCHES, StepDefaults(timedelta(seconds=1), 1)).validate()
    MutationDefinition(
        OPERATION, BRANCHES, StepDefaults(timedelta(seconds=1), 1), RepeatOnUnknown.LIMITED, 3
    ).validate()
    for repeat_on_unknown, max_sends in (
        (RepeatOnUnknown.LIMITED, 0), (RepeatOnUnknown.LIMITED, 1), (RepeatOnUnknown.LIMITED, 11),
        (RepeatOnUnknown.SAFE, 3), (RepeatOnUnknown.UNSAFE, 2), (None, 3),
    ):
        with pytest.raises(ValueError, match="maxSends"):
            MutationDefinition(
                OPERATION, BRANCHES, StepDefaults(timedelta(seconds=1), 1),
                repeat_on_unknown, max_sends,
            ).validate()
    wire = MutationDefinition(
        OPERATION, BRANCHES, StepDefaults(timedelta(seconds=1), 1), RepeatOnUnknown.LIMITED, 3
    ).to_wire()
    assert set(wire) == {"operation", "branches", "stepDefaults"}


def test_max_sends_override_requires_limited_operation_and_valid_range() -> None:
    """safe, unsafe, and undeclared operations reject the per-Step send cap."""
    for repeat_on_unknown in (RepeatOnUnknown.SAFE, RepeatOnUnknown.UNSAFE, None):
        with pytest.raises(ValueError, match="limited"):
            _tiered_step(TieredMutation(repeat_on_unknown), max_sends_override=2)
    for invalid in (0, 11, True):
        with pytest.raises(ValueError, match="max sends"):
            _tiered_step(TieredMutation(RepeatOnUnknown.LIMITED, 3), max_sends_override=invalid)
    assert _tiered_step(TieredMutation(RepeatOnUnknown.UNSAFE)).max_sends == 1
    assert _tiered_step(TieredMutation(None)).max_sends == 1
    assert _tiered_step(TieredMutation(RepeatOnUnknown.LIMITED, 3)).max_sends == 3
    assert _tiered_step(TieredMutation(RepeatOnUnknown.LIMITED, 3), 1).max_sends == 1
    assert _tiered_step(TieredMutation(RepeatOnUnknown.LIMITED, 3), 10).max_sends == 10


def test_max_sends_override_widens_limited_execute_attempts() -> None:
    """A raised cap adds attempts; unlimited attempts and explicit retry stay as given."""
    def attempts(step: MutationStep[str, str, str]) -> int:
        retry = step.get_step_options().execute_retry
        assert retry is not None
        return retry.maximum_attempts

    limited = TieredMutation(RepeatOnUnknown.LIMITED, 3, _limited_retry(4))
    assert attempts(_tiered_step(limited)) == 4
    assert attempts(_tiered_step(limited, 5)) == 6
    assert attempts(_tiered_step(limited, 2)) == 4
    unlimited = TieredMutation(RepeatOnUnknown.LIMITED, 3, _limited_retry(0))
    assert attempts(_tiered_step(unlimited, 10)) == 0
    assert attempts(_tiered_step(TieredMutation(RepeatOnUnknown.LIMITED, 3), 10)) == 0
    explicit = dex.RetryPolicy(maximum_attempts=2)
    assert attempts(_tiered_step(limited, 5, dex.StepOptions(execute_retry=explicit))) == 2


@pytest.mark.asyncio
async def test_mutation_step_passes_send_cap_to_every_call() -> None:
    """Every attempt of the Step sees the same send cap on its Call."""
    mutation = TieredMutation(RepeatOnUnknown.LIMITED, 3)
    step = _tiered_step(mutation, 5)
    context = SimpleNamespace(flow_id="flow-1", step_execution_id="step-1")
    for _ in range(2):
        await step.execute(cast(dex.AsyncContext, context), "value")
    assert mutation.call_max_sends == [5, 5]


def test_retry_translation_is_public_and_shared_with_step_factories() -> None:
    """A tool bridge and a Step factory reach the same translation."""
    assert dex_connectors.sdk.raise_dex_retry is dex_connectors.sdk.steps.raise_dex_retry
    assert "raise_dex_retry" in dex_connectors.sdk.__all__
