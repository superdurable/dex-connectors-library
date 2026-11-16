# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Go time.Time values at nanosecond precision, parsed and formatted as Go's encoding/json does."""

import re
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta, timezone
from typing import Final

_TIMESTAMP: Final = re.compile(
    r"([0-9]{4})-([0-9]{2})-([0-9]{2})T([0-9]{2}):([0-9]{2}):([0-9]{2})"
    r"(?:\.([0-9]{1,9}))?(Z|([+-])([0-9]{2}):([0-9]{2}))\Z",
    re.ASCII,
)
_NANOSECONDS_PER_SECOND: Final = 1_000_000_000
_SECONDS_PER_DAY: Final = 86_400
_UNIX_EPOCH: Final = datetime(1970, 1, 1, tzinfo=UTC)
# Unix nanoseconds of Go's zero time, 0001-01-01T00:00:00Z.
_GO_ZERO_UNIX_NANOSECONDS: Final = -62_135_596_800 * _NANOSECONDS_PER_SECOND


def _days_from_civil(year: int, month: int, day: int) -> int:
    """Days since 1970-01-01 of a proleptic Gregorian date, including year 0."""
    year -= month <= 2
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
    return year_of_era + era * 400 + (month <= 2), month, day


def _is_leap_year(year: int) -> bool:
    return year % 4 == 0 and (year % 100 != 0 or year % 400 == 0)


@dataclass(frozen=True, slots=True, order=False)
class GoInstant:
    """An instant with Go's nanosecond precision and the UTC offset it was written with."""

    unix_nanoseconds: int = _GO_ZERO_UNIX_NANOSECONDS
    offset_minutes: int = 0

    def is_zero(self) -> bool:
        """Report Go's IsZero: the instant is 0001-01-01T00:00:00Z in any offset."""
        return self.unix_nanoseconds == _GO_ZERO_UNIX_NANOSECONDS

    def is_before(self, other: "GoInstant") -> bool:
        """Report Go's Before, which compares instants and ignores offsets."""
        return self.unix_nanoseconds < other.unix_nanoseconds

    def utc(self) -> "GoInstant":
        """The same instant written in UTC, as Go's UTC method returns it."""
        return GoInstant(self.unix_nanoseconds, 0)

    def add_nanoseconds(self, nanoseconds: int) -> "GoInstant":
        """The instant nanoseconds later, keeping the offset, as Go's Add does."""
        return GoInstant(self.unix_nanoseconds + nanoseconds, self.offset_minutes)

    def format(self) -> str:
        """Format as Go's time.Time MarshalJSON does (RFC 3339 with trailing fraction zeros trimmed)."""
        local_seconds, nanoseconds = divmod(
            self.unix_nanoseconds + self.offset_minutes * 60 * _NANOSECONDS_PER_SECOND,
            _NANOSECONDS_PER_SECOND,
        )
        days, second_of_day = divmod(local_seconds, _SECONDS_PER_DAY)
        year, month, day = _civil_from_days(days)
        text = (
            f"{year:04d}-{month:02d}-{day:02d}T{second_of_day // 3600:02d}:"
            f"{second_of_day // 60 % 60:02d}:{second_of_day % 60:02d}"
        )
        fraction = f"{nanoseconds:09d}".rstrip("0")
        if fraction:
            text += "." + fraction
        if self.offset_minutes == 0:
            return text + "Z"
        sign = "+" if self.offset_minutes > 0 else "-"
        return f"{text}{sign}{abs(self.offset_minutes) // 60:02d}:{abs(self.offset_minutes) % 60:02d}"

    def format_cql_minute(self) -> str:
        """Format the UTC instant as a CQL date, yyyy-MM-dd HH:mm, as Go's "2006-01-02 15:04" layout."""
        return self.utc().format()[:16].replace("T", " ")

    def to_datetime(self) -> datetime:
        """Return an aware datetime with the same offset, truncated to microseconds.

        Raises ValueError for an instant outside datetime's years 1 to 9999.
        """
        microseconds = self.unix_nanoseconds // 1000
        try:
            moment = _UNIX_EPOCH + timedelta(microseconds=microseconds)
            return moment.astimezone(timezone(timedelta(minutes=self.offset_minutes)))
        except OverflowError:
            raise ValueError("time is outside the datetime range") from None

    @classmethod
    def from_datetime(cls, moment: datetime) -> "GoInstant":
        """Return the instant of an aware datetime, in UTC."""
        if moment.tzinfo is None:
            raise ValueError("time requires a timezone")
        elapsed = moment - _UNIX_EPOCH
        return cls((elapsed // timedelta(microseconds=1)) * 1000, 0)


def parse_go_time(text: str) -> GoInstant:
    """Parse an RFC 3339 timestamp as Go's time.Time UnmarshalJSON does; raises ValueError otherwise."""
    match = _TIMESTAMP.match(text)
    if match is None:
        raise ValueError("time is not an RFC 3339 timestamp")
    year, month, day, hour, minute, second = (int(match.group(index)) for index in range(1, 7))
    fraction, zone, sign, zone_hour, zone_minute = match.group(7, 8, 9, 10, 11)
    month_days = (31, 29 if _is_leap_year(year) else 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31)
    if (
        not 1 <= month <= 12
        or not 1 <= day <= month_days[month - 1]
        or hour > 23
        or minute > 59
        or second > 59
    ):
        raise ValueError("time is out of range")
    offset_minutes = 0
    if zone != "Z":
        if int(zone_hour) > 23 or int(zone_minute) > 59:
            raise ValueError("time zone offset is out of range")
        offset_minutes = (int(zone_hour) * 60 + int(zone_minute)) * (1 if sign == "+" else -1)
    seconds = _days_from_civil(year, month, day) * _SECONDS_PER_DAY + hour * 3600 + minute * 60 + second
    nanoseconds = int((fraction or "").ljust(9, "0"))
    return GoInstant((seconds - offset_minutes * 60) * _NANOSECONDS_PER_SECOND + nanoseconds, offset_minutes)


def parse_go_time_layout(text: str) -> GoInstant:
    """Parse with Go's time.Parse and the RFC3339Nano layout, which also accepts a one-digit hour."""
    if len(text) > 11 and text[10] == "T" and text[12:13] == ":":
        text = text[:11] + "0" + text[11:]
    return parse_go_time(text)
