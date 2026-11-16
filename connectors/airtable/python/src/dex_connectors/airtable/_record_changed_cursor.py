# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The recordChanged cursor: Go's version 2 token, decoded and encoded byte for byte.

Times are nanoseconds since the Unix epoch and None is Go's zero time.
"""

import json
import re
from dataclasses import dataclass, field
from typing import Any, Final

from dex_connectors.sdk.pollingtrigger import Cursor
from dex_connectors.sdk.projectconfig import encode_go_json_value

from ._record_changed_time import (
    NANOSECONDS_PER_MILLISECOND,
    NANOSECONDS_PER_SECOND,
    format_rfc3339_nanoseconds,
    parse_rfc3339_nanoseconds,
    truncate_to_millisecond,
)

RECORD_CHANGED_CURSOR_VERSION: Final = 2
# How far before the latest delivered change a poll still looks for late-visible changes.
RECORD_CHANGED_OVERLAP: Final = 5 * NANOSECONDS_PER_SECOND
MAXIMUM_RECORD_CHANGED_SEEN: Final = 2000
_RECORD_ID: Final = re.compile(r"rec[0-9A-Za-z]{14}", re.ASCII)
_INT64_DECIMAL: Final = re.compile(r"[+-]?[0-9]+", re.ASCII)
_TOKEN_FIELDS: Final = ("v", "fieldId", "after", "floor", "burst", "seen", "scan")


class RecordChangedCursorError(ValueError):
    """A stored token that must be reseeded."""


@dataclass(frozen=True, slots=True)
class RecordChangedScan:
    """One open listRecords iteration: its filter bound and the next page's offset."""

    since: str
    offset: str


@dataclass(slots=True)
class SeenRecord:
    """One delivered record and its modification time, truncated to Airtable's milliseconds."""

    record_id: str
    at: int


@dataclass(slots=True)
class RecordChangedPosition:
    """The decoded cursor, mutated while one fetch delivers records."""

    field_id: str
    after: int | None = None
    floor: int | None = None
    burst: int | None = None
    seen: list[SeenRecord] = field(default_factory=list)
    scan: RecordChangedScan | None = None

    def lower_bound(self) -> int | None:
        """Return the earliest modification time a poll still delivers; None means every record."""
        if self.after is None:
            return None
        return later(self.after - RECORD_CHANGED_OVERLAP, self.floor)

    def is_delivered(self, record_id: str, modified_at: int) -> bool:
        """Report a record older than the lower bound or already delivered at this time."""
        lower_bound = self.lower_bound()
        if lower_bound is not None and modified_at < lower_bound:
            return True
        at = truncate_to_millisecond(modified_at)
        return any(seen.record_id == record_id and seen.at == at for seen in self.seen)

    def deliver(self, record_id: str, modified_at: int) -> None:
        """Remember one delivered record; records arrive oldest first within a scan."""
        if self.burst is not None and modified_at > self.burst:
            self.close_burst()
        self.after = later(self.after, modified_at)
        self.seen.append(SeenRecord(record_id, truncate_to_millisecond(modified_at)))
        self._bound()

    def close_burst(self) -> None:
        """Move the floor past a burst time once every record at that time was listed."""
        if self.burst is not None:
            self.floor = later(self.floor, self.burst + NANOSECONDS_PER_MILLISECOND)
            self.burst = None
        self._bound()

    def filter_since(self) -> int:
        """Return the listRecords filter bound: the lower bound minus one second of formula rounding."""
        lower_bound = self.lower_bound()
        if lower_bound is None:
            return 0
        return truncate_to_millisecond(lower_bound - NANOSECONDS_PER_SECOND)

    def _bound(self) -> None:
        lower_bound = self.lower_bound()
        if lower_bound is not None:
            self.seen = [seen for seen in self.seen if not seen.at < lower_bound]
        if len(self.seen) <= MAXIMUM_RECORD_CHANGED_SEEN:
            return
        self.seen.sort(key=lambda seen: seen.at)
        dropped = self.seen[len(self.seen) - MAXIMUM_RECORD_CHANGED_SEEN - 1].at
        self.seen = [seen for seen in self.seen if seen.at > dropped]
        if self.after is not None and dropped < truncate_to_millisecond(self.after):
            # The scan, oldest first, already listed every record at the dropped time.
            self.floor = later(self.floor, dropped + NANOSECONDS_PER_MILLISECOND)
            return
        self.burst = later(self.burst, dropped)


def later(left: int | None, right: int | None) -> int | None:
    """Return the later time; None, Go's zero time, is the earliest."""
    if right is not None and (left is None or right > left):
        return right
    return left


def encode_record_changed_cursor(position: RecordChangedPosition) -> Cursor:
    """Encode the token as Go's json.Marshal of recordChangedCursorToken."""
    parts = [
        f'"v":{RECORD_CHANGED_CURSOR_VERSION}',
        '"fieldId":' + _go_string(position.field_id),
        '"after":' + _go_string(_format_cursor_time(position.after)),
    ]
    for name, at in (("floor", position.floor), ("burst", position.burst)):
        if at is not None:
            parts.append(f'"{name}":' + _go_string(_format_cursor_time(at)))
    if position.seen:
        after = truncate_to_millisecond(position.after or 0)
        entries = (
            _go_string(f"{seen.record_id}:{(after - seen.at) // NANOSECONDS_PER_MILLISECOND}")
            for seen in position.seen
        )
        parts.append('"seen":[' + ",".join(entries) + "]")
    if position.scan is not None:
        parts.append(
            '"scan":{"since":'
            + _go_string(position.scan.since)
            + ',"offset":'
            + _go_string(position.scan.offset)
            + "}"
        )
    return Cursor("{" + ",".join(parts) + "}")


