# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The cardChanged cursor token and action times, encoded and decoded as the Go connector does."""

import calendar
import json
import re
from dataclasses import dataclass, field
from datetime import UTC, datetime, timedelta
from typing import Any, Final

from dex_connectors.sdk.pollingtrigger import Cursor

ACTION_LOOKBACK_NANOSECONDS: Final = 60 * 1_000_000_000
"""Re-read window, because actions recorded in the same moment can surface late."""
MAXIMUM_SEEN_ACTION_IDS: Final = 2000
"""Keeps the cursor token far below the poll kit's 64 KiB bound."""

_NANOSECONDS_PER_SECOND: Final = 1_000_000_000
_UNIX_EPOCH: Final = datetime(1970, 1, 1, tzinfo=UTC)
_TRELLO_ID: Final = re.compile(r"[0-9a-fA-F]{24}\Z", re.ASCII)
# Go's time.RFC3339Nano parse: a dot or comma fraction with at least one digit, and Z or an offset.
_RFC3339: Final = re.compile(
    r"(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:[.,](\d+))?(?:(Z)|([+-])(\d{2}):(\d{2}))\Z", re.ASCII
)
_GO_HTML_ESCAPES: Final = {
    "<": "\\u003c",
    ">": "\\u003e",
    "&": "\\u0026",
    "\u2028": "\\u2028",
    "\u2029": "\\u2029",
}
_FIELDS: Final = {
    "boardid": "board_id",
    "watermark": "watermark",
    "seenactionids": "seen_action_ids",
    "flooractionid": "floor_action_id",
}


@dataclass(frozen=True, slots=True)
class GoTime:
    """An instant in Unix nanoseconds and the UTC offset in seconds it was written with."""

    unix_nanoseconds: int
    offset_seconds: int = 0


@dataclass(slots=True)
class CardActionCursor:
    """The committed position, field for field as the Go connector's cardActionCursor.

    watermark is the newest delivered action time, blank when the board had no card action at the
    baseline. seen_action_ids are the sorted lowercase IDs delivered within the lookback of watermark;
    every action ID at or below floor_action_id counts as delivered.
    """

    board_id: str = ""
    watermark: str = ""
    seen_action_ids: list[str] = field(default_factory=list)
    floor_action_id: str = ""


def parse_go_time(value: str) -> GoTime | None:
    """Parse an RFC 3339 time as Go's time.Parse(time.RFC3339Nano) does; None when Go rejects it."""
    matched = _RFC3339.match(value)
    if matched is None:
        return None
    year, month, day, hour, minute, second = (int(matched.group(index)) for index in range(1, 7))
    if not 1 <= month <= 12 or hour > 23 or minute > 59 or second > 59:
        return None
    if year < 1 or not 1 <= day <= calendar.monthrange(year, month)[1]:
        return None
    fraction = matched.group(7) or ""
    nanoseconds = int((fraction + "000000000")[:9]) if fraction else 0
    offset = 0
    if matched.group(9):
        offset_hours, offset_minutes = int(matched.group(10)), int(matched.group(11))
        if offset_hours > 24 or offset_minutes > 59:
            return None
        offset = (offset_hours * 3600 + offset_minutes * 60) * (-1 if matched.group(9) == "-" else 1)
    seconds = calendar.timegm((year, month, day, hour, minute, second, 0, 0, 0)) - offset
    return GoTime(seconds * _NANOSECONDS_PER_SECOND + nanoseconds, offset)


