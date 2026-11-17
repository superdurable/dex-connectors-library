# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded resend of BambooHR employee additions whose outcome is unknown."""

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

from dex_connectors.bamboohr import AddEmployeeInput, Client, Config, Credentials
from dex_connectors.bamboohr._generated.definitions import ADD_EMPLOYEE_DEFINITION
from dex_connectors.bamboohr.models import AddEmployeeOutput

CONNECTION = ConnectionRef("bamboohr", "bamboohr-test")
CALL_ID = CallID("6c9e932e-fe09-5f01-8889-39650948771e")


class AttemptContext:
    """Keep the last heartbeat across Execute attempts like Dex does."""

    def __init__(self) -> None:
        self.attempt = 1
        self.run_id = "run"
        self.deadline = datetime.now(UTC) + timedelta(seconds=30)
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


async def add(
    status: int, body: dict[str, object], context: AttemptContext, requests: list[httpx.Request]
) -> MutationResult[AddEmployeeOutput] | RetryError:
    """Run one Execute attempt of addEmployee with the generated send cap."""

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(status, json=body)

    credentials = StaticCredentialProvider({CONNECTION: Credentials(SecretString("inert-key"))})
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as transport:
        client = Client(Config("acme"), credentials, transport, "http://127.0.0.1:22180/api/v1")
        call = Call(
            CALL_ID,
            CONNECTION,
            OperationRef("bamboohr", "addEmployee"),
            context=context,
            max_sends=ADD_EMPLOYEE_DEFINITION.max_sends,
        )
        try:
            return await run_mutation(
                client.add_employee(), call, AddEmployeeInput(first_name="Sam", last_name="Park")
            )
        except RetryError as error:
            return error


@pytest.mark.asyncio
@pytest.mark.parametrize("status", [500, 409])
async def test_unknown_outcome_resends_until_the_send_cap(status: int) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    for attempt in range(1, ADD_EMPLOYEE_DEFINITION.max_sends):
        context.attempt = attempt
        assert isinstance(await add(status, {}, context, requests), RetryError)
        assert context.marker is not None and context.marker.state == "claimed"
        assert context.marker.send_count == attempt
    context.attempt = ADD_EMPLOYEE_DEFINITION.max_sends
    result = await add(status, {}, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == ADD_EMPLOYEE_DEFINITION.max_sends
    context.attempt += 1
    result = await add(status, {}, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == ADD_EMPLOYEE_DEFINITION.max_sends, "no send is allowed at the cap"


@pytest.mark.asyncio
async def test_definite_refusal_of_first_send_records_can_resend() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    result = await add(400, {}, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "providerRejected"
    assert context.marker is not None and context.marker.state == "canResend"
    assert len(requests) == 1


@pytest.mark.asyncio
async def test_refusal_after_a_possible_send_selects_uncertain() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert isinstance(await add(502, {}, context, requests), RetryError)
    context.attempt = 2
    result = await add(400, {}, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 2


@pytest.mark.asyncio
async def test_saved_employee_id_is_read_back_instead_of_resent() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    context.attempt = 2
    context.marker = Marker(CALL_ID, 1, "claimed", "142", 1)
    result = await add(200, {"id": "142"}, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "created"
    assert [request.method for request in requests] == ["GET"]


@pytest.mark.asyncio
async def test_untrusted_checkpoint_returns_retry_without_sending() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    context.attempt = 2
    context.raw_marker = {"bamboohrDispatchedCallId": "earlier"}
    assert isinstance(await add(201, {"id": "142"}, context, requests), RetryError)
    assert requests == []
