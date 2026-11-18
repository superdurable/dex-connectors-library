# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded resend of Slack posts whose outcome is unknown."""

import asyncio
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
from dex_connectors.slack.models import (
    MessageButton,
    PostButtonMessageInput,
    PostChannelMessageInput,
    PostMessageOutput,
    PostThreadReplyInput,
)

REFERENCE = ConnectionRef("slack", "workspace")
CALL_ID = CallID("74390522-858d-5ed6-bdf7-04e979ba9e4f")
SERVER_FAILURE = (500, {"ok": False, "error": "fatal_error"})


class AttemptContext:
    """Keep the last heartbeat across Execute attempts like Dex does."""

    def __init__(self) -> None:
        self.attempt = 1
        self.run_id = "run"
        self.deadline = datetime.now(UTC) + timedelta(seconds=30)
        self.heartbeat_value: object = None
        self.read_error: Exception | None = None

    @property
    def marker(self) -> Marker | None:
        """Decode this call's marker from the last heartbeat."""
        return call_marker(self.heartbeat_value)

    def has_last_heartbeat_value(self) -> bool:
        """Report whether an earlier attempt recorded a heartbeat."""
        return self.read_error is not None or self.heartbeat_value is not None

    def get_last_heartbeat_value(self, value_type: object) -> object:
        """Return the last heartbeat, translating a seeded marker to the requested format."""
        if self.read_error is not None:
            raise self.read_error
        if isinstance(self.heartbeat_value, Marker) and value_type is not Marker:
            marker = self.heartbeat_value
            return {"singleDispatchMarkers": {str(marker.call_id): marker.to_wire()}}
        if isinstance(self.heartbeat_value, dict) and value_type is Marker:
            return self.marker
        return self.heartbeat_value

    async def heartbeat(self, value: object = None) -> None:
        """Record the heartbeat exactly as the SDK wrote it."""
        self.heartbeat_value = value


def call_marker(heartbeat_value: object) -> Marker | None:
    """Decode CALL_ID's marker from a single-marker or per-call map heartbeat."""
    if isinstance(heartbeat_value, Marker):
        return heartbeat_value
    if not isinstance(heartbeat_value, dict):
        return None
    entry = heartbeat_value.get("singleDispatchMarkers", {}).get(str(CALL_ID))
    return None if entry is None else Marker.from_wire(entry)


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


Answer = Callable[[httpx.Request], tuple[int, dict[str, object]] | httpx.Response]


class BrokenBodyStream(httpx.AsyncByteStream):
    """Yield part of a body, then fail as a connection lost mid-body does."""

    async def __aiter__(self):  # type: ignore[no-untyped-def]
        yield b'{"ok":'
        raise httpx.ReadError("connection reset mid-body")


async def post(
    answer: Answer, context: AttemptContext, requests: list[httpx.Request],
    *, max_sends: int = POST_CHANNEL_MESSAGE_DEFINITION.max_sends, bot_tokens: list[str] | None = None,
    credentials: SequencedCredentials | None = None,
) -> MutationResult[PostMessageOutput] | RetryError:
    """Run one Execute attempt of postChannelMessage, as MutationStep passes the definition cap."""
    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        answered = answer(request)
        if isinstance(answered, httpx.Response):
            return answered
        status, body = answered
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
        marker = call_marker(value)
        if marker is not None and marker.state == "canResend":
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
    context.heartbeat_value = Marker.from_wire(
        {"callId": str(CALL_ID), "attempt": 2, "state": "canResend", "sendCount": 1}
    )
    result = await post(lambda _: SERVER_FAILURE, context, requests, bot_tokens=[""])
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert result.failure is not None and result.failure.kind == "AUTHENTICATION"
    assert not requests


