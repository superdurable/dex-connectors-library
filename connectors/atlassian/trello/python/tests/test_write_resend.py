# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded resend of Trello creates and comments whose outcome is unknown."""

from datetime import UTC, datetime, timedelta

import dex_connectors.sdk as sdk
import httpx
import pytest
from dex_connectors.sdk.singledispatch import Marker

import dex_connectors.atlassian_trello as trello
from dex_connectors.atlassian_trello._generated.definitions import (
    ADD_COMMENT_DEFINITION,
    CREATE_CARD_DEFINITION,
)

CONNECTION = sdk.ConnectionRef("trello", "trello-test")
CALL_ID = sdk.CallID("fed5a116-c5f6-53ac-81b1-a823fb443a88")
LIST_ID = "6512f0a1c2d3e4f5a6b70101"
CARD_ID = "6512f0a1c2d3e4f5a6b7c8d9"


class AttemptContext:
    """Keep the last heartbeat across Execute attempts like Dex does."""

    def __init__(self, attempt: int = 1, marker: Marker | None = None) -> None:
        self.attempt = attempt
        self.run_id = "run"
        self.deadline = datetime.now(UTC) + timedelta(seconds=30)
        self.marker = marker

    def has_last_heartbeat_value(self) -> bool:
        """Report whether an earlier attempt recorded a heartbeat."""
        return self.marker is not None

    def get_last_heartbeat_value(self, value_type: type[object]) -> object:
        """Return the last marker."""
        return self.marker

    async def heartbeat(self, value: object = None) -> None:
        """Record the attempt's marker."""
        self.marker = value if isinstance(value, Marker) else None


async def run(
    operation_name: str,
    status: int,
    body: str,
    context: AttemptContext,
    requests: list[httpx.Request],
    *,
    max_sends: int = CREATE_CARD_DEFINITION.max_sends,
) -> str:
    """Run one Execute attempt of a Trello write and return its branch or `retry`."""

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(status, content=body.encode(), headers={"Content-Type": "application/json"})

    client = trello.Client(
        trello.Config("http://127.0.0.1:12345/1"),
        sdk.StaticCredentialProvider(
            {CONNECTION: trello.Credentials(sdk.SecretString("test-key"), sdk.SecretString("test-token"))}
        ),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )
    call = sdk.Call(
        CALL_ID, CONNECTION, sdk.OperationRef("trello", operation_name), context=context, max_sends=max_sends
    )
    try:
        if operation_name == "createCard":
            result = await sdk.run_mutation(
                client.create_card(), call, trello.CreateCardInput(LIST_ID, "Approved request")
            )
        else:
            comment = trello.AddCommentInput(CARD_ID, "Approved.")
            result = await sdk.run_mutation(client.add_comment(), call, comment)
        return result.branch
    except sdk.RetryError:
        return "retry"
    finally:
        await client.http_client.aclose()


@pytest.mark.asyncio
async def test_unknown_outcome_resends_below_the_cap_then_is_uncertain() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    for attempt in range(1, CREATE_CARD_DEFINITION.max_sends):
        context.attempt = attempt
        assert await run("createCard", 500, "{}", context, requests) == "retry"
        assert context.marker is not None and context.marker.send_count == attempt
    context.attempt = CREATE_CARD_DEFINITION.max_sends
    assert await run("createCard", 500, "{}", context, requests) == "uncertain"
    context.attempt += 1
    assert await run("createCard", 500, "{}", context, requests) == "uncertain"
    assert len(requests) == 3


@pytest.mark.asyncio
async def test_send_cap_of_one_never_resends() -> None:
    requests: list[httpx.Request] = []
    assert await run("addComment", 409, "{}", AttemptContext(), requests, max_sends=1) == "uncertain"
    assert len(requests) == 1


@pytest.mark.asyncio
async def test_later_attempt_resends_and_saved_identity_is_never_resent() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext(attempt=2)
    assert await run("createCard", 200, f'{{"id":"{CARD_ID}"}}', context, requests) == "created"
    assert context.marker is not None and context.marker.provider_id == CARD_ID
    context.attempt = 3
    assert await run("createCard", 200, f'{{"id":"{CARD_ID}"}}', context, requests) == "uncertain"
    at_cap = AttemptContext(attempt=ADD_COMMENT_DEFINITION.max_sends + 1)
    assert await run("addComment", 200, f'{{"id":"{CARD_ID}"}}', at_cap, requests) == "uncertain"
    assert len(requests) == 1


@pytest.mark.asyncio
async def test_definite_refusal_records_can_resend_and_is_never_resent() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert await run("createCard", 400, "invalid value for idList", context, requests) == "providerRejected"
    assert context.marker is not None and context.marker.state == "canResend"
    assert len(requests) == 1


@pytest.mark.asyncio
async def test_refusal_of_a_resend_is_uncertain() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert await run("addComment", 502, "{}", context, requests) == "retry"
    context.attempt = 2
    assert await run("addComment", 401, "invalid token", context, requests) == "uncertain"
    assert len(requests) == 2


@pytest.mark.asyncio
async def test_untrusted_checkpoint_retries_without_sending() -> None:
    requests: list[httpx.Request] = []
    foreign = Marker(call_id=sdk.CallID("another-call"), attempt=1, state="claimed")
    assert await run("createCard", 200, "{}", AttemptContext(2, foreign), requests) == "retry"
    assert requests == []
