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
        # An unweakrefable context gets an unshared value; callers must tolerate that.
        # Test fakes may omit `attempt`; they then share one value per context.
        attempt: int = getattr(context, "attempt", 0)
        try:
            existing = self._by_context.get(context)
        except TypeError:
            return self._create(context)
        if existing is not None and existing[0] == attempt:
            return existing[1]
        value = self._create(context)
        self._by_context[context] = (attempt, value)
        return value

    def __len__(self) -> int:
        return len(self._by_context)


def _new_occurrence_counts(context: dex.AsyncContext) -> dict[tuple[str, str], int]:
    return {}


_call_occurrences: _StepAttemptRegistry[dict[tuple[str, str], int]] = _StepAttemptRegistry(
    _new_occurrence_counts
)


def _next_call_occurrence(context: dex.AsyncContext, connector_id: str, operation_id: str) -> int:
    counts = _call_occurrences.value_for(context)
    key = (connector_id, operation_id)
    counts[key] = counts.get(key, 0) + 1
    return counts[key]