@pytest.mark.asyncio
@pytest.mark.parametrize("attempt", [1, 2, 3, 9])
async def test_unreadable_checkpoint_always_retries(attempt: int) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    context.attempt = attempt
    context.read_error = RuntimeError("heartbeat store unavailable")
    result = await post(lambda _: SERVER_FAILURE, context, requests)
    # The retry budget, not the send cap, bounds a fail-closed checkpoint.
    assert isinstance(result, RetryError)
    assert not requests


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "body",
    [{"error": "channel_not_found"}, {"error": "invalid_auth"}, {"error": "ratelimited"}],
)
async def test_missing_ok_is_an_unknown_outcome(body: dict[str, object]) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    states: list[str] = []
    for attempt in range(1, POST_CHANNEL_MESSAGE_DEFINITION.max_sends):
        context.attempt = attempt
        assert isinstance(await post(lambda _: (200, body), context, requests), RetryError)
        assert context.marker is not None
        states.append(context.marker.state)
    context.attempt = POST_CHANNEL_MESSAGE_DEFINITION.max_sends
    result = await post(lambda _: (200, body), context, requests)
    # Without an explicit ok:false the post may exist, so every send counts toward the cap.
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == POST_CHANNEL_MESSAGE_DEFINITION.max_sends
    assert "canResend" not in states


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "body",
    [
        {"ok": False, "error": "channel_not_found", "ts": "2.0"},
        {"ok": False, "error": "invalid_auth", "ts": "2.0"},
        {"ok": False, "error": "ratelimited", "message": {"ts": "2.0"}},
        {"ts": "2.0"},
    ],
)
async def test_refusal_naming_a_timestamp_is_read_back(body: dict[str, object]) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()

    def answer(request: httpx.Request) -> tuple[int, dict[str, object]]:
        if request.method == "GET":
            return 200, {"ok": True, "messages": [{"ts": "2.0", "user": "UBOT", "text": "hello"}]}
        return 200, body

    result = await post(answer, context, requests)
    # A timestamp contradicts the refusal, so the post is read back and never resent.
    assert isinstance(result, MutationResult) and result.branch == "sent"
    assert [request.method for request in requests] == ["POST", "GET"]
    assert context.marker is not None and context.marker.provider_id == "2.0"


@pytest.mark.asyncio
async def test_accepted_status_with_unreadable_body_is_never_resent() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    result = await post(lambda _: httpx.Response(200, stream=BrokenBodyStream()), context, requests)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 1


@pytest.mark.asyncio
async def test_unreadable_unauthorized_body_is_an_unknown_outcome() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    result = await post(lambda _: httpx.Response(401, content=b'{"ok":'), context, requests)
    # An unreadable body cannot prove the refusal, so the post repeats below the cap.
    assert isinstance(result, RetryError)
    assert context.marker is not None and context.marker.state == "claimed"


class RefreshingCredentials(SequencedCredentials):
    """Replace the bot token once Slack rejects it."""

    def __init__(self) -> None:
        super().__init__(["rejected-bot"])
        self.forced_refreshes = 0

    async def resolve_after_rejection(self, call: Call) -> Credentials:
        """Return a replacement bot token after a rejection."""
        self.forced_refreshes += 1
        self.bot_tokens = ["replacement-bot"]
        return await self.resolve(call)


@pytest.mark.asyncio
async def test_unreadable_unauthorized_body_still_counts_after_a_refresh() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    credentials = RefreshingCredentials()
    result = await post(
        lambda _: httpx.Response(401, content=b'{"ok":'), context, requests, credentials=credentials
    )
    # An unreadable rejection cannot prove nothing was posted, so canResend is never recorded.
    assert isinstance(result, RetryError)
    assert credentials.forced_refreshes == 1
    assert len(requests) == 1
    assert context.marker is not None and context.marker.state == "claimed"


class FailingProviderIDContext(AttemptContext):
    """Fail every heartbeat write that would save a provider ID."""

    async def heartbeat(self, value: object = None) -> None:
        """Reject provider ID markers and record every other marker."""
        marker = call_marker(value)
        if marker is not None and marker.provider_id:
            raise ConnectionError("provider ID heartbeat unavailable")
        await super().heartbeat(value)


@pytest.mark.asyncio
async def test_lost_timestamp_checkpoint_never_retries_read_back() -> None:
    requests: list[httpx.Request] = []
    context = FailingProviderIDContext()

    def answer(request: httpx.Request) -> tuple[int, dict[str, object]]:
        if request.method == "GET":
            return 500, {"ok": False, "error": "internal_error"}
        return 200, {"ok": True, "ts": "2.0"}

    result = await post(answer, context, requests)
    # A Retry would leave a bare claim, which permits a resend of the accepted post.
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert [request.method for request in requests] == ["POST", "GET"]


@pytest.mark.asyncio
async def test_can_resend_after_unknown_still_counts_the_earlier_send() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()

    def answer(_: httpx.Request) -> tuple[int, dict[str, object]]:
        if len(requests) == 2:
            return 429, {"ok": False, "error": "ratelimited"}
        return SERVER_FAILURE

    for attempt in range(1, 4):
        context.attempt = attempt
        assert isinstance(await post(answer, context, requests), RetryError)
    context.attempt = 4
    result = await post(answer, context, requests)
    # Slack cannot read back without a timestamp; the 429 is uncounted, the two unknown sends are not.
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 4
    assert context.marker is not None and context.marker.send_count == 3


