# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Private in-process state shared by every call of one Dex Step attempt."""

import weakref
from collections.abc import Callable
from dataclasses import dataclass, field
from enum import Enum
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


_OperationKey = tuple[str, str]


@dataclass(slots=True)
class _OperationOccurrences:
    """Occurrence bookkeeping of one operation in one Step attempt, keyed by construction ordinal."""

    constructed_count: int = 0
    # A refused call that never began gives up its ordinal; later calls shift down past it.
    freed_ordinals: set[int] = field(default_factory=set)
    begun_occurrences: dict[int, int] = field(default_factory=dict)
    ordinal_holding_occurrence: dict[int, int] = field(default_factory=dict)
    is_mutation_running: bool = False


@dataclass(slots=True)
class _CallOccurrences:
    """Per-operation occurrences and the construction ordinal of every Call ID the SDK derived."""

    operations: dict[_OperationKey, _OperationOccurrences] = field(default_factory=dict)
    issued_ordinals: dict[str, tuple[_OperationKey, int]] = field(default_factory=dict)

    def for_operation(self, key: _OperationKey) -> _OperationOccurrences:
        return self.operations.setdefault(key, _OperationOccurrences())


def _new_call_occurrences(context: dex.AsyncContext) -> _CallOccurrences:
    return _CallOccurrences()


_call_occurrences: _StepAttemptRegistry[_CallOccurrences] = _StepAttemptRegistry(
    _new_call_occurrences
)


def _next_call_occurrence(context: dex.AsyncContext, connector_id: str, operation_id: str) -> int:
    operation = _call_occurrences.value_for(context).for_operation((connector_id, operation_id))
    operation.constructed_count += 1
    return operation.constructed_count


def _record_issued_call_id(
    context: dex.AsyncContext, connector_id: str, operation_id: str, call_id: str, ordinal: int
) -> None:
    """Remember the construction ordinal of a Call ID the SDK derived."""
    _call_occurrences.value_for(context).issued_ordinals[call_id] = ((connector_id, operation_id), ordinal)


class _MutationRefusal(Enum):
    """Why a Mutation call was refused before it was invoked; nothing was sent."""

    CONCURRENT = "concurrent"
    OCCURRENCE_HELD = "occurrenceHeld"


@dataclass(frozen=True, slots=True)
class _BegunMutation:
    """A running Mutation: the occurrence of its Call ID, or None to keep it, and its end function."""

    occurrence: int | None
    end: Callable[[], None]


def _begin_mutation_occurrence(
    context: dex.AsyncContext, connector_id: str, operation_id: str, call_id: str
) -> _BegunMutation | _MutationRefusal:
    """Mark a Mutation as running and return the occurrence its Call ID runs with.

    As in Go's beginMutationOccurrence, a call refused as concurrent before
    it ever began consumes no occurrence: later SDK-derived calls of the
    operation shift down past its ordinal. A call keeps its first begun
    occurrence on later runs, and a call whose occurrence another call
    already holds is refused, so two calls never share a Call ID.
    """
    occurrences = _call_occurrences.value_for(context)
    key = (connector_id, operation_id)
    operation = occurrences.for_operation(key)
    issued = occurrences.issued_ordinals.get(call_id)
    ordinal = issued[1] if issued is not None and issued[0] == key else None
    if operation.is_mutation_running:
        if ordinal is not None and ordinal not in operation.begun_occurrences:
            operation.freed_ordinals.add(ordinal)
        return _MutationRefusal.CONCURRENT
    occurrence: int | None = None
    if ordinal is not None:
        occurrence = operation.begun_occurrences.get(ordinal)
        if occurrence is None:
            operation.freed_ordinals.discard(ordinal)
            occurrence = ordinal - sum(1 for freed in operation.freed_ordinals if freed < ordinal)
            if operation.ordinal_holding_occurrence.get(occurrence, ordinal) != ordinal:
                return _MutationRefusal.OCCURRENCE_HELD
            operation.begun_occurrences[ordinal] = occurrence
            operation.ordinal_holding_occurrence[occurrence] = ordinal
    operation.is_mutation_running = True

    def end() -> None:
        operation.is_mutation_running = False

    return _BegunMutation(occurrence, end)
