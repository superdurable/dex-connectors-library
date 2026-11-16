# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The recordModifiedPolled cursor, encoded as the Go connector's JSON byte for byte."""

import json
from dataclasses import dataclass
from typing import Any, Final

from dex_connectors.sdk.pollingtrigger import Cursor

from .client import is_record_id
from .record_modified_trigger import Instant, parse_rfc3339

_FIELD_NAMES: Final = ("sObjectType", "systemModstamp", "recordId")


class _JSONMembers(list[tuple[str, Any]]):
    """The members of one decoded JSON object, in document order."""


@dataclass(slots=True)
class RecordPollCursor:
    """The committed cursor: every record ordered at or before (system_modstamp, record_id) was read.

    A blank record_id means every record stamped at or before system_modstamp was read.
    """

    s_object_type: str = ""
    system_modstamp: str = ""
    """An RFC 3339 time, whole seconds in UTC when this package or Go wrote it."""
    record_id: str = ""


def encode_record_poll_cursor(position: RecordPollCursor) -> Cursor:
    """Encode the cursor as Go's json.Marshal does: field order and an omitted empty recordId."""
    token = (
        '{"sObjectType":'
        + _go_json_string(position.s_object_type)
        + ',"systemModstamp":'
        + _go_json_string(position.system_modstamp)
    )
    if position.record_id:
        token += ',"recordId":' + _go_json_string(position.record_id)
    return Cursor(token + "}")


def decode_record_poll_cursor(token: str) -> tuple[RecordPollCursor, Instant]:
    """Decode an untrusted stored cursor as Go does and return it with its time; raises ValueError."""
    position = RecordPollCursor()
    try:
        decoded = json.loads(token, object_pairs_hook=_JSONMembers, parse_constant=_reject_constant)
        if decoded is not None:
            if not isinstance(decoded, _JSONMembers):
                raise ValueError("not an object")
            for key, value in decoded:
                _apply_field(position, key, value)
    except (ValueError, TypeError, RecursionError) as error:
        raise ValueError("Salesforce poll cursor is not decodable") from error
    after = parse_rfc3339(position.system_modstamp)
    if after is None or (position.record_id and not is_record_id(position.record_id)):
        raise ValueError("Salesforce poll cursor is not decodable")
    return position, after


def _go_json_string(value: str) -> str:
    # Go escapes <, >, &, U+2028, and U+2029 and writes other characters as UTF-8.
    encoded = json.dumps(value, ensure_ascii=False)
    for character, escape in (("<", "\\u003c"), (">", "\\u003e"), ("&", "\\u0026")):
        encoded = encoded.replace(character, escape)
    return encoded.replace("\u2028", "\\u2028").replace("\u2029", "\\u2029")


def _reject_constant(name: str) -> Any:
    raise ValueError(f"JSON constant {name} is not valid")


def _apply_field(position: RecordPollCursor, key: str, value: Any) -> None:
    """Assign one member as Go's decoder does: exact name first, then case-insensitive, null is no-op."""
    name = (
        key
        if key in _FIELD_NAMES
        else next((item for item in _FIELD_NAMES if item.lower() == key.lower()), "")
    )
    if not name or value is None:
        return
    if not isinstance(value, str):
        raise ValueError("not a string")
    if name == "sObjectType":
        position.s_object_type = value
    elif name == "systemModstamp":
        position.system_modstamp = value
    else:
        position.record_id = value
