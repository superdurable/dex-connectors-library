# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded resend and read-back of Microsoft Teams posts whose outcome is unknown."""

import asyncio
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
    run_mutation,
)
from dex_connectors.sdk.projectconfig import (
    ConnectionNotSelectedError,
    CredentialBrokerRejectedError,
    CredentialBrokerUnavailableError,
    ObjectNotFoundError,
)
from dex_connectors.sdk.projectconfig.broker import RejectedCredentialNotRefreshedError

from dex_connectors.microsoft_teams import (
    Client,
    Config,
    Credentials,
    PostChannelMessageInput,
    PostChatMessageInput,
    PostMessageOutput,
)

REFERENCE = ConnectionRef("microsoft", "teams")
CALL_ID = CallID("74390522-858d-5ed6-bdf7-04e979ba9e4f")
TEAM_ID = "fbe2bf47-16c8-47cf-b4a5-4b9b187c508b"
CHANNEL_ID = "19:4a95f7d8db4c4e7fae857bcebe0623e6@thread.tacv2"
CHAT_ID = "19:meeting_MjdhNjM4YzUtYzExZi00@thread.v2"
BAD_GATEWAY = httpx.Response(502, json={"error": {"code": "BadGateway"}})
THROTTLED = httpx.Response(429, json={"error": {"code": "TooManyRequests"}})
EMPTY_LISTING = httpx.Response(200, json={"value": []})


class AttemptContext:
    """Keep the last heartbeat across Execute attempts like Dex does."""

    def __init__(self) -> None:
        self.attempt = 1
        self.run_id = "run"
        self.first_attempt_at = datetime.now(UTC)
        self.deadline = datetime.now(UTC) + timedelta(seconds=30)
        self.heartbeat_value: object = None
        self.heartbeat_count = 0
        self.heartbeat_error: Exception | None = None

    def has_last_heartbeat_value(self) -> bool:
        """Report whether an earlier attempt recorded a heartbeat."""
        return self.heartbeat_value is not None

    def get_last_heartbeat_value(self, value_type: object) -> object:
        """Return the last heartbeat exactly as the SDK wrote it."""
        return self.heartbeat_value

    async def heartbeat(self, value: object = None) -> None:
        """Record the heartbeat unless a test makes the write fail."""
        if self.heartbeat_error is not None:
            raise self.heartbeat_error
        self.heartbeat_count += 1
        self.heartbeat_value = value


class AttemptCredentials(CredentialProvider[Credentials]):
    """Resolve a static token, failing transiently on the listed attempts."""

    def __init__(self, failing_attempts: frozenset[int] = frozenset()) -> None:
        self.failing_attempts = failing_attempts

    async def resolve(self, call: Call) -> Credentials:
        """Resolve the token or fail transiently."""
        if call.context.attempt in self.failing_attempts:
            raise RuntimeError("token endpoint unavailable")
        return Credentials(
            oauth_client_id="client", oauth_client_secret=SecretString("secret"),
            access_token=SecretString("access-token"), refresh_token=SecretString("refresh"),
        )


Answer = Callable[[httpx.Request, int], httpx.Response]


async def post(
    answer: Answer, context: AttemptContext, requests: list[httpx.Request], *, operation: str = "channel",
    credentials: AttemptCredentials | None = None, max_sends: int = 3,
) -> MutationResult[PostMessageOutput] | RetryError:
    """Run one Execute attempt of a post operation against a scripted Graph."""
    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return answer(request, len(requests) - 1)

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as transport:
        client = Client(Config(), credentials or AttemptCredentials(), transport)
        if operation == "chat":
            call = Call(CALL_ID, REFERENCE, OperationRef("microsoft-teams", "postChatMessage"),
                        context=context, max_sends=max_sends)
            mutation = client.post_chat_message()
            value: object = PostChatMessageInput(chat_id=CHAT_ID, content="Escalate")
        else:
            call = Call(CALL_ID, REFERENCE, OperationRef("microsoft-teams", "postChannelMessage"),
                        context=context, max_sends=max_sends)
            mutation = client.post_channel_message()
            value = PostChannelMessageInput(team_id=TEAM_ID, channel_id=CHANNEL_ID, content="Incident")
        try:
            return await run_mutation(mutation, call, value)
        except RetryError as error:
            return error


