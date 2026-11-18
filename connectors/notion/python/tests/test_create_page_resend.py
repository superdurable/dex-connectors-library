# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded resend of Notion page creations whose outcome is unknown."""

import asyncio
import json
from collections.abc import AsyncIterator, Callable
from datetime import UTC, datetime, timedelta

import dex_connectors.sdk as sdk
import httpx
import pytest
from dex_connectors.sdk.singledispatch import Marker, State

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
        return self.heartbeat_value is not None or self.read_error is not None

    def get_last_heartbeat_value(self, value_type: type[object]) -> object:
        """Return the last heartbeat, translating a seeded marker to the checkpoint map the SDK reads."""
        if self.read_error is not None:
            raise self.read_error
        if isinstance(self.heartbeat_value, Marker) and value_type is not Marker:
            marker = self.heartbeat_value
            return {"singleDispatchMarkers": {str(marker.call_id): marker.to_wire()}}
        return self.heartbeat_value

    async def heartbeat(self, value: object = None) -> None:
        """Record the attempt's heartbeat exactly as written."""
        self.heartbeat_value = value


async def create_page(
    status: int, body: dict[str, object], context: AttemptContext, requests: list[httpx.Request]
) -> sdk.MutationResult[CreatePageOutput] | sdk.RetryError:
    """Run one Execute attempt of createPage with the generated send cap."""

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if request.method == "GET":
            return httpx.Response(200, content=json.dumps(PAGE).encode())
        return httpx.Response(status, content=json.dumps(body).encode())

    return await run_create_page(respond, context)


class _BodyBreaksAfterHeaders(httpx.AsyncByteStream):
    """A response body whose connection drops after the status and headers arrived."""

    async def __aiter__(self) -> AsyncIterator[bytes]:
        raise httpx.ReadError("connection reset after headers")
        yield b""


class _RevokedCredentials:
    async def resolve(self, call: sdk.Call) -> notion.Credentials:
        raise sdk.ReauthorizationRequiredError()


async def run_create_page(
    respond: Callable[[httpx.Request], httpx.Response],
    context: AttemptContext,
    credentials: sdk.CredentialProvider[notion.Credentials] | None = None,
) -> sdk.MutationResult[CreatePageOutput] | sdk.RetryError:
    """Run one Execute attempt of createPage against a provider handler."""
    if credentials is None:
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
    context.heartbeat_value = Marker(CALL_ID, 1, State.CLAIMED, PAGE_ID, 1)
    result = await create_page(500, {}, context, requests)
    assert isinstance(result, sdk.MutationResult) and result.branch == "created"
    assert [request.method for request in requests] == ["GET"]


@pytest.mark.asyncio
async def test_accepted_post_whose_body_breaks_is_never_resent() -> None:
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200, stream=_BodyBreaksAfterHeaders())

    result = await run_create_page(respond, AttemptContext())
    assert isinstance(result, sdk.MutationResult) and result.branch == "uncertain"
    assert len(requests) == 1


class _SlowBody(httpx.AsyncByteStream):
    """Send the status and headers, then hold the body past the write window."""

    async def __aiter__(self) -> AsyncIterator[bytes]:
        await asyncio.sleep(30)
        yield b"{}"


@pytest.mark.asyncio
async def test_deadline_after_accepted_headers_is_never_resent() -> None:
    """A write window that ends after Notion answered 2xx still means the page was accepted."""
    requests: list[httpx.Request] = []
    context = AttemptContext()
    context.deadline = datetime.now(UTC) + timedelta(seconds=3)

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200, stream=_SlowBody())

    result = await run_create_page(respond, context)
    assert isinstance(result, sdk.MutationResult) and result.branch == "uncertain"
    assert posts(requests) == 1


@pytest.mark.asyncio
async def test_deadline_after_rate_limit_headers_records_can_resend() -> None:
    """A 429 proves nothing was written, even when the write window cuts its body."""
    requests: list[httpx.Request] = []
    context = AttemptContext()
    context.deadline = datetime.now(UTC) + timedelta(seconds=3)

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(429, stream=_SlowBody())

    assert isinstance(await run_create_page(respond, context), sdk.RetryError)
    assert context.marker is not None and context.marker.state == "canResend"
    assert posts(requests) == 1


@pytest.mark.asyncio
async def test_saved_page_id_is_read_back_when_the_decision_allows_a_send() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    context.attempt = 3
    context.heartbeat_value = Marker(CALL_ID, 2, State.CAN_RESEND, PAGE_ID, 1)
    result = await create_page(500, {}, context, requests)
    assert isinstance(result, sdk.MutationResult) and result.branch == "created"
    assert [request.method for request in requests] == ["GET"]


@pytest.mark.asyncio
async def test_unsaved_accepted_page_id_with_failed_read_back_selects_uncertain() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()

    async def lose_heartbeat(value: object = None) -> None:
        raise RuntimeError("heartbeat unavailable")

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if request.method == "GET":
            return httpx.Response(503, json={"object": "error", "code": "service_unavailable"})
        context.heartbeat = lose_heartbeat  # type: ignore[method-assign]
        return httpx.Response(200, json={"object": "page", "id": PAGE_ID, "properties": "bad"})

    result = await run_create_page(respond, context)
    assert isinstance(result, sdk.MutationResult) and result.branch == "uncertain"
    assert [request.method for request in requests] == ["POST", "GET"]


@pytest.mark.asyncio
async def test_oversized_refusal_is_an_unknown_outcome() -> None:
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        refusal = {"object": "error", "code": "validation_error", "message": "x" * 70000}
        return httpx.Response(400, json=refusal)

    assert isinstance(await run_create_page(respond, AttemptContext()), sdk.RetryError)
    assert len(requests) == 1


@pytest.mark.asyncio
async def test_terminal_credentials_during_read_back_select_uncertain() -> None:
    def respond(request: httpx.Request) -> httpx.Response:
        raise AssertionError("no request may be sent")

    context = AttemptContext()
    context.attempt = 2
    context.heartbeat_value = Marker(CALL_ID, 1, State.CLAIMED, PAGE_ID, 1)
    result = await run_create_page(respond, context, _RevokedCredentials())
    assert isinstance(result, sdk.MutationResult) and result.branch == "uncertain"


@pytest.mark.asyncio
async def test_unreadable_checkpoint_returns_retry_without_sending() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    context.attempt = 2
    context.read_error = RuntimeError("heartbeat store unavailable")
    assert isinstance(await create_page(200, PAGE, context, requests), sdk.RetryError)
    assert requests == []
