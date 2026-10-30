# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Async provider operation contracts independent of Dex Worker registration."""

from typing import Protocol, TypeVar

from .types import (
    Call,
    CallID,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
    QueryAttempt,
    QueryDefinition,
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
