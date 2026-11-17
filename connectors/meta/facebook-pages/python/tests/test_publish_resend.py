# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded resend of Facebook Page posts whose outcome is unknown."""

from datetime import UTC, datetime, timedelta

import httpx
import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    MutationResult,
    OperationRef,
    RetryError,
    SecretString,
    StaticCredentialProvider,
    run_mutation,
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

Answer = tuple[int, bytes]


class AttemptContext:
    """Keep the last heartbeat across Execute attempts like Dex does."""

    def __init__(self) -> None:
        self.attempt = 1
        self.run_id = "run"
        self.deadline = datetime.now(UTC) + timedelta(seconds=30)
        self.marker: Marker | None = None
        self.read_error: Exception | None = None

    def has_last_heartbeat_value(self) -> bool:
        """Report whether an earlier attempt recorded a heartbeat."""
        if self.read_error is not None:
            raise self.read_error
        return self.marker is not None

    def get_last_heartbeat_value(self, value_type: type[object]) -> object:
        """Return the last marker."""
        return self.marker

    async def heartbeat(self, value: object = None) -> None:
        """Record the attempt's marker."""
        self.marker = value if isinstance(value, Marker) else None


async def publish(
    answers: list[Answer],
    context: AttemptContext,
    requests: list[httpx.Request],
    *,
    max_sends: int = PUBLISH_TEXT_POST_DEFINITION.max_sends,
    token: str = TOKEN,
) -> MutationResult[PublishedPost] | RetryError:
    """Run one Execute attempt of publishTextPost, answering requests in order."""

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        status, body = answers[min(len(requests) - 1, len(answers) - 1)]
        return httpx.Response(status, content=body)

    credentials = StaticCredentialProvider({CONNECTION: Credentials(SecretString(token))})
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as transport:
        client = Client(Config(base_url="http://127.0.0.1:12345/v25.0"), credentials, transport)
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
    "failure", [(500, b""), (502, b""), (408, b""), (409, b""), (425, b""), (418, b""), (200, b"{}")]
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


async def test_credential_failure_after_possible_send_retries() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert isinstance(await publish([SERVER_FAILURE], context, requests), RetryError)
    context.attempt = 2
    assert isinstance(await publish([SERVER_FAILURE], context, requests, token="bad\ntoken"), RetryError)
    assert len(requests) == 1
    result = await publish([SERVER_FAILURE], AttemptContext(), requests, token="bad\ntoken")
    assert isinstance(result, MutationResult) and result.branch == "defect"
