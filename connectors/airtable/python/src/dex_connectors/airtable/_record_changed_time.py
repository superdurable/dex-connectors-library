# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Nanosecond UTC times parsed and formatted as Go's time package does for the recordChanged Trigger.

A time is an int of nanoseconds since the Unix epoch, so cursor tokens and event IDs match Go's output
byte for byte even beyond Python's microsecond datetime precision.
"""

import re
from datetime import UTC, datetime, timedelta
from typing import Final

NANOSECONDS_PER_MILLISECOND: Final = 1_000_000
NANOSECONDS_PER_SECOND: Final = 1_000_000_000
_NANOSECONDS_PER_DAY: Final = 86_400 * NANOSECONDS_PER_SECOND
_UNIX_EPOCH: Final = datetime(1970, 1, 1, tzinfo=UTC)
_RFC3339: Final = re.compile(
    r"([0-9]{4})-([0-9]{2})-([0-9]{2})T([0-9]{2}):([0-9]{2}):([0-9]{2})(?:[.,]([0-9]+))?"
    r"(Z|[+-][0-9]{2}:[0-9]{2})",
    re.ASCII,
)


def parse_rfc3339_nanoseconds(text: str) -> int | None:
    """Parse an RFC 3339 time as Go's time.Parse(time.RFC3339Nano) does; None when it is invalid.

    Fractional digits past the ninth are dropped, as Go drops them.
    """
    match = _RFC3339.fullmatch(text)
    if match is None:
        return None
    year, month, day, hour, minute, second = (int(match.group(index)) for index in range(1, 7))
    if not 1 <= month <= 12 or not 1 <= day <= _days_in_month(year, month):
        return None
    if hour > 23 or minute > 59 or second > 59:
        return None
    fraction = (match.group(7) or "")[:9].ljust(9, "0")
    zone = match.group(8)
    offset_seconds = 0
    if zone != "Z":
        zone_hours, zone_minutes = int(zone[1:3]), int(zone[4:6])
        if zone_hours > 23 or zone_minutes > 59:
            return None
        offset_seconds = (zone_hours * 3600 + zone_minutes * 60) * (-1 if zone[0] == "-" else 1)
    seconds = _days_from_civil(year, month, day) * 86_400 + hour * 3600 + minute * 60 + second
    return (seconds - offset_seconds) * NANOSECONDS_PER_SECOND + int(fraction)


def format_rfc3339_nanoseconds(at: int) -> str:
    """Format a time as Go's time.Time.UTC().Format(time.RFC3339Nano): trailing fraction zeros dropped."""
    date, nanoseconds = _split(at)
    fraction = str(nanoseconds).rjust(9, "0").rstrip("0")
    return date + ("." + fraction if fraction else "") + "Z"


def format_milliseconds(at: int) -> str:
    """Format a time as Go's Format("2006-01-02T15:04:05.000Z"), truncating to milliseconds."""
    date, nanoseconds = _split(at)
    return f"{date}.{nanoseconds // NANOSECONDS_PER_MILLISECOND:03d}Z"


def format_event_id_time(at: int) -> str:
    """Format a time as Go's Format("20060102T150405.000000000Z") with the period removed."""
    date, nanoseconds = _split(at)
    return date.replace("-", "").replace(":", "") + f"{nanoseconds:09d}Z"


def truncate_to_millisecond(at: int) -> int:
    """Round a time down to its millisecond, as Go's Truncate(time.Millisecond)."""
    return at - at % NANOSECONDS_PER_MILLISECOND


def nanoseconds_to_datetime(at: int) -> datetime:
    """Convert to an aware UTC datetime, dropping sub-microsecond digits."""
    return _UNIX_EPOCH + timedelta(microseconds=at // 1000)


def datetime_to_nanoseconds(at: datetime) -> int:
    """Convert an aware datetime to nanoseconds since the Unix epoch."""
    return (at - _UNIX_EPOCH) // timedelta(microseconds=1) * 1000


def _split(at: int) -> tuple[str, int]:
    days, within_day = divmod(at, _NANOSECONDS_PER_DAY)
    year, month, day = _civil_from_days(days)
    seconds, nanoseconds = divmod(within_day, NANOSECONDS_PER_SECOND)
    hour, minute, second = seconds // 3600, seconds // 60 % 60, seconds % 60
    return f"{year:04d}-{month:02d}-{day:02d}T{hour:02d}:{minute:02d}:{second:02d}", nanoseconds


def _days_in_month(year: int, month: int) -> int:
    if month == 2:
        return 29 if year % 4 == 0 and (year % 100 != 0 or year % 400 == 0) else 28
    return 30 if month in (4, 6, 9, 11) else 31


def _days_from_civil(year: int, month: int, day: int) -> int:
    # Howard Hinnant's days_from_civil: days since 1970-01-01 in the proleptic Gregorian calendar.
    year -= 1 if month <= 2 else 0
    era = year // 400
    year_of_era = year - era * 400
    day_of_year = (153 * (month + (-3 if month > 2 else 9)) + 2) // 5 + day - 1
    day_of_era = year_of_era * 365 + year_of_era // 4 - year_of_era // 100 + day_of_year
    return era * 146_097 + day_of_era - 719_468


def _civil_from_days(days: int) -> tuple[int, int, int]:
    days += 719_468
    era = days // 146_097
    day_of_era = days - era * 146_097
    year_of_era = (day_of_era - day_of_era // 1460 + day_of_era // 36_524 - day_of_era // 146_096) // 365
    day_of_year = day_of_era - (365 * year_of_era + year_of_era // 4 - year_of_era // 100)
    month_index = (5 * day_of_year + 2) // 153
    day = day_of_year - (153 * month_index + 2) // 5 + 1
    month = month_index + (3 if month_index < 10 else -9)
    return year_of_era + era * 400 + (1 if month <= 2 else 0), month, day
