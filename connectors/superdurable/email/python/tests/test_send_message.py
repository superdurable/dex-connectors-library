# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""SMTP outcomes mirror the Go in-process mailtest scenarios."""

from dataclasses import dataclass, field
from typing import Any, cast

import dex
import pytest
from dex_connectors.sdk import Call, CallID, ConnectionRef, IdempotencyKey, OperationRef, SecretString

from dex_connectors.superdurable_email import Config, Credentials
from dex_connectors.superdurable_email.client import Client
from dex_connectors.superdurable_email.models import SendMessageInput


class CredentialsProvider:
    """Resolve one inert test credential."""

    async def resolve(self, call: Call) -> Credentials:
        """Return a valid mailbox credential for each attempt."""
        return Credentials("agent@example.com", SecretString("inert"), "", SecretString(""))


@dataclass
class Context:
    """Track the Dex heartbeat claim used by SMTP mutations."""

    marker: object | None = None
    heartbeats: list[object | None] = field(default_factory=list)

    def has_last_heartbeat_value(self) -> bool:
        """Report a previously recorded submission marker."""
        return self.marker is not None

    async def heartbeat(self, value: object = ...) -> None:
        """Record or clear the submission marker."""
        self.marker = None if value is ... else value
        self.heartbeats.append(self.marker)


class SMTPServer:
    """Observe one SMTP submission without a network port."""

    def __init__(self, final_code: int = 250, lose_final_answer: bool = False) -> None:
        self.esmtp_features = {"size": "1000000"}
        self.final_code = final_code
        self.lose_final_answer = lose_final_answer
        self.commands: list[str] = []
        self.content = b""
        self.replies = 0
        self.sock: Any = None

    def mail(self, sender: str, options: list[str]) -> tuple[int, bytes]:
        """Accept the envelope sender."""
        self.commands.append("MAIL")
        return 250, b"ok"

    def rcpt(self, recipient: str) -> tuple[int, bytes]:
        """Accept one envelope recipient."""
        self.commands.append("RCPT")
        return 250, b"ok"

    def putcmd(self, command: str) -> None:
        """Record DATA without accepting its content yet."""
        self.commands.append(command)

    def getreply(self) -> tuple[int, bytes]:
        """Accept DATA or return the selected final answer."""
        self.replies += 1
        if self.replies == 1:
            return 354, b"go ahead"
        if self.lose_final_answer:
            raise ConnectionResetError("lost after final dot")
        return self.final_code, b"done"

    def send(self, content: bytes) -> None:
        """Record the bytes that include the final dot."""
        self.content += content

    def quit(self) -> None:
        """Ignore courtesy QUIT."""

    def close(self) -> None:
        """Ignore courtesy close."""


def make_call(context: Context) -> Call:
    """Build a stable sendMessage Call."""
    identifier = CallID("00000000-0000-4000-8000-000000000001")
    return Call(
        identifier,
        ConnectionRef("email", "mail"),
        OperationRef("email", "sendMessage"),
        IdempotencyKey(identifier),
        context=cast(dex.AsyncContext, context),
    )


def make_client() -> Client:
    """Construct a client whose transport is replaced by each test."""
    config = Config("127.0.0.1", "127.0.0.1", "", "", 35801, "implicitTLS", 35802, "implicitTLS")
    return Client(config, CredentialsProvider())


@pytest.mark.asyncio
async def test_send_message_accepted_with_stable_identifier_and_no_bcc_header(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """A final 250 selects sent and the Bcc stays only in the envelope."""
    client = make_client()
    server = SMTPServer()
    monkeypatch.setattr(client, "open_smtp", lambda *_: server)
    context = Context()
    attempt = await client.send_message().invoke(
        make_call(context),
        SendMessageInput(to=("jane@example.com",), bcc=("blind@example.com",), subject="Hello", text="Body"),
    )
    assert attempt.kind == "branch" and attempt.branch == "sent"
    assert attempt.receipt is not None and attempt.receipt.provider_object_id == (
        "dex-00000000-0000-4000-8000-000000000001@example.com"
    )
    assert b"Bcc:" not in server.content
    assert server.content.endswith(b"\r\n.\r\n")
    assert attempt.value is not None and attempt.value.recipients == ("jane@example.com", "blind@example.com")
    assert len(context.heartbeats) == 1


@pytest.mark.asyncio
async def test_send_message_temporary_refusal_clears_claim_and_retries(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """A final temporary SMTP reply proves nonacceptance and permits retry."""
    client = make_client()
    server = SMTPServer(final_code=451)
    monkeypatch.setattr(client, "open_smtp", lambda *_: server)
    context = Context()
    attempt = await client.send_message().invoke(
        make_call(context), SendMessageInput(to=("jane@example.com",), subject="Hello", text="Body")
    )
    assert attempt.kind == "retry"
    assert context.heartbeats[-1] is None


@pytest.mark.asyncio
async def test_send_message_lost_final_answer_is_uncertain_and_not_resent(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """Once the final dot may have reached SMTP, another attempt cannot submit."""
    client = make_client()
    server = SMTPServer(lose_final_answer=True)
    monkeypatch.setattr(client, "open_smtp", lambda *_: server)
    context = Context()
    call = make_call(context)
    input = SendMessageInput(to=("jane@example.com",), subject="Hello", text="Body")
    first = await client.send_message().invoke(call, input)
    assert first.kind == "uncertain" and context.marker is not None
    second = await client.send_message().invoke(call, input)
    assert second.kind == "uncertain"
    assert server.commands.count("MAIL") == 1


@pytest.mark.asyncio
async def test_send_message_rejects_invalid_input_before_network(monkeypatch: pytest.MonkeyPatch) -> None:
    """An invalid recipient selects defect and never opens SMTP."""
    client = make_client()
    monkeypatch.setattr(client, "open_smtp", lambda *_: pytest.fail("connected"))
    attempt = await client.send_message().invoke(
        make_call(Context()), SendMessageInput(to=("not an address",), subject="Hello", text="Body")
    )
    assert attempt.branch == "defect"


@pytest.mark.asyncio
async def test_send_message_rejects_all_subject_controls(monkeypatch: pytest.MonkeyPatch) -> None:
    """C0 and C1 subject controls select defect before a network request."""
    client = make_client()
    monkeypatch.setattr(client, "open_smtp", lambda *_: pytest.fail("connected"))
    for character in ("\t", "\0", "\x1b", "\x85"):
        attempt = await client.send_message().invoke(
            make_call(Context()),
            SendMessageInput(to=("jane@example.com",), subject="Hello" + character, text="Body"),
        )
        assert attempt.branch == "defect"


@pytest.mark.asyncio
async def test_final_smtp_dot_uses_ninety_second_reply_timeout(monkeypatch: pytest.MonkeyPatch) -> None:
    """Submission gives a scanning server time to answer after the final dot."""
    class TimeoutSocket:
        def __init__(self) -> None:
            self.timeouts: list[int] = []

        def settimeout(self, timeout: int) -> None:
            self.timeouts.append(timeout)

    client = make_client()
    server = SMTPServer()
    server.sock = TimeoutSocket()
    monkeypatch.setattr(client, "open_smtp", lambda *_: server)
    attempt = await client.send_message().invoke(
        make_call(Context()),
        SendMessageInput(to=("jane@example.com",), subject="Hello", text="Body"),
    )
    assert attempt.branch == "sent"
    assert server.sock.timeouts == [90]
