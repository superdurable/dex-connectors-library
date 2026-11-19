# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Gmail's provider boundary honors persisted single-dispatch decisions."""

import asyncio
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
from dex_connectors.sdk.projectconfig import (
    ConnectionNotSelectedError,
    CredentialBrokerRejectedError,
    CredentialBrokerUnavailableError,
    ObjectNotFoundError,
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
from dex_connectors.google_gmail.project_credentials import StoredCredentialMaterialError
from dex_connectors.google_gmail.provider import GmailCredentialMaterialError


def sdk_wrapped_material_error() -> ValueError:
    """Build the ValueError the SDK project provider raises after the decoder refuses material."""
    try:
        try:
            raise StoredCredentialMaterialError("Gmail credential fields are invalid")
        except StoredCredentialMaterialError:
            raise ValueError("project credential decoding failed") from None
    except ValueError as error:
        return error
    raise AssertionError("unreachable")


class HeartbeatContext:
    """Expose only Dex's public attempt and persisted heartbeat surface."""

    def __init__(self, attempt: int = 1, previous: object | None = None) -> None:
        self.attempt = attempt
        self.previous = previous
        self.read_error: Exception | None = None
        self.call_id: CallID | None = None
        self.run_id = "run-1"
        self.flow_started_at = datetime(2026, 3, 4, 9, 15, tzinfo=UTC)
        self.deadline = datetime.now(UTC) + timedelta(seconds=30)
        self.heartbeat_values: list[object] = []

    @property
    def heartbeats(self) -> list[Marker]:
        """Decode this call's marker from each heartbeat, whatever checkpoint format the SDK wrote."""
        return [_own_marker(value, self.call_id) for value in self.heartbeat_values]

    def has_last_heartbeat_value(self) -> bool:
        """Report whether a prior attempt left a checkpoint."""
        return self.previous is not None or self.read_error is not None

    def get_last_heartbeat_value(self, value_type: type[object]) -> object:
        """Return the seeded prior marker in the checkpoint format the SDK reads."""
        if self.read_error is not None:
            raise self.read_error
        if isinstance(self.previous, Marker) and value_type is not Marker:
            return {"singleDispatchMarkers": {str(self.previous.call_id): self.previous.to_wire()}}
        return self.previous

    async def heartbeat(self, value: object) -> None:
        """Capture the SDK's non-secret heartbeat exactly as written."""
        self.heartbeat_values.append(value)


def _own_marker(value: object, call_id: CallID | None) -> Marker:
    """Decode a single marker or this call's checkpoint-map entry."""
    if isinstance(value, Marker):
        return value
    assert isinstance(value, dict) and call_id is not None
    return Marker.from_wire(value["singleDispatchMarkers"][str(call_id)])


def _call(
    context: HeartbeatContext, call_id: CallID | None = None, operation_id: str = "sendMessage",
    max_sends: int | None = None,
) -> Call:
    """Build the Call a MutationStep passes, with the definition's send cap unless overridden."""
    call_id = call_id or CallID(str(uuid4()))
    context.call_id = call_id
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
    call: Call, response: httpx.MockTransport, credential_error: Exception | None = None,
    message_input: SendMessageInput | ReplyToMessageInput | None = None,
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
        if call.operation.operation_id == "sendMessage":
            send_input = message_input or SendMessageInput(["ada@example.com"], "Progress", "Update")
            assert isinstance(send_input, SendMessageInput)
            attempt = await SendMessageOperation(provider).invoke(call, send_input)
        else:
            reply_input = message_input or ReplyToMessageInput("source-1", "Answer")
            assert isinstance(reply_input, ReplyToMessageInput)
            attempt = await ReplyToMessageOperation(provider).invoke(call, reply_input)
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
    """At the definition's send cap a Sent Mail miss selects uncertain without another POST."""
    context = HeartbeatContext(4)
    call = _call(context, operation_id=operation_id)
    assert call.max_sends == 3
    context.previous = Marker(call.id, 3, State.CLAIMED, send_count=call.max_sends)
    attempt, requests = await _invoke(call, httpx.MockTransport(_gmail(
        httpx.Response(200, json={"messages": []}), httpx.Response(200, json={"id": "sent-4"}),
    )))
    assert attempt.kind == "uncertain" and attempt.failure is not None
    assert "no further" in attempt.failure.message
    assert _methods(requests) == ["GET"]
    assert context.heartbeats == []


@pytest.mark.asyncio
@pytest.mark.parametrize("operation_id", ["sendMessage", "replyToMessage"])
async def test_send_cap_reached_read_back_match_is_sent(operation_id: str) -> None:
    """At the send cap a Sent Mail match still selects sent without another POST."""
    context = HeartbeatContext(4)
    call = _call(context, operation_id=operation_id)
    context.previous = Marker(call.id, 3, State.CLAIMED, send_count=call.max_sends)
    attempt, requests = await _invoke(call, httpx.MockTransport(
        lambda _: httpx.Response(200, json={"messages": [{"id": "sent-3", "threadId": "thread-1"}]})
    ))
    assert attempt.branch == "sent" and attempt.value.message_id == "sent-3"
    assert "POST" not in _methods(requests)


@pytest.mark.asyncio
@pytest.mark.parametrize("operation_id", ["sendMessage", "replyToMessage"])
async def test_final_send_unconfirmed_selects_uncertain(operation_id: str) -> None:
    """The final send the cap allows selects uncertain when Sent Mail cannot confirm it."""
    context = HeartbeatContext(3)
    call = _call(context, operation_id=operation_id)
    context.previous = Marker(call.id, 2, State.CLAIMED, send_count=2)

    def answer(request: httpx.Request) -> httpx.Response:
        if request.method == "POST":
            return httpx.Response(500)
        if request.url.path.endswith("/messages/source-1"):
            return _source_message()
        return httpx.Response(200, json={"messages": []})

    attempt, requests = await _invoke(call, httpx.MockTransport(answer))
    assert attempt.kind == "uncertain" and attempt.failure is not None
    assert "the sent-mail search found no message" in attempt.failure.message
    assert _methods(requests).count("POST") == 1
    assert context.heartbeats[-1].state == State.CLAIMED
    assert context.heartbeats[-1].send_count == 3


@pytest.mark.asyncio
@pytest.mark.parametrize("operation_id", ["sendMessage", "replyToMessage"])
async def test_unconfirmed_send_below_the_cap_reads_back_then_resends(operation_id: str) -> None:
    """Below the cap an unconfirmed earlier send is read back, resent, and an unknown result retries."""
    context = HeartbeatContext(2)
    call = _call(context, operation_id=operation_id)
    context.previous = Marker(call.id, 1, State.CLAIMED, send_count=1)

    def answer(request: httpx.Request) -> httpx.Response:
        if request.method == "POST":
            return httpx.Response(500)
        if request.url.path.endswith("/messages/source-1"):
            return _source_message()
        return httpx.Response(200, json={"messages": []})

    attempt, requests = await _invoke(call, httpx.MockTransport(answer))
    assert attempt.kind == "retry"
    assert _methods(requests)[0] == "GET"
    assert _methods(requests).count("POST") == 1
    assert context.heartbeats[-1].send_count == 2


@pytest.mark.asyncio
async def test_send_cap_override_of_one_never_resends() -> None:
    """An application cap of one reads back only after a claim and selects uncertain on a miss."""
    context = HeartbeatContext(2)
    call = _call(context, max_sends=1)
    context.previous = Marker(call.id, 1, State.CLAIMED)
    attempt, requests = await _invoke(
        call, httpx.MockTransport(lambda _: httpx.Response(200, json={"messages": []}))
    )
    assert attempt.kind == "uncertain"
    assert [request.method for request in requests] == ["GET"]
    assert context.heartbeats == []


@pytest.mark.asyncio
async def test_send_cap_override_of_one_selects_uncertain_after_the_first_unknown_send() -> None:
    """An application cap of one selects uncertain when its only send stays unconfirmed."""
    context = HeartbeatContext(1)
    call = _call(context, max_sends=1)
    attempt, requests = await _invoke(call, httpx.MockTransport(
        lambda request: (
            httpx.Response(500) if request.method == "POST" else httpx.Response(200, json={"messages": []})
        )
    ))
    assert attempt.kind == "uncertain"
    assert _methods(requests).count("POST") == 1


@pytest.mark.asyncio
@pytest.mark.parametrize("operation_id", ["sendMessage", "replyToMessage"])
async def test_saved_provider_id_is_never_resent(operation_id: str) -> None:
    """A saved provider ID proves acceptance, so a Sent Mail miss selects uncertain without a POST."""
    context = HeartbeatContext(2)
    call = _call(context, operation_id=operation_id)
    context.previous = Marker(call.id, 1, State.CLAIMED, provider_id="sent-1", send_count=1)
    attempt, requests = await _invoke(call, httpx.MockTransport(_gmail(
        httpx.Response(200, json={"messages": []}), httpx.Response(200, json={"id": "sent-2"}),
    )))
    assert attempt.kind == "uncertain"
    assert "POST" not in _methods(requests)


@pytest.mark.asyncio
@pytest.mark.parametrize("operation_id", ["sendMessage", "replyToMessage"])
async def test_rejected_resend_keeps_the_earlier_send_unknown(operation_id: str) -> None:
    """A definite 4xx on a resend selects uncertain because the earlier send may still be delivered."""
    context = HeartbeatContext(2)
    call = _call(context, operation_id=operation_id)
    context.previous = Marker(call.id, 1, State.CLAIMED, send_count=1)
    attempt, requests = await _invoke(call, httpx.MockTransport(_gmail(
        httpx.Response(200, json={"messages": []}), httpx.Response(400),
    )))
    assert attempt.kind == "uncertain" and attempt.failure is not None
    assert attempt.failure.kind.value == "PROVIDER_REJECTION"
    assert "an earlier send may still be delivered" in attempt.failure.message
    assert _methods(requests).count("POST") == 1
    assert context.heartbeats[-1].state == State.CLAIMED
    assert context.heartbeats[-1].send_count == 2


@pytest.mark.asyncio
@pytest.mark.parametrize("operation_id", ["sendMessage", "replyToMessage"])
@pytest.mark.parametrize(("prior_sends", "expected_kind"), [(1, "retry"), (2, "uncertain")])
async def test_indefinite_resend_refusal_retries_below_the_cap(
    operation_id: str, prior_sends: int, expected_kind: str
) -> None:
    """A 409 on a resend is an unknown outcome: Retry below the cap, uncertain on the final send."""
    context = HeartbeatContext(prior_sends + 1)
    call = _call(context, operation_id=operation_id)
    context.previous = Marker(call.id, prior_sends, State.CLAIMED, send_count=prior_sends)

    def answer(request: httpx.Request) -> httpx.Response:
        if request.method == "POST":
            return httpx.Response(409)
        if request.url.path.endswith("/messages/source-1"):
            return _source_message()
        return httpx.Response(200, json={"messages": []})

    attempt, requests = await _invoke(call, httpx.MockTransport(answer))
    assert attempt.kind == expected_kind
    assert _methods(requests).count("POST") == 1


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
@pytest.mark.parametrize("status", [408, 409, 418])
async def test_unproven_refusal_on_first_send_retries_unknown(operation_id: str, status: int) -> None:
    """A 4xx outside the refusal allowlist leaves the outcome unknown, so Retry keeps the claim."""
    context = HeartbeatContext()
    attempt, _ = await _invoke(
        _call(context, operation_id=operation_id),
        httpx.MockTransport(_gmail(httpx.Response(200, json={"messages": []}), httpx.Response(status))),
    )
    assert attempt.kind == "retry" and attempt.failure is not None
    assert "Gmail may still have processed the request" in attempt.failure.message
    assert context.heartbeats[-1].state == State.CLAIMED
    assert State.CAN_RESEND not in [value.state for value in context.heartbeats]


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
async def test_reply_source_missing_after_unconfirmed_send_is_uncertain() -> None:
    """A definite source 404 after a possible earlier reply selects uncertain."""
    context = HeartbeatContext(2)
    call = _call(context, operation_id="replyToMessage")
    context.previous = Marker(call.id, 1, State.CLAIMED, send_count=1)
    attempt, requests = await _invoke(call, httpx.MockTransport(_missing_source))
    assert attempt.kind == "uncertain" and attempt.failure is not None
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
async def test_unreadable_checkpoint_fails_before_any_provider_request() -> None:
    """A heartbeat read error fails closed, retries, and cannot reach Gmail."""
    context = HeartbeatContext(2)
    call = _call(context)
    context.read_error = RuntimeError("heartbeat store unavailable")
    attempt, requests = await _invoke(call, httpx.MockTransport(lambda _: httpx.Response(200)))
    assert attempt.kind == "retry" and attempt.failure is not None
    assert "an earlier send may still be delivered" in attempt.failure.message
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
async def test_permanent_credential_failure_after_possible_send_selects_uncertain(
    operation_id: str, error: Exception
) -> None:
    """Reauthorization or invalid material after a possible send selects uncertain, never defect."""
    context = HeartbeatContext(2)
    call = _call(context, operation_id=operation_id)
    context.previous = Marker(call.id, 1, State.CLAIMED, send_count=1)
    attempt, _ = await _invoke(call, httpx.MockTransport(lambda _: httpx.Response(200)), error)
    assert attempt.kind == "uncertain" and attempt.failure is not None
    assert "need reconnection" in attempt.failure.message
    assert context.heartbeats == []


@pytest.mark.asyncio
@pytest.mark.parametrize("operation_id", ["sendMessage", "replyToMessage"])
async def test_invalid_input_after_possible_send_is_uncertain(operation_id: str) -> None:
    """Invalid input after a possible send selects uncertain instead of defect."""
    context = HeartbeatContext(2)
    call = _call(context, operation_id=operation_id)
    context.previous = Marker(call.id, 1, State.CLAIMED, send_count=1)
    invalid_input = (
        SendMessageInput(to=[], subject="", text_body="")
        if operation_id == "sendMessage"
        else ReplyToMessageInput(message_id="", text_body="")
    )
    attempt, requests = await _invoke(
        call,
        httpx.MockTransport(lambda _: httpx.Response(200, json={"messages": []})),
        message_input=invalid_input,
    )
    assert attempt.kind == "uncertain" and attempt.failure is not None
    assert "an earlier send may still be delivered" in attempt.failure.message
    assert "POST" not in [request.method for request in requests]


@pytest.mark.asyncio
@pytest.mark.parametrize("operation_id", ["sendMessage", "replyToMessage"])
async def test_refusal_naming_message_is_read_back_by_id(operation_id: str) -> None:
    """A 400 body that names a Gmail message contradicts the refusal, so the ID is read back."""
    context = HeartbeatContext()

    def answer(request: httpx.Request) -> httpx.Response:
        if request.method == "POST":
            return httpx.Response(400, json={"id": "sent-1", "threadId": "thread-1"})
        if request.url.path.endswith("/messages/source-1"):
            return _source_message()
        if request.url.path.endswith("/messages/sent-1"):
            return httpx.Response(200, json={"id": "sent-1", "threadId": "thread-1"})
        return httpx.Response(200, json={"messages": []})

    attempt, requests = await _invoke(_call(context, operation_id=operation_id), httpx.MockTransport(answer))
    assert attempt.branch == "sent" and attempt.value.message_id == "sent-1"
    assert attempt.value.thread_id == "thread-1"
    assert _methods(requests) == ["POST", "GET"]
    assert context.heartbeats[-1].provider_id == "sent-1"


@pytest.mark.asyncio
@pytest.mark.parametrize("operation_id", ["sendMessage", "replyToMessage"])
async def test_unconfirmed_refusal_naming_message_is_never_resent(operation_id: str) -> None:
    """A named message that read-back cannot confirm is never resent; a later miss is uncertain."""
    context = HeartbeatContext()
    call = _call(context, operation_id=operation_id)

    def answer(request: httpx.Request) -> httpx.Response:
        if request.method == "POST":
            return httpx.Response(400, json={"id": "sent-1"})
        if request.url.path.endswith("/messages/source-1"):
            return _source_message()
        if request.url.path.endswith("/messages/sent-1"):
            return httpx.Response(404)
        return httpx.Response(200, json={"messages": []})

    attempt, requests = await _invoke(call, httpx.MockTransport(answer))
    assert attempt.kind == "retry"
    assert context.heartbeats[-1].provider_id == "sent-1"
    context.attempt, context.previous = 2, context.heartbeats[-1]
    attempt, later_requests = await _invoke(call, httpx.MockTransport(answer))
    assert attempt.kind == "uncertain"
    assert [request.method for request in requests + later_requests].count("POST") == 1


@pytest.mark.asyncio
@pytest.mark.parametrize("operation_id", ["sendMessage", "replyToMessage"])
@pytest.mark.parametrize(
    ("status", "body", "expected_kind", "expected_branch"),
    [
        (200, {"id": "sent-1", "threadId": 7}, "branch", "sent"),
        (400, {"id": "sent-1", "threadId": 7}, "branch", "sent"),
        (400, {"id": 7}, "branch", "providerRejected"),
    ],
)
async def test_message_id_check_ignores_other_field_types(
    operation_id: str, status: int, body: dict[str, object], expected_kind: str, expected_branch: str
) -> None:
    """A body names a Gmail message exactly when its id is a non-empty string."""
    context = HeartbeatContext()

    def answer(request: httpx.Request) -> httpx.Response:
        if request.method == "POST":
            return httpx.Response(status, json=body)
        if request.url.path.endswith("/messages/source-1"):
            return _source_message()
        if request.url.params.get("format") == "minimal":
            return httpx.Response(200, json={"id": "sent-1", "threadId": "thread-1"})
        return httpx.Response(200, json={"messages": []})

    attempt, requests = await _invoke(_call(context, operation_id=operation_id), httpx.MockTransport(answer))
    assert attempt.kind == expected_kind
    if expected_branch:
        assert attempt.branch == expected_branch
    if expected_branch == "sent":
        assert attempt.value.message_id == "sent-1"
        assert not [request for request in requests if request.url.params.get("q")]


@pytest.mark.asyncio
@pytest.mark.parametrize("operation_id", ["sendMessage", "replyToMessage"])
async def test_counted_can_resend_searches_before_resending(operation_id: str) -> None:
    """A canResend marker that still counts an earlier send reads back before any POST."""
    context = HeartbeatContext(3)
    call = _call(context, operation_id=operation_id)
    context.previous = Marker(call.id, 2, State.CAN_RESEND, send_count=1)
    attempt, requests = await _invoke(call, httpx.MockTransport(_gmail(
        httpx.Response(200, json={"messages": [{"id": "sent-1", "threadId": "thread-1"}]}),
        httpx.Response(200, json={"id": "sent-2", "threadId": "thread-1"}),
    )))
    assert attempt.branch == "sent" and attempt.value.message_id == "sent-1"
    assert "POST" not in _methods(requests)


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("operation_id", "failure"),
    [("sendMessage", "search"), ("replyToMessage", "search"), ("replyToMessage", "source_read")],
)
async def test_pre_send_retry_after_counted_can_resend_records_nothing(
    operation_id: str, failure: str
) -> None:
    """notSent would hide the counted earlier send from the next attempt."""
    context = HeartbeatContext(3)
    call = _call(context, operation_id=operation_id)
    context.previous = Marker(call.id, 2, State.CAN_RESEND, send_count=1)

    def answer(request: httpx.Request) -> httpx.Response:
        if request.method == "POST":
            return httpx.Response(200, json={"id": "sent-2", "threadId": "thread-1"})
        if request.url.path.endswith("/messages/source-1"):
            return httpx.Response(500) if failure == "source_read" else _source_message()
        if failure == "search":
            return httpx.Response(500)
        return httpx.Response(200, json={"messages": []})

    attempt, requests = await _invoke(call, httpx.MockTransport(answer))
    assert attempt.kind == "retry"
    assert "POST" not in [request.method for request in requests]
    assert context.heartbeats == []


