# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Reply read-back, acceptance, and credential handling around the send cap."""

import asyncio
import json
from collections.abc import AsyncIterator, Callable
from datetime import UTC, datetime, timedelta

import httpx
import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    CredentialProvider,
    MutationResult,
    OperationRef,
    ReauthorizationRequiredError,
    RetryError,
    SecretString,
    StaticCredentialProvider,
    projectconfig,
    run_mutation,
)
from dex_connectors.sdk.singledispatch import Marker

from dex_connectors.intercom import Client, Config, Credentials, ReplyToConversationInput
from dex_connectors.intercom._generated.connection import CredentialMaterialError
from dex_connectors.intercom._generated.definitions import REPLY_TO_CONVERSATION_DEFINITION
from dex_connectors.intercom.models import ReplyToConversationOutput, UpdateConversationStateInput

CONNECTION = ConnectionRef("intercom", "intercom-test")
CALL_ID = CallID("086d15f9-0a76-5218-9367-313eebf7077a")
REPLY = ReplyToConversationInput(
    conversation_id="2002", admin_id="77", message_type="comment", body="We are working on it"
)
STATIC_CREDENTIALS = StaticCredentialProvider(
    {CONNECTION: Credentials(SecretString("inert-intercom-token"), SecretString(""))}
)


class AttemptContext:
    """Keep the last heartbeat across Execute attempts like Dex does."""

    def __init__(self, deadline_seconds: float = 30) -> None:
        self.attempt = 1
        self.run_id = "run"
        self.first_attempt_at = datetime.fromtimestamp(1, UTC)
        self.deadline = datetime.now(UTC) + timedelta(seconds=deadline_seconds)
        self.heartbeat_value: object = None

    @property
    def marker(self) -> Marker | None:
        """Decode this call's checkpoint from either SDK heartbeat format."""
        if isinstance(self.heartbeat_value, Marker) or self.heartbeat_value is None:
            return self.heartbeat_value
        assert isinstance(self.heartbeat_value, dict)
        entry = self.heartbeat_value["singleDispatchMarkers"].get(str(CALL_ID))
        return None if entry is None else Marker.from_wire(entry)

    def has_last_heartbeat_value(self) -> bool:
        """Report whether an earlier attempt recorded a heartbeat."""
        return self.heartbeat_value is not None

    def get_last_heartbeat_value(self, value_type: type[object]) -> object:
        """Return the last heartbeat as the reading SDK expects it."""
        if isinstance(self.heartbeat_value, dict) and value_type is Marker:
            return self.marker
        return self.heartbeat_value

    async def heartbeat(self, value: object = None) -> None:
        """Record the heartbeat exactly as the SDK wrote it."""
        self.heartbeat_value = value


class BreakingBody(httpx.AsyncByteStream):
    """A 2xx body that fails after its first bytes, or stalls until cancelled."""

    def __init__(self, stalls: bool = False) -> None:
        self.stalls = stalls

    async def __aiter__(self) -> AsyncIterator[bytes]:
        yield b'{"type":"conversation"'
        if self.stalls:
            await asyncio.sleep(30)
        raise httpx.ReadError("connection reset while reading the body")


def conversation(*parts: dict[str, object]) -> dict[str, object]:
    """Render conversation 2002 with the given parts."""
    return {
        "type": "conversation",
        "id": "2002",
        "state": "open",
        "open": True,
        "conversation_parts": {"conversation_parts": list(parts), "total_count": len(parts)},
    }


FOUND_PART = {
    "id": "3001",
    "part_type": "comment",
    "body": "We are working on it",
    "author": {"type": "admin", "id": "77"},
    "created_at": 1793318400,
}


