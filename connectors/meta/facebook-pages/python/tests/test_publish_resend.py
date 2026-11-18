# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded resend of Facebook Page posts whose outcome is unknown."""

import asyncio
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
    run_mutation,
)
from dex_connectors.sdk.projectconfig import (
    ConnectionNotSelectedError,
    CredentialBrokerRejectedError,
    CredentialBrokerUnavailableError,
    ObjectNotFoundError,
)
from dex_connectors.sdk.singledispatch import Marker

from dex_connectors.meta_facebook_pages import (
    Client,
    Config,
    Credentials,
    PublishedPost,
    PublishTextPostInput,
)
from dex_connectors.meta_facebook_pages._generated.definitions import PUBLISH_TEXT_POST_DEFINITION

CONNECTION = ConnectionRef("facebook-pages", "facebook-pages")
CALL_ID = CallID("d2357ac8-f73e-5df4-952e-b68da926f540")
TOKEN = "synthetic-page-token"
PUBLISHED = (200, b'{"id":"123456789_987654321"}')
SERVER_FAILURE = (500, b'{"error":{"message":"unknown"}}')

Answer = tuple[int, bytes | httpx.AsyncByteStream]


class BrokenBodyStream(httpx.AsyncByteStream):
    """Send part of a response body, then drop the connection."""

    async def __aiter__(self):  # type: ignore[no-untyped-def]
        yield b'{"id":'
        raise httpx.ReadError("connection dropped")


class LateBodyStream(httpx.AsyncByteStream):
    """Send the accepted body only after the Fence write window has closed."""

    def __init__(self, delay_seconds: float) -> None:
        self.delay_seconds = delay_seconds

    async def __aiter__(self):  # type: ignore[no-untyped-def]
        await asyncio.sleep(self.delay_seconds)
        yield PUBLISHED[1]


class FailingCredentialProvider:
    """Fail every credential resolution with one error."""

    def __init__(self, error: Exception) -> None:
        self.error = error

    async def resolve(self, call: Call) -> Credentials:
        """Raise the configured credential error."""
        raise self.error


class AttemptContext:
    """Keep the last heartbeat across Execute attempts like Dex does."""

    def __init__(self, deadline_seconds: float = 30) -> None:
        self.attempt = 1
        self.run_id = "run"
        self.deadline = datetime.now(UTC) + timedelta(seconds=deadline_seconds)
        self.heartbeat_value: object = None
        self.read_error: Exception | None = None

    @property
    def marker(self) -> Marker | None:
        """Decode this call's marker from a single marker or the checkpoint map."""
        if isinstance(self.heartbeat_value, Marker) or self.heartbeat_value is None:
            return self.heartbeat_value
        assert isinstance(self.heartbeat_value, dict)
        return Marker.from_wire(self.heartbeat_value["singleDispatchMarkers"][str(CALL_ID)])

    def has_last_heartbeat_value(self) -> bool:
        """Report whether an earlier attempt recorded a heartbeat."""
        if self.read_error is not None:
            raise self.read_error
        return self.heartbeat_value is not None

    def get_last_heartbeat_value(self, value_type: type[object]) -> object:
        """Return the last heartbeat exactly as the SDK wrote it."""
        return self.heartbeat_value

    async def heartbeat(self, value: object = None) -> None:
        """Record the attempt's heartbeat exactly as written."""
        self.heartbeat_value = value


