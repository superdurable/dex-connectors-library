# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Private in-process state shared by every call of one Dex Step attempt."""

import weakref
from collections.abc import Callable
from dataclasses import dataclass, field
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


@dataclass(slots=True)
class _CallOccurrences:
    """Per-operation counts of constructed calls, the Call IDs they were given, and begun Mutations."""

    constructed_counts: dict[tuple[str, str], int] = field(default_factory=dict)
    issued_call_ids: set[str] = field(default_factory=set)
    begun_mutation_counts: dict[tuple[str, str], int] = field(default_factory=dict)
    running_mutations: set[tuple[str, str]] = field(default_factory=set)


def _new_call_occurrences(context: dex.AsyncContext) -> _CallOccurrences:
    return _CallOccurrences()


_call_occurrences: _StepAttemptRegistry[_CallOccurrences] = _StepAttemptRegistry(
    _new_call_occurrences
)


def _next_call_occurrence(context: dex.AsyncContext, connector_id: str, operation_id: str) -> int:
    counts = _call_occurrences.value_for(context).constructed_counts
    key = (connector_id, operation_id)
    counts[key] = counts.get(key, 0) + 1
    return counts[key]


def _record_issued_call_id(context: dex.AsyncContext, call_id: str) -> None:
    """Remember a Call ID the SDK derived, so its Mutation may take the begun occurrence."""
    _call_occurrences.value_for(context).issued_call_ids.add(call_id)


def _is_issued_call_id(context: dex.AsyncContext, call_id: str) -> bool:
    return call_id in _call_occurrences.value_for(context).issued_call_ids


def _begin_mutation_occurrence(
    context: dex.AsyncContext, connector_id: str, operation_id: str
) -> tuple[int, Callable[[], None]] | None:
    """Mark a Mutation as running and return its begun occurrence and end function.

    Returns None while another call of the operation runs, without consuming
    an occurrence, as Go's beginMutationOccurrence does.
    """
    occurrences = _call_occurrences.value_for(context)
    key = (connector_id, operation_id)
    if key in occurrences.running_mutations:
        return None
    occurrences.running_mutations.add(key)
    occurrence = occurrences.begun_mutation_counts.get(key, 0) + 1
    occurrences.begun_mutation_counts[key] = occurrence
    return occurrence, lambda: occurrences.running_mutations.discard(key)
