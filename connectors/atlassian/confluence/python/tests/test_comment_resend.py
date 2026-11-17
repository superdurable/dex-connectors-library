# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded resend of Confluence footer comments whose outcome is unknown."""

from datetime import UTC, datetime, timedelta

import dex_connectors.sdk as sdk
import httpx
import pytest
from dex_connectors.sdk.singledispatch import Marker

import dex_connectors.atlassian_confluence as confluence
from dex_connectors.atlassian_confluence._generated.definitions import ADD_COMMENT_DEFINITION

CONNECTION = sdk.ConnectionRef("atlassian", "confluence-site")
CALL_ID = sdk.CallID("1d4a9b66-087a-52c9-aee1-005513d3b6ac")
COMMENT = confluence.AddCommentInput(page_id="600002", body="Approved", body_format="plainText")


class AttemptContext:
    """Keep the last heartbeat across Execute attempts like Dex does."""

    def __init__(self, attempt: int = 1, marker: Marker | None = None) -> None:
        self.attempt = attempt
        self.run_id = "run"
        self.first_attempt_at = datetime.now(UTC)
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
    post_status: int, context: AttemptContext, requests: list[httpx.Request], *, max_sends: int = 3
) -> str:
    """Run one Execute attempt of addComment; read-back finds no comment."""

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if request.method == "GET":
            return httpx.Response(200, json={"results": []})
        return httpx.Response(post_status, json={"id": "700001"} if post_status < 300 else {})

    client = confluence.Client(
        confluence.Config(cloud_id="11223344-a1b2-4b33-8c44-def123456789", endpoint="http://127.0.0.1"),
        sdk.StaticCredentialProvider(
            {
                CONNECTION: confluence.Credentials(
                    "client", sdk.SecretString("secret"), sdk.SecretString("token"), sdk.SecretString("")
                )
            }
        ),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )
    operation = sdk.OperationRef("confluence", "addComment")
    call = sdk.Call(CALL_ID, CONNECTION, operation, context=context, max_sends=max_sends)
    try:
        return (await sdk.run_mutation(client.add_comment(), call, COMMENT)).branch
    except sdk.RetryError:
        return "retry"


def posts(requests: list[httpx.Request]) -> int:
    return sum(1 for request in requests if request.method == "POST")


@pytest.mark.asyncio
async def test_unknown_outcome_reads_back_and_resends_below_the_cap_then_is_uncertain() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    for attempt in range(1, ADD_COMMENT_DEFINITION.max_sends + 1):
        context.attempt = attempt
        assert await run(500, context, requests) == "retry"
    assert posts(requests) == ADD_COMMENT_DEFINITION.max_sends
    assert sum(1 for request in requests if request.method == "GET") == ADD_COMMENT_DEFINITION.max_sends - 1
    context.attempt += 1
    assert await run(500, context, requests) == "uncertain"
    assert posts(requests) == ADD_COMMENT_DEFINITION.max_sends


@pytest.mark.asyncio
async def test_send_cap_of_one_reads_back_instead_of_resending() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert await run(500, context, requests, max_sends=1) == "retry"
    context.attempt = 2
    assert await run(500, context, requests, max_sends=1) == "uncertain"
    assert posts(requests) == 1


@pytest.mark.asyncio
async def test_definite_refusal_records_can_resend_and_is_never_resent() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert await run(403, context, requests) == "providerRejected"
    assert context.marker is not None and context.marker.state == "canResend"
    assert posts(requests) == 1


@pytest.mark.asyncio
async def test_refusal_of_a_resend_is_uncertain() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert await run(502, context, requests) == "retry"
    context.attempt = 2
    assert await run(400, context, requests) == "uncertain"
    assert posts(requests) == 2


@pytest.mark.asyncio
async def test_untrusted_checkpoint_retries_without_sending() -> None:
    requests: list[httpx.Request] = []
    foreign = Marker(call_id=sdk.CallID("another-call"), attempt=1, state="claimed")
    assert await run(201, AttemptContext(2, foreign), requests) == "retry"
    assert requests == []
