# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Credential failures mirror the Go credential_failure_test.go cases."""

from typing import cast

import dex
import pytest
from dex_connectors.sdk import Call, CallID, ConnectionRef, IdempotencyKey, OperationRef
from dex_connectors.sdk.projectconfig.broker import CredentialBrokerUnavailableError

from dex_connectors.superdurable_email import (
    Config,
    Credentials,
    MessageReference,
    MoveMessageInput,
    ReplyToMessageInput,
    SearchMessagesInput,
    SendMessageInput,
    SetFlagsInput,
)
from dex_connectors.superdurable_email._generated.definitions import REPLY_TO_MESSAGE_DEFINITION
from dex_connectors.superdurable_email.client import Client
from dex_connectors.superdurable_email.operations import _SubmissionMarker

from .test_send_message import CALL_ID, Context, SMTPServer, make_call

REFERENCE = MessageReference("INBOX", 42, 97)
SEND_INPUT = SendMessageInput(to=("jane@example.com",), subject="Hello", text="Body")


class UnavailableBroker:
    """Fail every resolution as a temporarily unavailable credential broker does."""

    async def resolve(self, call: Call) -> Credentials:
        """Raise the broker's transient error."""
        raise CredentialBrokerUnavailableError("credential broker is unavailable")


class MissingConnection:
    """Fail every resolution with a terminal configuration error."""

    async def resolve(self, call: Call) -> Credentials:
        """Raise the static provider's missing-connection error."""
        raise ValueError("connection is not configured")


def client_with(provider: object) -> Client:
    """Construct a client whose credential provider is provider."""
    config = Config("127.0.0.1", "127.0.0.1", "", "", 35801, "implicitTLS", 35802, "implicitTLS")
    return Client(config, cast(object, provider))  # type: ignore[arg-type]


def call_for(operation: str, context: Context) -> Call:
    """Build a stable Call of operation."""
    identifier = CallID(CALL_ID)
    return Call(
        identifier,
        ConnectionRef("email", "mail"),
        OperationRef("email", operation),
        IdempotencyKey(identifier),
        context=cast(dex.AsyncContext, context),
        max_sends=REPLY_TO_MESSAGE_DEFINITION.max_sends,
    )


@pytest.mark.asyncio
async def test_operations_retry_an_unavailable_credential_broker() -> None:
    """A transient broker failure returns Retry and never selects defect."""
    client = client_with(UnavailableBroker())
    search = await client.search_messages().invoke(
        call_for("searchMessages", Context()), SearchMessagesInput()
    )
    assert search.kind == "retry"
    flags = await client.set_flags().invoke(
        call_for("setFlags", Context()), SetFlagsInput(REFERENCE, is_seen=True)
    )
    assert flags.kind == "retry"
    move = await client.move_message().invoke(
        call_for("moveMessage", Context()), MoveMessageInput(REFERENCE, "Archive")
    )
    assert move.kind == "retry"

    context = Context()
    send = await client.send_message().invoke(make_call(context), SEND_INPUT)
    assert send.kind == "retry"
    assert context.heartbeats == [_SubmissionMarker(CALL_ID, 1, 0)], "recorded as no submission"


@pytest.mark.asyncio
async def test_terminal_credential_failures_select_defect_only_before_a_possible_change() -> None:
    """A terminal credential failure selects defect on a first attempt and Retry on a later safe write."""
    client = client_with(MissingConnection())
    search = await client.search_messages().invoke(
        call_for("searchMessages", Context()), SearchMessagesInput()
    )
    assert search.kind == "branch" and search.branch == "defect"
    first = await client.set_flags().invoke(
        call_for("setFlags", Context()), SetFlagsInput(REFERENCE, is_seen=True)
    )
    assert first.kind == "branch" and first.branch == "defect"
    later = await client.set_flags().invoke(
        call_for("setFlags", Context(attempt=2)), SetFlagsInput(REFERENCE, is_seen=True)
    )
    assert later.kind == "retry", "an earlier attempt may have applied the flags"
    moved = await client.move_message().invoke(
        call_for("moveMessage", Context(attempt=2)), MoveMessageInput(REFERENCE, "Archive")
    )
    assert moved.kind == "retry"


@pytest.mark.asyncio
async def test_reply_after_an_accepted_reply_retries_an_unavailable_broker(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """The accepted marker stays the last checkpoint, so a later attempt can still select sent."""
    client = client_with(UnavailableBroker())
    monkeypatch.setattr(client, "open_smtp", lambda *_: pytest.fail("connected"))
    context = Context(attempt=2, visible_marker=_SubmissionMarker(CALL_ID, 1, 1, True))
    attempt = await client.reply_to_message().invoke(
        call_for("replyToMessage", context), ReplyToMessageInput(REFERENCE, "Thanks")
    )
    assert attempt.kind == "retry"
    assert context.heartbeats == []


@pytest.mark.asyncio
async def test_send_after_an_accepted_send_retries_an_unavailable_broker(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """A transient failure after acceptance never resubmits and never clears the accepted marker."""
    client = client_with(UnavailableBroker())
    server = SMTPServer()
    monkeypatch.setattr(client, "open_smtp", lambda *_: server)
    context = Context(attempt=2, visible_marker=_SubmissionMarker(CALL_ID, 1, 1, True))
    attempt = await client.send_message().invoke(make_call(context), SEND_INPUT)
    assert attempt.kind == "retry"
    assert context.heartbeats == []
    assert server.submissions == 0
