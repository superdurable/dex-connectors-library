# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Single-dispatch markers and Dex heartbeat fence."""

from .fence import CheckpointError, DispatchNotAllowedError, Fence, InsufficientAttemptTimeError
from .markers import (
    MAXIMUM_MAX_SENDS,
    MINIMUM_MAX_SENDS,
    Decision,
    DecisionKind,
    Marker,
    State,
    decide_dispatch,
    validate_max_sends,
)

__all__ = [
    "MAXIMUM_MAX_SENDS",
    "MINIMUM_MAX_SENDS",
    "CheckpointError",
    "Decision",
    "DecisionKind",
    "DispatchNotAllowedError",
    "Fence",
    "InsufficientAttemptTimeError",
    "Marker",
    "State",
    "decide_dispatch",
    "validate_max_sends",
]
