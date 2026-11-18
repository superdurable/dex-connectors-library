# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Write outcomes that must never resend an accepted reply or hide an earlier send."""

import asyncio
from collections.abc import AsyncIterator, Awaitable, Callable
from datetime import UTC, datetime, timedelta
from typing import Any

import httpx
import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    IdempotencyKey,
    OperationRef,
    ReauthorizationRequiredError,
    SecretString,
)
from dex_connectors.sdk.projectconfig import (
    ConnectionNotSelectedError,
    CredentialBrokerRejectedError,
    CredentialBrokerUnavailableError,
    ObjectNotFoundError,
)
from dex_connectors.sdk.singledispatch import Fence

from dex_connectors.front import (
    Client,
    Config,
    Credentials,
    ReplyToConversationInput,
    UpdateConversationInput,
)

CALL_ID = CallID("76e76ff6-9e73-5db4-bb93-69b6a679fae5")
CONVERSATION = {"id": "cnv_100", "status": "archived", "tags": []}
REPLY = ReplyToConversationInput("cnv_100", "Hello")
TAGGING = UpdateConversationInput("cnv_100", add_tag_ids=["tag_100"])

Handler = Callable[[httpx.Request], httpx.Response | Awaitable[httpx.Response]]


class HeartbeatContext:
    """A Dex context whose heartbeat survives across attempts."""

    def __init__(self, deadline_seconds: float = 30) -> None:
        self.attempt = 1
        self.deadline = datetime.now(UTC) + timedelta(seconds=deadline_seconds)
        self.markers: list[object] = []

    def has_last_heartbeat_value(self) -> bool:
        return bool(self.markers)

    def get_last_heartbeat_value(self, value_type: type[object]) -> object:
        return self.markers[-1]

    async def heartbeat(self, value: object) -> None:
        self.markers.append(value)


class StaticCredentials:
    async def resolve(self, call: Call) -> Credentials:
        return Credentials(SecretString("front-inert-token"))


class FailingCredentials:
    def __init__(self, error: Exception) -> None:
        self.error = error

    async def resolve(self, call: Call) -> Credentials:
        raise self.error


class BrokenBody(httpx.AsyncByteStream):
    """A 2xx body that breaks after its headers arrived."""

    async def __aiter__(self) -> AsyncIterator[bytes]:
        yield b'{"status":'
        raise httpx.ReadError("connection reset")


def new_call(context: HeartbeatContext, operation: str, max_sends: int = 3) -> Call:
    return Call(
        CALL_ID,
        ConnectionRef("front", "front-test"),
        OperationRef("front", operation),
        IdempotencyKey(CALL_ID),
        context=context,
        max_sends=max_sends,
    )


def new_client(provider: httpx.AsyncClient, credentials: Any = None) -> Client:
    return Client(Config(), credentials or StaticCredentials(), provider, api_base_url="http://127.0.0.1:22130")


def recording(answers: Handler) -> tuple[httpx.AsyncClient, list[httpx.Request]]:
    requests: list[httpx.Request] = []

    async def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        response = answers(request)
        return await response if asyncio.iscoroutine(response) else response  # type: ignore[return-value]

    return httpx.AsyncClient(transport=httpx.MockTransport(answer)), requests


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "response",
    [
        httpx.Response(202, json={"status": "accepted"}),
        httpx.Response(202, content=b"{invalid"),
        httpx.Response(202, headers={"Content-Type": "application/json"}, stream=BrokenBody()),
    ],
    ids=["without id", "unparsable", "body breaks after 2xx headers"],
)
async def test_accepted_reply_without_a_usable_id_is_never_resent(response: httpx.Response) -> None:
    """A 2xx accepted the reply, and Front offers nothing to read it back by."""
    provider, requests = recording(lambda request: response)
    async with provider:
        attempt = await new_client(provider).reply_to_conversation().invoke(
            new_call(HeartbeatContext(), "replyToConversation"), REPLY
        )
    assert attempt.kind == "uncertain"
    assert len(requests) == 1


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("error", "kind"),
    [
        (ReauthorizationRequiredError(), "uncertain"),
        (ObjectNotFoundError("connection"), "uncertain"),
        (ConnectionNotSelectedError("connection"), "uncertain"),
        (CredentialBrokerRejectedError("broker"), "uncertain"),
        (ConnectionError("broker unavailable"), "retry"),
        (CredentialBrokerUnavailableError("broker"), "retry"),
        (ValueError("connection is not configured"), "retry"),
    ],
    ids=[
        "terminal",
        "object not found",
        "not selected",
        "broker rejected",
        "transient",
        "broker unavailable",
        "not configured",
    ],
)
async def test_credential_failure_after_a_possible_send_never_selects_defect(
    error: Exception, kind: str
) -> None:
    """notSent or defect would hide the first attempt's possible send."""
    provider, requests = recording(lambda request: httpx.Response(502, json={}))
    context = HeartbeatContext()
    async with provider:
        first = await new_client(provider).reply_to_conversation().invoke(
            new_call(context, "replyToConversation"), REPLY
        )
        assert first.kind == "retry"
        recorded = len(context.markers)
        context.attempt = 2
        second = await new_client(provider, FailingCredentials(error)).reply_to_conversation().invoke(
            new_call(context, "replyToConversation"), REPLY
        )
    assert second.kind == kind
    assert len(context.markers) == recorded
    assert len(requests) == 1


