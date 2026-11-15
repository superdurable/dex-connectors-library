# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The databaseRowChanged cursor document, decoded and encoded byte for byte as the Go connector does."""

import base64
import hashlib
import json
from dataclasses import dataclass, field
from typing import Any, Final

from dex_connectors.sdk.projectconfig import encode_go_json_value

from ._go_time import GoTime, parse_go_time

DATABASE_ROW_CURSOR_VERSION: Final = 1
DATABASE_ROW_LOOKBACK_NANOSECONDS: Final = 2 * 60 * 1_000_000_000
MAXIMUM_SEEN_DATABASE_ROWS: Final = 2000
# Keeps the cursor under the kit's 64 KiB token limit with one millisecond timestamp per row.
_MAXIMUM_SEEN_ROW_BYTES: Final = 48 << 10
_INT64_MINIMUM, _INT64_MAXIMUM = -(1 << 63), (1 << 63) - 1
# Go's zero time.Time, which a JSON null leaves in place.
GO_ZERO_TIME: Final = parse_go_time("0001-01-01T00:00:00Z")


class DatabaseRowCursorDecodeError(ValueError):
    """The token is not a cursor Go's json.Unmarshal would accept."""


@dataclass(slots=True)
class SeenRowGroup:
    """The rows delivered or baselined at one last edited time, as short row-ID digests."""

    last_edited_time: GoTime
    row_keys: list[str] | None


@dataclass(slots=True)
class DatabaseRowCursor:
    """The committed position: newest last edited time, recent row versions, and Notion's start cursor."""

    version: int
    data_source_id: str
    high_water: GoTime
    query_from: GoTime | None = None
    start_cursor: str = ""
    seen: list[SeenRowGroup] = field(default_factory=list)


def decode_database_row_cursor(token: str) -> DatabaseRowCursor:
    """Decode token as Go's json.Unmarshal into the cursor struct: keys match case-insensitively, later
    keys win, unknown keys are ignored, and null leaves the zero value.

    Raises DatabaseRowCursorDecodeError where Go reports an error.
    """
    try:
        document = json.loads(token, object_pairs_hook=_JSONObject, parse_constant=_refuse_constant)
    except (ValueError, RecursionError) as error:
        raise DatabaseRowCursorDecodeError("cursor is not JSON") from error
    cursor = DatabaseRowCursor(0, "", GO_ZERO_TIME)
    if document is None:
        return cursor
    if not isinstance(document, _JSONObject):
        raise DatabaseRowCursorDecodeError("cursor is not an object")
    for name, value in _fold_members(
        document, ("v", "dataSourceId", "highWater", "queryFrom", "startCursor", "seen")
    ):
        if value is None:
            if name == "queryFrom":
                cursor.query_from = None
            elif name == "seen":
                cursor.seen = []
            continue
        if name == "v":
            if (
                isinstance(value, bool)
                or not isinstance(value, int)
                or not _INT64_MINIMUM <= value <= _INT64_MAXIMUM
            ):
                raise DatabaseRowCursorDecodeError("cursor version is not an integer")
            cursor.version = value
        elif name == "dataSourceId":
            cursor.data_source_id = _decode_string(value)
        elif name == "highWater":
            cursor.high_water = _decode_time(value)
        elif name == "queryFrom":
            cursor.query_from = _decode_time(value)
        elif name == "startCursor":
            cursor.start_cursor = _decode_string(value)
        else:
            cursor.seen = _decode_seen(value)
    return cursor


def encode_database_row_cursor(cursor: DatabaseRowCursor) -> str:
    """Encode cursor as Go's json.Marshal of the cursor struct, in field order with omitempty fields.

    Raises ValueError for a time Go cannot encode.
    """
    parts = [
        '{"v":',
        str(cursor.version),
        ',"dataSourceId":',
        _encode_string(cursor.data_source_id),
        ',"highWater":"',
        cursor.high_water.format_rfc3339_nano(),
        '"',
    ]
    if cursor.query_from is not None:
        parts += [',"queryFrom":"', cursor.query_from.format_rfc3339_nano(), '"']
    if cursor.start_cursor:
        parts += [',"startCursor":', _encode_string(cursor.start_cursor)]
    if cursor.seen:
        groups = []
        for group in cursor.seen:
            rows = (
                "null"
                if group.row_keys is None
                else "[" + ",".join(map(_encode_string, group.row_keys)) + "]"
            )
            groups.append('{"at":"' + group.last_edited_time.format_rfc3339_nano() + '","rows":' + rows + "}")
        parts += [',"seen":[', ",".join(groups), "]"]
    return "".join(parts) + "}"


