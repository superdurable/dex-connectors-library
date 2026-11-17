# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Per-Step heartbeat checkpoint shared by every Fence of one Step attempt."""

import asyncio
import weakref
from typing import Any

import dex

from .markers import Marker, checkpoint_heartbeat, markers_from_heartbeat


class StepCheckpoint:
    """Merge the markers of every Fence in one Step attempt into each heartbeat.

    The format is internal to the connector SDKs and may change.
    """

    def __init__(self, attempt: int, markers: dict[str, Marker]) -> None:
        self.attempt = attempt
        self._markers = markers
        self._lock = asyncio.Lock()

    async def record(self, context: dex.AsyncContext, marker: Marker) -> None:
        """Persist every known marker with `marker` replacing its Call ID's entry.

        The entry is committed only after the heartbeat succeeds. The lock spans
        the heartbeat so the last persisted value holds every committed entry.
        """
        async with self._lock:
            markers = {**self._markers, str(marker.call_id): marker}
            await context.heartbeat(checkpoint_heartbeat(markers))
            self._markers = markers


_step_checkpoints: "weakref.WeakKeyDictionary[object, StepCheckpoint]" = weakref.WeakKeyDictionary()


def step_checkpoint_for(context: dex.AsyncContext) -> StepCheckpoint:
    """Return the checkpoint shared by every Fence of the context's current attempt.

    Entries disappear with their context. A context that cannot be weakly
    referenced gets an unshared checkpoint, which can lose other calls'
    entries; a lost entry only overcounts earlier sends.
    """
    attempt = context.attempt
    try:
        existing = _step_checkpoints.get(context)
    except TypeError:
        return StepCheckpoint(attempt, _previous_attempt_markers(context))
    if existing is not None and existing.attempt == attempt:
        return existing
    checkpoint = StepCheckpoint(attempt, _previous_attempt_markers(context))
    _step_checkpoints[context] = checkpoint
    return checkpoint


def _previous_attempt_markers(context: dex.AsyncContext) -> dict[str, Marker]:
    try:
        if not context.has_last_heartbeat_value():
            return {}
        return markers_from_heartbeat(context.get_last_heartbeat_value(dict[str, Any]))
    except Exception:
        # Seeding nothing loses entries, which only overcounts earlier sends.
        return {}
