# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded resend of monday.com creates and updates whose outcome is unknown."""

import json
from collections.abc import AsyncIterator
from datetime import UTC, datetime, timedelta
from typing import Any

import dex_connectors.sdk as sdk
import httpx
import pytest
from dex_connectors.sdk.singledispatch import Marker

import dex_connectors.monday as monday
from dex_connectors.monday._generated.definitions import CREATE_ITEM_DEFINITION

CONNECTION = sdk.ConnectionRef("monday", "monday-test")
CALL_ID = sdk.CallID("d2f0e0a4-68c7-541e-aa07-d5ec3d088f1e")
TOKEN = "mondayTestToken0123456789abcdefSENTINELKEY"
CREATED_ITEM = {
    "data": {
        "create_item": {
            "id": "9876543210",
            "name": "Fire drill",
            "state": "active",
            "board": {"id": "1234567890"},
            "group": {"id": "topics", "title": "February"},
        }
    }
}
ADDED_UPDATE = {"data": {"create_update": {"id": "5550004", "item_id": "9876543210"}}}


class AttemptContext:
    """Keep the last heartbeat across Execute attempts like Dex does."""

    def __init__(self, attempt: int = 1) -> None:
        self.attempt = attempt
        self.run_id = "run"
        self.deadline = datetime.now(UTC) + timedelta(seconds=30)
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
        """Return the last heartbeat exactly as the SDK wrote it."""
        if self.read_error is not None:
            raise self.read_error
        return self.heartbeat_value

    async def heartbeat(self, value: object = None) -> None:
        """Record the attempt's heartbeat exactly as written."""
        self.heartbeat_value = value


class _BrokenBody(httpx.AsyncByteStream):
    """A response body that breaks after its first bytes."""

    async def __aiter__(self) -> AsyncIterator[bytes]:
        yield b'{"data":{"create_item":'
        raise httpx.ReadError("connection reset")


async def run(
    operation_name: str,
    status: int,
    body: Any,
    context: AttemptContext,
    requests: list[httpx.Request],
    *,
    max_sends: int = CREATE_ITEM_DEFINITION.max_sends,
    breaks_body: bool = False,
    has_credentials: bool = True,
) -> str:
    """Run one Execute attempt of a monday.com write and return its branch or `retry`."""

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if breaks_body:
            return httpx.Response(status, stream=_BrokenBody(), headers={"Content-Type": "application/json"})
        content = body if isinstance(body, str) else json.dumps(body)
        return httpx.Response(status, content=content.encode(), headers={"Content-Type": "application/json"})

    client = monday.Client(
        monday.Config(),
        sdk.StaticCredentialProvider(
            {CONNECTION: monday.Credentials("personal-api-token", sdk.SecretString(TOKEN))}
            if has_credentials
            else {}
        ),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
        endpoint="http://127.0.0.1:45101/v2",
    )
    call = sdk.Call(
        CALL_ID,
        CONNECTION,
        sdk.OperationRef("monday", operation_name),
        context=context,
        idempotency_key=sdk.IdempotencyKey(CALL_ID),
        max_sends=max_sends,
    )
    try:
        if operation_name == "createItem":
            created = await sdk.run_mutation(
                client.create_item(), call, monday.CreateItemInput("1234567890", item_name="Fire drill")
            )
            return created.branch
        added = await sdk.run_mutation(
            client.add_update(), call, monday.AddUpdateInput("9876543210", "Fire drill done")
        )
        return added.branch
    except sdk.RetryError:
        return "retry"
    finally:
        await client.http_client.aclose()


@pytest.mark.asyncio
async def test_unknown_outcome_resends_below_the_cap_then_is_uncertain() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    for attempt in range(1, CREATE_ITEM_DEFINITION.max_sends):
        context.attempt = attempt
        assert await run("createItem", 500, "{}", context, requests) == "retry"
        assert context.marker is not None and context.marker.send_count == attempt
    context.attempt = CREATE_ITEM_DEFINITION.max_sends
    assert await run("createItem", 500, "{}", context, requests) == "uncertain"
    context.attempt += 1
    assert await run("createItem", 500, "{}", context, requests) == "uncertain"
    assert len(requests) == 3
    assert {request.headers["Idempotency-Key"] for request in requests} == {CALL_ID}


@pytest.mark.asyncio
async def test_proven_unprocessed_refusal_does_not_count_a_send() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    blocked = {"errors": [{"extensions": {"code": "API_TEMPORARILY_BLOCKED"}}]}
    assert await run("addUpdate", 200, blocked, context, requests) == "retry"
    assert context.marker is not None and context.marker.state == "canResend"
    assert context.marker.send_count == 0
    context.attempt = 2
    assert await run("addUpdate", 200, ADDED_UPDATE, context, requests) == "added"
    assert context.marker is not None and context.marker.provider_id == "5550004"


@pytest.mark.asyncio
async def test_send_cap_of_one_never_resends() -> None:
    requests: list[httpx.Request] = []
    conflict = {"errors": [{"extensions": {"code": "IDEMPOTENCY_CONFLICT"}}]}
    assert await run("addUpdate", 409, conflict, AttemptContext(), requests, max_sends=1) == "uncertain"
    assert len(requests) == 1


