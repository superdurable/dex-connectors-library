# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded, nonce-deduplicated resend of Discord messages whose outcome is unknown."""

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
    FailureKind,
    IdempotencyKey,
    MutationResult,
    OperationRef,
    ReauthorizationRequiredError,
    RetryError,
    SecretString,
    StaticCredentialProvider,
    run_mutation,
)
from dex_connectors.sdk.singledispatch import Marker

from dex_connectors.discord import Client, Config, CreateMessageInput, Credentials, Message
from dex_connectors.discord._generated.definitions import CREATE_MESSAGE_DEFINITION

CONNECTION = ConnectionRef("discord", "discord")
CALL_ID = CallID("9e237e1d-1296-5851-b23a-ee559a9b4208")
CHANNEL_ID = "112233445566778899"
MESSAGE_ID = "998877665544332211"
CREATED_BODY = {
    "id": MESSAGE_ID,
    "channel_id": CHANNEL_ID,
    "author": {"id": "123456789012345678", "username": "dex-bot", "bot": True},
    "content": "Release @everyone",
    "timestamp": "2026-10-13T20:00:00.123000+00:00",
    "edited_timestamp": None,
    "type": 0,
}
SERVER_FAILURE = (500, {"code": 0})


class AttemptContext:
    """Keep the last heartbeat across Execute attempts like Dex does."""

    def __init__(self) -> None:
        self.attempt = 1
        self.run_id = "run"
        self.deadline = datetime.now(UTC) + timedelta(seconds=30)
        self.heartbeat_value: object = None
        self.read_error: Exception | None = None
        self.write_error: Exception | None = None

    @property
    def marker(self) -> Marker | None:
        """Return the last recorded single-dispatch marker.

        SDK 0.12 records one Marker; later releases key wire entries by call ID.
        """
        if isinstance(self.heartbeat_value, Marker):
            return self.heartbeat_value
        if isinstance(self.heartbeat_value, dict):
            entries = self.heartbeat_value.get("singleDispatchMarkers")
            if isinstance(entries, dict) and str(CALL_ID) in entries:
                return Marker.from_wire(entries[str(CALL_ID)])
        return None

    def has_last_heartbeat_value(self) -> bool:
        """Report whether an earlier attempt recorded a heartbeat."""
        if self.read_error is not None:
            raise self.read_error
        return self.heartbeat_value is not None

    def get_last_heartbeat_value(self, value_type: object) -> object:
        """Return the last heartbeat as the SDK wrote it."""
        return self.heartbeat_value

    async def heartbeat(self, value: object = None) -> None:
        """Record the heartbeat exactly as the SDK wrote it."""
        if self.write_error is not None:
            raise self.write_error
        self.heartbeat_value = value


class FailingCredentialProvider:
    """Raise one credential error on every resolve."""

    def __init__(self, error: Exception) -> None:
        self.error = error

    async def resolve(self, call: Call) -> Credentials:
        """Raise the configured error."""
        raise self.error


class StalledBodyStream(httpx.AsyncByteStream):
    """Send part of a response body, then stall past the Fence write window."""

    async def __aiter__(self) -> AsyncIterator[bytes]:
        yield b'{"id":'
        await asyncio.sleep(5)
        yield b"null}"


class RawBody:
    """A response body sent byte for byte instead of as JSON."""

    def __init__(self, content: bytes) -> None:
        self.content = content


Answer = Callable[[httpx.Request], tuple[int, object]]


async def create(
    answer: Answer,
    context: AttemptContext,
    requests: list[httpx.Request],
    *,
    max_sends: int = CREATE_MESSAGE_DEFINITION.max_sends,
    max_response_bytes: int = 0,
    credentials: CredentialProvider[Credentials] | None = None,
) -> MutationResult[Message] | RetryError:
    """Run one Execute attempt of createMessage, as the Mutation Step passes the definition cap."""

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        status, body = answer(request)
        if isinstance(body, httpx.AsyncByteStream):
            return httpx.Response(status, stream=body)
        if isinstance(body, RawBody):
            return httpx.Response(status, content=body.content)
        return httpx.Response(status, json=body)

    if credentials is None:
        credentials = StaticCredentialProvider({CONNECTION: Credentials(SecretString("inert.test.token"))})
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as transport:
        config = Config(endpoint="http://127.0.0.1:22048", max_response_bytes=max_response_bytes)
        client = Client(config, credentials, transport)
        call = Call(
            CALL_ID,
            CONNECTION,
            OperationRef("discord", "createMessage"),
            IdempotencyKey(str(CALL_ID)),
            context=context,
            max_sends=max_sends,
        )
        try:
            return await run_mutation(
                client.create_message(), call, CreateMessageInput(CHANNEL_ID, "Release @everyone")
            )
        except RetryError as error:
            return error


