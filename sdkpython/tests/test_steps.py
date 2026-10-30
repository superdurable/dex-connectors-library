# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Dex registration and connector Step factory validation."""

from datetime import timedelta

import dex
import pytest

from dex_connectors.sdk import (
    BranchDefinition,
    Call,
    CallID,
    ConnectionRef,
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
    StepAnnotations,
    StepDefaults,
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
