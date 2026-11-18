# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider operation and credential resolution contracts without Dex."""

import asyncio
from dataclasses import replace
from datetime import UTC, datetime, timedelta
from typing import Any, cast

import dex
import pytest

from dex_connectors.sdk import (
    BranchDefinition,
    Call,
    CallID,
    ConnectionRef,
    CredentialRefreshResult,
    CredentialRefreshState,
    Failure,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
    OperationRef,
    QueryAttempt,
    QueryDefinition,
    Receipt,
    RetryError,
    StaticCredentialProvider,
    StepDefaults,
    derive_call_id,
    derive_step_call_id,
    resolve_credential,
    resolve_credential_after_rejection,
    run_mutation,
    run_query,
)

CALL_ID = CallID("5af8fafa-06ff-5b2d-bb13-04693809fabf")
CONNECTION = ConnectionRef("sample", "default")
OPERATION = OperationRef("sample", "createItem")
CALL = Call(CALL_ID, CONNECTION, OPERATION)
RECEIPT = Receipt(CALL_ID, "sample", datetime(2026, 10, 29, tzinfo=UTC))
FAILURE = Failure(FailureKind.RATE_LIMIT, "sample", "createItem", "throttled")
DEFAULTS = StepDefaults(timedelta(seconds=10), 1)
BRANCHES = (BranchDefinition("success", "Created"), BranchDefinition("defect", "Local defect", True))


class ExampleQuery:
    """Classify one scripted Query attempt."""

    def __init__(self, attempt: QueryAttempt[str]) -> None:
        self.attempt = attempt

    def definition(self) -> QueryDefinition:
        """Return the static Query definition."""
        return QueryDefinition(OPERATION, BRANCHES, DEFAULTS)

    async def invoke(self, call: Call, input: str) -> QueryAttempt[str]:
        """Return the script's classification."""
        return self.attempt


class ExampleMutation:
    """Classify one scripted Mutation attempt."""

    def __init__(self, attempt: MutationAttempt[str], key: str = "") -> None:
        self.attempt = attempt
        self.key = key
        self.observed_call: Call | None = None

    def definition(self) -> MutationDefinition:
        """Return the static Mutation definition."""
        return MutationDefinition(
            OPERATION, BRANCHES + (BranchDefinition("uncertain", "Unknown", True),), DEFAULTS
        )

    def idempotency_key(self, call_id: CallID, input: str) -> IdempotencyKey:
        """Use an explicit or fallback key."""
        return IdempotencyKey(self.key)

    async def invoke(self, call: Call, input: str) -> MutationAttempt[str]:
        """Capture the deduplication key used for provider execution."""
        self.observed_call = call
        return self.attempt


class ExampleRefreshDriver:
    """Inert deterministic credential refresh driver."""

    def refresh_required(self, state: CredentialRefreshState[str]) -> bool:
        """Never refresh an inert static credential."""
        return False

    async def refresh(self, state: CredentialRefreshState[str]) -> CredentialRefreshResult[str]:
        """Return inert replacement material for protocol checking."""
        return CredentialRefreshResult("replacement", datetime.now(UTC) + timedelta(hours=1))


class ExampleRefreshingProvider:
    """Exercise both coordinated refresh paths."""

    async def resolve(self, call: Call) -> str:
        """Return the unrefreshed value."""
        return "old"

    async def resolve_with_refresh(self, call: Call, driver: ExampleRefreshDriver) -> str:
        """Return normally refreshed material."""
        return "current"

    async def resolve_after_rejection(self, call: Call, driver: ExampleRefreshDriver) -> str:
        """Return forced replacement material."""
        return "replacement"


