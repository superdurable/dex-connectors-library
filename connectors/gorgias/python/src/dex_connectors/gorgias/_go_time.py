# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Go time.Time values with nanosecond precision and their fixed offset, parsed and formatted as Go does.

The ticketChanged cursor shares its bytes with the Go connector, so its times keep Go's nanosecond
precision and RFC 3339 rendering, which Python datetime cannot hold.
"""

import re
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from typing import Final

_RFC3339: Final = re.compile(
    r"([0-9]{4})-([0-9]{2})-([0-9]{2})T([0-9]{1,2}):([0-9]{2}):([0-9]{2})(?:[.,]([0-9]+))?"
    r"(Z|[+-][0-9]{2}:[0-9]{2})?",
    re.ASCII,
)
_HTTP_DATE_FORMATS: Final = (
    # Go's http.TimeFormat, time.RFC850, and time.ANSIC, the layouts http.ParseTime accepts.
    re.compile(
        r"([A-Za-z]{3}), ([0-9]{2}) ([A-Za-z]{3}) ([0-9]{4}) ([0-9]{1,2}):([0-9]{2}):([0-9]{2}) GMT", re.ASCII
    ),
    re.compile(
        r"([A-Za-z]{6,9}), ([0-9]{2})-([A-Za-z]{3})-([0-9]{2}) ([0-9]{1,2}):([0-9]{2}):([0-9]{2}) [A-Z]{3,5}",
        re.ASCII,
    ),
    re.compile(
        r"([A-Za-z]{3}) ([A-Za-z]{3}) ([ 0-9][0-9]) ([0-9]{1,2}):([0-9]{2}):([0-9]{2}) ([0-9]{4})", re.ASCII
    ),
)
_SHORT_DAY_NAMES: Final = ("sun", "mon", "tue", "wed", "thu", "fri", "sat")
_LONG_DAY_NAMES: Final = ("sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday")
_SHORT_MONTH_NAMES: Final = (
    "jan",
    "feb",
    "mar",
    "apr",
    "may",
    "jun",
    "jul",
    "aug",
    "sep",
    "oct",
    "nov",
    "dec",
)
_NANOSECONDS_PER_SECOND: Final = 1_000_000_000
_SECONDS_PER_DAY: Final = 86_400
_UNIX_EPOCH: Final = datetime(1970, 1, 1, tzinfo=UTC)


@dataclass(frozen=True, slots=True, order=False)
class GoTime:
    """An instant in Unix nanoseconds and the fixed zone offset Go keeps for formatting."""

    unix_nanoseconds: int
    offset_seconds: int = 0

    def add(self, nanoseconds: int) -> "GoTime":
        """Return the instant moved by nanoseconds, keeping the offset as Go's Time.Add does."""
        return GoTime(self.unix_nanoseconds + nanoseconds, self.offset_seconds)

    def in_utc(self) -> "GoTime":
        """Return the same instant rendered in UTC, as Go's Time.UTC does."""
        return GoTime(self.unix_nanoseconds)

    def is_before(self, other: "GoTime") -> bool:
        """Report whether this instant precedes other."""
        return self.unix_nanoseconds < other.unix_nanoseconds

    def is_after(self, other: "GoTime") -> bool:
        """Report whether this instant follows other."""
        return self.unix_nanoseconds > other.unix_nanoseconds

    def format_rfc3339_nano(self) -> str:
        """Render Go's time.RFC3339Nano: trailing fractional zeros dropped, Z for a zero offset.

        Raises ValueError for a year outside 0 to 9999, which Go's JSON encoding refuses as well.
        """
        local_nanoseconds = self.unix_nanoseconds + self.offset_seconds * _NANOSECONDS_PER_SECOND
        seconds, fraction = divmod(local_nanoseconds, _NANOSECONDS_PER_SECOND)
        days, second_of_day = divmod(seconds, _SECONDS_PER_DAY)
        year, month, day = _civil_from_days(days)
        if not 0 <= year <= 9999:
            raise ValueError("Go time year is outside the range [0,9999]")
        hour, remainder = divmod(second_of_day, 3600)
        minute, second = divmod(remainder, 60)
        text = f"{year:04d}-{month:02d}-{day:02d}T{hour:02d}:{minute:02d}:{second:02d}"
        if fraction:
            text += "." + f"{fraction:09d}".rstrip("0")
        if self.offset_seconds == 0:
            return text + "Z"
        sign = "-" if self.offset_seconds < 0 else "+"
        offset_minutes = abs(self.offset_seconds) // 60
        return text + f"{sign}{offset_minutes // 60:02d}:{offset_minutes % 60:02d}"

    @property
    def unix_microseconds(self) -> int:
        """Return the instant in Unix microseconds, floored as Go's Time.UnixMicro does."""
        return self.unix_nanoseconds // 1000

    def format_query_microseconds(self) -> str:
        """Render UTC with exactly six fractional digits, Go's "2006-01-02T15:04:05.000000Z07:00" layout."""
        whole = GoTime(self.unix_nanoseconds - self.unix_nanoseconds % _NANOSECONDS_PER_SECOND)
        microseconds = (self.unix_nanoseconds % _NANOSECONDS_PER_SECOND) // 1000
        return whole.format_rfc3339_nano()[:-1] + f".{microseconds:06d}Z"

    @staticmethod
    def from_datetime(value: datetime) -> "GoTime":
        """Return the instant of an aware datetime in UTC."""
        delta = value - _UNIX_EPOCH
        return GoTime(
            (delta.days * _SECONDS_PER_DAY + delta.seconds) * _NANOSECONDS_PER_SECOND
            + delta.microseconds * 1000
        )

    def to_datetime(self) -> datetime:
        """Return the instant as an aware UTC datetime, truncated to microseconds."""
        return _UNIX_EPOCH + timedelta(microseconds=self.unix_nanoseconds // 1000)


UNIX_EPOCH: Final = GoTime(0)


def parse_go_time(value: str) -> GoTime:
    """Parse value as Go's time.Parse(time.RFC3339Nano, value) does.

    Fractions of any length after a period or comma are truncated to nanoseconds; an offset keeps its
    zone for formatting. Raises ValueError for any value Go refuses.
    """
    match = _RFC3339.fullmatch(value)
    if match is None or match.group(8) is None:
        raise ValueError("time is not RFC 3339")
    return _parse_rfc3339_match(match)


def parse_gorgias_time(value: str) -> GoTime:
    """Parse a Gorgias ISO 8601 timestamp in UTC as the Go connector's parseGorgiasTimestamp does.

    It accepts RFC 3339 with an offset and Gorgias's documented offset-less UTC form, with or without a
    fraction. Raises ValueError for any value Go refuses.
    """
    match = _RFC3339.fullmatch(value)
    if match is None:
        raise ValueError("timestamp is not ISO 8601")
    return _parse_rfc3339_match(match).in_utc()


def parse_http_date(value: str) -> GoTime | None:
    """Parse an HTTP Date header as Go's http.ParseTime does, in UTC; None when no layout matches."""
    for index, layout in enumerate(_HTTP_DATE_FORMATS):
        match = layout.fullmatch(value)
        if match is None:
            continue
        groups = match.groups()
        if index == 0:
            weekday, day, month, year, hour, minute, second = groups
            is_weekday_known = weekday.lower() in _SHORT_DAY_NAMES
        elif index == 1:
            weekday, day, month, year, hour, minute, second = groups
            is_weekday_known = weekday.lower() in _LONG_DAY_NAMES
            year = str((1900 if int(year) >= 69 else 2000) + int(year))
        else:
            weekday, month, day, hour, minute, second, year = groups
            is_weekday_known = weekday.lower() in _SHORT_DAY_NAMES
        if not is_weekday_known or month.lower() not in _SHORT_MONTH_NAMES:
            continue
        month_number = _SHORT_MONTH_NAMES.index(month.lower()) + 1
        try:
            return _civil_time(
                int(year), month_number, int(day.strip()), int(hour), int(minute), int(second), 0, 0
            )
        except ValueError:
            continue
    return None


def _parse_rfc3339_match(match: re.Match[str]) -> GoTime:
    year, month, day, hour, minute, second = (int(part) for part in match.groups()[:6])
    fraction, offset = match.group(7) or "", match.group(8) or "Z"
    offset_seconds = 0
    if offset != "Z":
        offset_hours, offset_minutes = int(offset[1:3]), int(offset[4:6])
        if offset_hours > 24 or offset_minutes > 60:
            raise ValueError("time has an invalid zone offset")
        offset_seconds = (offset_hours * 3600 + offset_minutes * 60) * (-1 if offset[0] == "-" else 1)
    nanoseconds = int((fraction + "000000000")[:9])
    return _civil_time(year, month, day, hour, minute, second, nanoseconds, offset_seconds)


def _civil_time(
    year: int,
    month: int,
    day: int,
    hour: int,
    minute: int,
    second: int,
    nanoseconds: int,
    offset_seconds: int,
) -> GoTime:
    if not 1 <= month <= 12 or not 1 <= day <= _days_in_month(year, month):
        raise ValueError("time has an invalid date")
    if hour > 23 or minute > 59 or second > 59:
        raise ValueError("time has an invalid clock time")
    local_seconds = _days_from_civil(year, month, day) * _SECONDS_PER_DAY + hour * 3600 + minute * 60 + second
    unix_nanoseconds = (local_seconds - offset_seconds) * _NANOSECONDS_PER_SECOND + nanoseconds
    return GoTime(unix_nanoseconds, offset_seconds)


def _is_leap_year(year: int) -> bool:
    return year % 4 == 0 and (year % 100 != 0 or year % 400 == 0)


def _days_in_month(year: int, month: int) -> int:
    if month == 2:
        return 29 if _is_leap_year(year) else 28
    return 30 if month in (4, 6, 9, 11) else 31


def _days_from_civil(year: int, month: int, day: int) -> int:
    # Howard Hinnant's proleptic Gregorian day count, valid for year 0 as Go's calendar is.
    year -= month <= 2
    era = year // 400
    year_of_era = year - era * 400
    day_of_year = (153 * (month + (-3 if month > 2 else 9)) + 2) // 5 + day - 1
    day_of_era = year_of_era * 365 + year_of_era // 4 - year_of_era // 100 + day_of_year
    return era * 146097 + day_of_era - 719468


def _civil_from_days(days: int) -> tuple[int, int, int]:
    days += 719468
    era = days // 146097
    day_of_era = days - era * 146097
    year_of_era = (day_of_era - day_of_era // 1460 + day_of_era // 36524 - day_of_era // 146096) // 365
    day_of_year = day_of_era - (365 * year_of_era + year_of_era // 4 - year_of_era // 100)
    month_index = (5 * day_of_year + 2) // 153
    day = day_of_year - (153 * month_index + 2) // 5 + 1
    month = month_index + (3 if month_index < 10 else -9)
    return year_of_era + era * 400 + (month <= 2), month, day