async def publish(
    answers: list[Answer],
    context: AttemptContext,
    requests: list[httpx.Request],
    *,
    max_sends: int = PUBLISH_TEXT_POST_DEFINITION.max_sends,
    token: str = TOKEN,
    max_response_bytes: int = 1048576,
    credential_error: Exception | None = None,
) -> MutationResult[PublishedPost] | RetryError:
    """Run one Execute attempt of publishTextPost, answering requests in order."""

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        status, body = answers[min(len(requests) - 1, len(answers) - 1)]
        if isinstance(body, httpx.AsyncByteStream):
            return httpx.Response(status, stream=body)
        return httpx.Response(status, content=body)

    credentials: CredentialProvider[Credentials] = StaticCredentialProvider(
        {CONNECTION: Credentials(SecretString(token))}
    )
    if credential_error is not None:
        credentials = FailingCredentialProvider(credential_error)
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as transport:
        config = Config(base_url="http://127.0.0.1:12345/v25.0", max_response_bytes=max_response_bytes)
        client = Client(config, credentials, transport)
        call = Call(
            CALL_ID,
            CONNECTION,
            OperationRef("facebook-pages", "publishTextPost"),
            context=context,
            max_sends=max_sends,
        )
        try:
            post = PublishTextPostInput("123456789", "Hello from Dex")
            return await run_mutation(client.publish_text_post(), call, post)
        except RetryError as error:
            return error