def methods(requests: list[httpx.Request]) -> list[str]:
    """Return the HTTP methods in request order."""
    return [request.method for request in requests]


def uncertain(result: object) -> bool:
    """Report whether the attempt selected uncertain."""
    return isinstance(result, MutationResult) and result.branch == "uncertain"


async def test_accepted_post_without_message_id_is_uncertain_and_never_resent() -> None:
    for operation in ("channel", "chat"):
        requests: list[httpx.Request] = []
        result = await post(lambda _r, _i: httpx.Response(201, json={"id": 42}), AttemptContext(), requests,
                            operation=operation)
        assert uncertain(result)
        assert methods(requests) == ["POST"]


class StalledStream(httpx.AsyncByteStream):
    """A response body that stalls after the 2xx headers arrived."""

    async def __aiter__(self) -> AsyncIterator[bytes]:
        await asyncio.sleep(5)
        yield b"{}"


async def test_write_deadline_that_cuts_an_accepted_body_is_never_resent() -> None:
    """2xx headers arrived, so the Fence write-window timeout proves the post, not an unknown outcome."""
    for operation in ("channel", "chat"):
        context = AttemptContext()
        context.deadline = datetime.now(UTC) + timedelta(seconds=1.2)
        requests: list[httpx.Request] = []
        result = await post(
            lambda _r, _i: httpx.Response(201, stream=StalledStream()), context, requests, operation=operation
        )
        assert uncertain(result), operation
        assert methods(requests) == ["POST"]


async def test_chat_post_with_valid_id_and_failed_id_checkpoint_is_never_resent() -> None:
    context = AttemptContext()

    def answer(_request: httpx.Request, _index: int) -> httpx.Response:
        context.heartbeat_error = RuntimeError("dex unavailable")
        return httpx.Response(201, json={"id": "1616990032050"})

    requests: list[httpx.Request] = []
    result = await post(answer, context, requests, operation="chat")
    assert uncertain(result)
    assert methods(requests) == ["POST"]


async def test_channel_post_with_valid_id_and_unusable_body_reads_back_by_id() -> None:
    context = AttemptContext()
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request, _index: int) -> httpx.Response:
        if request.method == "POST":
            return httpx.Response(201, json={"id": "1616990032050", "createdDateTime": 42})
        assert request.url.path.endswith("/messages/1616990032050")
        return httpx.Response(404, json={"error": {"code": "NotFound"}})

    assert isinstance(await post(answer, context, requests), RetryError)
    context.attempt = 2
    result = await post(answer, context, requests)
    assert uncertain(result), "a missing read-back after acceptance is uncertain, never notFound or a resend"
    assert methods(requests) == ["POST", "GET"]


async def test_accepted_post_whose_body_breaks_is_never_resent() -> None:
    class BrokenStream(httpx.AsyncByteStream):
        async def __aiter__(self) -> AsyncIterator[bytes]:
            yield b'{"id":"16169'
            raise httpx.ReadError("connection reset")

    for operation in ("channel", "chat"):
        requests: list[httpx.Request] = []
        result = await post(
            lambda _r, _i: httpx.Response(201, stream=BrokenStream()), AttemptContext(), requests,
            operation=operation,
        )
        assert uncertain(result)
        assert methods(requests) == ["POST"]


async def test_throttled_resend_is_followed_by_read_back() -> None:
    context = AttemptContext()
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request, index: int) -> httpx.Response:
        if request.method == "GET":
            return EMPTY_LISTING
        return BAD_GATEWAY if index == 0 else THROTTLED

    assert isinstance(await post(answer, context, requests), RetryError)
    context.attempt = 2
    assert isinstance(await post(answer, context, requests), RetryError)
    assert methods(requests) == ["POST", "GET", "POST"]
    context.attempt = 3
    assert isinstance(await post(answer, context, requests), RetryError)
    assert methods(requests)[3:] == ["GET", "POST"], "the attempt after a throttled resend reads back first"


