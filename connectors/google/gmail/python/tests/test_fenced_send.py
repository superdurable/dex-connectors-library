# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Gmail's provider boundary honors persisted single-dispatch decisions."""

import base64
import json
from collections.abc import Callable
from datetime import UTC, datetime, timedelta
from typing import cast
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import Call, CallID, ConnectionRef, IdempotencyKey, MutationAttempt, OperationRef
from dex_connectors.sdk.credentials import (
    ReauthorizationRequiredError,
    SecretString,
    StaticCredentialProvider,
)
from dex_connectors.sdk.singledispatch import Marker, State

from dex_connectors.google_gmail import (
    Config,
    Credentials,
    GmailProvider,
    ReplyToMessageInput,
    SendMessageInput,
    SendMessageOutput,
)
from dex_connectors.google_gmail.definitions import (
    REPLY_TO_MESSAGE_DEFINITION,
    SEND_MESSAGE_DEFINITION,
)
from dex_connectors.google_gmail.operations import ReplyToMessageOperation, SendMessageOperation
from dex_connectors.google_gmail.provider import GmailCredentialMaterialError


class HeartbeatContext:
    """Expose only Dex's public attempt and persisted heartbeat surface."""

    def __init__(self, attempt: int = 1, previous: object | None = None) -> None:
        self.attempt = attempt
        self.previous = previous
        self.run_id = "run-1"
        self.flow_started_at = datetime(2026, 3, 4, 9, 15, tzinfo=UTC)
        self.deadline = datetime.now(UTC) + timedelta(seconds=30)
        self.heartbeats: list[Marker] = []

    def has_last_heartbeat_value(self) -> bool:
        """Report whether a prior attempt left a checkpoint."""
        return self.previous is not None

    def get_last_heartbeat_value(self, value_type: type[Marker]) -> object:
        """Return the inert prior marker to the SDK fence."""
        return self.previous

    async def heartbeat(self, value: object) -> None:
        """Capture the SDK's non-secret dispatch marker."""
        assert isinstance(value, Marker)
        self.heartbeats.append(value)


def _call(
    context: HeartbeatContext, call_id: CallID | None = None, operation_id: str = "sendMessage",
    max_sends: int | None = None,
) -> Call:
    """Build the Call a MutationStep passes, with the definition's send cap unless overridden."""
    call_id = call_id or CallID(str(uuid4()))
    definition = SEND_MESSAGE_DEFINITION if operation_id == "sendMessage" else REPLY_TO_MESSAGE_DEFINITION
    return Call(
        call_id, ConnectionRef("google", "support"), OperationRef("gmail", operation_id),
        IdempotencyKey(call_id), context=cast(dex.AsyncContext, context),
        max_sends=definition.max_sends if max_sends is None else max_sends,
    )


def _source_message() -> httpx.Response:
    return httpx.Response(200, json={
        "id": "source-1", "threadId": "thread-1", "internalDate": "1772615700000",
        "payload": {"headers": [
            {"name": "From", "value": "ada@example.com"},
            {"name": "Message-ID", "value": "<source@example.com>"},
            {"name": "Subject", "value": "Question"},
        ]},
    })


def _message_id_header(request: httpx.Request) -> str:
    raw = json.loads(request.content)["raw"]
    mime = base64.urlsafe_b64decode(raw + "=" * (-len(raw) % 4)).decode()
    return next(line for line in mime.split("\r\n") if line.startswith("Message-ID: "))


class FailingCredentialProvider:
    """Fail every lookup with one error, as an unavailable or revoked credential store does."""

    def __init__(self, error: Exception) -> None:
        self.error = error

    async def resolve(self, call: Call) -> Credentials:
        """Raise the configured credential failure."""
        raise self.error


