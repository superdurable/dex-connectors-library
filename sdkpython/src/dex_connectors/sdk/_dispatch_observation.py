# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Private per-dispatch record of the provider response status seen while a Fence sends."""

from contextvars import ContextVar
from dataclasses import dataclass


@dataclass(slots=True)
class _DispatchResponseObservation:
    """The HTTP status of the latest request of one Fence dispatch, or None before its headers."""

    status_code: int | None = None


# A mutable holder, because asyncio.wait_for on Python 3.11 runs the send in a copied context.
_current_dispatch_observation: ContextVar[_DispatchResponseObservation | None] = ContextVar(
    "dex_connectors_dispatch_observation", default=None
)


def _record_dispatch_response_status(status_code: int) -> None:
    """Record a response status for the enclosing Fence dispatch; a no-op outside one."""
    observation = _current_dispatch_observation.get()
    if observation is not None:
        observation.status_code = status_code


def _clear_dispatch_response_status() -> None:
    """Forget an earlier request's status as a new request starts; a no-op outside a dispatch."""
    observation = _current_dispatch_observation.get()
    if observation is not None:
        observation.status_code = None
