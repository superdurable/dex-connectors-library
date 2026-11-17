# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded resend of Notion page creations whose outcome is unknown."""

import json
from datetime import UTC, datetime, timedelta

import dex_connectors.sdk as sdk
import httpx
import pytest
from dex_connectors.sdk.singledispatch import Marker

import dex_connectors.notion as notion
from dex_connectors.notion._generated.definitions import CREATE_PAGE_DEFINITION
from dex_connectors.notion.models import CreatePageInput, CreatePageOutput, PropertyValue

CONNECTION = sdk.ConnectionRef("notion", "notion-test")
CALL_ID = sdk.CallID("6eea9f59-2558-5319-bc0f-28be0a0e2fd3")
DATA_SOURCE_ID = "2a4d6f80-1b3c-4d5e-8f70-112233445566"
PAGE_ID = "1f3c9a2e-5b7d-4e8a-9c01-23456789abcd"
MAX_SENDS = CREATE_PAGE_DEFINITION.max_sends
PAGE = {
    "object": "page",
    "id": PAGE_ID,
    "parent": {"type": "data_source_id", "data_source_id": DATA_SOURCE_ID},
    "properties": {"Name": {"id": "title", "type": "title", "title": []}},
}


class AttemptContext:
    """Keep the last heartbeat across Execute attempts like Dex does."""

    def __init__(self) -> None:
        self.attempt = 1
        self.run_id = "run"
        self.deadline = datetime.now(UTC) + timedelta(seconds=120)
        self.marker: Marker | None = None
        self.raw_marker: object = None

    def has_last_heartbeat_value(self) -> bool:
        """Report whether an earlier attempt recorded a heartbeat."""
        return self.marker is not None or self.raw_marker is not None

    def get_last_heartbeat_value(self, value_type: type[object]) -> object:
        """Return the last marker, or an untrusted raw value."""
        return self.marker if self.marker is not None else self.raw_marker

    async def heartbeat(self, value: object = None) -> None:
        """Record the attempt's marker."""
        self.marker = value if isinstance(value, Marker) else None


async def create_page(
    status: int, body: dict[str, object], context: AttemptContext, requests: list[httpx.Request]
) -> sdk.MutationResult[CreatePageOutput] | sdk.RetryError:
    """Run one Execute attempt of createPage with the generated send cap."""

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if request.method == "GET":
            return httpx.Response(200, content=json.dumps(PAGE).encode())
        return httpx.Response(status, content=json.dumps(body).encode())

    credentials = sdk.StaticCredentialProvider(
        {CONNECTION: notion.Credentials(sdk.SecretString("ntn_test_token_value"))}
    )
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as transport:
        client = notion.Client(notion.Config(endpoint="http://127.0.0.1:12345"), credentials, transport)
        call = sdk.Call(
            CALL_ID,
            CONNECTION,
            sdk.OperationRef("notion", "createPage"),
            context=context,
            max_sends=MAX_SENDS,
        )
        page_input = CreatePageInput(
            data_source_id=DATA_SOURCE_ID, properties={"Name": PropertyValue("title", "Ada")}
        )
        try:
            return await sdk.run_mutation(client.create_page(), call, page_input)
        except sdk.RetryError as error:
            return error


def posts(requests: list[httpx.Request]) -> int:
    """Count the create POSTs among the recorded requests."""
    return sum(1 for request in requests if request.method == "POST")


@pytest.mark.asyncio
@pytest.mark.parametrize("status", [500, 409, 529])
async def test_unknown_outcome_resends_until_the_send_cap(status: int) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    for attempt in range(1, MAX_SENDS):
        context.attempt = attempt
        assert isinstance(await create_page(status, {"code": "unknown"}, context, requests), sdk.RetryError)
        assert context.marker is not None and context.marker.send_count == attempt
    context.attempt = MAX_SENDS
    result = await create_page(status, {}, context, requests)
    assert isinstance(result, sdk.MutationResult) and result.branch == "uncertain"
    context.attempt += 1
    result = await create_page(status, {}, context, requests)
    assert isinstance(result, sdk.MutationResult) and result.branch == "uncertain"
    assert posts(requests) == MAX_SENDS, "no send is allowed at the cap"


@pytest.mark.asyncio
async def test_definite_refusal_of_first_send_records_can_resend() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    result = await create_page(400, {"code": "validation_error"}, context, requests)
    assert isinstance(result, sdk.MutationResult) and result.branch == "providerRejected"
    assert context.marker is not None and context.marker.state == "canResend"


@pytest.mark.asyncio
async def test_refusal_after_a_possible_send_selects_uncertain() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert isinstance(await create_page(502, {}, context, requests), sdk.RetryError)
    context.attempt = 2
    result = await create_page(400, {"code": "validation_error"}, context, requests)
    assert isinstance(result, sdk.MutationResult) and result.branch == "uncertain"
    assert posts(requests) == 2


@pytest.mark.asyncio
async def test_saved_page_id_is_read_back_instead_of_resent() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    context.attempt = 2
    context.marker = Marker(CALL_ID, 1, "claimed", PAGE_ID, 1)  # type: ignore[arg-type]
    result = await create_page(500, {}, context, requests)
    assert isinstance(result, sdk.MutationResult) and result.branch == "created"
    assert [request.method for request in requests] == ["GET"]


@pytest.mark.asyncio
async def test_untrusted_checkpoint_returns_retry_without_sending() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    context.attempt = 2
    context.raw_marker = {"callId": "foreign", "attempt": 1, "state": "claimed"}
    assert isinstance(await create_page(200, PAGE, context, requests), sdk.RetryError)
    assert requests == []
