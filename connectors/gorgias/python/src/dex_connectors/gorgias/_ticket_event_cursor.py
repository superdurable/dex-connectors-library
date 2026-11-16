# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The ticketChanged cursor document, decoded and encoded byte for byte as the Go connector does."""

import json
import re
from dataclasses import dataclass, field
from typing import Any, Final

from dex_connectors.sdk.projectconfig import encode_go_json_value

from ._go_time import GoTime, parse_go_time

TICKET_EVENT_OVERLAP_NANOSECONDS: Final = 15 * 1_000_000_000
"""How far before the committed high-water mark each poll starts reading again."""
MAXIMUM_SEEN_EVENT_KEYS: Final = 1500
"""Bounds the cursor's dedupe set under the kit's 64 KiB cursor token limit."""

_INT64_MINIMUM, _INT64_MAXIMUM = -(1 << 63), (1 << 63) - 1
_DECIMAL_INTEGER: Final = re.compile(r"[+-]?[0-9]+", re.ASCII)


class TicketEventCursorDecodeError(ValueError):
    """The token is not a cursor Go's json.Unmarshal and time.Parse accept."""


@dataclass(slots=True)
class TicketEventCursor:
    """The committed position: the newest created_datetime read, a scan start, and recent event keys."""

    since: str = ""
    scan_from: str = ""
    seen: list[str] = field(default_factory=list)


@dataclass(frozen=True, slots=True)
class DecodedTicketEventCursor:
    """A decoded cursor with its high-water mark and the start of the next request, both in UTC."""

    position: TicketEventCursor
    since: GoTime
    start: GoTime


def decode_ticket_event_cursor(token: str) -> DecodedTicketEventCursor:
    """Decode token as Go's json.Unmarshal into the cursor struct, then parse its times.

    Keys match case-insensitively, later keys win, unknown keys are ignored, and null leaves the zero
    value. Raises TicketEventCursorDecodeError where Go reports errTicketEventCursorInvalid.
    """
    try:
        document = json.loads(token, object_pairs_hook=JSONObject, parse_constant=refuse_json_constant)
    except (ValueError, RecursionError) as error:
        raise TicketEventCursorDecodeError("cursor is not JSON") from error
    position = TicketEventCursor()
    if document is not None:
        if not isinstance(document, JSONObject):
            raise TicketEventCursorDecodeError("cursor is not an object")
        for name, value in fold_json_members(document, ("since", "scanFrom", "seen")):
            if value is None:
                if name == "seen":
                    position.seen = []
                continue
            if name == "seen":
                if not isinstance(value, list):
                    raise TicketEventCursorDecodeError("cursor seen is not an array")
                position.seen = ["" if key is None else _decode_string(key) for key in value]
            elif name == "since":
                position.since = _decode_string(value)
            else:
                position.scan_from = _decode_string(value)
    try:
        since = parse_go_time(position.since)
        start = since.add(-TICKET_EVENT_OVERLAP_NANOSECONDS)
        if position.scan_from:
            scan_from = parse_go_time(position.scan_from)
            if scan_from.is_after(start):
                start = scan_from
    except ValueError as error:
        raise TicketEventCursorDecodeError("cursor time is not RFC 3339") from error
    return DecodedTicketEventCursor(position, since.in_utc(), start.in_utc())


def encode_ticket_event_cursor(position: TicketEventCursor) -> str:
    """Encode position as Go's json.Marshal of the cursor struct, in field order with omitempty fields."""
    parts = ['{"since":', _encode_string(position.since)]
    if position.scan_from:
        parts += [',"scanFrom":', _encode_string(position.scan_from)]
    if position.seen:
        parts += [',"seen":[', ",".join(map(_encode_string, position.seen)), "]"]
    return "".join(parts) + "}"


def seen_event_key(event_id: int, created_at: GoTime) -> str:
    """Name one event version as "<event id>@<created_datetime Unix microseconds>"."""
    return f"{event_id}@{created_at.unix_microseconds}"


def prune_seen_event_keys(keys: list[str], window_start: GoTime) -> list[str]:
    """Keep distinct keys recorded at or after window_start: the newest MAXIMUM_SEEN_EVENT_KEYS, oldest
    first."""
    kept: list[tuple[str, int]] = []
    is_kept: set[str] = set()
    window_start_microseconds = window_start.unix_microseconds
    for key in keys:
        _, separator, created_text = key.partition("@")
        if not separator or key in is_kept:
            continue
        created_at = _parse_go_int64(created_text)
        if created_at is None or created_at < window_start_microseconds:
            continue
        is_kept.add(key)
        kept.append((key, created_at))
    kept.sort(key=lambda entry: entry[1])
    return [key for key, _ in kept[max(len(kept) - MAXIMUM_SEEN_EVENT_KEYS, 0) :]]


class JSONObject(list[tuple[str, Any]]):
    """One JSON object's members in document order, duplicates kept."""


def refuse_json_constant(name: str) -> None:
    """Reject NaN and Infinity, which Go's JSON decoder refuses."""
    raise ValueError(f"JSON does not allow {name}")


def fold_json_members(document: JSONObject, names: tuple[str, ...]) -> list[tuple[str, Any]]:
    """Return the members Go's json.Unmarshal assigns to the named fields, in document order."""
    # Go prefers an exact field name and otherwise matches case-insensitively.
    folded = {name.lower(): name for name in names}
    members = []
    for key, value in document:
        name = key if key in names else folded.get(key.lower())
        if name is not None:
            members.append((name, value))
    return members


def _parse_go_int64(text: str) -> int | None:
    if not _DECIMAL_INTEGER.fullmatch(text):
        return None
    value = int(text)
    return value if _INT64_MINIMUM <= value <= _INT64_MAXIMUM else None


def _decode_string(value: object) -> str:
    if not isinstance(value, str):
        raise TicketEventCursorDecodeError("cursor string field has another type")
    return value


def _encode_string(value: str) -> str:
    return encode_go_json_value(value).decode()