def sequence(*answers: tuple[int, object]) -> Answer:
    """Answer each request with the next response, repeating the last."""
    responses = list(answers)

    def answer(_: httpx.Request) -> tuple[int, object]:
        return responses.pop(0) if len(responses) > 1 else responses[0]

    return answer


def assert_same_enforced_nonce(requests: list[httpx.Request]) -> None:
    """Every resend repeats the request with the same enforced nonce."""
    bodies = [json.loads(request.content) for request in requests]
    assert bodies and all(body == bodies[0] for body in bodies)
    assert bodies[0]["enforce_nonce"] is True
    assert len(bodies[0]["nonce"]) == 25


def is_branch(result: object, branch: str) -> bool:
    return isinstance(result, MutationResult) and result.branch == branch


@pytest.mark.parametrize(
    "first_answer",
    [(500, {"code": 0}), (502, {}), (408, {}), (409, {}), (425, {}), (403, CREATED_BODY)],
)
async def test_unknown_outcome_resends_the_same_nonce_below_the_cap(first_answer: tuple[int, object]) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    answer = sequence(first_answer, (200, CREATED_BODY))
    assert isinstance(await create(answer, context, requests), RetryError)
    assert context.marker is not None and context.marker.state == "claimed"
    context.attempt = 2
    result = await create(answer, context, requests)
    assert is_branch(result, "created")
    assert len(requests) == 2
    assert_same_enforced_nonce(requests)


async def test_unknown_outcome_selects_uncertain_at_the_cap() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    for attempt in range(1, CREATE_MESSAGE_DEFINITION.max_sends):
        context.attempt = attempt
        assert isinstance(await create(lambda _: SERVER_FAILURE, context, requests), RetryError)
    context.attempt = CREATE_MESSAGE_DEFINITION.max_sends
    assert is_branch(await create(lambda _: SERVER_FAILURE, context, requests), "uncertain")
    context.attempt += 1
    assert is_branch(await create(lambda _: SERVER_FAILURE, context, requests), "uncertain")
    assert len(requests) == 3
    assert_same_enforced_nonce(requests)


@pytest.mark.parametrize(
    ("answer", "branch"),
    [
        ((404, {"code": 10003}), "notFound"),
        ((403, {"code": 50013}), "providerRejected"),
        ((400, {"code": 50035}), "providerRejected"),
        ((401, {"code": 0}), "providerRejected"),
        ((422, {"code": 50035}), "providerRejected"),
    ],
)
async def test_definite_refusal_of_the_first_send_is_never_resent(
    answer: tuple[int, object], branch: str
) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert is_branch(await create(lambda _: answer, context, requests), branch)
    assert context.marker is not None and context.marker.state == "canResend"
    assert len(requests) == 1


async def test_refusal_after_a_possible_send_selects_uncertain() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    answer = sequence(SERVER_FAILURE, (403, {"code": 50013}))
    assert isinstance(await create(answer, context, requests), RetryError)
    context.attempt = 2
    assert is_branch(await create(answer, context, requests), "uncertain")


async def test_rate_limits_and_outages_retry_without_selecting_uncertain() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    answer = sequence((429, {"retry_after": 1.5}), (503, {"code": 0}), (200, CREATED_BODY))
    for attempt in (1, 2):
        context.attempt = attempt
        assert isinstance(await create(answer, context, requests), RetryError)
        assert context.marker is not None and context.marker.state == "canResend"
    context.attempt = 3
    assert is_branch(await create(answer, context, requests), "created")
    assert_same_enforced_nonce(requests)


async def test_unreadable_checkpoint_retries_without_sending() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    context.read_error = RuntimeError("heartbeat unavailable")
    assert isinstance(await create(lambda _: (200, CREATED_BODY), context, requests), RetryError)
    assert requests == []