async def _invoke(
    call: Call, response: httpx.MockTransport, credential_error: Exception | None = None
) -> tuple[MutationAttempt[SendMessageOutput], list[httpx.Request]]:
    requests: list[httpx.Request] = []

    async def observe(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return await response.handle_async_request(request)

    credentials: StaticCredentialProvider[Credentials] | FailingCredentialProvider = (
        StaticCredentialProvider({
            call.connection: Credentials(SecretString("inert-token"), "owner@example.org")
        })
        if credential_error is None
        else FailingCredentialProvider(credential_error)
    )
    async with httpx.AsyncClient(transport=httpx.MockTransport(observe)) as http_client:
        provider = GmailProvider(
            Config(endpoint="http://localhost/gmail/v1"), credentials, http_client=http_client
        )
        attempt = (
            await SendMessageOperation(provider).invoke(
                call, SendMessageInput(["ada@example.com"], "Progress", "Update")
            )
            if call.operation.operation_id == "sendMessage"
            else await ReplyToMessageOperation(provider).invoke(
                call, ReplyToMessageInput("source-1", "Answer")
            )
        )
    return attempt, requests


@pytest.mark.asyncio
@pytest.mark.parametrize("operation_id", ["sendMessage", "replyToMessage"])
async def test_operation_accepts_context_without_flow_start(operation_id: str) -> None:
    """The public Call context may omit Flow start time for agent tool Steps."""
    context = HeartbeatContext()
    del context.flow_started_at
    call = _call(context, operation_id=operation_id)

    def answer(request: httpx.Request) -> httpx.Response:
        if request.method == "POST":
            return httpx.Response(200, json={"id": "sent-1", "threadId": "thread-1"})
        return _source_message()

    attempt, requests = await _invoke(call, httpx.MockTransport(answer))
    assert attempt.branch == "sent"
    assert any(request.method == "POST" for request in requests)


@pytest.mark.asyncio
@pytest.mark.parametrize("operation_id", ["sendMessage", "replyToMessage"])
async def test_operation_reports_missing_context_run_id_without_http(operation_id: str) -> None:
    """An omitted run ID reaches the clear no-send defect through the operation entrypoint."""
    context = HeartbeatContext()
    del context.run_id
    call = _call(context, operation_id=operation_id)
    attempt, requests = await _invoke(
        call, httpx.MockTransport(lambda _: httpx.Response(200, json={}))
    )
    assert attempt.branch == "defect" and attempt.failure is not None
    assert attempt.failure.kind.value == "LOCAL_DEFECT"
    assert attempt.failure.message == "the Dex call context has no run ID, so nothing was sent"
    assert requests == []


def _gmail(
    search: httpx.Response | None, post: httpx.Response
) -> Callable[[httpx.Request], httpx.Response]:
    """Answer Sent Mail search, the reply source read, and messages.send."""

    def answer(request: httpx.Request) -> httpx.Response:
        if request.method == "POST":
            return post
        if request.url.path.endswith("/messages/source-1"):
            return _source_message()
        if search is None:
            raise httpx.ConnectError("Sent Mail search is unavailable", request=request)
        return search

    return answer


def _methods(requests: list[httpx.Request]) -> list[str]:
    return [request.method for request in requests if not request.url.path.endswith("/source-1")]


@pytest.mark.asyncio
@pytest.mark.parametrize("operation_id", ["sendMessage", "replyToMessage"])
async def test_claimed_previous_attempt_resends_after_sent_mail_miss(operation_id: str) -> None:
    """An unconfirmed claim below the send cap reads back, then resends once with the same Message-ID."""
    context = HeartbeatContext(2)
    call = _call(context, operation_id=operation_id)
    context.previous = Marker(call.id, 1, State.CLAIMED, send_count=1)
    attempt, requests = await _invoke(call, httpx.MockTransport(_gmail(
        httpx.Response(200, json={"messages": []}),
        httpx.Response(200, json={"id": "sent-2", "threadId": "thread-1"}),
    )))
    assert attempt.branch == "sent"
    assert _methods(requests) == ["GET", "POST"]
    claim = context.heartbeats[0]
    assert claim.state == State.CLAIMED and claim.send_count == 2
    assert context.heartbeats[-1].provider_id == "sent-2"
    post = next(request for request in requests if request.method == "POST")
    first_attempt_context = HeartbeatContext()
    _, first_requests = await _invoke(
        _call(first_attempt_context, call.id, operation_id),
        httpx.MockTransport(_gmail(None, httpx.Response(200, json={"id": "sent-1"}))),
    )
    first_post = next(request for request in first_requests if request.method == "POST")
    assert _message_id_header(post) == _message_id_header(first_post)


@pytest.mark.asyncio
@pytest.mark.parametrize("operation_id", ["sendMessage", "replyToMessage"])
async def test_claimed_previous_attempt_found_by_read_back_is_not_resent(operation_id: str) -> None:
    """A Sent Mail match confirms the earlier send, so the attempt selects sent without a POST."""
    context = HeartbeatContext(2)
    call = _call(context, operation_id=operation_id)
    context.previous = Marker(call.id, 1, State.CLAIMED, send_count=1)
    attempt, requests = await _invoke(call, httpx.MockTransport(_gmail(
        httpx.Response(200, json={"messages": [{"id": "sent-1", "threadId": "thread-1"}]}),
        httpx.Response(500),
    )))
    assert attempt.branch == "sent"
    assert _methods(requests) == ["GET"]


@pytest.mark.asyncio
@pytest.mark.parametrize("operation_id", ["sendMessage", "replyToMessage"])
async def test_inconclusive_read_back_resends_below_the_cap(operation_id: str) -> None:
    """A failed Sent Mail search below the send cap still permits one resend."""
    context = HeartbeatContext(2)
    call = _call(context, operation_id=operation_id)
    context.previous = Marker(call.id, 1, State.CLAIMED, send_count=1)
    attempt, requests = await _invoke(call, httpx.MockTransport(_gmail(
        httpx.Response(503), httpx.Response(200, json={"id": "sent-2", "threadId": "thread-1"}),
    )))
    assert attempt.branch == "sent"
    assert _methods(requests) == ["GET", "POST"]


@pytest.mark.asyncio
@pytest.mark.parametrize("operation_id", ["sendMessage", "replyToMessage"])
async def test_send_cap_reached_only_reads_back(operation_id: str) -> None:
    """At the definition's send cap a Sent Mail miss retries without another POST."""
    context = HeartbeatContext(4)
    call = _call(context, operation_id=operation_id)
    assert call.max_sends == 3
    context.previous = Marker(call.id, 3, State.CLAIMED, send_count=call.max_sends)
    attempt, requests = await _invoke(call, httpx.MockTransport(_gmail(
        httpx.Response(200, json={"messages": []}), httpx.Response(200, json={"id": "sent-4"}),
    )))
    assert attempt.kind == "retry"
    assert _methods(requests) == ["GET"]
    assert context.heartbeats == []


@pytest.mark.asyncio
async def test_send_cap_override_of_one_never_resends() -> None:
    """An application cap of one keeps the read-back-only behavior after a claim."""
    context = HeartbeatContext(2)
    call = _call(context, max_sends=1)
    context.previous = Marker(call.id, 1, State.CLAIMED)
    attempt, requests = await _invoke(
        call, httpx.MockTransport(lambda _: httpx.Response(200, json={"messages": []}))
    )
    assert attempt.kind == "retry"
    assert [request.method for request in requests] == ["GET"]
    assert context.heartbeats == []


@pytest.mark.asyncio
@pytest.mark.parametrize("operation_id", ["sendMessage", "replyToMessage"])
async def test_saved_provider_id_is_never_resent(operation_id: str) -> None:
    """A saved provider ID proves acceptance, so a Sent Mail miss retries without a POST."""
    context = HeartbeatContext(2)
    call = _call(context, operation_id=operation_id)
    context.previous = Marker(call.id, 1, State.CLAIMED, provider_id="sent-1", send_count=1)
    attempt, requests = await _invoke(call, httpx.MockTransport(_gmail(
        httpx.Response(200, json={"messages": []}), httpx.Response(200, json={"id": "sent-2"}),
    )))
    assert attempt.kind == "retry"
    assert "POST" not in _methods(requests)


@pytest.mark.asyncio
@pytest.mark.parametrize("operation_id", ["sendMessage", "replyToMessage"])
async def test_rejected_resend_keeps_the_earlier_send_unknown(operation_id: str) -> None:
    """A definite 4xx on a resend retries because the earlier send may still be delivered."""
    context = HeartbeatContext(2)
    call = _call(context, operation_id=operation_id)
    context.previous = Marker(call.id, 1, State.CLAIMED, send_count=1)
    attempt, requests = await _invoke(call, httpx.MockTransport(_gmail(
        httpx.Response(200, json={"messages": []}), httpx.Response(400),
    )))
    assert attempt.kind == "retry" and attempt.failure is not None
    assert attempt.failure.kind.value == "PROVIDER_REJECTION"
    assert "an earlier send may still be delivered" in attempt.failure.message
    assert _methods(requests).count("POST") == 1
    assert context.heartbeats[-1].state == State.CLAIMED
    assert context.heartbeats[-1].send_count == 2


@pytest.mark.asyncio
@pytest.mark.parametrize("operation_id", ["sendMessage", "replyToMessage"])
async def test_rejected_first_send_is_provider_rejected(operation_id: str) -> None:
    """A definite 4xx with no earlier send stays providerRejected and proves nothing was sent."""
    context = HeartbeatContext()
    attempt, _ = await _invoke(
        _call(context, operation_id=operation_id),
        httpx.MockTransport(_gmail(httpx.Response(200, json={"messages": []}), httpx.Response(400))),
    )
    assert attempt.branch == "providerRejected"
    assert context.heartbeats[-1].state == State.CAN_RESEND
    assert context.heartbeats[-1].attempt == 1


@pytest.mark.asyncio
@pytest.mark.parametrize("operation_id", ["sendMessage", "replyToMessage"])
async def test_rejected_send_after_can_resend_records_can_resend(operation_id: str) -> None:
    """A 4xx after a proven unsent earlier attempt is still a clean rejection."""
    context = HeartbeatContext(2)
    call = _call(context, operation_id=operation_id)
    context.previous = Marker(call.id, 1, State.CAN_RESEND, send_count=0)
    attempt, requests = await _invoke(call, httpx.MockTransport(_gmail(
        httpx.Response(200, json={"messages": []}), httpx.Response(400),
    )))
    assert attempt.branch == "providerRejected"
    assert _methods(requests).count("POST") == 1
    assert context.heartbeats[-1].state == State.CAN_RESEND
    assert context.heartbeats[-1].attempt == 2


def _missing_source(request: httpx.Request) -> httpx.Response:
    if request.url.path.endswith("/messages/source-1"):
        return httpx.Response(404)
    return httpx.Response(200, json={"messages": []})


@pytest.mark.asyncio
async def test_reply_source_missing_after_unconfirmed_send_retries() -> None:
    """A source 404 after a possible earlier reply keeps that reply unknown."""
    context = HeartbeatContext(2)
    call = _call(context, operation_id="replyToMessage")
    context.previous = Marker(call.id, 1, State.CLAIMED, send_count=1)
    attempt, requests = await _invoke(call, httpx.MockTransport(_missing_source))
    assert attempt.kind == "retry" and attempt.failure is not None
    assert attempt.failure.kind.value == "NOT_FOUND"
    assert "POST" not in [request.method for request in requests]


@pytest.mark.asyncio
async def test_reply_source_missing_on_first_attempt_is_provider_rejected() -> None:
    """A source 404 before any send stays providerRejected."""
    attempt, _ = await _invoke(
        _call(HeartbeatContext(), operation_id="replyToMessage"), httpx.MockTransport(_missing_source)
    )
    assert attempt.branch == "providerRejected"


@pytest.mark.asyncio
async def test_not_sent_previous_attempt_can_post_after_read_back() -> None:
    """A prior notSent checkpoint permits one POST and persists the provider ID."""
    context = HeartbeatContext(2)
    call = _call(context)
    context.previous = Marker(call.id, 1, State.NOT_SENT)

    def answer(request: httpx.Request) -> httpx.Response:
        if request.method == "GET":
            return httpx.Response(200, json={"messages": []})
        return httpx.Response(200, json={"id": "sent-1", "threadId": "thread-1"})

    attempt, requests = await _invoke(call, httpx.MockTransport(answer))
    assert attempt.branch == "sent"
    assert [request.method for request in requests] == ["GET", "POST"]
    assert [value.state for value in context.heartbeats] == [
        State.CLAIMED, State.CLAIMED
    ]
    assert context.heartbeats[-1].provider_id == "sent-1"


@pytest.mark.asyncio
async def test_rate_limit_records_positive_can_resend_proof() -> None:
    """A definite 429 refusal records canResend for the next Dex attempt."""
    context = HeartbeatContext()
    attempt, requests = await _invoke(
        _call(context), httpx.MockTransport(lambda _: httpx.Response(429))
    )
    assert attempt.kind == "retry"
    assert [request.method for request in requests] == ["POST"]
    assert context.heartbeats[-1].state == State.CAN_RESEND


@pytest.mark.asyncio
async def test_service_unavailable_keeps_the_claim_after_sent_mail_miss() -> None:
    """A 503 may have been processed, so a Sent Mail miss keeps Go's no-resend claim."""
    context = HeartbeatContext()
    requests: list[str] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request.method)
        if request.method == "POST":
            return httpx.Response(503)
        return httpx.Response(200, json={"messages": []})

    attempt, _ = await _invoke(_call(context), httpx.MockTransport(answer))
    assert attempt.kind == "retry"
    assert requests == ["POST", "GET", "GET", "GET", "GET"]
    assert context.heartbeats[-1].state == State.CLAIMED