@pytest.mark.parametrize(
    "failure",
    [
        (500, b""),
        (502, b""),
        (408, b""),
        (409, b""),
        (425, b""),
        (418, b""),
        (400, b'{"error":{"code":2,"is_transient":true}}'),
    ],
)
async def test_unknown_outcome_resends_below_cap(failure: Answer) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    answers = [failure, PUBLISHED]
    assert isinstance(await publish(answers, context, requests), RetryError)
    assert context.marker is not None and context.marker.state == "claimed"
    context.attempt = 2
    result = await publish(answers, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "published"
    assert context.marker is not None and context.marker.send_count == 2
    assert len(requests) == 2


async def test_unknown_outcome_selects_uncertain_at_cap() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    for attempt in range(1, PUBLISH_TEXT_POST_DEFINITION.max_sends):
        context.attempt = attempt
        assert isinstance(await publish([SERVER_FAILURE], context, requests), RetryError)
    context.attempt = PUBLISH_TEXT_POST_DEFINITION.max_sends
    result = await publish([SERVER_FAILURE], context, requests)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    context.attempt += 1
    result = await publish([SERVER_FAILURE], context, requests)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 3


async def test_application_send_cap_of_one_never_resends() -> None:
    requests: list[httpx.Request] = []
    result = await publish([SERVER_FAILURE], AttemptContext(), requests, max_sends=1)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 1


@pytest.mark.parametrize("status", [400, 401, 403, 404, 413, 422])
async def test_definite_refusal_is_never_resent(status: int) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    result = await publish([(status, b'{"error":{"code":100}}')], context, requests)
    assert isinstance(result, MutationResult) and result.branch == "providerRejected"
    assert context.marker is not None and context.marker.state == "canResend"
    assert len(requests) == 1


async def test_graph_throttle_retries_with_can_resend() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert isinstance(await publish([(400, b'{"error":{"code":613}}')], context, requests), RetryError)
    assert context.marker is not None and context.marker.state == "canResend"


async def test_graph_transient_error_is_unknown_and_selects_uncertain_at_cap() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    answer: Answer = (400, b'{"error":{"code":2,"is_transient":true}}')
    for attempt in range(1, PUBLISH_TEXT_POST_DEFINITION.max_sends):
        context.attempt = attempt
        assert isinstance(await publish([answer], context, requests), RetryError)
        assert context.marker is not None and context.marker.state == "claimed"
    context.attempt = PUBLISH_TEXT_POST_DEFINITION.max_sends
    result = await publish([answer], context, requests)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 3


@pytest.mark.parametrize(
    ("answer", "max_response_bytes"),
    [((200, b"{}"), 1048576), ((200, b"not JSON"), 1048576), ((200, b'{"id":"' + b"1" * 80 + b'"}'), 64)],
)
async def test_accepted_unreadable_response_is_never_resent(answer: Answer, max_response_bytes: int) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    result = await publish([answer], context, requests, max_response_bytes=max_response_bytes)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 1


async def test_accepted_body_breaking_after_headers_selects_uncertain() -> None:
    requests: list[httpx.Request] = []
    result = await publish([(200, BrokenBodyStream())], AttemptContext(), requests)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 1


async def test_accepted_body_arriving_after_write_window_is_published_once() -> None:
    requests: list[httpx.Request] = []
    # The 1.2 s deadline closes the write window after 0.7 s, before the body arrives.
    result = await publish([(200, LateBodyStream(1.0))], AttemptContext(deadline_seconds=1.2), requests)
    assert isinstance(result, MutationResult) and result.branch == "published"
    assert len(requests) == 1


@pytest.mark.parametrize(
    "error",
    [
        CredentialBrokerUnavailableError("broker down"),
        OSError("store read failed"),
        RuntimeError("bug"),
        ValueError("connection is not configured"),
        LookupError("unlisted lookup failure"),
    ],
)
async def test_transient_credential_failure_before_send_retries_without_a_send(error: Exception) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert isinstance(await publish([PUBLISHED], context, requests, credential_error=error), RetryError)
    assert requests == []
    assert context.marker is not None and context.marker.state == "notSent"
    context.attempt = 2
    result = await publish([PUBLISHED], context, requests)
    assert isinstance(result, MutationResult) and result.branch == "published"
    assert context.marker is not None and context.marker.send_count == 1
    assert len(requests) == 1


@pytest.mark.parametrize(
    "error",
    [
        ReauthorizationRequiredError(),
        ObjectNotFoundError(),
        ConnectionNotSelectedError(),
        CredentialBrokerRejectedError(),
    ],
)
async def test_terminal_credential_failure_before_send_selects_defect(error: Exception) -> None:
    requests: list[httpx.Request] = []
    result = await publish([PUBLISHED], AttemptContext(), requests, credential_error=error)
    assert isinstance(result, MutationResult) and result.branch == "defect"
    assert requests == []


async def test_unreadable_client_error_is_unknown() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    answers: list[Answer] = [(400, b'{"error":{"code":100,"message":"' + b"x" * 100 + b'"}}'), PUBLISHED]
    assert isinstance(await publish(answers, context, requests, max_response_bytes=64), RetryError)
    assert context.marker is not None and context.marker.state == "claimed"
    context.attempt = 2
    result = await publish(answers, context, requests, max_response_bytes=64)
    assert isinstance(result, MutationResult) and result.branch == "published"
    assert len(requests) == 2


async def test_unlisted_client_error_with_post_id_selects_uncertain() -> None:
    requests: list[httpx.Request] = []
    result = await publish([(409, PUBLISHED[1])], AttemptContext(), requests)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 1


async def test_refusal_after_rate_limited_resend_selects_uncertain() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    answers: list[Answer] = [SERVER_FAILURE, (429, b""), (400, b'{"error":{"code":100}}')]
    for attempt in (1, 2):
        context.attempt = attempt
        assert isinstance(await publish(answers, context, requests), RetryError)
    context.attempt = 3
    result = await publish(answers, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 3


async def test_refusal_of_a_resend_selects_uncertain() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    answers = [SERVER_FAILURE, (400, b'{"error":{"code":100}}')]
    assert isinstance(await publish(answers, context, requests), RetryError)
    context.attempt = 2
    result = await publish(answers, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 2


async def test_checkpointed_post_completes_without_resending() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    result = await publish([PUBLISHED], context, requests)
    assert isinstance(result, MutationResult) and result.branch == "published"
    context.attempt = 2
    result = await publish([PUBLISHED], context, requests)
    assert isinstance(result, MutationResult) and result.branch == "published"
    assert result.value.post_id == "123456789_987654321"
    assert len(requests) == 1


async def test_fail_closed_returns_retry() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    context.read_error = OSError("heartbeat unavailable")
    assert isinstance(await publish([PUBLISHED], context, requests), RetryError)
    assert requests == []


async def test_invalid_credentials_after_possible_send_select_uncertain() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert isinstance(await publish([SERVER_FAILURE], context, requests), RetryError)
    context.attempt = 2
    result = await publish([SERVER_FAILURE], context, requests, token="bad\ntoken")
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 1
    result = await publish([SERVER_FAILURE], AttemptContext(), requests, token="bad\ntoken")
    assert isinstance(result, MutationResult) and result.branch == "defect"