@pytest.mark.asyncio
async def test_transient_credentials_after_a_counted_send_record_nothing() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    context.attempt = 3
    seeded = Marker.from_wire({"callId": str(CALL_ID), "attempt": 2, "state": "canResend", "sendCount": 1})
    context.heartbeat_value = seeded

    class UnavailableCredentials(SequencedCredentials):
        async def resolve(self, call: Call) -> Credentials:
            raise ConnectionError("credential store unavailable")

    result = await post(
        lambda _: SERVER_FAILURE, context, requests, credentials=UnavailableCredentials(["bot-token"])
    )
    # notSent would hide the counted first post from the next attempt.
    assert isinstance(result, RetryError)
    assert not requests
    assert context.heartbeat_value is seeded


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
    context.heartbeat_value = Marker.from_wire(
        {"callId": str(CALL_ID), "attempt": 1, "state": "claimed", "providerId": "2.0", "sendCount": 1}
    )

    def answer(request: httpx.Request) -> tuple[int, dict[str, object]]:
        if request.method == "GET":
            return 200, {"ok": True, "messages": []}
        return 200, {"ok": True, "channel": "C123", "ts": "2.0"}

    result = await post(answer, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert [request.method for request in requests] == ["GET"]


class StalledBodyStream(httpx.AsyncByteStream):
    """Stall the body after headers arrive, past the Fence write window."""

    async def __aiter__(self):  # type: ignore[no-untyped-def]
        await asyncio.sleep(30)
        yield b"{}"


WRITE_OPERATIONS = ("postChannelMessage", "postThreadReply", "postButtonMessage")


async def post_operation(
    operation: str, answer: Answer, context: AttemptContext, requests: list[httpx.Request]
) -> MutationResult[PostMessageOutput] | RetryError:
    """Run one Execute attempt of the named Slack write with the manifest send cap."""
    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        answered = answer(request)
        if isinstance(answered, httpx.Response):
            return answered
        status, body = answered
        return httpx.Response(status, json=body)

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as transport:
        client = Client(Config(), SequencedCredentials(["bot-token"]), transport)
        call = Call(CALL_ID, REFERENCE, OperationRef("slack", operation), context=context, max_sends=3)
        try:
            if operation == "postThreadReply":
                return await run_mutation(
                    client.post_thread_reply(), call, PostThreadReplyInput("C123", "1.0", "hello")
                )
            if operation == "postButtonMessage":
                return await run_mutation(
                    client.post_button_message(),
                    call,
                    PostButtonMessageInput("C123", "1.0", "Approve?", (MessageButton("approve", "Approve"),)),
                )
            return await run_mutation(
                client.post_channel_message(), call, PostChannelMessageInput("C123", "hello")
            )
        except RetryError as error:
            return error


@pytest.mark.asyncio
@pytest.mark.parametrize("operation", WRITE_OPERATIONS)
@pytest.mark.parametrize("status", [200, 409])
async def test_write_deadline_cutting_the_body_after_headers_is_never_resent(
    operation: str, status: int
) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    # A short Step deadline keeps the Fence write window near one second.
    context.deadline = datetime.now(UTC) + timedelta(seconds=1.5)
    result = await post_operation(
        operation, lambda _: httpx.Response(status, stream=StalledBodyStream()), context, requests
    )
    # Slack answered before the window cut the body, so the post may exist and is never resent.
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 1
    assert context.marker is not None and context.marker.state == "claimed"


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "response",
    [
        lambda: httpx.Response(409, content=b"conflict"),
        lambda: httpx.Response(409, content=b'{"ok":false,"error":"' + b"x" * (4 << 20) + b'"}'),
        lambda: httpx.Response(409, stream=BrokenBodyStream()),
    ],
    ids=["not-json", "oversized", "broken-mid-body"],
)
async def test_conflict_with_unusable_body_is_never_resent(response: Callable[[], httpx.Response]) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    result = await post_operation("postThreadReply", lambda _: response(), context, requests)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 1


@pytest.mark.asyncio
async def test_conflict_naming_a_timestamp_is_read_back() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()

    def answer(request: httpx.Request) -> tuple[int, dict[str, object]]:
        if request.method == "GET":
            return 200, {"ok": True, "messages": [{"ts": "2.0", "thread_ts": "1.0", "text": "hello"}]}
        return 409, {"ok": False, "error": "conflict", "ts": "2.0"}

    result = await post_operation("postThreadReply", answer, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "sent"
    assert [request.method for request in requests] == ["POST", "GET"]
    assert context.marker is not None and context.marker.provider_id == "2.0"
