# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The conversationReceived cursor token and snapshot items, byte for byte as the Go connector writes them."""

import hashlib
import json
from dataclasses import dataclass
from typing import Any, Final

from dex_connectors.sdk.projectconfig import encode_go_json_value

from .client import is_page_token

CONVERSATION_RECEIVED_CURSOR_VERSION: Final = 1
"""Versions the stored cursor token; another version records a new baseline."""
# Go's unicode.IsSpace set, which strings.TrimSpace trims.
GO_SPACE: Final = (
    "\t\n\v\f\r \x85\xa0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a"
    "\u2028\u2029\u202f\u205f\u3000"
)
_INT64_MINIMUM: Final = -(2**63)
_INT64_MAXIMUM: Final = 2**63 - 1
_CURSOR_FIELD_NAMES: Final = {
    "v": "v",
    "inboxid": "inboxId",
    "baselinecomplete": "baselineComplete",
    "sweeppagetoken": "sweepPageToken",
    "sweepstartseen": "sweepStartSeen",
    "previoussweepstartseen": "previousSweepStartSeen",
}
_STRING_FIELDS: Final = frozenset({"inboxId", "sweepPageToken"})


@dataclass(slots=True)
class ConversationReceivedCursorToken:
    """The JSON position stored in the poll cursor; the keyed snapshot beside it holds tracked IDs."""

    inbox_id: str
    """The polled shared inbox; a cursor of another inbox records a new baseline."""
    is_baseline_complete: bool = False
    """False until the first full walk of the list has recorded every existing conversation."""
    sweep_page_token: str = ""
    """Hiver's next_page token where the current sweep continues; empty starts a sweep."""
    sweep_start_seen: int = 0
    """The lowest snapshot seen counter a conversation refreshed by the current sweep can have."""
    previous_sweep_start_seen: int = 0
    """sweep_start_seen of the last completed sweep; IDs below it at the next sweep end are dropped."""
    version: int = CONVERSATION_RECEIVED_CURSOR_VERSION

    def encode(self) -> str:
        """Return Go's json.Marshal text, with the omitempty fields left out when empty or zero."""
        fields = [
            f'"v":{self.version}',
            '"inboxId":' + encode_go_json_value(self.inbox_id).decode("utf-8"),
            '"baselineComplete":' + ("true" if self.is_baseline_complete else "false"),
        ]
        if self.sweep_page_token:
            fields.append('"sweepPageToken":' + encode_go_json_value(self.sweep_page_token).decode("utf-8"))
        if self.sweep_start_seen:
            fields.append(f'"sweepStartSeen":{self.sweep_start_seen}')
        if self.previous_sweep_start_seen:
            fields.append(f'"previousSweepStartSeen":{self.previous_sweep_start_seen}')
        return "{" + ",".join(fields) + "}"


class _CursorFields:
    def __init__(self, values: dict[str, Any]) -> None:
        self.values = values


def _cursor_pairs(pairs: list[tuple[str, Any]]) -> _CursorFields:
    values: dict[str, Any] = {}
    for key, value in pairs:
        name = _CURSOR_FIELD_NAMES.get(key.lower())
        if name is None:
            raise ValueError("unknown cursor field")
        if value is None:
            continue
        if name in _STRING_FIELDS:
            if not isinstance(value, str):
                raise ValueError("cursor field must be a string")
        elif name == "baselineComplete":
            if not isinstance(value, bool):
                raise ValueError("cursor field must be a boolean")
        elif (
            isinstance(value, bool)
            or not isinstance(value, int)
            or not _INT64_MINIMUM <= value <= _INT64_MAXIMUM
        ):
            raise ValueError("cursor field must be an integer")
        values[name] = value
    return _CursorFields(values)


def _reject_constant(value: str) -> None:
    raise ValueError("invalid JSON number")


def _reject_float(value: str) -> None:
    raise ValueError("cursor integers have no fraction or exponent")


def decode_conversation_received_cursor_token(encoded: str, inbox_id: str) -> ConversationReceivedCursorToken:
    """Decode a stored token as Go's json.Decoder with DisallowUnknownFields does, for inbox_id.

    Field names match case-insensitively, a later duplicate wins, null leaves a field zero, and text after
    the first JSON value is ignored. Raises ValueError for a token of another version or another inbox, or
    one holding an invalid sweep position, so the poll kit records a new baseline.
    """
    try:
        document, _ = json.JSONDecoder(
            object_pairs_hook=_cursor_pairs, parse_constant=_reject_constant, parse_float=_reject_float
        ).raw_decode(encoded.lstrip(" \t\r\n"))
        if document is not None and not isinstance(document, _CursorFields):
            raise ValueError("cursor is not an object")
    except (ValueError, TypeError, RecursionError) as error:
        raise ValueError("Hiver conversationReceived cursor is not a version 1 cursor") from error
    fields = document.values if isinstance(document, _CursorFields) else {}
    token = ConversationReceivedCursorToken(
        fields.get("inboxId", ""),
        fields.get("baselineComplete", False),
        fields.get("sweepPageToken", ""),
        fields.get("sweepStartSeen", 0),
        fields.get("previousSweepStartSeen", 0),
        fields.get("v", 0),
    )
    if token.version != CONVERSATION_RECEIVED_CURSOR_VERSION:
        raise ValueError("Hiver conversationReceived cursor is not a version 1 cursor")
    if token.inbox_id != inbox_id:
        raise ValueError("Hiver conversationReceived cursor belongs to another shared inbox")
    if (
        (token.sweep_page_token and not is_page_token(token.sweep_page_token))
        or token.sweep_start_seen < 0
        or token.previous_sweep_start_seen < 0
        or token.previous_sweep_start_seen > token.sweep_start_seen
    ):
        raise ValueError("Hiver conversationReceived cursor holds an invalid sweep position")
    return token


@dataclass(frozen=True, slots=True)
class ConversationSnapshotItem:
    """Tracks one conversation ID; its hash ignores status, assignee, and tags."""

    conversation_id: str

    def snapshot_key(self) -> str:
        """Return the Hiver conversation ID."""
        return self.conversation_id

    def snapshot_hash(self) -> str:
        """Return the first 8 bytes of SHA-256 over the ID as hex; only membership matters."""
        return hashlib.sha256(self.conversation_id.encode("utf-8")).hexdigest()[:16]

    def snapshot_value(self) -> bytes | None:
        """Store nothing beside the key."""
        return None


def conversation_received_event_id(inbox_id: str, conversation_id: str) -> str:
    """Return `conversation-<inbox>-<conversation>`, a pure function of the conversation."""
    return "conversation-" + inbox_id + "-" + conversation_id