async def test_credential_failure_after_possible_send_records_nothing() -> None:
    context = AttemptContext()
    requests: list[httpx.Request] = []
    credentials = AttemptCredentials(frozenset({3}))

    def answer(request: httpx.Request, index: int) -> httpx.Response:
        if request.method == "GET":
            return EMPTY_LISTING
        return BAD_GATEWAY if index == 0 else THROTTLED

    for attempt in (1, 2):
        context.attempt = attempt
        assert isinstance(await post(answer, context, requests, credentials=credentials), RetryError)
    heartbeat_count = context.heartbeat_count
    context.attempt = 3
    assert isinstance(await post(answer, context, requests, credentials=credentials), RetryError)
    assert context.heartbeat_count == heartbeat_count, "a possible earlier send never records notSent"
    context.attempt = 4
    request_count = len(requests)
    assert isinstance(await post(answer, context, requests, credentials=credentials), RetryError)
    assert methods(requests)[request_count] == "GET"


async def test_short_write_window_with_failed_not_sent_checkpoint_retries_without_sending() -> None:
    context = AttemptContext()
    context.deadline = datetime.now(UTC) + timedelta(milliseconds=50)
    context.heartbeat_error = RuntimeError("dex unavailable")
    requests: list[httpx.Request] = []
    result = await post(lambda _r, _i: BAD_GATEWAY, context, requests, operation="chat", max_sends=1)
    assert isinstance(result, RetryError), "nothing was sent, so the last allowed send is never uncertain"
    assert requests == []


async def test_definite_refusal_is_never_resent() -> None:
    context = AttemptContext()
    requests: list[httpx.Request] = []
    forbidden = httpx.Response(403, json={"error": {"code": "Forbidden"}})
    result = await post(lambda _r, _i: forbidden, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "providerRejected"
    assert methods(requests) == ["POST"]


TERMINAL_CREDENTIAL_ERRORS: tuple[Exception, ...] = (
    ReauthorizationRequiredError(),
    ObjectNotFoundError(),
    ConnectionNotSelectedError(),
    CredentialBrokerRejectedError(),
)
TRANSIENT_CREDENTIAL_ERRORS: tuple[Exception, ...] = (
    CredentialBrokerUnavailableError(),
    RejectedCredentialNotRefreshedError(),
    ValueError("connection is not configured"),
    TimeoutError(),
    RuntimeError("unlisted"),
)


class FailingFromAttemptCredentials(AttemptCredentials):
    """Resolve the token before `first_failing_attempt` and raise `error` from then on."""

    def __init__(self, error: Exception, first_failing_attempt: int) -> None:
        super().__init__()
        self.error = error
        self.first_failing_attempt = first_failing_attempt

    async def resolve(self, call: Call) -> Credentials:
        """Raise the configured error from the configured attempt."""
        if call.context.attempt >= self.first_failing_attempt:
            raise self.error
        return await super().resolve(call)


@pytest.mark.parametrize("error", TERMINAL_CREDENTIAL_ERRORS + TRANSIENT_CREDENTIAL_ERRORS)
async def test_every_credential_failure_is_classified_the_same_way(error: Exception) -> None:
    """Only the four terminal classes end the post; every other failure retries without a request."""
    is_terminal = error in TERMINAL_CREDENTIAL_ERRORS
    first_branch, later_branch = ("defect", "uncertain") if is_terminal else ("retry", "retry")
    requests: list[httpx.Request] = []
    first = await post(lambda _r, _i: BAD_GATEWAY, AttemptContext(), requests,
                       credentials=FailingFromAttemptCredentials(error, 1))
    assert requests == []
    assert (first.branch if isinstance(first, MutationResult) else "retry") == first_branch

    context = AttemptContext()
    later_failing = FailingFromAttemptCredentials(error, 2)
    unknown = await post(lambda _r, _i: BAD_GATEWAY, context, requests, credentials=later_failing)
    assert isinstance(unknown, RetryError)
    context.attempt = 2
    later = await post(lambda _r, _i: BAD_GATEWAY, context, requests, credentials=later_failing)
    assert methods(requests) == ["POST"]
    assert (later.branch if isinstance(later, MutationResult) else "retry") == later_branch
