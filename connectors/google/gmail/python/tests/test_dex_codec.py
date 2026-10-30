# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Gmail wire models through the released Dex Python 1.28 codec."""

import json
from datetime import UTC, datetime

from dex import CodecRegistry, Value

from dex_connectors.google_gmail import (
    GetMessageInput,
    Message,
    ReplyToMessageInput,
    SendMessageInput,
    SendMessageOutput,
)


def test_operation_inputs_and_send_output_round_trip_with_go_names() -> None:
    """Required fields retain Go names and optional HTML is absent when blank."""
    registry = CodecRegistry()
    cases = (
        (GetMessageInput("source-1"), {"messageId": "source-1"}),
        (
            SendMessageInput(["ada@example.com"], "Progress", "Update"),
            {"to": ["ada@example.com"], "subject": "Progress", "textBody": "Update"},
        ),
        (
            ReplyToMessageInput("source-1", "Answer"),
            {"messageId": "source-1", "textBody": "Answer"},
        ),
        (
            SendMessageOutput("owner@example.org", ["ada@example.com"], "", ""),
            {
                "sender": "owner@example.org",
                "recipients": ["ada@example.com"],
                "messageId": "",
                "threadId": "",
            },
        ),
    )
    for value, expected in cases:
        codec = registry.resolve(type(value))
        encoded = codec.encode(value)
        assert json.loads(encoded.data) == expected
        assert codec.decode(encoded) == value


def test_message_uses_go_omission_and_rfc3339_utc() -> None:
    """Go optional fields disappear while ReceivedAt emits an RFC3339 Z timestamp."""
    codec = CodecRegistry().resolve(Message)
    message = Message(
        message_id="message-1",
        thread_id="thread-1",
        from_address="Ada <ada@example.com>",
        subject="Question",
        received_at=datetime(2026, 3, 4, 9, 15, tzinfo=UTC),
    )
    encoded = codec.encode(message)
    assert json.loads(encoded.data) == {
        "messageId": "message-1",
        "threadId": "thread-1",
        "from": "Ada <ada@example.com>",
        "subject": "Question",
        "receivedAt": "2026-03-04T09:15:00Z",
    }
    assert codec.decode(encoded) == message
    assert codec.decode(Value(encoded.kind, encoded.data.replace("Z", "+00:00"))) == message
