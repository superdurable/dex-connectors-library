# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider operation and credential resolution contracts without Dex."""

from datetime import UTC, datetime, timedelta
from typing import Any

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


def test_operation_protocols_are_typed() -> None:
    """Keep the concrete operation methods structurally compatible."""
    query: Any = ExampleQuery(QueryAttempt.branch_result("success", "ok", RECEIPT))
    mutation: Any = ExampleMutation(MutationAttempt.branch_result("success", "ok", RECEIPT))
    assert query.definition().operation == mutation.definition().operation