async def reply(
    respond: Callable[[httpx.Request], httpx.Response],
    context: AttemptContext,
    requests: list[httpx.Request],
    credentials: CredentialProvider[Credentials] = STATIC_CREDENTIALS,
) -> MutationResult[ReplyToConversationOutput] | RetryError:
    """Run one Execute attempt of replyToConversation with the generated send cap."""

    def record(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return respond(request)

    async with httpx.AsyncClient(transport=httpx.MockTransport(record)) as transport:
        client = Client(Config(), credentials, transport, endpoint="http://127.0.0.1:37345")
        call = Call(
            CALL_ID,
            CONNECTION,
            OperationRef("intercom", "replyToConversation"),
            context=context,
            max_sends=REPLY_TO_CONVERSATION_DEFINITION.max_sends,
        )
        try:
            return await run_mutation(client.reply_to_conversation(), call, REPLY)
        except RetryError as error:
            return error


def read_back_response(read_back: str) -> httpx.Response:
    """Answer the conversation read-back as the scenario names it."""
    if read_back == "found":
        return httpx.Response(200, json=conversation(FOUND_PART))
    if read_back == "absent":
        return httpx.Response(200, json=conversation())
    return httpx.Response(429, json={"type": "error.list", "errors": [{"code": "rate_limit_exceeded"}]})


@pytest.mark.asyncio
@pytest.mark.parametrize(("read_back", "branch"), [("found", "replied"), ("absent", "uncertain")])
async def test_2xx_whose_body_breaks_is_read_back_and_never_resent(read_back: str, branch: str) -> None:
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        if request.method == "POST":
            return httpx.Response(200, stream=BreakingBody())
        return read_back_response(read_back)

    result = await reply(respond, AttemptContext(), requests)
    assert isinstance(result, MutationResult), "a 2xx was accepted, so the attempt never retries"
    assert result.branch == branch
    assert [request.method for request in requests] == ["POST", "GET"]
    if branch == "replied":
        assert result.value.part.id == "3001"
        assert not result.value.was_already_applied


@pytest.mark.asyncio
@pytest.mark.parametrize(("later_read_back", "branch"), [("found", "replied"), ("absent", "uncertain")])
async def test_accepted_reply_whose_read_back_is_rate_limited_is_never_resent(
    later_read_back: str, branch: str
) -> None:
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        if request.method == "POST":
            assert not requests[:-1], "the accepted reply is never sent again"
            return httpx.Response(200, stream=BreakingBody())
        return read_back_response("rateLimited" if len(requests) == 2 else later_read_back)

    context = AttemptContext()
    assert isinstance(await reply(respond, context, requests), RetryError)
    assert context.marker is not None and context.marker.provider_id
    context.attempt = 2
    result = await reply(respond, context, requests)
    assert isinstance(result, MutationResult)
    assert result.branch == branch
    assert [request.method for request in requests] == ["POST", "GET", "GET"]


@pytest.mark.asyncio
async def test_2xx_whose_body_outlasts_the_write_window_is_read_back() -> None:
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        if request.method == "POST":
            return httpx.Response(200, stream=BreakingBody(stalls=True))
        return read_back_response("found")

    result = await reply(respond, AttemptContext(deadline_seconds=1), requests)
    assert isinstance(result, MutationResult)
    assert result.branch == "replied"
    assert [request.method for request in requests] == ["POST", "GET"]


@pytest.mark.asyncio
async def test_read_back_precedes_a_send_after_a_rate_limited_resend() -> None:
    requests: list[httpx.Request] = []
    answers = iter(
        [
            httpx.Response(502, json={"type": "error.list", "errors": [{"code": "server_error"}]}),
            httpx.Response(200, json=conversation()),
            httpx.Response(429, json={"type": "error.list", "errors": [{"code": "rate_limit_exceeded"}]}),
            httpx.Response(200, json=conversation(FOUND_PART)),
        ]
    )
    context = AttemptContext()
    for attempt in (1, 2):
        context.attempt = attempt
        assert isinstance(await reply(lambda _: next(answers), context, requests), RetryError)
    assert context.marker is not None and context.marker.state == "canResend"
    context.attempt = 3
    result = await reply(lambda _: next(answers), context, requests)
    assert isinstance(result, MutationResult) and result.branch == "replied"
    assert result.value.was_already_applied
    assert [request.method for request in requests] == ["POST", "GET", "POST", "GET"]


class FailingCredentials:
    """Resolve no credentials and raise the given error."""

    def __init__(self, error: Exception) -> None:
        self.error = error

    async def resolve(self, call: Call) -> Credentials:
        """Raise the configured credential error."""
        raise self.error


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("error", "has_possible_earlier_send", "branch"),
    [
        (LookupError("credential store unavailable"), False, ""),
        (ReauthorizationRequiredError(), False, "defect"),
        (LookupError("credential store unavailable"), True, ""),
        (ReauthorizationRequiredError(), True, "uncertain"),
        (projectconfig.ObjectNotFoundError("connection"), False, "defect"),
        (projectconfig.ConnectionNotSelectedError("connection"), False, "defect"),
        (projectconfig.CredentialBrokerRejectedError("broker"), True, "uncertain"),
        # The generated project credential source raises this for malformed stored material.
        (CredentialMaterialError("Intercom stored credential is malformed"), False, "defect"),
        (CredentialMaterialError("Intercom stored credential is malformed"), True, "uncertain"),
        (projectconfig.CredentialBrokerUnavailableError("broker"), False, ""),
        (ValueError("connection is not configured"), False, ""),
        (ValueError("connection is not configured"), True, ""),
    ],
)
async def test_credential_failures(error: Exception, has_possible_earlier_send: bool, branch: str) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    if has_possible_earlier_send:
        unknown = await reply(
            lambda _: httpx.Response(502, json={"type": "error.list"}), context, requests
        )
        assert isinstance(unknown, RetryError)
        context.attempt = 2
    sent_before = len(requests)
    result = await reply(
        lambda _: pytest.fail("no request may be sent"), context, requests, FailingCredentials(error)
    )
    assert len(requests) == sent_before
    assert context.marker is not None
    assert context.marker.state == ("claimed" if has_possible_earlier_send else "notSent")
    if not branch:
        assert isinstance(result, RetryError)
        return
    assert isinstance(result, MutationResult) and result.branch == branch