async def test_oversized_confirmation_selects_uncertain_without_resending() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    answer: Answer = lambda _: (200, CREATED_BODY | {"content": "x" * 2000})  # noqa: E731
    result = await create(answer, context, requests, max_response_bytes=512)
    assert is_branch(result, "uncertain"), "a 2xx proves Discord created the message"
    assert len(requests) == 1


async def test_write_window_cutting_an_accepted_body_selects_uncertain_without_resending() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    context.deadline = datetime.now(UTC) + timedelta(seconds=1.2)
    result = await create(lambda _: (200, StalledBodyStream()), context, requests)
    assert is_branch(result, "uncertain"), "the 2xx status arrived before the write window ended"
    assert len(requests) == 1


@pytest.mark.parametrize(
    "body",
    [b"{", {"id": "x"}, {"channel_id": CHANNEL_ID}, {"id": int(MESSAGE_ID)}, {"ID": MESSAGE_ID}],
)
async def test_accepted_answer_without_a_message_id_selects_uncertain_without_resending(body: object) -> None:
    requests: list[httpx.Request] = []
    answer: Answer = lambda _: (200, RawBody(body) if isinstance(body, bytes) else body)  # noqa: E731
    result = await create(answer, AttemptContext(), requests)
    assert is_branch(result, "uncertain")
    assert isinstance(result, MutationResult) and result.failure is not None
    assert result.failure.kind == FailureKind.PROTOCOL
    assert len(requests) == 1


INCOMPLETE_ACCEPTED_BODY = {"id": MESSAGE_ID, "channel_id": CHANNEL_ID}
READ_BACK_PATH = f"/channels/{CHANNEL_ID}/messages/{MESSAGE_ID}"


def read_back(*answers: tuple[int, object], on_post: Callable[[], None] | None = None) -> Answer:
    """Answer the POST with an incomplete accepted message and each GET with the next answer."""
    next_read_back = sequence(*answers)

    def answer(request: httpx.Request) -> tuple[int, object]:
        if request.method == "POST":
            if on_post is not None:
                on_post()
            return 200, INCOMPLETE_ACCEPTED_BODY
        return next_read_back(request)

    return answer


def assert_one_post_then_read_backs(requests: list[httpx.Request], read_back_count: int) -> None:
    assert len(requests) == 1 + read_back_count
    assert requests[0].method == "POST"
    for request in requests[1:]:
        assert request.method == "GET", "an accepted message is never resent"
        assert request.url.path == READ_BACK_PATH


async def test_accepted_answer_with_a_message_id_reads_the_message_back() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    result = await create(read_back((200, CREATED_BODY)), context, requests)
    assert is_branch(result, "created")
    assert isinstance(result, MutationResult) and result.value.id == MESSAGE_ID
    assert context.marker is not None and context.marker.provider_id == MESSAGE_ID
    assert_one_post_then_read_backs(requests, 1)


@pytest.mark.parametrize("max_sends", [3, 1])
async def test_unknown_read_back_retries_with_the_saved_message_id_and_never_resends(max_sends: int) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    answer = read_back((503, {"code": 0}), (500, {"code": 0}), (200, CREATED_BODY))
    for attempt in (1, 2):
        context.attempt = attempt
        assert isinstance(await create(answer, context, requests, max_sends=max_sends), RetryError)
        assert context.marker is not None and context.marker.provider_id == MESSAGE_ID
    context.attempt = 3
    result = await create(answer, context, requests, max_sends=max_sends)
    assert is_branch(result, "created")
    assert_one_post_then_read_backs(requests, 3)


@pytest.mark.parametrize(
    "answer",
    [
        (404, {"code": 10008}),
        (403, {"code": 50001}),
        (409, {"code": 0}),
        # A complete 2xx does not change on a retry, so an unusable one is final.
        (200, CREATED_BODY | {"id": "998877665544332212"}),
        (200, INCOMPLETE_ACCEPTED_BODY),
    ],
)
async def test_final_read_back_answer_selects_uncertain(answer: tuple[int, object]) -> None:
    requests: list[httpx.Request] = []
    result = await create(read_back(answer), AttemptContext(), requests)
    assert is_branch(result, "uncertain")
    assert isinstance(result, MutationResult) and result.receipt.provider_object_id == MESSAGE_ID
    assert_one_post_then_read_backs(requests, 1)


