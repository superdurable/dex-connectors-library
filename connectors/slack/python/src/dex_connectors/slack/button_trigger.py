# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Decode and match Slack block_actions payloads for the buttonClicked Trigger."""

from __future__ import annotations

import math
import re
from dataclasses import dataclass, field
from datetime import UTC, datetime, timedelta
from typing import Any, Final

from dex_connectors.sdk import TriggerEvent

from .models import ButtonClickedTriggerConfiguration, ButtonClickEvent

# Go's zero time, which the Go connector reports for an unparsable click timestamp.
_GO_ZERO_TIME: Final = datetime(1, 1, 1, tzinfo=UTC)
_UNIX_EPOCH: Final = datetime(1970, 1, 1, tzinfo=UTC)
_GO_DECIMAL_FLOAT: Final = re.compile(r"[+-]?([0-9]+\.?[0-9]*|\.[0-9]+)([eE][+-]?[0-9]+)?", re.ASCII)


def validate_button_clicked_trigger_configuration(configuration: ButtonClickedTriggerConfiguration) -> None:
    """Raise ValueError for a blank channel or blank or repeated action and clicker IDs."""
    if not configuration.channel_id.strip():
        raise ValueError("Slack channel ID is required")
    if not _is_unique_and_not_blank(configuration.action_ids):
        raise ValueError("Slack button action IDs must be non-empty and unique")
    if not _is_unique_and_not_blank(configuration.clicker_user_ids):
        raise ValueError("Slack clicker user IDs must be non-empty and unique")


def _is_unique_and_not_blank(values: tuple[str, ...] | None) -> bool:
    items = values or ()
    return all(value.strip() for value in items) and len(set(items)) == len(items)


@dataclass(frozen=True, slots=True)
class _BlockAction:
    type: str = ""
    action_id: str = ""
    value: str = ""
    action_ts: str = ""


@dataclass(frozen=True, slots=True)
class BlockActionsPayload:
    """The block_actions fields the Trigger reads, decoded with Go json.Unmarshal semantics."""

    type: str = ""
    team_id: str = ""
    user_id: str = ""
    channel_id: str = ""
    container_type: str = ""
    container_message_ts: str = ""
    container_channel_id: str = ""
    container_thread_ts: str = ""
    message_ts: str = ""
    message_thread_ts: str = ""
    actions: tuple[_BlockAction, ...] = field(default_factory=tuple)

    @property
    def clicked_channel_id(self) -> str:
        """Return the clicked message's channel, from the container or the payload's channel."""
        return self.container_channel_id or self.channel_id


def _object(value: Any) -> dict[str, Any]:
    if value is None:
        return {}
    if not isinstance(value, dict):
        raise ValueError("Slack block_actions payload has an invalid object")
    return value


def _text(value: dict[str, Any], name: str) -> str:
    item = value.get(name)
    if item is None:
        return ""
    if not isinstance(item, str):
        raise ValueError("Slack block_actions payload has an invalid field")
    return item


def decode_block_actions_payload(value: Any) -> BlockActionsPayload:
    """Decode a block_actions payload where null, including a null action, is the zero value, as in Go."""
    payload = _object(value)
    container = _object(payload.get("container"))
    message = _object(payload.get("message"))
    raw_actions = payload.get("actions")
    if raw_actions is not None and not isinstance(raw_actions, list):
        raise ValueError("Slack block_actions payload has invalid actions")
    actions = []
    for raw_action in raw_actions or []:
        action = _object(raw_action)
        actions.append(
            _BlockAction(
                _text(action, "type"),
                _text(action, "action_id"),
                _text(action, "value"),
                _text(action, "action_ts"),
            )
        )
    return BlockActionsPayload(
        _text(payload, "type"),
        _text(_object(payload.get("team")), "id"),
        _text(_object(payload.get("user")), "id"),
        _text(_object(payload.get("channel")), "id"),
        _text(container, "type"),
        _text(container, "message_ts"),
        _text(container, "channel_id"),
        _text(container, "thread_ts"),
        _text(message, "ts"),
        _text(message, "thread_ts"),
        tuple(actions),
    )


def match_button_click_payload(
    configuration: ButtonClickedTriggerConfiguration, payload: BlockActionsPayload
) -> tuple[list[TriggerEvent[ButtonClickEvent]], list[str]]:
    """Return an event per accepted button action and a reason per ignored action or payload.

    Reasons are Slack enum values only, never message text.
    """
    channel_id = payload.clicked_channel_id
    message_timestamp = payload.container_message_ts or payload.message_ts
    if payload.type != "block_actions":
        return [], ["not_block_actions"]
    if payload.container_type != "message":
        return [], ["not_a_message"]
    if channel_id != configuration.channel_id:
        return [], ["channel_mismatch"]
    if not payload.user_id:
        return [], ["missing_user"]
    if not payload.team_id or not message_timestamp:
        return [], ["missing_message_identity"]
    if not _contains_or_empty(configuration.clicker_user_ids, payload.user_id):
        return [], ["clicker_mismatch"]
    thread_timestamp = payload.container_thread_ts or payload.message_thread_ts or message_timestamp
    events: list[TriggerEvent[ButtonClickEvent]] = []
    ignored: list[str] = []
    for action in payload.actions:
        if action.type != "button":
            ignored.append("not_a_button")
            continue
        if not action.action_id or not action.action_ts:
            ignored.append("missing_action_identity")
            continue
        if not _contains_or_empty(configuration.action_ids, action.action_id):
            ignored.append("action_mismatch")
            continue
        events.append(
            TriggerEvent(
                # Interactive payloads have no event ID. A click is unique by its message, button, and time.
                ":".join(
                    (payload.team_id, channel_id, message_timestamp, action.action_id, action.action_ts)
                ),
                slack_timestamp_time(action.action_ts),
                ButtonClickEvent(
                    payload.team_id,
                    channel_id,
                    message_timestamp,
                    thread_timestamp,
                    payload.user_id,
                    action.action_id,
                    action.value,
                    action.action_ts,
                ),
            )
        )
    if not payload.actions:
        ignored.append("no_actions")
    return events, ignored


def slack_timestamp_time(timestamp: str) -> datetime:
    """Parse a Slack "seconds.micros" timestamp to the microsecond, returning Go's zero time when invalid."""
    if _GO_DECIMAL_FLOAT.fullmatch(timestamp) is None:
        return _GO_ZERO_TIME
    seconds = float(timestamp)
    if seconds <= 0 or math.isinf(seconds):
        return _GO_ZERO_TIME
    fraction, whole = math.modf(seconds)
    try:
        return _UNIX_EPOCH + timedelta(seconds=int(whole), microseconds=math.floor(fraction * 1e6 + 0.5))
    except OverflowError:
        return _GO_ZERO_TIME


def _contains_or_empty(allowed: tuple[str, ...] | None, value: str) -> bool:
    return not allowed or value in allowed