def format_trello_time(moment: GoTime) -> str:
    """Format as Go's 2006-01-02T15:04:05.000Z07:00 layout, keeping the moment's offset."""
    local = moment.unix_nanoseconds + moment.offset_seconds * _NANOSECONDS_PER_SECOND
    seconds, nanoseconds = divmod(local, _NANOSECONDS_PER_SECOND)
    text = (_UNIX_EPOCH + timedelta(seconds=seconds)).strftime(
        "%Y-%m-%dT%H:%M:%S"
    ) + f".{nanoseconds // 1_000_000:03d}"
    if moment.offset_seconds == 0:
        return text + "Z"
    sign = "-" if moment.offset_seconds < 0 else "+"
    hours, minutes = divmod(abs(moment.offset_seconds) // 60, 60)
    return f"{text}{sign}{hours:02d}:{minutes:02d}"


def encode_card_action_cursor(position: CardActionCursor) -> Cursor:
    """Encode position as Go's json.Marshal does, with the same field order and omitted empty fields."""
    document: dict[str, Any] = {"boardId": position.board_id}
    if position.watermark:
        document["watermark"] = position.watermark
    if position.seen_action_ids:
        document["seenActionIds"] = position.seen_action_ids
    if position.floor_action_id:
        document["floorActionId"] = position.floor_action_id
    encoded = json.dumps(document, ensure_ascii=False, separators=(",", ":"))
    for character, escaped in _GO_HTML_ESCAPES.items():
        encoded = encoded.replace(character, escaped)
    return Cursor(encoded)


def decode_card_action_cursor(token: str, board_id: str) -> CardActionCursor:
    """Decode a stored token as Go's strict json.Decoder does and check it belongs to board_id.

    Keys match case-insensitively, an unknown key fails, null leaves a field empty, and text after the
    first JSON value is ignored. Raises ValueError for an undecodable token, another board, an invalid
    watermark, or an action ID that is not a lowercase Trello ID.
    """
    try:
        document, _ = json.JSONDecoder().raw_decode(token.lstrip(" \t\r\n"))
    except (ValueError, RecursionError):
        raise ValueError("Trello card action cursor is not decodable") from None
    position = CardActionCursor()
    if document is not None:
        if not isinstance(document, dict):
            raise ValueError("Trello card action cursor is not decodable")
        for key, value in document.items():
            attribute = _FIELDS.get(key.lower() if key.isascii() else key)
            if attribute is None:
                raise ValueError("Trello card action cursor is not decodable")
            if attribute == "seen_action_ids":
                if value is not None and (
                    not isinstance(value, list)
                    or any(item is not None and not isinstance(item, str) for item in value)
                ):
                    raise ValueError("Trello card action cursor is not decodable")
                position.seen_action_ids = [item or "" for item in value or []]
            else:
                if value is not None and not isinstance(value, str):
                    raise ValueError("Trello card action cursor is not decodable")
                setattr(position, attribute, value or "")
    if position.board_id != board_id:
        raise ValueError("Trello card action cursor belongs to another board")
    if position.watermark and parse_go_time(position.watermark) is None:
        raise ValueError("Trello card action cursor has an invalid watermark")
    for action_id in (*position.seen_action_ids, position.floor_action_id):
        if action_id and (not _TRELLO_ID.match(action_id) or action_id != action_id.lower()):
            raise ValueError("Trello card action cursor has an invalid action ID")
    return position


def seen_action_ids_within(
    previous: list[str], floor: str, added: list[str], watermark: GoTime | None
) -> tuple[list[str], str]:
    """Merge previous and added IDs and keep those created inside the lookback before watermark.

    Keeps the newest MAXIMUM_SEEN_ACTION_IDS, sorted, and returns the floor raised to the largest ID
    dropped over that bound, or blank once the floor's time is outside the window. A None watermark is
    Go's zero time, before every ID.
    """
    horizon = None
    if watermark is not None:
        horizon = watermark.unix_nanoseconds - ACTION_LOOKBACK_NANOSECONDS - _NANOSECONDS_PER_SECOND
    merged = set(previous) | {action_id.lower() for action_id in added}
    kept = sorted(action_id for action_id in merged if _is_inside(action_id, horizon))
    if len(kept) > MAXIMUM_SEEN_ACTION_IDS:
        floor = max(floor, kept[len(kept) - MAXIMUM_SEEN_ACTION_IDS - 1])
        kept = kept[len(kept) - MAXIMUM_SEEN_ACTION_IDS :]
    if not _is_inside(floor, horizon):
        floor = ""
    return kept, floor


def _is_inside(action_id: str, horizon: int | None) -> bool:
    created = object_id_seconds(action_id)
    return created is not None and (horizon is None or created * _NANOSECONDS_PER_SECOND >= horizon)


def object_id_seconds(action_id: str) -> int | None:
    """Return the creation second Trello's Mongo-style IDs carry in their first four bytes."""
    if len(action_id) != 24 or not _TRELLO_ID.match(action_id):
        return None
    return int(action_id[:8], 16)