@pytest.mark.parametrize(
    ("answer", "max_response_bytes"),
    [((408, {}), 0), ((429, {"retry_after": 0.5}), 0), ((200, CREATED_BODY | {"content": "x" * 2000}), 512)],
)
async def test_transient_read_back_answer_retries_with_the_saved_message_id(
    answer: tuple[int, object], max_response_bytes: int
) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    result = await create(read_back(answer), context, requests, max_response_bytes=max_response_bytes)
    assert isinstance(result, RetryError)
    assert context.marker is not None and context.marker.provider_id == MESSAGE_ID
    assert_one_post_then_read_backs(requests, 1)


async def test_unknown_read_back_without_a_saved_message_id_selects_uncertain() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()

    def fail_later_heartbeats() -> None:
        # The claim is already persisted, so only the message ID checkpoint fails.
        context.write_error = RuntimeError("heartbeat unavailable")

    answer = read_back((503, {"code": 0}), on_post=fail_later_heartbeats)
    assert is_branch(await create(answer, context, requests), "uncertain")
    assert_one_post_then_read_backs(requests, 1)


async def test_saved_message_id_with_a_credential_failure_never_resends() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    answer = read_back((503, {"code": 0}))
    assert isinstance(await create(answer, context, requests), RetryError)
    context.attempt = 2
    unavailable = FailingCredentialProvider(RuntimeError("broker timeout"))
    assert isinstance(await create(answer, context, requests, credentials=unavailable), RetryError)
    context.attempt = 3
    revoked = FailingCredentialProvider(ReauthorizationRequiredError("revoked"))
    assert is_branch(await create(answer, context, requests, credentials=revoked), "uncertain")
    assert_one_post_then_read_backs(requests, 1)


async def test_terminal_credential_failure_after_a_possible_send_selects_uncertain() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert isinstance(await create(lambda _: SERVER_FAILURE, context, requests), RetryError)
    context.attempt = 2
    revoked = FailingCredentialProvider(ReauthorizationRequiredError("revoked"))
    assert is_branch(
        await create(lambda _: SERVER_FAILURE, context, requests, credentials=revoked), "uncertain"
    )
    assert len(requests) == 1


async def test_transient_credential_failure_retries_without_sending() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    unavailable = FailingCredentialProvider(RuntimeError("broker down"))
    assert isinstance(
        await create(lambda _: SERVER_FAILURE, context, requests, credentials=unavailable), RetryError
    )
    assert context.marker is not None and context.marker.state == "notSent"
    assert requests == []


async def test_message_timestamp_without_timezone_reads_as_utc() -> None:
    """Read a provider timestamp without an offset as UTC, as the Go connector does."""
    requests: list[httpx.Request] = []
    body = CREATED_BODY | {"timestamp": "2026-10-13T20:00:00.123"}
    result = await create(lambda _: (200, body), AttemptContext(), requests)
    assert is_branch(result, "created")
    assert isinstance(result, MutationResult)
    assert result.value.timestamp == datetime(2026, 10, 13, 20, 0, 0, 123000, tzinfo=UTC)
    assert len(requests) == 1


async def test_read_back_timestamp_without_timezone_selects_created() -> None:
    """A read-back whose timestamp has no offset converts as UTC, so the matching message is created."""
    requests: list[httpx.Request] = []
    read_back_body = CREATED_BODY | {"timestamp": "2026-10-13T20:00:00"}
    result = await create(read_back((200, read_back_body)), AttemptContext(), requests)
    assert is_branch(result, "created")
    assert isinstance(result, MutationResult) and result.value.id == MESSAGE_ID
    assert_one_post_then_read_backs(requests, 1)


class FailingCloseStream(httpx.AsyncByteStream):
    """Send a complete body, then fail while the response closes."""

    def __init__(self, content: bytes) -> None:
        self.content = content

    async def __aiter__(self) -> AsyncIterator[bytes]:
        yield self.content

    async def aclose(self) -> None:
        raise RuntimeError("connection reset while closing")


async def test_unrelated_failure_after_an_observed_2xx_is_accepted_and_never_resent() -> None:
    """An exception after the 2xx status arrived keeps that status, so the message is not posted again."""
    requests: list[httpx.Request] = []
    context = AttemptContext()
    answer: Answer = lambda _: (200, FailingCloseStream(json.dumps(CREATED_BODY).encode()))  # noqa: E731
    result = await create(answer, context, requests)
    assert is_branch(result, "uncertain")
    assert len(requests) == 1
