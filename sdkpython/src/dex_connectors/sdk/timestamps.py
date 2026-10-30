# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""RFC 3339 nanosecond timestamps shared by connector wire values."""

import re
from dataclasses import dataclass
from datetime import datetime

_TIMESTAMP = re.compile(r"^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(?:\.(\d{1,9}))?(Z|[+-]\d{2}:\d{2})$")


@dataclass(frozen=True, slots=True)
class RFC3339NanoTimestamp:
    """Offset-preserving timestamp with up to nine fractional digits."""

    wire: str
    parsed: datetime

    @classmethod
    def from_wire(cls, value: str) -> "RFC3339NanoTimestamp":
        """Validate and preserve a Go RFC3339Nano wire timestamp."""
        match = _TIMESTAMP.fullmatch(value)
        if match is None:
            raise ValueError("timestamp must be RFC 3339 with at most nanosecond precision")
        seconds, fraction, offset = match.groups()
        parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
        fractional = (fraction or "").rstrip("0")
        return cls(seconds + (f".{fractional}" if fractional else "") + offset, parsed)

    @classmethod
    def from_datetime(cls, value: datetime) -> "RFC3339NanoTimestamp":
        """Format an aware Python datetime without changing its offset."""
        if value.tzinfo is None or value.utcoffset() is None:
            raise ValueError("timestamp requires a timezone offset")
        return cls.from_wire(value.isoformat(timespec="microseconds").replace("+00:00", "Z"))

    def __str__(self) -> str:
        """Return the canonical wire representation."""
        return self.wire


def format_rfc3339_nano(value: RFC3339NanoTimestamp | datetime) -> str:
    """Format a preserved or locally constructed provider timestamp."""
    return str(
        value if isinstance(value, RFC3339NanoTimestamp) else RFC3339NanoTimestamp.from_datetime(value)
    )
