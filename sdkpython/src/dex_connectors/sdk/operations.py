# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Async provider operation contracts independent of Dex Worker registration."""

from dataclasses import replace
from datetime import UTC, datetime
from typing import NoReturn, Protocol, TypeVar, cast

from .errors import Failure, FailureKind, RetryError
from .timestamps import RFC3339NanoTimestamp
from .types import (
    Call,
    CallID,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
    MutationResult,
    QueryAttempt,
    QueryDefinition,
    QueryResult,
    Receipt,
)

Input = TypeVar("Input", contravariant=True)
Output = TypeVar("Output")


class Query(Protocol[Input, Output]):
    """Read-only provider operation invoked once per Dex Execute attempt.

    Implementations classify a definite business outcome as a declared branch and
    a safe-to-repeat failure as retry. They must not retain credentials in results.
    """

    def definition(self) -> QueryDefinition:
        """Return the immutable operation identity, branches, and Step defaults."""
        ...

    async def invoke(self, call: Call, input: Input) -> QueryAttempt[Output]:
        """Perform one provider call and classify its attempt."""
        ...


class Mutation(Protocol[Input, Output]):
    """Provider write with stable deduplication across Dex Execute retries.

    An uncertain attempt is valid only when a request may have reached the
    provider and neither safe repetition nor read-back can resolve the outcome.
    """

    def definition(self) -> MutationDefinition:
        """Return the immutable operation identity, branches, and Step defaults."""
        ...

    def idempotency_key(self, call_id: CallID, input: Input) -> IdempotencyKey:
        """Return a provider-safe key stable for the logical call and input.

        An empty key asks the SDK to use the Call ID as the fallback.
        """
        ...

    async def invoke(self, call: Call, input: Input) -> MutationAttempt[Output]:
        """Perform one provider write and classify its attempt."""
        ...


async def run_query(operation: Query[Input, Output], call: Call, input: Input) -> QueryResult[Output]:
    """Invoke a Query once and validate its declared terminal branch and receipt.

    A retry raises RetryError for the Dex-facing Step to schedule. Invalid
    operation output selects the declared defect route with a safe local failure.
    """
    definition = operation.definition()
    try:
        definition.validate()
        _validate_call(call, definition.operation)
    except ValueError as error:
        return _query_defect(call, str(error))
    attempt = await operation.invoke(call, input)
    if attempt.kind == "retry":
        _raise_retry(attempt.failure, attempt.retry_after_seconds)
    if attempt.branch not in {branch.id for branch in definition.branches or ()}:
        return _query_defect(call, "query returned an unknown branch")
    try:
        receipt = _complete_receipt(attempt.receipt, call)
    except ValueError as error:
        return _query_defect(call, str(error))
    return QueryResult(attempt.branch, cast(Output, attempt.value), receipt, attempt.failure)


async def run_mutation(
    operation: Mutation[Input, Output], call: Call, input: Input
) -> MutationResult[Output]:
    """Invoke a Mutation once with stable idempotency and validate its outcome.

    The returned Call ID is the fallback key. A retry raises RetryError; only a
    declared uncertain route may represent an unconfirmable, non-repeatable write.
    """
    definition = operation.definition()
    try:
        definition.validate()
        _validate_call(call, definition.operation)
    except ValueError as error:
        return _mutation_defect(call, str(error))
    idempotency_key = operation.idempotency_key(call.id, input) or IdempotencyKey(call.id)
    if not idempotency_key.strip():
        return _mutation_defect(call, "mutation returned an invalid idempotency key")
    call = replace(call, idempotency_key=IdempotencyKey(idempotency_key))
    attempt = await operation.invoke(call, input)
    if attempt.kind == "retry":
        _raise_retry(attempt.failure, attempt.retry_after_seconds)
    if attempt.branch not in {branch.id for branch in definition.branches or ()}:
        return _mutation_defect(call, "mutation returned an unknown branch")
    if attempt.kind == "uncertain" and attempt.branch != "uncertain":
        return _mutation_defect(call, "mutation returned invalid uncertainty")
    try:
        receipt = _complete_receipt(attempt.receipt, call)
    except ValueError as error:
        return _mutation_defect(call, str(error))
    return MutationResult(attempt.branch, cast(Output, attempt.value), receipt, attempt.failure)


def _validate_call(call: Call, operation: object) -> None:
    call.id.validate()
    call.connection.validate()
    if call.operation != operation:
        raise ValueError("call operation does not match the definition")


def _complete_receipt(receipt: Receipt | None, call: Call) -> Receipt:
    if receipt is None:
        raise ValueError("operation returned no receipt")
    if receipt.call_id and receipt.call_id != call.id:
        raise ValueError("operation returned a receipt for another call")
    if receipt.idempotency_key and receipt.idempotency_key != call.idempotency_key:
        raise ValueError("operation returned a receipt with another idempotency key")
    is_zero_time = (
        receipt.observed_at.parsed == datetime.min.replace(tzinfo=UTC)
        if isinstance(receipt.observed_at, RFC3339NanoTimestamp)
        else receipt.observed_at == datetime.min.replace(tzinfo=UTC)
    )
    return replace(
        receipt,
        call_id=call.id,
        provider=receipt.provider or call.operation.connector_id,
        idempotency_key=call.idempotency_key,
        observed_at=datetime.now(UTC) if is_zero_time else receipt.observed_at,
    )


def _raise_retry(failure: Failure | None, delay_seconds: float) -> NoReturn:
    if failure is None:
        raise ValueError("retry attempt requires a failure")
    raise RetryError(failure, delay_seconds)


def _failure(call: Call, message: str) -> Failure:
    return Failure(
        FailureKind.LOCAL_DEFECT, call.operation.connector_id, call.operation.operation_id, message
    )


def _defect_receipt(call: Call) -> Receipt:
    return Receipt(call.id, call.operation.connector_id, datetime.now(UTC), call.idempotency_key)


def _query_defect(call: Call, message: str) -> QueryResult[Output]:
    return QueryResult("defect", cast(Output, None), _defect_receipt(call), _failure(call, message))


def _mutation_defect(call: Call, message: str) -> MutationResult[Output]:
    return MutationResult("defect", cast(Output, None), _defect_receipt(call), _failure(call, message))
