# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Private in-process state shared by every call of one Dex Step attempt."""

import weakref
from collections.abc import Callable
from typing import Generic, TypeVar

import dex

T = TypeVar("T")


class _StepAttemptRegistry(Generic[T]):
    """Hold one value per context object and attempt; values disappear with their context."""

    def __init__(self, create: Callable[[dex.AsyncContext], T]) -> None:
        self._create = create
        self._by_context: weakref.WeakKeyDictionary[object, tuple[int, T]] = weakref.WeakKeyDictionary()

    def value_for(self, context: dex.AsyncContext) -> T:
        """Return the context's value for its current attempt, creating it when absent.

        Raises TypeError for a context that is unhashable or does not support
        weak references, since an unshared value would break call counting
        and checkpoints.
        """
        # Test fakes may omit `attempt`; they then share one value per context.
        attempt: int = getattr(context, "attempt", 0)
        try:
            existing = self._by_context.get(context)
        except TypeError as error:
            raise TypeError(
                "connector calls require a hashable Dex Step context that supports weak references; "
                f"{type(context).__name__} is not, so its calls cannot share Step attempt state"
            ) from error
        if existing is not None and existing[0] == attempt:
            return existing[1]
        value = self._create(context)
        self._by_context[context] = (attempt, value)
        return value

    def __len__(self) -> int:
        return len(self._by_context)


def _new_occurrence_counts(context: dex.AsyncContext) -> dict[tuple[str, str], int]:
    return {}


def _new_running_mutations(context: dex.AsyncContext) -> set[tuple[str, str]]:
    return set()


_call_occurrences: _StepAttemptRegistry[dict[tuple[str, str], int]] = _StepAttemptRegistry(
    _new_occurrence_counts
)
_running_mutations: _StepAttemptRegistry[set[tuple[str, str]]] = _StepAttemptRegistry(
    _new_running_mutations
)


def _next_call_occurrence(context: dex.AsyncContext, connector_id: str, operation_id: str) -> int:
    counts = _call_occurrences.value_for(context)
    key = (connector_id, operation_id)
    counts[key] = counts.get(key, 0) + 1
    return counts[key]


def _begin_running_mutation(
    context: dex.AsyncContext, connector_id: str, operation_id: str
) -> Callable[[], None] | None:
    """Mark a Mutation call as running and return its end function, or None while another runs."""
    running = _running_mutations.value_for(context)
    key = (connector_id, operation_id)
    if key in running:
        return None
    running.add(key)
    return lambda: running.discard(key)