@pytest.mark.asyncio
@pytest.mark.parametrize(("max_sends", "kind"), [(3, "retry"), (1, "uncertain")])
async def test_write_window_timeout_after_dispatch_is_an_unknown_outcome(
    monkeypatch: pytest.MonkeyPatch, max_sends: int, kind: str
) -> None:
    """The Fence's TimeoutError resends below the cap and selects uncertain at it."""

    async def hang(*args: object, **kwargs: object) -> None:
        await asyncio.sleep(10)

    monkeypatch.setattr(Client, "exchange", hang)
    provider, _ = recording(lambda request: httpx.Response(500))
    async with provider:
        attempt = await new_client(provider).reply_to_conversation().invoke(
            new_call(HeartbeatContext(deadline_seconds=1), "replyToConversation", max_sends), REPLY
        )
    assert attempt.kind == kind


class StalledBody(httpx.AsyncByteStream):
    """A 2xx body whose first bytes arrive and whose rest never does."""

    async def __aiter__(self) -> AsyncIterator[bytes]:
        yield b'{"message_uid":'
        await asyncio.sleep(30)


@pytest.mark.asyncio
@pytest.mark.parametrize("max_sends", [3, 1])
async def test_2xx_headers_then_the_write_deadline_cuts_the_body_is_never_resent(
    monkeypatch: pytest.MonkeyPatch, max_sends: int
) -> None:
    """The Fence's TimeoutError after a 2xx status means Front accepted the reply."""
    original_dispatch = Fence.dispatch

    async def cut_at_the_write_deadline(
        fence: Fence, send: Callable[[datetime], Awaitable[object]]
    ) -> object:
        async def send_until_the_window_closes(deadline: datetime) -> object:
            return await asyncio.wait_for(send(deadline), 0.3)

        return await original_dispatch(fence, send_until_the_window_closes)

    monkeypatch.setattr(Fence, "dispatch", cut_at_the_write_deadline)
    provider, requests = recording(lambda request: httpx.Response(202, stream=StalledBody()))
    async with provider:
        attempt = await new_client(provider).reply_to_conversation().invoke(
            new_call(HeartbeatContext(), "replyToConversation", max_sends), REPLY
        )
    assert attempt.kind == "uncertain"
    assert len(requests) == 1


@pytest.mark.asyncio
async def test_slow_reply_ends_before_the_write_window_and_keeps_the_claim() -> None:
    """The client stops before the Fence cancels it, so the send still counts toward the cap."""

    async def slow(request: httpx.Request) -> httpx.Response:
        await asyncio.sleep(10)
        return httpx.Response(202, json={"message_uid": "abc123"})

    provider, _ = recording(slow)
    context = HeartbeatContext(deadline_seconds=1.5)
    async with provider:
        attempt = await new_client(provider).reply_to_conversation().invoke(
            new_call(context, "replyToConversation"), REPLY
        )
    assert attempt.kind == "retry"
    assert "claimed" in repr(context.markers[-1]).lower()


