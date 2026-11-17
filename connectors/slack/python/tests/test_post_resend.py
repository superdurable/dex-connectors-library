# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded resend of Slack posts whose outcome is unknown."""

import json
from collections.abc import Callable
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
    RetryError,
    SecretString,
    run_mutation,
)
from dex_connectors.sdk.singledispatch import Marker

from dex_connectors.slack import Client, Config, Credentials
from dex_connectors.slack._generated.definitions import POST_CHANNEL_MESSAGE_DEFINITION
from dex_connectors.slack.models import PostChannelMessageInput, PostMessageOutput

REFERENCE = ConnectionRef("slack", "workspace")
CALL_ID = CallID("74390522-858d-5ed6-bdf7-04e979ba9e4f")
SERVER_FAILURE = (500, {"ok": False, "error": "fatal_error"})


class AttemptContext:
    """Keep the last heartbeat across Execute attempts like Dex does."""

    def __init__(self) -> None:
        self.attempt = 1
        self.run_id = "run"
        self.deadline = datetime.now(UTC) + timedelta(seconds=30)
        self.marker: Marker | None = None

    def has_last_heartbeat_value(self) -> bool:
        """Report whether an earlier attempt recorded a heartbeat."""
        return self.marker is not None

    def get_last_heartbeat_value(self, value_type: type[object]) -> object:
        """Return the last marker."""
        return self.marker

    async def heartbeat(self, value: object = None) -> None:
        """Record the attempt's marker."""
        self.marker = value if isinstance(value, Marker) else None


class SequencedCredentials(CredentialProvider[Credentials]):
    """Return one bot token per resolution, repeating the last."""

    def __init__(self, bot_tokens: list[str]) -> None:
        self.bot_tokens = bot_tokens
        self.resolutions = 0

    async def resolve(self, call: Call) -> Credentials:
        """Resolve the next bot token with a constant user token."""
        bot_token = self.bot_tokens[min(self.resolutions, len(self.bot_tokens) - 1)]
        self.resolutions += 1
        empty = SecretString("")
        return Credentials(
            oauth_client_id="", oauth_client_secret=empty, bot_token=SecretString(bot_token),
            bot_refresh_token=empty, user_token=SecretString("user-token"), user_refresh_token=empty,
            app_token=empty,
        )


Answer = Callable[[httpx.Request], tuple[int, dict[str, object]]]


async def post(
    answer: Answer, context: AttemptContext, requests: list[httpx.Request],
    *, max_sends: int = POST_CHANNEL_MESSAGE_DEFINITION.max_sends, bot_tokens: list[str] | None = None,
    credentials: SequencedCredentials | None = None,
) -> MutationResult[PostMessageOutput] | RetryError:
    """Run one Execute attempt of postChannelMessage, as MutationStep passes the definition cap."""
    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        status, body = answer(request)
        return httpx.Response(status, json=body)

    provider = credentials or SequencedCredentials(bot_tokens or ["bot-token"])
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as transport:
        client = Client(Config(), provider, transport)
        call = Call(CALL_ID, REFERENCE, OperationRef("slack", "postChannelMessage"),
                    context=context, max_sends=max_sends)
        try:
            return await run_mutation(
                client.post_channel_message(), call, PostChannelMessageInput("C123", "hello")
            )
        except RetryError as error:
            return error


