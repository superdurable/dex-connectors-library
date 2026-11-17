# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded resend of WordPress posts whose creation outcome is unknown."""

from collections.abc import Callable
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

from dex_connectors.wordpress import Client, Config, CreatedPost, CreatePostInput, Credentials
from dex_connectors.wordpress._generated.definitions import CREATE_POST_DEFINITION

CONNECTION = ConnectionRef("wordpress", "wordpress")
CALL_ID = CallID("053fca46-6608-58b2-a693-d0aefe234186")
CREATED_BODY = {"id": 43, "status": "draft"}
SERVER_FAILURE = (500, {"code": "internal_server_error"})


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


Answer = Callable[[httpx.Request], tuple[int, object]]


async def create(
    answer: Answer,
    context: AttemptContext,
    requests: list[httpx.Request],
    *,
    max_sends: int = CREATE_POST_DEFINITION.max_sends,
    username: str = "editor",
) -> MutationResult[CreatedPost] | RetryError:
    """Run one Execute attempt of createPost, as the Mutation Step passes the definition cap."""

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        status, body = answer(request)
        return httpx.Response(status, json=body)

    credentials = StaticCredentialProvider({CONNECTION: Credentials(username, SecretString("abcd efgh"))})
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as transport:
        client = Client(Config(site_url="http://127.0.0.1:12345"), credentials, transport)
        call = Call(
            CALL_ID, CONNECTION, OperationRef("wordpress", "createPost"), context=context, max_sends=max_sends
        )
        try:
            post = CreatePostInput("Meridian release", "<p>Draft.</p>")
            return await run_mutation(client.create_post(), call, post)
        except RetryError as error:
            return error


def sequence(*answers: tuple[int, object]) -> Answer:
    """Answer each request with the next response, repeating the last."""
    responses = list(answers)

    def answer(_: httpx.Request) -> tuple[int, object]:
        return responses.pop(0) if len(responses) > 1 else responses[0]

    return answer


@pytest.mark.parametrize("status", [500, 502, 408, 409, 425, 418])
async def test_unknown_outcome_resends_below_cap(status: int) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    answer = sequence((status, {"code": "error"}), (201, CREATED_BODY))
    assert isinstance(await create(answer, context, requests), RetryError)
    assert context.marker is not None and context.marker.state == "claimed"
    context.attempt = 2
    result = await create(answer, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "created"
    assert context.marker is not None and context.marker.send_count == 2
    assert len(requests) == 2


async def test_unknown_outcome_selects_uncertain_at_cap() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    for attempt in range(1, CREATE_POST_DEFINITION.max_sends):
        context.attempt = attempt
        assert isinstance(await create(lambda _: SERVER_FAILURE, context, requests), RetryError)
    context.attempt = CREATE_POST_DEFINITION.max_sends
    result = await create(lambda _: SERVER_FAILURE, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    context.attempt += 1
    result = await create(lambda _: SERVER_FAILURE, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 3


async def test_application_send_cap_of_one_never_resends() -> None:
    requests: list[httpx.Request] = []
    result = await create(lambda _: SERVER_FAILURE, AttemptContext(), requests, max_sends=1)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 1


async def test_malformed_success_resends_below_cap() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    answer = sequence((201, "not a post"), (201, CREATED_BODY))
    assert isinstance(await create(answer, context, requests), RetryError)
    context.attempt = 2
    result = await create(answer, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "created"


@pytest.mark.parametrize("status", [400, 401, 403, 404, 413, 422])
async def test_definite_refusal_is_never_resent(status: int) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    result = await create(lambda _: (status, {"code": "rest_error"}), context, requests)
    assert isinstance(result, MutationResult) and result.branch == "providerRejected"
    assert context.marker is not None and context.marker.state == "canResend"
    assert len(requests) == 1


async def test_refusal_of_a_resend_selects_uncertain() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    answer = sequence(SERVER_FAILURE, (400, {"code": "rest_invalid_param"}))
    assert isinstance(await create(answer, context, requests), RetryError)
    context.attempt = 2
    result = await create(answer, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 2


async def test_accepted_but_unexpected_response_is_never_resent() -> None:
    requests: list[httpx.Request] = []
    unexpected = (200, {"id": 43, "status": "trash"})
    result = await create(lambda _: unexpected, AttemptContext(), requests)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 1


async def test_confirmed_post_is_never_resent() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    result = await create(lambda _: (201, CREATED_BODY), context, requests)
    assert isinstance(result, MutationResult) and result.branch == "created"
    context.attempt = 2
    result = await create(lambda _: (201, CREATED_BODY), context, requests)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 1


async def test_fail_closed_returns_retry() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    context.read_error = OSError("heartbeat unavailable")
    assert isinstance(await create(lambda _: (201, CREATED_BODY), context, requests), RetryError)
    assert requests == []


async def test_credential_failure_after_possible_send_retries() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert isinstance(await create(lambda _: SERVER_FAILURE, context, requests), RetryError)
    context.attempt = 2
    assert isinstance(await create(lambda _: SERVER_FAILURE, context, requests, username="a:b"), RetryError)
    assert len(requests) == 1
    result = await create(lambda _: SERVER_FAILURE, AttemptContext(), requests, username="a:b")
    assert isinstance(result, MutationResult) and result.branch == "defect"