@pytest.mark.asyncio
async def test_foreign_checkpoint_fails_before_any_provider_request() -> None:
    """A foreign heartbeat fails closed and cannot reach Gmail."""
    context = HeartbeatContext(2)
    call = _call(context)
    context.previous = Marker(CallID(str(uuid4())), 1, State.NOT_SENT)
    attempt, requests = await _invoke(call, httpx.MockTransport(lambda _: httpx.Response(200)))
    assert attempt.branch == "defect"
    assert requests == []


@pytest.mark.asyncio
@pytest.mark.parametrize("operation_id", ["sendMessage", "replyToMessage"])
async def test_transient_credential_failure_after_possible_send_retries(operation_id: str) -> None:
    """A possibly delivered send keeps read-back alive through a transient credential failure."""
    context = HeartbeatContext(2)
    call = _call(context, operation_id=operation_id)
    context.previous = Marker(call.id, 1, State.CLAIMED, send_count=1)
    attempt, requests = await _invoke(
        call, httpx.MockTransport(lambda _: httpx.Response(200)), RuntimeError("store unavailable")
    )
    assert attempt.kind == "retry" and attempt.failure is not None
    assert attempt.failure.kind.value == "AUTHENTICATION"
    assert "an earlier send may still be delivered" in attempt.failure.message
    assert requests == []
    assert context.heartbeats == []