def decode_record_changed_cursor(encoded: str) -> RecordChangedPosition:
    """Validate a stored token as Go's decodeRecordChangedCursor does.

    Raises RecordChangedCursorError for every token that must be reseeded.
    """
    try:
        token = _decode_token(encoded)
    except (ValueError, TypeError, RecursionError) as error:
        raise RecordChangedCursorError("Airtable recordChanged cursor is not a version 2 cursor") from error
    if token["v"] != RECORD_CHANGED_CURSOR_VERSION:
        raise RecordChangedCursorError("Airtable recordChanged cursor is not a version 2 cursor")
    seen_entries: list[str] = token["seen"]
    if len(seen_entries) > MAXIMUM_RECORD_CHANGED_SEEN:
        raise RecordChangedCursorError("Airtable recordChanged cursor holds too many record IDs")
    scan: RecordChangedScan | None = token["scan"]
    position = RecordChangedPosition(token["fieldId"], scan=scan)
    position.after = _parse_cursor_time(token["after"])
    position.floor = _parse_cursor_time(token["floor"])
    position.burst = _parse_cursor_time(token["burst"])
    if position.after is None and (seen_entries or position.floor is not None or position.burst is not None):
        raise RecordChangedCursorError("Airtable recordChanged cursor remembers records without a time")
    if scan is not None and (
        not scan.since or not scan.offset or parse_rfc3339_nanoseconds(scan.since) is None
    ):
        raise RecordChangedCursorError("Airtable recordChanged cursor scan is invalid")
    after = truncate_to_millisecond(position.after or 0)
    for entry in seen_entries:
        record_id, separator, delta = entry.partition(":")
        milliseconds = int(delta) if _INT64_DECIMAL.fullmatch(delta) else -1
        if not separator or not 0 <= milliseconds < 1 << 63 or not _RECORD_ID.fullmatch(record_id):
            raise RecordChangedCursorError("Airtable recordChanged cursor holds an invalid record entry")
        position.seen.append(SeenRecord(record_id, after - milliseconds * NANOSECONDS_PER_MILLISECOND))
    return position


class _JSONObject(list[tuple[str, Any]]):
    """A JSON object's key and value pairs in document order."""


class _JSONFloat(float):
    """A JSON number with a fraction or exponent, which Go refuses for an int field."""


def _decode_token(encoded: str) -> dict[str, Any]:
    # Go's json.Unmarshal: keys match fields ignoring case, later keys win, and unknown keys are ignored.
    document = json.loads(
        encoded,
        object_pairs_hook=_JSONObject,
        parse_float=_JSONFloat,
        parse_constant=_refuse_constant,
    )
    token: dict[str, Any] = {
        "v": 0,
        "fieldId": "",
        "after": "",
        "floor": "",
        "burst": "",
        "seen": [],
        "scan": None,
    }
    if document is None:
        return token
    for name, value in _object_pairs(document):
        if name == "v":
            if value is not None:
                if type(value) is not int or not -(1 << 63) <= value < 1 << 63:
                    raise ValueError("v is not an int")
                token["v"] = value
        elif name == "seen":
            token["seen"] = [] if value is None else [_go_string_value(item, "") for item in _array(value)]
        elif name == "scan":
            token["scan"] = None if value is None else _decode_scan(value)
        else:
            token[name] = _go_string_value(value, token[name])
    return token


def _decode_scan(value: Any) -> RecordChangedScan:
    fields = {"since": "", "offset": ""}
    for name, item in _object_pairs(value, ("since", "offset")):
        fields[name] = _go_string_value(item, fields[name])
    return RecordChangedScan(fields["since"], fields["offset"])


def _object_pairs(value: Any, names: tuple[str, ...] = _TOKEN_FIELDS) -> list[tuple[str, Any]]:
    if not isinstance(value, _JSONObject):
        raise ValueError("JSON value is not an object")
    matched: list[tuple[str, Any]] = []
    for key, item in value:
        name = next((name for name in names if name == key), None) or next(
            (name for name in names if name.lower() == key.lower()), None
        )
        if name is not None:
            matched.append((name, item))
    return matched


def _array(value: Any) -> list[Any]:
    if not isinstance(value, list) or isinstance(value, _JSONObject):
        raise ValueError("JSON value is not an array")
    return value


def _go_string_value(value: Any, current: str) -> str:
    if value is None:
        return current
    if not isinstance(value, str):
        raise ValueError("JSON value is not a string")
    return value


def _refuse_constant(name: str) -> Any:
    raise ValueError(f"JSON has no {name}")


def _parse_cursor_time(text: str) -> int | None:
    if text == "":
        return None
    at = parse_rfc3339_nanoseconds(text)
    if at is None:
        raise RecordChangedCursorError("Airtable recordChanged cursor time is invalid")
    return at


def _format_cursor_time(at: int | None) -> str:
    return "" if at is None else format_rfc3339_nanoseconds(at)


def _go_string(text: str) -> str:
    return encode_go_json_value(text).decode()