class SeenRows:
    """Row-ID digests indexed by last edited time in Unix nanoseconds."""

    def __init__(self, groups: list[SeenRowGroup]) -> None:
        """Index the committed groups."""
        self._keys_by_instant: dict[int, set[str]] = {}
        for group in groups:
            for key in group.row_keys or ():
                self._add_key(group.last_edited_time, key)

    def contains(self, last_edited_time: GoTime, row_id: str) -> bool:
        """Report whether this row version was already delivered or baselined."""
        return seen_row_key(row_id) in self._keys_by_instant.get(last_edited_time.unix_nanoseconds, ())

    def add(self, last_edited_time: GoTime, row_id: str) -> None:
        """Remember one row version."""
        self._add_key(last_edited_time, seen_row_key(row_id))

    def groups_within(self, high_water: GoTime) -> list[SeenRowGroup]:
        """Keep the newest row versions edited inside the lookback, bounded in count and encoded bytes."""
        lower_bound = high_water.unix_nanoseconds - DATABASE_ROW_LOOKBACK_NANOSECONDS
        instants = sorted(
            (instant for instant in self._keys_by_instant if instant >= lower_bound), reverse=True
        )
        groups: list[SeenRowGroup] = []
        remaining, remaining_bytes = MAXIMUM_SEEN_DATABASE_ROWS, _MAXIMUM_SEEN_ROW_BYTES
        key_bytes = len('"",') + len(seen_row_key(""))
        for instant in instants:
            last_edited_time = GoTime(instant)
            group_bytes = len('{"at":"","rows":[]},') + len(last_edited_time.format_rfc3339_nano())
            fitting = min(remaining, int((remaining_bytes - group_bytes) / key_bytes))
            if fitting <= 0:
                break
            keys = sorted(self._keys_by_instant[instant])[:fitting]
            remaining -= len(keys)
            remaining_bytes -= group_bytes + len(keys) * key_bytes
            groups.append(SeenRowGroup(last_edited_time, keys))
        groups.reverse()
        return groups

    def _add_key(self, last_edited_time: GoTime, key: str) -> None:
        self._keys_by_instant.setdefault(last_edited_time.unix_nanoseconds, set()).add(key)


def seen_row_key(row_id: str) -> str:
    """Return the 96-bit row-ID digest: unique enough within one data source's lookback, and short."""
    return base64.urlsafe_b64encode(hashlib.sha256(row_id.encode()).digest()[:12]).decode().rstrip("=")


class _JSONObject(list[tuple[str, Any]]):
    """One JSON object's members in document order, duplicates kept."""


def _refuse_constant(name: str) -> None:
    raise ValueError(f"JSON does not allow {name}")


def _fold_members(document: _JSONObject, names: tuple[str, ...]) -> list[tuple[str, Any]]:
    # Go prefers an exact field name and otherwise matches case-insensitively.
    folded = {name.lower(): name for name in names}
    members = []
    for key, value in document:
        name = key if key in names else folded.get(key.lower())
        if name is not None:
            members.append((name, value))
    return members


def _decode_string(value: object) -> str:
    if not isinstance(value, str):
        raise DatabaseRowCursorDecodeError("cursor string field has another type")
    return value


def _decode_time(value: object) -> GoTime:
    try:
        return parse_go_time(_decode_string(value))
    except ValueError as error:
        raise DatabaseRowCursorDecodeError("cursor time is not RFC 3339") from error


def _decode_seen(value: object) -> list[SeenRowGroup]:
    if not isinstance(value, list):
        raise DatabaseRowCursorDecodeError("cursor seen is not an array")
    groups = []
    for item in value:
        group = SeenRowGroup(GO_ZERO_TIME, None)
        if item is not None:
            if not isinstance(item, _JSONObject):
                raise DatabaseRowCursorDecodeError("cursor seen group is not an object")
            for name, member in _fold_members(item, ("at", "rows")):
                if member is None:
                    if name == "rows":
                        group.row_keys = None
                    continue
                if name == "at":
                    group.last_edited_time = _decode_time(member)
                    continue
                if not isinstance(member, list):
                    raise DatabaseRowCursorDecodeError("cursor seen rows is not an array")
                group.row_keys = ["" if key is None else _decode_string(key) for key in member]
        groups.append(group)
    return groups


def _encode_string(value: str) -> str:
    return encode_go_json_value(value).decode()
