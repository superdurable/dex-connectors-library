# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Gmail's provider boundary honors persisted single-dispatch decisions."""

from datetime import UTC, datetime, timedelta
from typing import cast
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import Call, CallID, ConnectionRef, IdempotencyKey, MutationAttempt, OperationRef
from dex_connectors.sdk.credentials import SecretString, StaticCredentialProvider
from dex_connectors.sdk.singledispatch import Marker, State

from dex_connectors.google_gmail import (
    Config,
    Credentials,
    GmailProvider,
    SendMessageInput,
    SendMessageOutput,
)
from dex_connectors.google_gmail.operations import SendMessageOperation


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


def _call(context: HeartbeatContext, call_id: CallID | None = None) -> Call:
    call_id = call_id or CallID(str(uuid4()))
    return Call(
        call_id, ConnectionRef("google", "support"), OperationRef("gmail", "sendMessage"),
        IdempotencyKey(call_id), context=cast(dex.AsyncContext, context),
    )


async def _invoke(
    call: Call, response: httpx.MockTransport
) -> tuple[MutationAttempt[SendMessageOutput], list[httpx.Request]]:
    requests: list[httpx.Request] = []

    async def observe(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return await response.handle_async_request(request)

    credentials = StaticCredentialProvider({
        call.connection: Credentials(SecretString("inert-token"), "owner@example.org")
    })
    async with httpx.AsyncClient(transport=httpx.MockTransport(observe)) as http_client:
        provider = GmailProvider(
            Config(endpoint="http://localhost/gmail/v1"), credentials, http_client=http_client
        )
        attempt = await SendMessageOperation(provider).invoke(
            call, SendMessageInput(["ada@example.com"], "Progress", "Update")
        )
    return attempt, requests


@pytest.mark.asyncio
async def test_claimed_previous_attempt_can_read_back_but_cannot_post() -> None:
    """A search miss after a claimed POST stays retryable without a duplicate send."""
    context = HeartbeatContext(2)
    call = _call(context)
    context.previous = Marker(call.id, 1, State.CLAIMED)
    attempt, requests = await _invoke(
        call, httpx.MockTransport(lambda _: httpx.Response(200, json={"messages": []}))
    )
    assert attempt.kind == "retry"
    assert [request.method for request in requests] == ["GET"]
    assert context.heartbeats == []


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
async def test_foreign_checkpoint_fails_before_any_provider_request() -> None:
    """A foreign heartbeat fails closed and cannot reach Gmail."""
    context = HeartbeatContext(2)
    call = _call(context)
    context.previous = Marker(CallID(str(uuid4())), 1, State.NOT_SENT)
    attempt, requests = await _invoke(call, httpx.MockTransport(lambda _: httpx.Response(200)))
    assert attempt.branch == "defect"
    assert requests == []
