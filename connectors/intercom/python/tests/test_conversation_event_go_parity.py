# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay notifications the Go decoder recorded and require the same outcome and inbox payload.

The SDK encodes a dataclass payload with Go's map key order, so its bytes may order fields differently
from Go's struct encoding; Go and Python both decode inbox payloads by field name, so either replica
replays the other's events.
"""

import base64
import json
from datetime import datetime
from pathlib import Path
from typing import Any

import pytest
from dex_connectors.sdk.projectconfig import decode_trigger_event, encode_trigger_event
from dex_connectors.sdk.webhooktrigger import WebhookRequest

from dex_connectors.intercom import ConversationEvent
from dex_connectors.intercom.triggers import (
    _decode_conversation_event_payload,
    _decode_conversation_event_request,
)

_CASES: list[dict[str, Any]] = json.loads(
    (Path(__file__).parent / "fixtures" / "conversation-event-go-decoder-cases.json").read_text()
)


def _request(case: dict[str, Any]) -> WebhookRequest:
    received_at = datetime.fromisoformat(case["receivedAt"])
    return WebhookRequest("/webhooks/intercom", (), base64.b64decode(case["body"]), received_at)


@pytest.mark.parametrize("case", _CASES, ids=[case["name"] for case in _CASES])
def test_decoder_matches_the_go_recording(case: dict[str, Any]) -> None:
    """Each recorded body decodes, is ignored, or is refused as Go did, with Go's payload bytes."""
    if case["outcome"] == "invalid":
        with pytest.raises(ValueError):
            _decode_conversation_event_request(_request(case))
        return
    event = _decode_conversation_event_request(_request(case))
    if case["outcome"] == "unhandled":
        assert event is None
        return
    assert event is not None
    assert event.id == case["id"]
    assert event.occurred_at == datetime.fromisoformat(case["occurredAt"])
    stored = encode_trigger_event(event)
    expected_prefix = (
        b'{"eventId":'
        + json.dumps(case["id"]).encode()
        + b',"occurredAt":'
        + json.dumps(case["occurredAt"]).encode()
        + b',"payload":'
    )
    assert stored.startswith(expected_prefix) and stored.endswith(b"}")
    stored_payload = stored[len(expected_prefix) : -1]
    assert json.loads(stored_payload) == json.loads(case["payloadJson"])
    # Same bytes in another order: Go's compact, HTML-escaped encoding of the same members.
    assert sorted(stored_payload) == sorted(case["payloadJson"].encode())


@pytest.mark.parametrize(
    "case", [case for case in _CASES if case["outcome"] == "event"], ids=lambda case: case["name"]
)
def test_go_inbox_payload_replays_to_the_decoded_event(case: dict[str, Any]) -> None:
    """A payload a Go replica stored replays to the same ConversationEvent this package decodes."""
    decoded = _decode_conversation_event_request(_request(case))
    assert decoded is not None
    payload_json = case["payloadJson"]
    replayed = _decode_conversation_event_payload(payload_json, payload_json.encode())
    assert isinstance(replayed, ConversationEvent)
    assert replayed == decoded.payload
    stored = b'{"eventId":"x","occurredAt":"2026-01-01T00:00:00Z","payload":' + payload_json.encode() + b"}"
    assert decode_trigger_event(stored, _decode_conversation_event_payload).payload == decoded.payload