@pytest.mark.asyncio
async def test_query_and_mutation_outcomes() -> None:
    """Validate branch, retry, uncertainty, and receipt identity."""
    success = await run_query(ExampleQuery(QueryAttempt.branch_result("success", "ok", RECEIPT)), CALL, "x")
    assert success.branch == "success"
    assert success.receipt.call_id == CALL_ID
    defect = await run_query(ExampleQuery(QueryAttempt.branch_result("other", "x", RECEIPT)), CALL, "x")
    assert defect.branch == "defect" and defect.failure is not None
    assert defect.failure.kind == FailureKind.LOCAL_DEFECT
    with pytest.raises(RetryError) as retry:
        await run_query(ExampleQuery(QueryAttempt.retry(FAILURE, 2)), CALL, "x")
    assert retry.value.retry_after_seconds == 2
    mutation = ExampleMutation(MutationAttempt.branch_result("success", "ok", RECEIPT))
    result = await run_mutation(mutation, CALL, "x")
    assert result.branch == "success"
    assert mutation.observed_call is not None and mutation.observed_call.idempotency_key == str(CALL_ID)
    uncertain = await run_mutation(
        ExampleMutation(MutationAttempt.uncertain("x", FAILURE, RECEIPT)), CALL, "x"
    )
    assert uncertain.branch == "uncertain" and uncertain.failure == FAILURE
    wrong_receipt = Receipt(CallID("5af8fafa-06ff-5b2d-bb13-04693809fab0"), "sample", datetime.now(UTC))
    invalid = await run_mutation(
        ExampleMutation(MutationAttempt.branch_result("success", "x", wrong_receipt)), CALL, "x"
    )
    assert invalid.branch == "defect"
    zero_result = await run_mutation(
        ExampleMutation(MutationAttempt.branch_result("success", "x", Receipt())), CALL, "x"
    )
    assert zero_result.receipt.call_id == CALL_ID
    assert zero_result.receipt.provider == "sample"
    assert zero_result.receipt.to_wire()["observedAt"] != "0001-01-01T00:00:00Z"
    with pytest.raises(RetryError):
        await run_mutation(ExampleMutation(MutationAttempt.retry(FAILURE)), CALL, "x")


@pytest.mark.asyncio
async def test_invalid_operation_attempts_select_defect_without_retry() -> None:
    """Construction failures in provider classifications fail the Flow through defect."""
    class InvalidQuery(ExampleQuery):
        async def invoke(self, call: Call, input: str) -> QueryAttempt[str]:
            """Return an invalid failure from inside the provider operation."""
            return QueryAttempt.branch_result(
                "success", input, RECEIPT,
                Failure(FailureKind.LOCAL_DEFECT, "sample", "createItem", ""),
            )

    class InvalidMutation(ExampleMutation):
        async def invoke(self, call: Call, input: str) -> MutationAttempt[str]:
            """Return a retry classification missing its required failure."""
            return MutationAttempt.retry(None)  # type: ignore[arg-type]

    query = await run_query(InvalidQuery(QueryAttempt.branch_result("success", "ok", RECEIPT)), CALL, "x")
    mutation = await run_mutation(
        InvalidMutation(MutationAttempt.branch_result("success", "ok", RECEIPT)), CALL, "x"
    )
    assert query.branch == mutation.branch == "defect"
    assert query.failure is not None and query.failure.kind == FailureKind.LOCAL_DEFECT
    assert mutation.failure is not None and mutation.failure.kind == FailureKind.LOCAL_DEFECT


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("retry_after_seconds", "whole_seconds"),
    [(0.5, 0), (1.9, 1), (2.0, 2)],
)
async def test_retry_delay_truncates_to_dex_whole_seconds(
    retry_after_seconds: float, whole_seconds: int
) -> None:
    """Preserve provider precision until the Dex whole-second conversion boundary."""
    with pytest.raises(RetryError) as query_retry:
        await run_query(ExampleQuery(QueryAttempt.retry(FAILURE, retry_after_seconds)), CALL, "x")
    assert query_retry.value.retry_after_seconds == retry_after_seconds
    assert query_retry.value.retry_after_whole_seconds == whole_seconds

    with pytest.raises(RetryError) as mutation_retry:
        await run_mutation(
            ExampleMutation(MutationAttempt.retry(FAILURE, retry_after_seconds)), CALL, "x"
        )
    assert mutation_retry.value.retry_after_seconds == retry_after_seconds
    assert mutation_retry.value.retry_after_whole_seconds == whole_seconds


