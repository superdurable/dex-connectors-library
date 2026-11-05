# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Native protocol failure routes match the Go connector's declared branches."""

from typing import Any

import pytest
from dex_connectors.sdk import Failure, FailureKind

from dex_connectors.superdurable_email import (
    GetMessageInput,
    MessageReference,
    MoveMessageInput,
    SearchMessagesInput,
    SetFlagsInput,
)
from dex_connectors.superdurable_email.client import MailFailure

from .test_imap_operations import IMAPServer, make_call
from .test_send_message import SMTPServer, make_client


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("operation", "input_value", "outcome", "expected_kind", "expected_branch"),
    [
        ("searchMessages", SearchMessagesInput(), "rejected", "branch", "providerRejected"),
        ("searchMessages", SearchMessagesInput(), "invalid", "branch", "invalidResponse"),
        ("searchMessages", SearchMessagesInput(), "retry", "retry", ""),
        (
            "getMessage",
            GetMessageInput(MessageReference("INBOX", 42, 97)),
            "rejected",
            "branch",
            "providerRejected",
        ),
        (
            "getMessage",
            GetMessageInput(MessageReference("INBOX", 42, 97)),
            "invalid",
            "branch",
            "invalidResponse",
        ),
        ("getMessage", GetMessageInput(MessageReference("INBOX", 42, 97)), "retry", "retry", ""),
        (
            "setFlags",
            SetFlagsInput(MessageReference("INBOX", 42, 97), is_seen=True),
            "rejected",
            "branch",
            "providerRejected",
        ),
        ("setFlags", SetFlagsInput(MessageReference("INBOX", 42, 97), is_seen=True), "invalid", "retry", ""),
        (
            "moveMessage",
            MoveMessageInput(MessageReference("INBOX", 42, 97), "Archive"),
            "rejected",
            "branch",
            "providerRejected",
        ),
        (
            "moveMessage",
            MoveMessageInput(MessageReference("INBOX", 42, 97), "Archive"),
            "invalid",
            "retry",
            "",
        ),
    ],
)
async def test_imap_failure_routing(
    monkeypatch: pytest.MonkeyPatch,
    operation: str,
    input_value: Any,
    outcome: str,
    expected_kind: str,
    expected_branch: str,
) -> None:
    """Classify each protocol outcome before any Flow branch wiring."""
    client = make_client()
    failure = MailFailure(outcome, Failure(FailureKind.PROTOCOL, "email", operation, "safe failure"))

    def refuse(*args: object) -> None:
        raise failure

    monkeypatch.setattr(client, "open_imap", refuse)
    operation_value = getattr(
        client, "".join("_" + c.lower() if c.isupper() else c for c in operation).lstrip("_")
    )()
    attempt = await operation_value.invoke(make_call(operation), input_value)
    assert attempt.kind == expected_kind
    assert attempt.branch == expected_branch


@pytest.mark.asyncio
async def test_missing_uid_and_provider_rejected_flags(monkeypatch: pytest.MonkeyPatch) -> None:
    """No UID selects notFound; ignored STORE selects providerRejected after read-back."""
    client = make_client()
    server = IMAPServer()
    monkeypatch.setattr(client, "open_imap", lambda *_: server)
    reference = MessageReference("INBOX", 42, 97)
    server.has_source = False
    missing = await client.set_flags().invoke(make_call("setFlags"), SetFlagsInput(reference, is_seen=True))
    assert missing.branch == "notFound"

    server.has_source = True
    server.seen = False
    original_uid = server.uid

    def ignored_store(command: str, *arguments: str) -> tuple[str, list[Any]]:
        if command == "STORE":
            return "OK", [b"ignored"]
        return original_uid(command, *arguments)

    monkeypatch.setattr(server, "uid", ignored_store)
    rejected = await client.set_flags().invoke(make_call("setFlags"), SetFlagsInput(reference, is_seen=True))
    assert rejected.branch == "providerRejected"


@pytest.mark.asyncio
async def test_smtp_permanent_rejection_does_not_retry(monkeypatch: pytest.MonkeyPatch) -> None:
    """A final SMTP 550 proves no message was accepted."""
    from dex_connectors.superdurable_email import SendMessageInput

    from .test_send_message import Context
    from .test_send_message import make_call as make_send_call

    client = make_client()
    server = SMTPServer(final_code=550)
    monkeypatch.setattr(client, "open_smtp", lambda *_: server)
    context = Context()
    attempt = await client.send_message().invoke(
        make_send_call(context), SendMessageInput(to=("jane@example.com",), subject="Hello", text="Body")
    )
    assert attempt.kind == "branch" and attempt.branch == "providerRejected"
    assert context.marker is None