@pytest.mark.asyncio
async def test_unreadable_refusal_body_is_an_unknown_reply_outcome() -> None:
    """An oversized 400 body cannot prove that Front refused the reply."""
    provider, _ = recording(lambda request: httpx.Response(400, content=b"x" * (128 << 10)))
    async with provider:
        attempt = await new_client(provider).reply_to_conversation().invoke(
            new_call(HeartbeatContext(), "replyToConversation"), REPLY
        )
    assert attempt.kind == "retry"


@pytest.mark.asyncio
@pytest.mark.parametrize(("max_sends", "kind"), [(3, "retry"), (1, "uncertain")])
async def test_merged_redirect_after_a_reply_is_an_unknown_outcome(max_sends: int, kind: str) -> None:
    """A 3xx is not a definite refusal, so it resends below the cap."""
    location = {"Location": "https://api2.frontapp.com/conversations/cnv_200"}
    provider, _ = recording(lambda request: httpx.Response(301, headers=location, json={}))
    async with provider:
        attempt = await new_client(provider).reply_to_conversation().invoke(
            new_call(HeartbeatContext(), "replyToConversation", max_sends), REPLY
        )
    assert attempt.kind == kind
    assert attempt.failure is not None and "cnv_200" in attempt.failure.message


@pytest.mark.asyncio
async def test_update_retries_a_refusal_whose_body_cannot_be_read() -> None:
    """Repeating an absolute write is safe, so an unreadable refusal retries."""

    def answer(request: httpx.Request) -> httpx.Response:
        if request.method == "GET":
            return httpx.Response(200, json=CONVERSATION)
        return httpx.Response(400, content=b"x" * (128 << 10))

    provider, _ = recording(answer)
    async with provider:
        attempt = await new_client(provider).update_conversation().invoke(
            new_call(HeartbeatContext(), "updateConversation", 1), TAGGING
        )
    assert attempt.kind == "retry"


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("error", "attempt_number", "kind"),
    [
        (ConnectionError("broker unavailable"), 1, "retry"),
        (CredentialBrokerUnavailableError("broker"), 1, "retry"),
        (ValueError("connection is not configured"), 1, "retry"),
        (ReauthorizationRequiredError(), 1, "branch"),
        (ObjectNotFoundError("connection"), 1, "branch"),
        (ConnectionNotSelectedError("connection"), 1, "branch"),
        (CredentialBrokerRejectedError("broker"), 1, "branch"),
        (ReauthorizationRequiredError(), 2, "retry"),
    ],
    ids=[
        "transient",
        "broker unavailable",
        "not configured",
        "terminal first attempt",
        "object not found",
        "not selected",
        "broker rejected",
        "terminal after earlier write",
    ],
)
async def test_update_classifies_credential_failures(
    error: Exception, attempt_number: int, kind: str
) -> None:
    """Only a terminal failure on the first attempt selects defect."""
    provider, requests = recording(lambda request: httpx.Response(500))
    context = HeartbeatContext()
    context.attempt = attempt_number
    async with provider:
        attempt = await new_client(provider, FailingCredentials(error)).update_conversation().invoke(
            new_call(context, "updateConversation", 1), TAGGING
        )
    assert attempt.kind == kind
    if kind == "branch":
        assert attempt.branch == "defect"
    assert not requests


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("status", "body", "branch"),
    [
        (404, b"{}", "notFound"),
        (403, b"{}", "providerRejected"),
        (301, b"{}", "notFound"),
        (200, b'{"id":"bad"}', ""),
        (409, b"{}", ""),
        (410, b"{}", ""),
        (500, b"{}", ""),
        (400, b'{"_error":"front-inert-token"}', ""),
    ],
)
async def test_update_first_read_selects_a_branch_only_for_a_definite_answer(
    status: int, body: bytes, branch: str
) -> None:
    """An earlier attempt's change may have applied, so an unknown read retries on every attempt."""
    provider, requests = recording(lambda request: httpx.Response(status, content=body))
    context = HeartbeatContext()
    async with provider:
        attempt = await new_client(provider).update_conversation().invoke(
            new_call(context, "updateConversation", 1), TAGGING
        )
    assert [request.method for request in requests] == ["GET"]
    if not branch:
        assert attempt.kind == "retry"
        return
    assert attempt.kind == "branch" and attempt.branch == branch
