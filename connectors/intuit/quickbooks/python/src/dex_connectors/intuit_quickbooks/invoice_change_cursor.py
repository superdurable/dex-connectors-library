# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The invoiceChanged cursor: a MetaData.LastUpdatedTime lookback window, as the Go connector encodes it.

Times are integer nanoseconds since the Unix epoch, so window arithmetic matches Go's time.Time exactly.
The token is Go's JSON document byte for byte, so Go and Python replicas share one binding cursor.
"""

import json
import re
from collections import Counter
from dataclasses import dataclass, field
from datetime import UTC, datetime, timedelta
from typing import Any, Final

from dex_connectors.sdk.pollingtrigger import Cursor
from dex_connectors.sdk.projectconfig import encode_go_json_value

SECOND: Final = 1_000_000_000
INVOICE_CHANGE_LOOKBACK: Final = 30 * SECOND
"""How far before the newest delivered LastUpdatedTime each poll rereads, in nanoseconds."""
MAXIMUM_SEEN_INVOICE_VERSIONS: Final = 1000
"""The most versions the cursor remembers inside the lookback window."""
INVOICE_TIE_PAGE_OVERLAP: Final = 20
"""Previous-page positions reread, since QuickBooks leaves the order of equal timestamps unspecified."""
# Go's zero time.Time, 0001-01-01T00:00:00Z, the "no watermark" position.
ZERO_TIME: Final = -62_135_596_800 * SECOND

_REALM_ID = re.compile(r"[1-9][0-9]{0,24}", re.ASCII)
_GO_INTEGER = re.compile(r"[+-]?[0-9]+", re.ASCII)
# Go's time.Parse(time.RFC3339) falls back to its layout parser, which also takes a one-digit hour and a zone
# offset of up to 24 hours or 60 minutes.
_RFC3339 = re.compile(
    r"([0-9]{4})-([0-9]{2})-([0-9]{2})T([0-9]{1,2}):([0-9]{2}):([0-9]{2})(?:[.,]([0-9]+))?"
    r"(Z|[+-][0-9]{2}:[0-9]{2})",
    re.ASCII,
)
_INT64_MINIMUM, _INT64_MAXIMUM = -(1 << 63), (1 << 63) - 1
_EPOCH: Final = datetime(1970, 1, 1, tzinfo=UTC)
_DAYS_IN_MONTH: Final = (31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31)
_UNPAIRED_SURROGATE = re.compile("[\ud800-\udfff]")
_STRING_FIELDS: Final = {
    "realmid": "realm_id",
    "environment": "environment",
    "since": "since",
    "from": "from_",
}


class InvoiceChangeCursorInvalidError(ValueError):
    """An invoiceChanged cursor token that is not a valid Go cursor document."""


@dataclass(slots=True)
class InvoiceChangeCursorPosition:
    """The committed cursor fields in Go's struct order, as decoded from the token."""

    realm_id: str = ""
    environment: str = ""
    since: str = ""
    from_: str = ""
    seen: list[str] = field(default_factory=list)
    offset: int = 0


@dataclass(slots=True)
class InvoiceWindow:
    """A decoded cursor position; seen maps "<Id>:<SyncToken>" to its LastUpdatedTime second."""

    since: int = ZERO_TIME
    from_: int = ZERO_TIME
    seen: dict[str, int] = field(default_factory=dict)
    offset: int = 0

    def advance(self, invoices: list[tuple[str, int]], is_full_page: bool) -> "InvoiceWindow":
        """Record a page of (version key, second) and return the next window.

        The lower bound never moves back; the page offset continues only while the lower bound stays put.
        """
        following = InvoiceWindow(self.since, self.from_, dict(self.seen))
        for key, second in invoices:
            following.seen[key] = second
            if second > following.since:
                following.since = second
        if following.since == ZERO_TIME:
            return following
        floor = following.since - INVOICE_CHANGE_LOOKBACK
        if floor > following.from_:
            following.from_ = floor
        following.prune_to_bound()
        if is_full_page and following.from_ == self.from_:
            following.offset = max(0, self.offset + len(invoices) - INVOICE_TIE_PAGE_OVERLAP)
        return following

    def prune_to_bound(self) -> None:
        """Drop versions below the lower bound, then whole oldest seconds while too many remain.

        A single second with more versions than the bound moves the window past it.
        """
        seconds: Counter[int] = Counter()
        for key, second in list(self.seen.items()):
            if second < self.from_:
                del self.seen[key]
                continue
            seconds[second] += 1
        ordered = sorted(seconds)
        remaining = len(self.seen)
        while remaining > MAXIMUM_SEEN_INVOICE_VERSIONS:
            remaining -= seconds[ordered[0]]
            if len(ordered) == 1:
                self.since = ordered[0] + SECOND
                self.from_, self.seen = self.since, {}
                return
            self.from_ = ordered[1]
            self.seen = {key: second for key, second in self.seen.items() if second >= self.from_}
            ordered = ordered[1:]