@pytest.mark.asyncio
async def test_credential_resolution_modes() -> None:
    """Resolve static, refreshed, and rejected credentials without exposing secrets."""
    driver = ExampleRefreshDriver()
    static = StaticCredentialProvider({CONNECTION: "secret-material"})
    assert await resolve_credential(static, CALL, driver) == "secret-material"
    assert "secret-material" not in repr(static)
    with pytest.raises(ValueError, match="does not support"):
        await resolve_credential_after_rejection(static, CALL, driver)
    refreshing = ExampleRefreshingProvider()
    assert await resolve_credential(refreshing, CALL, driver) == "current"
    assert await resolve_credential_after_rejection(refreshing, CALL, driver) == "replacement"
    with pytest.raises(ValueError, match="not configured"):
        await static.resolve(Call(CALL_ID, ConnectionRef("sample", "missing"), OPERATION))
    with pytest.raises(ValueError, match="required"):
        await resolve_credential(None, CALL, driver)  # type: ignore[arg-type]


class StepContext:
    """Weakly referenceable Step identity, as a real Dex context is."""

    def __init__(self) -> None:
        self.flow_id = "flow-1"
        self.step_execution_id = "step-1"
        self.attempt = 1


class BlockingMutation(ExampleMutation):
    """Hold invoke open until released, so another call overlaps it."""

    def __init__(self, operation: OperationRef = OPERATION) -> None:
        super().__init__(MutationAttempt.branch_result("success", "ok", RECEIPT))
        self.operation = operation
        self.started = asyncio.Event()
        self.release = asyncio.Event()
        self.invocations = 0

    def definition(self) -> MutationDefinition:
        """Return the Mutation definition of the configured operation."""
        return replace(super().definition(), operation=self.operation)

    async def invoke(self, call: Call, input: str) -> MutationAttempt[str]:
        """Block until the test releases the call."""
        self.invocations += 1
        self.started.set()
        await self.release.wait()
        return await super().invoke(call, input)


@pytest.mark.asyncio
async def test_concurrent_call_of_the_same_mutation_is_refused_before_sending() -> None:
    """A second call of a running operation selects defect; others and later calls run."""
    context = StepContext()
    step_call = Call(CALL_ID, CONNECTION, OPERATION, context=cast(dex.AsyncContext, context))
    running = BlockingMutation()
    first = asyncio.create_task(run_mutation(running, step_call, "first"))
    await running.started.wait()

    concurrent = ExampleMutation(MutationAttempt.branch_result("success", "ok", RECEIPT))
    refused = await run_mutation(concurrent, step_call, "second")
    assert refused.branch == "defect" and refused.failure is not None
    assert "concurrent calls" in refused.failure.message
    assert concurrent.observed_call is None, "the refused call never reaches invoke"

    other_operation = OperationRef("sample", "deleteItem")
    other = BlockingMutation(other_operation)
    other.release.set()
    other_call = replace(step_call, operation=other_operation)
    assert (await run_mutation(other, other_call, "other")).branch == "success"

    running.release.set()
    assert (await first).branch == "success"
    assert (await run_mutation(concurrent, step_call, "third")).branch == "success"
    assert running.invocations == 1