@pytest.mark.asyncio
async def test_saved_identity_is_never_resent() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext(attempt=2)
    assert await run("createItem", 200, CREATED_ITEM, context, requests) == "created"
    assert context.marker is not None and context.marker.provider_id == "9876543210"
    context.attempt = 3
    assert await run("createItem", 200, CREATED_ITEM, context, requests) == "uncertain"
    assert len(requests) == 1


@pytest.mark.asyncio
async def test_identified_but_unusable_write_is_uncertain_without_resend() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    unusable = {"data": {"create_item": {"id": "9876543210", "state": "unknown"}}}
    assert await run("createItem", 200, unusable, context, requests) == "uncertain"
    assert context.marker is not None and context.marker.provider_id == "9876543210"
    assert len(requests) == 1


@pytest.mark.asyncio
async def test_refusal_beside_a_mutation_value_is_uncertain_without_resend() -> None:
    requests: list[httpx.Request] = []
    beside = {
        "data": {"create_item": {"id": "x"}},
        "errors": [{"extensions": {"code": "ColumnValueException"}}],
    }
    assert await run("createItem", 200, beside, AttemptContext(), requests) == "uncertain"
    assert len(requests) == 1


@pytest.mark.asyncio
async def test_two_xx_whose_body_breaks_is_uncertain_without_resend() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert await run("createItem", 200, "", context, requests, breaks_body=True) == "uncertain"
    assert context.marker is not None and context.marker.state == "claimed"
    assert len(requests) == 1


@pytest.mark.asyncio
async def test_rate_limit_after_an_unknown_outcome_keeps_counting_toward_the_cap() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    for attempt, status in enumerate([500, 429, 500], start=1):
        context.attempt = attempt
        assert await run("createItem", status, "{}", context, requests) == "retry"
    context.attempt = 4
    assert await run("createItem", 500, "{}", context, requests) == "uncertain"
    assert len(requests) == 4


@pytest.mark.asyncio
async def test_credential_failure_after_a_rate_limited_resend_never_records_not_sent() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    for attempt, status in enumerate([500, 429], start=1):
        context.attempt = attempt
        assert await run("addUpdate", status, "{}", context, requests) == "retry"
    before = context.heartbeat_value
    context.attempt = 3
    assert await run("addUpdate", 200, ADDED_UPDATE, context, requests, has_credentials=False) == "uncertain"
    assert context.heartbeat_value is before
    assert len(requests) == 2


@pytest.mark.asyncio
async def test_definite_refusal_records_can_resend_and_is_never_resent() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    rejected = {"errors": [{"extensions": {"code": "ColumnValueException"}}]}
    assert await run("createItem", 200, rejected, context, requests) == "providerRejected"
    assert context.marker is not None and context.marker.state == "canResend"
    assert len(requests) == 1


@pytest.mark.asyncio
async def test_refusal_of_a_resend_is_uncertain() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert await run("addUpdate", 502, "{}", context, requests) == "retry"
    context.attempt = 2
    assert await run("addUpdate", 403, "{}", context, requests) == "uncertain"
    assert len(requests) == 2


@pytest.mark.asyncio
async def test_unreadable_checkpoint_retries_without_sending() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext(2)
    context.read_error = RuntimeError("heartbeat store unavailable")
    assert await run("createItem", 200, CREATED_ITEM, context, requests) == "retry"
    assert requests == []


@pytest.mark.asyncio
async def test_absolute_update_retries_unknown_outcomes_and_discards_a_reflected_credential() -> None:
    requests: list[httpx.Request] = []

    async def update(status: int, body: str) -> str:
        def respond(request: httpx.Request) -> httpx.Response:
            requests.append(request)
            return httpx.Response(status, content=body.encode())

        client = monday.Client(
            monday.Config(),
            sdk.StaticCredentialProvider(
                {CONNECTION: monday.Credentials("personal-api-token", sdk.SecretString(TOKEN))}
            ),
            httpx.AsyncClient(transport=httpx.MockTransport(respond)),
            endpoint="http://127.0.0.1:45101/v2",
        )
        call = sdk.Call(
            CALL_ID,
            CONNECTION,
            sdk.OperationRef("monday", "updateItemColumnValues"),
            context=AttemptContext(),
            idempotency_key=sdk.IdempotencyKey(CALL_ID),
        )
        value = monday.ColumnValue(type="text", text="Ready")
        try:
            result = await sdk.run_mutation(
                client.update_item_column_values(),
                call,
                monday.UpdateItemColumnValuesInput("1234567890", "9876543210", {"text": value}),
            )
            return result.branch
        except sdk.RetryError:
            return "retry"
        finally:
            await client.http_client.aclose()

    assert await update(418, "{}") == "retry"
    assert await update(302, "") == "retry"
    assert await update(200, '{"data":{"change_multiple_column_values":{"id":"x"}}}') == "retry"
    reflected = '{"data":{"change_multiple_column_values":{"id":"9876543210","name":"' + TOKEN + '"}}}'
    assert await update(200, reflected) == "invalidResponse"
    assert await update(404, "{}") == "notFound"
