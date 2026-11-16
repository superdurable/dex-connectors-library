# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The crmObjectChanged search cursor, encoded as the Go connector's JSON byte for byte."""

import json
import re
from dataclasses import dataclass, field
from typing import Any, Final

from dex_connectors.sdk.pollingtrigger import Cursor

SEARCH_INDEX_LAG_WINDOW_MILLISECONDS: Final = 2 * 60 * 1000
"""Re-reads recent changes, because HubSpot indexes new and updated records after a delay."""
MAXIMUM_SEEN_RECORD_VERSIONS: Final = 1500
"""Keeps the cursor document well under the kit's 64 KiB token bound."""

_RECORD_ID: Final = re.compile(r"[0-9]{1,20}\Z")
_SEARCH_OFFSET: Final = re.compile(r"[0-9]{1,5}\Z")
_OBJECT_TYPES: Final = frozenset({"contacts", "companies", "deals"})
_FIELD_NAMES: Final = (
    "objectType",
    "floorMs",
    "highWaterMs",
    "anchorMs",
    "after",
    "seen",
    "droppedThroughMs",
)
_INT64_MINIMUM: Final = -(2**63)
_INT64_MAXIMUM: Final = 2**63 - 1


class _JSONMembers(list[tuple[str, Any]]):
    """The members of one decoded JSON object, in document order."""


@dataclass(slots=True)
class CRMObjectChangeCursor:
    """The committed Trigger cursor; all times are Unix milliseconds."""

    object_type: str
    floor_ms: int = 0
    """The baseline's newest change; nothing at or before it is delivered."""
    high_water_ms: int = 0
    anchor_ms: int = 0
    """The GTE bound of the next search."""
    after: str = ""
    """HubSpot's offset, set only while over a page of records share anchor_ms."""
    seen: dict[str, int] = field(default_factory=dict)
    """Each record delivered inside the lag window and its delivered version."""
    dropped_through_ms: int = 0
    """The newest version pruned from seen for capacity; the lag-window re-read starts after it."""


def lag_window_start(position: CRMObjectChangeCursor) -> int:
    """Return where the next caught-up search starts: in the lag window, past the baseline and drops."""
    return max(
        position.floor_ms + 1,
        position.high_water_ms - SEARCH_INDEX_LAG_WINDOW_MILLISECONDS,
        position.dropped_through_ms + 1,
    )


def prune_seen(position: CRMObjectChangeCursor) -> None:
    """Keep the newest versions inside the lag window, dropping the oldest past the capacity.

    A drop moves dropped_through_ms past the dropped versions, so a burst larger than seen is not
    delivered again on every poll.
    """
    window_start = lag_window_start(position)
    kept = [
        (object_id, modified) for object_id, modified in position.seen.items() if modified >= window_start
    ]
    kept.sort(key=lambda version: (-version[1], len(version[0]), version[0]))
    if len(kept) > MAXIMUM_SEEN_RECORD_VERSIONS:
        position.dropped_through_ms = max(position.dropped_through_ms, kept[MAXIMUM_SEEN_RECORD_VERSIONS][1])
        kept = kept[:MAXIMUM_SEEN_RECORD_VERSIONS]
    position.seen = dict(kept)


def encode_crm_object_change_cursor(position: CRMObjectChangeCursor) -> Cursor:
    """Encode the cursor as Go's json.Marshal does: field order, omitted empty fields, sorted seen keys."""
    token = (
        f'{{"objectType":{json.dumps(position.object_type)},"floorMs":{position.floor_ms},'
        f'"highWaterMs":{position.high_water_ms},"anchorMs":{position.anchor_ms}'
    )
    if position.after:
        token += f',"after":{json.dumps(position.after)}'
    if position.seen:
        versions = ",".join(f"{json.dumps(key)}:{position.seen[key]}" for key in sorted(position.seen))
        token += f',"seen":{{{versions}}}'
    if position.dropped_through_ms != 0:
        token += f',"droppedThroughMs":{position.dropped_through_ms}'
    return Cursor(token + "}")


def decode_crm_object_change_cursor(token: str) -> CRMObjectChangeCursor:
    """Decode and validate a stored cursor, which is untrusted persisted state, as the Go connector does.

    Raises ValueError naming the first problem.
    """
    position = CRMObjectChangeCursor("")
    try:
        decoded = json.loads(token, object_pairs_hook=_JSONMembers, parse_constant=_reject_constant)
        if decoded is not None:
            if not isinstance(decoded, _JSONMembers):
                raise ValueError("not an object")
            for key, value in decoded:
                _apply_field(position, key, value)
    except (ValueError, TypeError, RecursionError) as error:
        raise ValueError("HubSpot change cursor is not decodable") from error
    if position.object_type not in _OBJECT_TYPES:
        raise ValueError("HubSpot change cursor has no valid object type")
    if (
        position.floor_ms < 0
        or position.high_water_ms < position.floor_ms
        or position.anchor_ms <= position.floor_ms
        or position.dropped_through_ms < 0
        or position.dropped_through_ms > position.high_water_ms
    ):
        raise ValueError("HubSpot change cursor times are inconsistent")
    if position.after and not _SEARCH_OFFSET.fullmatch(position.after):
        raise ValueError("HubSpot change cursor has an invalid paging offset")
    if len(position.seen) > MAXIMUM_SEEN_RECORD_VERSIONS:
        raise ValueError("HubSpot change cursor holds too many record versions")
    if any(not _RECORD_ID.fullmatch(object_id) for object_id in position.seen):
        raise ValueError("HubSpot change cursor has an invalid record ID")
    return position


def _reject_constant(name: str) -> Any:
    raise ValueError(f"JSON constant {name} is not valid")


def _apply_field(position: CRMObjectChangeCursor, key: str, value: Any) -> None:
    """Assign one member as Go's decoder does: exact name first, then case-insensitive, null is no-op."""
    name = (
        key
        if key in _FIELD_NAMES
        else next((item for item in _FIELD_NAMES if item.lower() == key.lower()), "")
    )
    if not name or value is None:
        return
    if name in ("objectType", "after"):
        if not isinstance(value, str):
            raise ValueError("not a string")
        if name == "objectType":
            position.object_type = value
        else:
            position.after = value
    elif name == "seen":
        if not isinstance(value, _JSONMembers):
            raise ValueError("not an object")
        position.seen = {
            object_id: 0 if modified is None else _int64(modified) for object_id, modified in value
        }
    elif name == "floorMs":
        position.floor_ms = _int64(value)
    elif name == "highWaterMs":
        position.high_water_ms = _int64(value)
    elif name == "anchorMs":
        position.anchor_ms = _int64(value)
    else:
        position.dropped_through_ms = _int64(value)


def _int64(value: Any) -> int:
    """Accept an int64 literal; a fraction, an exponent, or a boolean is rejected as Go rejects it."""
    if type(value) is not int or not _INT64_MINIMUM <= value <= _INT64_MAXIMUM:
        raise ValueError("not an int64")
    return value
