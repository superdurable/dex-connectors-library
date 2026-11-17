# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""SMTP outcomes mirror the Go in-process mailtest scenarios."""

import json
from dataclasses import dataclass, field
from typing import Any, cast

import dex
import pytest
from dex_connectors.sdk import Call, CallID, ConnectionRef, IdempotencyKey, OperationRef, SecretString

from dex_connectors.superdurable_email import Config, Credentials
from dex_connectors.superdurable_email._generated.definitions import SEND_MESSAGE_DEFINITION
from dex_connectors.superdurable_email.client import Client
from dex_connectors.superdurable_email.models import SendMessageInput
from dex_connectors.superdurable_email.operations import (
    _decide_submission_from_marker,
    _SubmissionMarker,
)

CALL_ID = "00000000-0000-4000-8000-000000000001"


class CredentialsProvider:
    """Resolve one inert test credential."""

    async def resolve(self, call: Call) -> Credentials:
        """Return a valid mailbox credential for each attempt."""
        return Credentials("agent@example.com", SecretString("inert"), "", SecretString(""))


@dataclass
class Context:
    """Keep the last heartbeat across Execute attempts like Dex does."""

    attempt: int = 1
    marker: object | None = None
    visible_marker: object | None = None
    heartbeats: list[object | None] = field(default_factory=list)
    has_recorded_this_attempt: bool = False

    def has_last_heartbeat_value(self) -> bool:
        """Report a submission marker an earlier attempt recorded."""
        return self.visible_marker is not None

    def get_last_heartbeat_value(self, value_type: type[object]) -> object:
        """Return the marker an earlier attempt recorded."""
        return self.visible_marker

    async def heartbeat(self, value: object = ...) -> None:
        """Record or clear the submission marker."""
        self.marker = None if value is ... else value
        self.heartbeats.append(self.marker)
        self.has_recorded_this_attempt = True

    def next_attempt(self) -> None:
        """Start the following attempt, which sees the last persisted heartbeat."""
        self.attempt += 1
        if self.has_recorded_this_attempt:
            self.visible_marker = self.marker
        self.has_recorded_this_attempt = False


class SMTPServer:
    """Observe SMTP submissions without a network port; one instance serves every attempt."""

    def __init__(
        self, final_code: int = 250, lose_final_answer: bool = False, lost_final_answers: int = 0
    ) -> None:
        self.esmtp_features = {"size": "1000000"}
        self.final_code = final_code
        # lost_final_answers drops that many final-dot answers, then answers final_code.
        self.lost_final_answers = lost_final_answers or (1_000 if lose_final_answer else 0)
        self.commands: list[str] = []
        self.content = b""
        self.submissions = 0
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
        self.replies = 0

    def getreply(self) -> tuple[int, bytes]:
        """Accept DATA or return the selected final answer."""
        self.replies += 1
        if self.replies == 1:
            return 354, b"go ahead"
        self.submissions += 1
        if self.lost_final_answers > 0:
            self.lost_final_answers -= 1
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
        max_sends=SEND_MESSAGE_DEFINITION.max_sends,
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
    assert context.heartbeats == [
        _SubmissionMarker(CALL_ID, 1, 1),
        _SubmissionMarker(CALL_ID, 1, 1, True),
    ], "the claim before the message and the acceptance after it are recorded"
    context.next_attempt()
    replayed = await client.send_message().invoke(
        make_call(context), SendMessageInput(to=("jane@example.com",), subject="Hello", text="Body")
    )
    assert replayed.kind == "uncertain", "a replay after an accepted submission never resubmits"
    assert server.submissions == 1