@pytest.mark.asyncio
async def test_refused_concurrent_mutation_keeps_its_occurrence() -> None:
    """Refused calls keep their ordinals, so the next call gets occurrence 4 regardless of timing."""
    context = cast(dex.AsyncContext, StepContext())
    unbound_receipt = replace(RECEIPT, call_id=CallID(""))

    def derived_call() -> Call:
        return Call(derive_step_call_id(context, OPERATION), CONNECTION, OPERATION, context=context)

    running = BlockingMutation()
    running.attempt = MutationAttempt.branch_result("success", "ok", unbound_receipt)
    refused_mutations = [
        ExampleMutation(MutationAttempt.branch_result("success", "ok", unbound_receipt))
        for _ in range(2)
    ]
    # gather builds every Call before any Mutation begins.
    calls = [derived_call() for _ in range(3)]
    first = asyncio.create_task(run_mutation(running, calls[0], "first"))
    await running.started.wait()
    refused = await asyncio.gather(*(
        run_mutation(mutation, call, "concurrent")
        for mutation, call in zip(refused_mutations, calls[1:], strict=True)
    ))
    assert [result.branch for result in refused] == ["defect", "defect"]
    running.release.set()
    assert (await first).receipt.call_id == calls[0].id

    later = ExampleMutation(MutationAttempt.branch_result("success", "ok", unbound_receipt))
    later_call = derived_call()
    fourth_occurrence = derive_call_id("flow-1", "step-1", OPERATION, occurrence=4)
    assert later_call.id == fourth_occurrence, "construction consumed occurrences 1 to 3"
    result = await run_mutation(later, later_call, "later")
    assert later.observed_call is not None and later.observed_call.id == fourth_occurrence
    assert result.receipt.call_id == fourth_occurrence
    assert str(later.observed_call.idempotency_key) == str(fourth_occurrence)

    assert await _run_and_observe_call_id(later_call) == fourth_occurrence, "a rerun keeps its ID"
    assert await _run_and_observe_call_id(calls[1]) == calls[1].id, "a refused call later runs with its own ID"

    application_call = Call(CALL_ID, CONNECTION, OPERATION, context=context)
    assert await _run_and_observe_call_id(application_call) == CALL_ID


async def _run_and_observe_call_id(call: Call) -> CallID | None:
    """Run a succeeding Mutation and return the Call ID its invoke saw, or None when refused."""
    mutation = ExampleMutation(
        MutationAttempt.branch_result("success", "ok", replace(RECEIPT, call_id=CallID("")))
    )
    await run_mutation(mutation, call, "input")
    return mutation.observed_call.id if mutation.observed_call is not None else None


@pytest.mark.asyncio
async def test_occurrences_follow_construction_order_only() -> None:
    """Unused calls, reruns, and refused calls keep their IDs; overlaps never shift an occurrence."""
    context = cast(dex.AsyncContext, StepContext())

    def derived_call() -> Call:
        return Call(derive_step_call_id(context, OPERATION), CONNECTION, OPERATION, context=context)

    def occurrence(n: int) -> CallID:
        return derive_call_id("flow-1", "step-1", OPERATION, occurrence=n)

    first = derived_call()
    running = BlockingMutation()
    running.attempt = MutationAttempt.branch_result("success", "ok", replace(RECEIPT, call_id=CallID("")))
    task = asyncio.create_task(run_mutation(running, first, "first"))
    await running.started.wait()
    assert await _run_and_observe_call_id(first) is None, "the running call itself is refused"
    running.release.set()
    await task
    assert await _run_and_observe_call_id(first) == occurrence(1), "a rerun keeps its ID"
    unused = derived_call()
    assert unused.id == occurrence(2)
    assert await _run_and_observe_call_id(derived_call()) == occurrence(3), "an unused call shifts nothing"

    blocking = BlockingMutation()
    blocking.attempt = running.attempt
    holder = derived_call()
    task = asyncio.create_task(run_mutation(blocking, holder, "holder"))
    await blocking.started.wait()
    refused = derived_call()
    assert await _run_and_observe_call_id(refused) is None
    blocking.release.set()
    await task
    assert await _run_and_observe_call_id(derived_call()) == occurrence(6), "the refused call kept occurrence 5"
    assert await _run_and_observe_call_id(refused) == occurrence(5), "the refused call reruns with its own ID"


def test_step_attempt_state_requires_a_weakly_referenceable_context() -> None:
    """A context without weak references raises instead of silently not sharing state."""
    from types import SimpleNamespace

    context = SimpleNamespace(flow_id="flow-1", step_execution_id="step-1", attempt=1)
    with pytest.raises(TypeError, match="weak references"):
        derive_step_call_id(cast(dex.AsyncContext, context), OPERATION)


def test_operation_protocols_are_typed() -> None:
    """Keep the concrete operation methods structurally compatible."""
    query: Any = ExampleQuery(QueryAttempt.branch_result("success", "ok", RECEIPT))
    mutation: Any = ExampleMutation(MutationAttempt.branch_result("success", "ok", RECEIPT))
    assert query.definition().operation == mutation.definition().operation
