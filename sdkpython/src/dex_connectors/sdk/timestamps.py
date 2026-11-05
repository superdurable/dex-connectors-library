# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Go-compatible RFC 3339 timestamp input and lossless wire values."""

import re
from dataclasses import dataclass
from datetime import date, datetime

_TIMESTAMP = re.compile(
    r"^([0-9]{4})-([0-9]{2})-([0-9]{2})T([0-9]{1,2}):([0-9]{2}):([0-9]{2})(?:([.,])([0-9]+))?(Z|[+-][0-9]{2}:[0-9]{2})$"
)


def _go_time_parts(value: str) -> tuple[re.Match[str], str]:
    match = _TIMESTAMP.fullmatch(value)
    if match is None:
        raise ValueError("timestamp must be a Go RFC 3339 time")
    year, month, day, hour, minute, second, _, fraction, offset = match.groups()
    try:
        date(int(year), int(month), int(day))
    except ValueError as error:
        if year != "0000" or not 1 <= int(month) <= 12 or not 1 <= int(day) <= 31:
            raise ValueError("timestamp has an invalid date") from error
        if int(day) > (29 if int(month) == 2 else 30 if int(month) in (4, 6, 9, 11) else 31):
            raise ValueError("timestamp has an invalid date") from error
    if int(hour) > 23 or int(minute) > 59 or int(second) > 59:
        raise ValueError("timestamp has an invalid clock time")
    if offset != "Z" and (int(offset[1:3]) > 24 or int(offset[4:6]) > 60):
        raise ValueError("timestamp has an invalid timezone offset")
    canonical_fraction = (fraction or "").rstrip("0")
    canonical_offset = "Z" if offset in ("+00:00", "-00:00") else offset
    canonical = (
        f"{year}-{month}-{day}T{int(hour):02d}:{minute}:{second}"
        + (f".{canonical_fraction}" if canonical_fraction else "")
        + canonical_offset
    )
    return match, canonical


def parse_go_datetime_input(value: str, field_name: str) -> datetime:
    """Parse an input only when Python datetime can represent every Go time component."""
    try:
        match, canonical = _go_time_parts(value)
        year, _, _, _, _, _, _, fraction, offset = match.groups()
        if year == "0000" or (fraction and any(digit != "0" for digit in fraction[6:])):
            raise ValueError("cannot be represented exactly as a Python datetime")
        if offset != "Z" and (int(offset[1:3]) > 23 or int(offset[4:6]) > 59):
            raise ValueError("cannot be represented exactly as a Python datetime")
        return datetime.fromisoformat(canonical.replace("Z", "+00:00"))
    except ValueError as error:
        raise ValueError(f"{field_name}: {error}") from error


@dataclass(frozen=True, slots=True)
class RFC3339NanoTimestamp:
    """Offset-preserving Go timestamp input with arbitrary fractional precision."""

    wire: str

    @property
    def parsed(self) -> datetime:
        """Return a datetime when the timestamp is exactly representable."""
        return parse_go_datetime_input(self.wire, "timestamp")

    @classmethod
    def from_wire(cls, value: str) -> "RFC3339NanoTimestamp":
        """Validate Go time.Time JSON input without rounding supplied precision."""
        _, canonical = _go_time_parts(value)
        return cls(canonical)

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