@pytest.mark.asyncio
async def test_send_message_temporary_refusal_is_retried_without_counting_it(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """A final temporary SMTP reply proves nonacceptance, so it does not use up the send cap."""
    client = make_client()
    server = SMTPServer(final_code=451)
    monkeypatch.setattr(client, "open_smtp", lambda *_: server)
    context = Context()
    input = SendMessageInput(to=("jane@example.com",), subject="Hello", text="Body")
    for attempt_number in range(1, 4):
        attempt = await client.send_message().invoke(make_call(context), input)
        assert attempt.kind == "retry"
        assert context.heartbeats[-1] == _SubmissionMarker(CALL_ID, attempt_number, 0)
        context.next_attempt()
    server.final_code = 250
    attempt = await client.send_message().invoke(make_call(context), input)
    assert attempt.kind == "branch" and attempt.branch == "sent", "three refusals leave the whole send cap"


@pytest.mark.asyncio
async def test_send_message_lost_final_answer_is_resent_until_the_send_cap(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """A lost final-dot answer is resent with the same Message-ID while the send cap allows."""
    client = make_client()
    server = SMTPServer(lose_final_answer=True)
    monkeypatch.setattr(client, "open_smtp", lambda *_: server)
    context = Context()
    input = SendMessageInput(to=("jane@example.com",), subject="Hello", text="Body")
    for submission in range(1, SEND_MESSAGE_DEFINITION.max_sends):
        attempt = await client.send_message().invoke(make_call(context), input)
        assert attempt.kind == "retry"
        assert context.marker == _SubmissionMarker(CALL_ID, submission, submission)
        context.next_attempt()
    last = await client.send_message().invoke(make_call(context), input)
    assert last.kind == "uncertain", "the last submission the cap allows is not repeated"
    context.next_attempt()
    replayed = await client.send_message().invoke(make_call(context), input)
    assert replayed.kind == "uncertain"
    assert server.submissions == SEND_MESSAGE_DEFINITION.max_sends
    assert server.content.count(b"Message-ID: <dex-" + CALL_ID.encode()) == SEND_MESSAGE_DEFINITION.max_sends


@pytest.mark.asyncio
async def test_send_message_refusal_after_an_earlier_possible_submission_is_uncertain(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """A refusal of a resubmission cannot prove the first one was not accepted."""
    client = make_client()
    server = SMTPServer(final_code=550, lost_final_answers=1)
    monkeypatch.setattr(client, "open_smtp", lambda *_: server)
    context = Context()
    input = SendMessageInput(to=("jane@example.com",), subject="Hello", text="Body")
    assert (await client.send_message().invoke(make_call(context), input)).kind == "retry"
    context.next_attempt()
    assert (await client.send_message().invoke(make_call(context), input)).kind == "uncertain"


@pytest.mark.asyncio
async def test_send_message_retries_after_a_foreign_checkpoint_without_connecting(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """A checkpoint of another Call ID is untrusted, so nothing is submitted."""
    client = make_client()
    monkeypatch.setattr(client, "open_smtp", lambda *_: pytest.fail("connected"))
    context = Context(attempt=2, visible_marker=_SubmissionMarker("earlier", 1, 1))
    attempt = await client.send_message().invoke(
        make_call(context), SendMessageInput(to=("jane@example.com",), subject="Hello", text="Body")
    )
    assert attempt.kind == "retry"


@pytest.mark.parametrize(
    ("marker", "attempt", "expected_kind", "expected_bound"),
    [
        (None, 1, "allowed", 0),
        (None, 3, "allowed", 2),
        (_SubmissionMarker(CALL_ID), 2, "allowed", 1),
        (_SubmissionMarker("other", 1, 1), 2, "untrusted", None),
        (_SubmissionMarker(CALL_ID, 1, 1), 3, "allowed", 2),
        (_SubmissionMarker(CALL_ID, 1, 0), 2, "allowed", 0),
        (_SubmissionMarker(CALL_ID, 2, 1), 2, "untrusted", None),
        (_SubmissionMarker(CALL_ID, 1, 2), 2, "untrusted", None),
        (_SubmissionMarker(CALL_ID, 1, 1, True), 2, "accepted", None),
        (_SubmissionMarker(CALL_ID, 3, 3), 4, "capReached", 3),
    ],
)
def test_decide_submission_matches_the_go_decision_table(
    marker: _SubmissionMarker | None, attempt: int, expected_kind: str, expected_bound: int | None
) -> None:
    """Repeat Go's TestDecideSubmissionFromMarker cases."""
    decision = _decide_submission_from_marker(CALL_ID, attempt, 3, marker)
    assert decision.kind == expected_kind
    if expected_bound is not None:
        assert decision.maximum_prior_submissions == expected_bound


def test_submission_marker_wire_form_matches_go() -> None:
    """The Dex codec writes the same keys as Go and omits zero values."""
    codec = dex.CodecRegistry().resolve(_SubmissionMarker)
    for marker, expected in (
        (
            _SubmissionMarker("call", 2, 2),
            {"emailSubmittedCallId": "call", "emailAttempt": 2, "emailSubmissionCount": 2},
        ),
        (_SubmissionMarker("call", 1, 0), {"emailSubmittedCallId": "call", "emailAttempt": 1}),
        (
            _SubmissionMarker("call", 1, 1, True),
            {
                "emailSubmittedCallId": "call",
                "emailAttempt": 1,
                "emailSubmissionCount": 1,
                "emailAccepted": True,
            },
        ),
    ):
        assert json.loads(codec.encode(marker).data) == expected
    legacy = codec.decode(dex.Value(dex.WireKind.JSON, '{"emailSubmittedCallId":"call"}'))
    assert legacy == _SubmissionMarker("call")


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