@pytest.mark.asyncio
@pytest.mark.parametrize("operation_id", ["sendMessage", "replyToMessage"])
@pytest.mark.parametrize("error", [ReauthorizationRequiredError(), GmailCredentialMaterialError("invalid")])
async def test_permanent_credential_failure_after_possible_send_is_defect(
    operation_id: str, error: Exception
) -> None:
    """Reauthorization or invalid credential material still selects defect."""
    context = HeartbeatContext(2)
    call = _call(context, operation_id=operation_id)
    context.previous = Marker(call.id, 1, State.CLAIMED, send_count=1)
    attempt, _ = await _invoke(call, httpx.MockTransport(lambda _: httpx.Response(200)), error)
    assert attempt.branch == "defect"
    assert context.heartbeats == []


@pytest.mark.asyncio
@pytest.mark.parametrize("operation_id", ["sendMessage", "replyToMessage"])
async def test_transient_credential_failure_on_first_attempt_is_defect(operation_id: str) -> None:
    """A clean first attempt keeps selecting defect on a credential failure."""
    attempt, requests = await _invoke(
        _call(HeartbeatContext(), operation_id=operation_id),
        httpx.MockTransport(lambda _: httpx.Response(200)), RuntimeError("store unavailable"),
    )
    assert attempt.branch == "defect"
    assert requests == []
