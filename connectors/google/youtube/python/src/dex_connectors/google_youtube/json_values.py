# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Decode provider JSON with the same finite-number restriction as Go."""

import json
import math
from typing import Any


def _reject_nonfinite(value: str) -> None:
    raise json.JSONDecodeError("invalid JSON number", value, 0)


def _finite_float(value: str) -> float:
    parsed = float(value)
    if not math.isfinite(parsed):
        raise json.JSONDecodeError("invalid JSON number", value, 0)
    return parsed


def strict_json_loads(body: bytes | str) -> Any:
    """Reject NaN and infinities in untrusted provider responses."""
    return json.loads(body, parse_constant=_reject_nonfinite, parse_float=_finite_float)