def format_rfc3339_seconds(nanoseconds: int) -> str:
    """Format a time as Go's Format(time.RFC3339) does for a UTC time: whole seconds and Z."""
    days, second_of_day = divmod(nanoseconds // SECOND, 86_400)
    # Hinnant's civil_from_days, so year 0, which a Go cursor may hold, formats as Go formats it.
    era, day_of_era = divmod(days + 719_468, 146_097)
    year_of_era = (day_of_era - day_of_era // 1460 + day_of_era // 36_524 - day_of_era // 146_096) // 365
    day_of_year = day_of_era - (365 * year_of_era + year_of_era // 4 - year_of_era // 100)
    month_index = (5 * day_of_year + 2) // 153
    day = day_of_year - (153 * month_index + 2) // 5 + 1
    month = month_index + 3 if month_index < 10 else month_index - 9
    year = year_of_era + era * 400 + (month <= 2)
    hour, minute, second = second_of_day // 3600, second_of_day // 60 % 60, second_of_day % 60
    return f"{year:04d}-{month:02d}-{day:02d}T{hour:02d}:{minute:02d}:{second:02d}Z"


def unix_nanoseconds(moment: datetime) -> int:
    """Return a timezone-aware datetime as nanoseconds since the Unix epoch."""
    return (moment - _EPOCH) // timedelta(microseconds=1) * 1000


def encode_invoice_change_cursor(realm_id: str, environment: str, window: InvoiceWindow) -> Cursor:
    """Encode the cursor as Go's json.Marshal does: struct field order, omitempty, HTML-safe strings."""
    members = [
        '"realmId":' + encode_go_json_value(realm_id).decode(),
        '"environment":' + encode_go_json_value(environment).decode(),
    ]
    if window.since != ZERO_TIME:
        members.append('"since":' + encode_go_json_value(format_rfc3339_seconds(window.since)).decode())
        members.append('"from":' + encode_go_json_value(format_rfc3339_seconds(window.from_)).decode())
    seen = sorted(f"{second // SECOND}/{key}" for key, second in window.seen.items())
    if seen:
        members.append('"seen":' + encode_go_json_value(seen).decode())
    if window.offset:
        members.append(f'"offset":{window.offset}')
    return Cursor(token="{" + ",".join(members) + "}")


def _is_go_field_key(name: str, field_name: str) -> bool:
    """Match a key to a lower-case field name as Go's encoding/json does, with Unicode simple folding.

    Only the Kelvin sign and the long s fold to ASCII letters; casefold would also expand ligatures.
    """
    return name.lower().replace("\u017f", "s") == field_name


def _go_string(text: str) -> str:
    """Replace each unpaired surrogate with U+FFFD, as Go's decoder does for an escaped lone surrogate."""
    return _UNPAIRED_SURROGATE.sub("\ufffd", text)


def _reject_json_constant(constant: str) -> None:
    raise InvoiceChangeCursorInvalidError("QuickBooks invoice cursor is not decodable")


def _decode_cursor_position(token: str) -> InvoiceChangeCursorPosition:
    """Fill the cursor fields as Go's encoding/json does.

    Keys match case-insensitively, the last duplicate wins, unknown keys and null values are ignored, and
    a value of the wrong JSON type rejects the token.
    """
    invalid = InvoiceChangeCursorInvalidError("QuickBooks invoice cursor is not decodable")
    try:
        members = json.loads(token, object_pairs_hook=list, parse_constant=_reject_json_constant)
    except (ValueError, RecursionError):
        raise invalid from None
    position = InvoiceChangeCursorPosition()
    if members is None:
        return position
    if not isinstance(members, list) or any(not isinstance(member, tuple) for member in members):
        raise invalid
    value: Any
    for name, value in members:
        if value is None:
            continue
        string_field = next((key for key in _STRING_FIELDS if _is_go_field_key(name, key)), None)
        if string_field is not None:
            if not isinstance(value, str):
                raise invalid
            setattr(position, _STRING_FIELDS[string_field], _go_string(value))
        elif _is_go_field_key(name, "seen"):
            if not isinstance(value, list) or any(
                item is not None and not isinstance(item, str) for item in value
            ):
                raise invalid
            position.seen = [_go_string(item) if isinstance(item, str) else "" for item in value]
        elif _is_go_field_key(name, "offset"):
            if type(value) is not int or not _INT64_MINIMUM <= value <= _INT64_MAXIMUM:
                raise invalid
            position.offset = value
    return position


def _parse_rfc3339(text: str) -> int:
    """Parse a time as Go's time.Parse(time.RFC3339) does, to nanoseconds since the Unix epoch."""
    match = _RFC3339.fullmatch(text)
    if match is None:
        raise ValueError("not an RFC 3339 time")
    year, month, day, hour, minute, second = (int(part) for part in match.groups()[:6])
    fraction, offset = match.group(7), match.group(8)
    is_leap = year % 4 == 0 and (year % 100 != 0 or year % 400 == 0)
    if not 1 <= month <= 12 or not 1 <= day <= _DAYS_IN_MONTH[month - 1] + (month == 2 and is_leap):
        raise ValueError("invalid date")
    if hour > 23 or minute > 59 or second > 59:
        raise ValueError("invalid time of day")
    offset_seconds = 0
    if offset != "Z":
        hours, minutes = int(offset[1:3]), int(offset[4:6])
        if hours > 24 or minutes > 60:
            raise ValueError("invalid offset")
        offset_seconds = (-1 if offset[0] == "-" else 1) * (hours * 3600 + minutes * 60)
    seconds = _days_from_civil(year, month, day) * 86_400 + hour * 3600 + minute * 60 + second
    seconds -= offset_seconds
    return seconds * SECOND + int((fraction or "")[:9].ljust(9, "0"))


def _days_from_civil(year: int, month: int, day: int) -> int:
    """Return the proleptic Gregorian day number relative to 1970-01-01, year 0 included, as Go counts."""
    year -= month <= 2
    era = year // 400
    year_of_era = year - era * 400
    day_of_year = (153 * (month + (-3 if month > 2 else 9)) + 2) // 5 + day - 1
    day_of_era = year_of_era * 365 + year_of_era // 4 - year_of_era // 100 + day_of_year
    return era * 146_097 + day_of_era - 719_468


def decode_invoice_change_cursor(token: str) -> tuple[InvoiceChangeCursorPosition, InvoiceWindow]:
    """Decode and validate a cursor token; raise InvoiceChangeCursorInvalidError for any invalid token."""
    position = _decode_cursor_position(token)
    if not _REALM_ID.fullmatch(position.realm_id):
        raise InvoiceChangeCursorInvalidError("QuickBooks invoice cursor is not decodable")
    if position.offset < 0 or len(position.seen) > MAXIMUM_SEEN_INVOICE_VERSIONS:
        raise InvoiceChangeCursorInvalidError("QuickBooks invoice cursor is out of bounds")
    window = InvoiceWindow(offset=position.offset)
    if position.since == "" and position.from_ == "" and not position.seen:
        return position, window
    try:
        since, from_ = _parse_rfc3339(position.since), _parse_rfc3339(position.from_)
    except (ValueError, OverflowError):
        raise InvoiceChangeCursorInvalidError("QuickBooks invoice cursor has an invalid watermark") from None
    if from_ > since:
        raise InvoiceChangeCursorInvalidError("QuickBooks invoice cursor has an invalid watermark")
    window.since, window.from_ = since, from_
    for entry in position.seen:
        unix, separator, key = entry.partition("/")
        seconds = int(unix) if _GO_INTEGER.fullmatch(unix) else None
        if seconds is None or not _INT64_MINIMUM <= seconds <= _INT64_MAXIMUM:
            seconds = None
        second = seconds * SECOND if seconds is not None else 0
        if not separator or seconds is None or ":" not in key or second < from_ or second > since:
            raise InvoiceChangeCursorInvalidError(
                "QuickBooks invoice cursor has an invalid delivered version"
            )
        window.seen[key] = second
    return position, window
