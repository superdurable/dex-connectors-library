# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Private per-Step heartbeat checkpoint shared by every Fence of one Step attempt."""

import asyncio
import weakref
from typing import Any

import dex

from .markers import Marker, _checkpoint_heartbeat, _markers_from_heartbeat


class _StepCheckpoint:
    """Merge the markers of every Fence in one Step attempt into each heartbeat."""

    def __init__(self, attempt: int, markers: dict[str, Marker]) -> None:
        self.attempt = attempt
        self._markers = markers
        self._lock = asyncio.Lock()

    async def record(self, context: dex.AsyncContext, marker: Marker) -> None:
        # The lock spans the heartbeat so the last persisted value has every committed entry.
        async with self._lock:
            markers = {**self._markers, str(marker.call_id): marker}
            await context.heartbeat(_checkpoint_heartbeat(markers))
            self._markers = markers


_step_checkpoints: "weakref.WeakKeyDictionary[object, _StepCheckpoint]" = weakref.WeakKeyDictionary()


def _step_checkpoint_for(context: dex.AsyncContext) -> _StepCheckpoint:
    # An unweakrefable context gets an unshared checkpoint; lost entries only overcount.
    attempt = context.attempt
    try:
        existing = _step_checkpoints.get(context)
    except TypeError:
        return _StepCheckpoint(attempt, _previous_attempt_markers(context))
    if existing is not None and existing.attempt == attempt:
        return existing
    checkpoint = _StepCheckpoint(attempt, _previous_attempt_markers(context))
    _step_checkpoints[context] = checkpoint
    return checkpoint


def _previous_attempt_markers(context: dex.AsyncContext) -> dict[str, Marker]:
    try:
        if not context.has_last_heartbeat_value():
            return {}
        return _markers_from_heartbeat(context.get_last_heartbeat_value(dict[str, Any]))
    except Exception:
        # Seeding nothing loses entries, which only overcounts earlier sends.
        return {}