class _SlowAcceptedBody(httpx.AsyncByteStream):
    """Send 2xx headers, then hold the body past the write window."""

    async def __aiter__(self):  # type: ignore[no-untyped-def]
        await asyncio.sleep(30)
        yield b"{}"


@pytest.mark.asyncio
@pytest.mark.parametrize("operation_id", ["sendMessage", "replyToMessage"])
async def test_deadline_after_accepted_headers_is_never_resent(operation_id: str) -> None:
    """A write window that ends after Gmail answered 2xx still means the send was accepted."""
    context = HeartbeatContext()
    context.deadline = datetime.now(UTC) + timedelta(seconds=3)

    def answer(request: httpx.Request) -> httpx.Response:
        if request.method == "POST":
            return httpx.Response(200, stream=_SlowAcceptedBody())
        if request.url.path.endswith("/messages/source-1"):
            return _source_message()
        return httpx.Response(200, json={"messages": []})

    call = _call(context, operation_id=operation_id)
    attempt, requests = await _invoke(call, httpx.MockTransport(answer))
    assert attempt.branch == "sent"
    assert [request.method for request in requests].count("POST") == 1
    assert context.heartbeats[-1].state == State.CLAIMED


@pytest.mark.asyncio
@pytest.mark.parametrize("operation_id", ["sendMessage", "replyToMessage"])
@pytest.mark.parametrize(
    "error",
    [
        RuntimeError("store unavailable"),
        CredentialBrokerUnavailableError(),
        TimeoutError(),
        ValueError("connection is not configured"),
    ],
)
async def test_transient_credential_failure_on_first_attempt_retries(
    operation_id: str, error: Exception
) -> None:
    """A clean first attempt records notSent and retries a transient credential failure."""
    context = HeartbeatContext()
    attempt, requests = await _invoke(
        _call(context, operation_id=operation_id), httpx.MockTransport(lambda _: httpx.Response(200)), error
    )
    assert attempt.kind == "retry"
    assert requests == []
    assert [value.state for value in context.heartbeats] == [State.NOT_SENT]


@pytest.mark.asyncio
@pytest.mark.parametrize("operation_id", ["sendMessage", "replyToMessage"])
@pytest.mark.parametrize(
    "error",
    [
        ReauthorizationRequiredError(),
        GmailCredentialMaterialError("invalid"),
        sdk_wrapped_material_error(),
        ObjectNotFoundError(),
        ConnectionNotSelectedError(),
        CredentialBrokerRejectedError(),
    ],
)
async def test_permanent_credential_failure_on_first_attempt_is_defect(
    operation_id: str, error: Exception
) -> None:
    """A clean first attempt selects defect when only reconnection repairs the credentials."""
    attempt, requests = await _invoke(
        _call(HeartbeatContext(), operation_id=operation_id),
        httpx.MockTransport(lambda _: httpx.Response(200)),
        error,
    )
    assert attempt.branch == "defect"
    assert requests == []