@pytest.mark.asyncio
async def test_unknown_outcome_resends_until_the_generated_send_cap() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    for attempt in range(1, POST_CHANNEL_MESSAGE_DEFINITION.max_sends):
        context.attempt = attempt
        assert isinstance(await post(lambda _: SERVER_FAILURE, context, requests), RetryError)
        assert context.marker is not None and context.marker.state == "claimed"
        assert context.marker.send_count == attempt
    context.attempt = POST_CHANNEL_MESSAGE_DEFINITION.max_sends
    result = await post(lambda _: SERVER_FAILURE, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    context.attempt += 1
    result = await post(lambda _: SERVER_FAILURE, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 3
    assert {json.loads(request.content)["client_msg_id"] for request in requests} == {str(CALL_ID)}


@pytest.mark.asyncio
async def test_send_cap_of_one_never_resends() -> None:
    requests: list[httpx.Request] = []
    result = await post(lambda _: SERVER_FAILURE, AttemptContext(), requests, max_sends=1)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 1


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "refusal",
    [
        (200, {"ok": False, "error": "channel_not_found"}),
        (200, {"ok": False, "error": "not_in_channel"}),
        (400, {"ok": False, "error": "invalid_arguments"}),
        (403, {"ok": False, "error": "restricted_action"}),
        (401, {"ok": False, "error": "invalid_auth"}),
    ],
)
async def test_definite_refusal_is_never_resent(refusal: tuple[int, dict[str, object]]) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    result = await post(lambda _: refusal, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "providerRejected"
    assert len(requests) == 1
    # canResend proves Slack posted nothing, so agent hosts classify the refusal as failed.
    assert context.marker is not None and context.marker.state == "canResend"
    assert context.marker.attempt == 1 and context.marker.send_count == 0
    assert not context.marker.provider_id


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "unknown",
    [
        (200, {"ok": False}),
        (200, {}),
        (408, {"ok": False, "error": "request_timeout"}),
        (418, {"ok": False, "error": "teapot"}),
        (400, {"ok": True, "channel": "C123"}),
        (403, {"ok": True, "channel": "C123"}),
        (401, {"ok": True, "channel": "C123"}),
    ],
)
async def test_non_definite_refusal_resends_until_the_send_cap(
    unknown: tuple[int, dict[str, object]],
) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    states: list[str] = []
    for attempt in range(1, POST_CHANNEL_MESSAGE_DEFINITION.max_sends):
        context.attempt = attempt
        assert isinstance(await post(lambda _: unknown, context, requests), RetryError)
        assert context.marker is not None
        states.append(context.marker.state)
    context.attempt = POST_CHANNEL_MESSAGE_DEFINITION.max_sends
    result = await post(lambda _: unknown, context, requests)
    # The outcome is unknown, so the post repeats with one client_msg_id until the cap.
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == POST_CHANNEL_MESSAGE_DEFINITION.max_sends
    assert {json.loads(request.content)["client_msg_id"] for request in requests} == {str(CALL_ID)}
    assert context.marker is not None and context.marker.state == "claimed"
    assert context.marker.send_count == POST_CHANNEL_MESSAGE_DEFINITION.max_sends
    assert "canResend" not in states


@pytest.mark.asyncio
async def test_client_error_naming_a_timestamp_is_read_back_without_can_resend() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()

    def answer(request: httpx.Request) -> tuple[int, dict[str, object]]:
        if request.method == "GET":
            return 200, {"ok": True, "messages": [{"ts": "2.0", "user": "UBOT", "text": "hello"}]}
        return 400, {"ok": True, "channel": "C123", "ts": "2.0"}

    result = await post(answer, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "sent"
    assert [request.method for request in requests] == ["POST", "GET"]
    assert context.marker is not None and context.marker.state == "claimed"
    assert context.marker.provider_id == "2.0"


class FailingCanResendContext(AttemptContext):
    """Fail every canResend heartbeat write."""

    async def heartbeat(self, value: object = None) -> None:
        """Reject canResend markers and record every other marker."""
        if isinstance(value, Marker) and value.state == "canResend":
            raise ConnectionError("can-resend heartbeat unavailable")
        await super().heartbeat(value)


@pytest.mark.asyncio
async def test_definite_refusal_with_lost_checkpoint_still_selects_provider_rejected() -> None:
    requests: list[httpx.Request] = []
    context = FailingCanResendContext()
    result = await post(lambda _: (200, {"ok": False, "error": "channel_not_found"}), context, requests)
    assert isinstance(result, MutationResult) and result.branch == "providerRejected"
    assert len(requests) == 1
    assert context.marker is not None and context.marker.state == "claimed"


@pytest.mark.asyncio
async def test_definite_refusal_of_a_resend_selects_uncertain() -> None:
    requests: list[httpx.Request] = []
    refusal = (200, {"ok": False, "error": "channel_not_found"})
    context = AttemptContext()
    assert isinstance(await post(lambda _: SERVER_FAILURE, context, requests), RetryError)
    context.attempt = 2
    result = await post(lambda _: refusal, context, requests)
    # The first post may exist, so the refusal cannot report that nothing changed.
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert result.failure is not None and result.failure.kind == "NOT_FOUND"
    assert len(requests) == 2
    assert context.marker is not None and context.marker.state == "claimed"
    assert context.marker.send_count == 2


@pytest.mark.asyncio
async def test_terminal_credentials_after_a_counted_send_select_uncertain() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    context.attempt = 3
    # A canResend marker that still counts one possible send allows a send without read-back.
    context.marker = Marker.from_wire(
        {"callId": str(CALL_ID), "attempt": 2, "state": "canResend", "sendCount": 1}
    )
    result = await post(lambda _: SERVER_FAILURE, context, requests, bot_tokens=[""])
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert result.failure is not None and result.failure.kind == "AUTHENTICATION"
    assert not requests


@pytest.mark.asyncio
@pytest.mark.parametrize(("attempt", "is_retry"), [(1, True), (2, True), (3, False)])
async def test_unreadable_checkpoint_retries_below_the_send_cap(attempt: int, is_retry: bool) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    context.attempt = attempt
    context.marker = Marker.from_wire(
        {"callId": "5f0c4d1e-8b9a-4c3d-9e2f-1a2b3c4d5e6f", "attempt": 1, "state": "canResend"}
    )
    result = await post(lambda _: SERVER_FAILURE, context, requests)
    if is_retry:
        assert isinstance(result, RetryError)
    else:
        assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert not requests


@pytest.mark.asyncio
async def test_terminal_credentials_before_a_resend_select_uncertain() -> None:
    requests: list[httpx.Request] = []
    credentials = SequencedCredentials(["bot-token", ""])
    context = AttemptContext()
    first = await post(lambda _: SERVER_FAILURE, context, requests, credentials=credentials)
    assert isinstance(first, RetryError)
    context.attempt = 2
    result = await post(lambda _: SERVER_FAILURE, context, requests, credentials=credentials)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert result.failure is not None and result.failure.kind == "AUTHENTICATION"
    assert len(requests) == 1
    assert context.marker is not None and context.marker.attempt == 1 and context.marker.state == "claimed"


@pytest.mark.asyncio
async def test_checkpointed_timestamp_is_read_back_not_resent() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    context.attempt = 2
    context.marker = Marker.from_wire(
        {"callId": str(CALL_ID), "attempt": 1, "state": "claimed", "providerId": "2.0", "sendCount": 1}
    )

    def answer(request: httpx.Request) -> tuple[int, dict[str, object]]:
        if request.method == "GET":
            return 200, {"ok": True, "messages": []}
        return 200, {"ok": True, "channel": "C123", "ts": "2.0"}

    result = await post(answer, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert [request.method for request in requests] == ["GET"]