@pytest.mark.asyncio
async def test_oversized_refusal_is_an_unknown_outcome() -> None:
    requests: list[httpx.Request] = []
    body = json.dumps({"type": "error.list", "padding": "a" * 5_000_000}).encode()
    result = await reply(lambda _: httpx.Response(400, content=body), AttemptContext(), requests)
    assert isinstance(result, RetryError), "an uninspectable refusal may hide an applied reply"
    assert result.failure.kind == "RESPONSE_TOO_LARGE"


@pytest.mark.asyncio
async def test_credential_echoing_refusal_resends_below_the_cap_and_is_uncertain_at_the_cap() -> None:
    requests: list[httpx.Request] = []
    echo = {"type": "error.list", "errors": [{"message": "inert-intercom-token"}]}

    def respond(request: httpx.Request) -> httpx.Response:
        if request.method == "GET":
            return httpx.Response(200, json=conversation())
        return httpx.Response(422, json=echo)

    context = AttemptContext()
    for attempt in (1, 2, 3):
        context.attempt = attempt
        result = await reply(respond, context, requests)
        assert isinstance(result, RetryError), "a refusal that echoes the token may hide an applied reply"
        assert "inert-intercom-token" not in result.failure.message
        assert context.marker is not None and context.marker.state == "claimed"
    context.attempt = 4
    result = await reply(respond, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert [request.method for request in requests].count("POST") == 3


async def update_state(
    respond: Callable[[httpx.Request], httpx.Response], requests: list[httpx.Request]
) -> MutationResult[object] | RetryError:
    """Run one Execute attempt of updateConversationState closing conversation 2002."""

    def record(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return respond(request)

    async with httpx.AsyncClient(transport=httpx.MockTransport(record)) as transport:
        client = Client(
            Config(max_response_bytes=4096), STATIC_CREDENTIALS, transport, endpoint="http://127.0.0.1:37345"
        )
        call = Call(
            CALL_ID, CONNECTION, OperationRef("intercom", "updateConversationState"), context=AttemptContext()
        )
        try:
            return await run_mutation(
                client.update_conversation_state(),
                call,
                UpdateConversationStateInput("2002", "77", state="closed"),
            )
        except RetryError as error:
            return error


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("status", "body"),
    [
        (200, b'{"type":"conversation","id":"another"}'),
        (200, b'{"type":"conversation","id":"2002","title":"inert-intercom-token"}'),
        (200, json.dumps({"type": "conversation", "padding": "a" * 8000}).encode()),
        (404, json.dumps({"type": "error.list", "padding": "a" * 8000}).encode()),
        (403, b'{"type":"error.list","errors":[{"message":"inert-intercom-token"}]}'),
        (409, b'{"type":"error.list","errors":[{"code":"conflict"}]}'),
        (410, b'{"type":"error.list","errors":[{"code":"gone"}]}'),
        (302, b"{}"),
    ],
    ids=["invalid", "echoing 200", "oversized 200", "oversized 404", "echoing 403", "409", "410", "302"],
)
async def test_update_state_retries_an_inconclusive_read_before_the_change(status: int, body: bytes) -> None:
    requests: list[httpx.Request] = []
    result = await update_state(lambda _: httpx.Response(status, content=body), requests)
    assert isinstance(result, RetryError), "only a readable listed refusal selects a branch"
    assert [request.method for request in requests] == ["GET"]


@pytest.mark.asyncio
async def test_update_state_keeps_a_readable_listed_refusal_of_the_read_definite() -> None:
    requests: list[httpx.Request] = []
    refusal = {"type": "error.list", "errors": [{"code": "forbidden"}]}
    result = await update_state(lambda _: httpx.Response(403, json=refusal), requests)
    assert isinstance(result, MutationResult) and result.branch == "providerRejected"


@pytest.mark.asyncio
async def test_update_state_retries_a_credential_echoing_refusal_of_the_write() -> None:
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        if request.method == "GET":
            return httpx.Response(200, json=conversation())
        echo = {"type": "error.list", "errors": [{"message": "inert-intercom-token"}]}
        return httpx.Response(400, json=echo)

    result = await update_state(respond, requests)
    assert isinstance(result, RetryError)
    assert [request.method for request in requests] == ["GET", "POST"]
