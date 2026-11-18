# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded resend of BambooHR employee additions whose outcome is unknown."""

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
    StaticCredentialProvider,
    run_mutation,
)
from dex_connectors.sdk.singledispatch import Marker, State

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
        self.heartbeat_value: object = None
        self.heartbeat_read_error: Exception | None = None

    @property
    def marker(self) -> Marker | None:
        """Decode this call's checkpoint from either SDK heartbeat format."""
        if isinstance(self.heartbeat_value, Marker) or self.heartbeat_value is None:
            return self.heartbeat_value
        assert isinstance(self.heartbeat_value, dict)
        entry = self.heartbeat_value["singleDispatchMarkers"].get(str(CALL_ID))
        return None if entry is None else Marker.from_wire(entry)

    def has_last_heartbeat_value(self) -> bool:
        """Report whether an earlier attempt recorded a heartbeat."""
        return self.heartbeat_value is not None or self.heartbeat_read_error is not None

    def get_last_heartbeat_value(self, value_type: type[object]) -> object:
        """Return the last heartbeat as the reading SDK expects it."""
        if self.heartbeat_read_error is not None:
            raise self.heartbeat_read_error
        seeded = self.heartbeat_value
        if isinstance(seeded, Marker) and value_type is not Marker:
            return {"singleDispatchMarkers": {str(seeded.call_id): seeded.to_wire()}}
        if isinstance(seeded, dict) and value_type is Marker:
            return self.marker
        return seeded

    async def heartbeat(self, value: object = None) -> None:
        """Record the heartbeat exactly as the SDK wrote it."""
        self.heartbeat_value = value


async def add(
    status: int, body: dict[str, object], context: AttemptContext, requests: list[httpx.Request]
) -> MutationResult[AddEmployeeOutput] | RetryError:
    """Run one Execute attempt of addEmployee with the generated send cap."""

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(status, json=body)

    return await run_add(respond, context)


class _BodyBreaksAfterHeaders(httpx.AsyncByteStream):
    """A response body whose connection drops after the status and headers arrived."""

    async def __aiter__(self) -> AsyncIterator[bytes]:
        raise httpx.ReadError("connection reset after headers")
        yield b""


class _FailingCredentialProvider:
    def __init__(self, error: Exception) -> None:
        self.error = error

    async def resolve(self, call: Call) -> Credentials:
        raise self.error


async def run_add(
    respond: Callable[[httpx.Request], httpx.Response],
    context: AttemptContext,
    credentials: CredentialProvider[Credentials] | None = None,
) -> MutationResult[AddEmployeeOutput] | RetryError:
    """Run one Execute attempt of addEmployee against a provider handler."""
    if credentials is None:
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
@pytest.mark.parametrize("status", [500, 408])
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
@pytest.mark.parametrize("status", [400, 409])
async def test_definite_refusal_of_first_send_records_can_resend(status: int) -> None:
    """A 400, or the documented duplicate-email 409, is a definite outcome of a first send."""
    requests: list[httpx.Request] = []
    context = AttemptContext()
    result = await add(status, {}, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "providerRejected"
    assert context.marker is not None and context.marker.state == "canResend"
    assert len(requests) == 1


@pytest.mark.asyncio
@pytest.mark.parametrize("status", [400, 409])
async def test_refusal_after_a_possible_send_selects_uncertain(status: int) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert isinstance(await add(502, {}, context, requests), RetryError)
    context.attempt = 2
    result = await add(status, {}, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 2


@pytest.mark.asyncio
async def test_saved_employee_id_is_read_back_instead_of_resent() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    context.attempt = 2
    context.heartbeat_value = Marker(CALL_ID, 1, State.CLAIMED, "142", 1)
    result = await add(200, {"id": "142"}, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "created"
    assert [request.method for request in requests] == ["GET"]


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("headers", "branch"),
    [({"Location": "https://acme.bamboohr.com/api/v1/employees/142"}, "created"), ({}, "uncertain")],
)
async def test_accepted_post_whose_body_breaks_is_never_resent(headers: dict[str, str], branch: str) -> None:
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(201, headers=headers, stream=_BodyBreaksAfterHeaders())

    result = await run_add(respond, AttemptContext())
    assert isinstance(result, MutationResult) and result.branch == branch
    assert len(requests) == 1


@pytest.mark.asyncio
async def test_send_before_a_429_still_counts_for_a_later_refusal() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    for attempt, status in ((1, 500), (2, 429)):
        context.attempt = attempt
        assert isinstance(await add(status, {}, context, requests), RetryError)
    context.attempt = 3
    result = await add(400, {}, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert [request.method for request in requests] == ["POST", "POST", "POST"]


@pytest.mark.asyncio
async def test_credential_failure_after_a_possible_send_retries_only_when_transient() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert isinstance(await add(502, {}, context, requests), RetryError)

    def respond(request: httpx.Request) -> httpx.Response:
        raise AssertionError("no request may be sent")

    context.attempt = 2
    transient = await run_add(respond, context, _FailingCredentialProvider(RuntimeError("store unavailable")))
    assert isinstance(transient, RetryError)
    context.attempt = 3
    terminal = await run_add(respond, context, _FailingCredentialProvider(ReauthorizationRequiredError()))
    assert isinstance(terminal, MutationResult) and terminal.branch == "uncertain"
    assert len(requests) == 1


@pytest.mark.asyncio
async def test_terminal_credentials_during_read_back_select_uncertain() -> None:
    def respond(request: httpx.Request) -> httpx.Response:
        raise AssertionError("no request may be sent")

    context = AttemptContext()
    context.attempt = 2
    context.heartbeat_value = Marker(CALL_ID, 1, State.CLAIMED, "142", 1)
    result = await run_add(respond, context, _FailingCredentialProvider(ReauthorizationRequiredError()))
    assert isinstance(result, MutationResult) and result.branch == "uncertain"


@pytest.mark.asyncio
async def test_unreadable_checkpoint_returns_retry_without_sending() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    context.attempt = 2
    context.heartbeat_read_error = RuntimeError("heartbeat store unavailable")
    assert isinstance(await add(201, {"id": "142"}, context, requests), RetryError)
    assert requests == []
