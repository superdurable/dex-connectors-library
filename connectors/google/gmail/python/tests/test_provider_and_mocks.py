# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Send request shape and manifest mock scenarios before Step factory wiring."""

import base64
import json
from datetime import UTC, datetime
from email import policy
from email.parser import BytesParser
from types import SimpleNamespace
from typing import cast
from uuid import uuid4

import pytest
from dex.context import AsyncContext
from dex_connectors.sdk import Call, CallID, ConnectionRef, IdempotencyKey, OperationRef

from dex_connectors.google_gmail import GetMessageInput, ReplyToMessageInput, SendMessageInput
from dex_connectors.google_gmail.gmailmock import (
    Mock,
    get_message_deleted_message,
    get_message_empty_message_id,
    get_message_malformed_payload,
    get_message_rate_limited,
    get_message_revoked_grant,
    reply_to_message_connection_lost,
    reply_to_message_empty_body,
    reply_to_message_malformed_source,
    reply_to_message_source_missing,
    send_message_connection_lost,
    send_message_invalid_recipient,
    send_message_no_recipients,
    send_message_unavailable,
)
from dex_connectors.google_gmail.models import Message
from dex_connectors.google_gmail.provider import (
    build_reply_message,
    build_send_message,
    parse_mailbox,
    rfc_message_id_address,
    sent_message_search_query,
    validate_send_input,
)


@pytest.fixture
def call() -> Call:
    """Use a UUID call ID and the same key across retries."""
    call_id = CallID(str(uuid4()))
    return Call(
        call_id,
        ConnectionRef("google", "support"),
        OperationRef("gmail", "sendMessage"),
        IdempotencyKey(call_id),
    )


def test_send_message_has_stable_headers_and_base64url_request(call: Call) -> None:
    """The send body includes the run-scoped Message-ID and safe call header."""
    started_at = datetime(2026, 3, 4, 9, 15, tzinfo=UTC)
    input_value = SendMessageInput(["Ada <ada@example.com>"], "Progress", "Line one\nLine two", "<p>Hi</p>")
    recipients = validate_send_input(input_value, "owner@example.org")
    first = build_send_message(call, "owner@example.org", recipients, input_value, "run-1", started_at)
    assert first == build_send_message(
        call, "owner@example.org", recipients, input_value, "run-1", started_at
    )
    document = json.loads(first)
    assert set(document) == {"raw"}
    raw = base64.urlsafe_b64decode(document["raw"] + "=" * (-len(document["raw"]) % 4))
    parsed = BytesParser(policy=policy.default).parsebytes(raw)
    assert parsed["Message-ID"] == f"<{rfc_message_id_address(call, 'run-1', started_at)}>"
    assert parsed["X-Dex-Call-ID"] == call.id
    assert parsed["From"] == "owner@example.org"
    assert parsed["To"] == "Ada <ada@example.com>"
    assert parsed.get_body(preferencelist=("plain",)).get_content() == "Line one\r\nLine two"
    assert sent_message_search_query(call, "run-1", started_at).startswith("in:sent rfc822msgid:")


def test_reply_message_preserves_thread_headers(call: Call) -> None:
    """The reply POST supplies Gmail thread ID and RFC threading headers."""
    source = Message(
        thread_id="thread-1",
        rfc_message_id="<source@example.com>",
        references="<prior@example.com>",
        subject="Question",
    )
    payload = build_reply_message(
        call,
        "owner@example.org",
        "Ada <ada@example.com>",
        source,
        ReplyToMessageInput("source-1", "Answer"),
        "run-1",
        datetime.now(UTC),
    )
    document = json.loads(payload)
    assert document["threadId"] == "thread-1"
    raw = base64.urlsafe_b64decode(document["raw"] + "=" * (-len(document["raw"]) % 4))
    parsed = BytesParser(policy=policy.default).parsebytes(raw)
    assert parsed["Subject"] == "Re: Question"
    assert parsed["In-Reply-To"] == "<source@example.com>"
    assert parsed["References"] == "<prior@example.com> <source@example.com>"


@pytest.mark.parametrize(
    "value", ["a@example.com\r\nBcc: victim@example.com", "bad", "", "a@example.com, b@example.com"]
)
def test_mailbox_validation_rejects_injection_and_malformed_addresses(value: str) -> None:
    """Untrusted mailbox text cannot add another header or recipient."""
    with pytest.raises(ValueError):
        parse_mailbox(value)


def test_manifest_mock_defaults_and_failures(call: Call) -> None:
    """Every operation has a happy default and its manifest's failure scenarios."""
    mock = Mock("support")
    assert mock.get_message.definition().operation.operation_id == "getMessage"
    assert mock.send_message.definition().operation.operation_id == "sendMessage"
    assert mock.reply_to_message.definition().operation.operation_id == "replyToMessage"
    for case in (
        get_message_deleted_message(),
        get_message_revoked_grant(),
        get_message_malformed_payload(),
        get_message_empty_message_id(),
        get_message_rate_limited(),
        send_message_invalid_recipient(),
        send_message_connection_lost(),
        send_message_no_recipients(),
        send_message_unavailable(),
        reply_to_message_source_missing(),
        reply_to_message_malformed_source(),
        reply_to_message_connection_lost(),
        reply_to_message_empty_body(),
    ):
        assert case.kind in ("branch", "retry")
        if case.kind == "retry":
            assert case.failure is not None
        else:
            assert case.branch


@pytest.mark.asyncio
async def test_mock_operations_answer_through_public_interfaces(call: Call) -> None:
    """Manifest defaults can answer Query and Mutation invocation contracts."""
    mock = Mock("support")
    context = cast(AsyncContext, SimpleNamespace(flow_id="flow-1"))
    query_call = Call(call.id, mock.reference, OperationRef("gmail", "getMessage"), context=context)
    send_call = Call(
        call.id, mock.reference, OperationRef("gmail", "sendMessage"), IdempotencyKey(call.id), context
    )
    read = await mock.get_message.invoke(query_call, GetMessageInput("message-1"))
    sent = await mock.send_message.invoke(
        send_call, SendMessageInput(["ada@example.com"], "Progress", "Update")
    )
    assert read.branch == "read" and read.value is not None
    assert read.value.message_id == "18c0a1b2c3d4e5f6"
    assert sent.branch == "sent" and sent.value is not None
    assert sent.value.message_id == "18c0a1b2c3d4e600"
    assert len(mock.get_message.calls()) == 1
    assert len(mock.send_message.calls()) == 1
