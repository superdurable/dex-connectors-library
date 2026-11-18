# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Recovery of createTicket, addNote, and updateTicket writes whose outcome is unknown or unusable."""

import json
from collections.abc import AsyncIterator, Callable
from datetime import UTC, datetime, timedelta
from typing import Any

import httpx
import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    MutationResult,
    OperationRef,
    ReauthorizationRequiredError,
    RetryError,
    SecretString,
    StaticCredentialProvider,
    run_mutation,
)
from dex_connectors.sdk.singledispatch import Marker, State

from dex_connectors.gorgias import Client, Config, Credentials
from dex_connectors.gorgias._generated.definitions import ADD_NOTE_DEFINITION, CREATE_TICKET_DEFINITION
from dex_connectors.gorgias.models import (
    AddNoteInput,
    CreateTicketInput,
    TicketRequesterInput,
    UpdateTicketInput,
)

REFERENCE = ConnectionRef("gorgias", "gorgias-test")
CALL_ID = CallID("3e3a7a5b-fac4-5d1b-a1bf-06bd4a3a9269")
EXTERNAL_ID = "dex-" + str(CALL_ID)
CREDENTIALS = StaticCredentialProvider(
    {REFERENCE: Credentials("agent@acme.example.com", SecretString("inert-key"))}
)
TICKET = {
    "id": 7002,
    "subject": "New ticket",
    "status": "open",
    "priority": "high",
    "channel": "api",
    "via": "api",
    "customer": {"id": 502, "email": "ada@example.com", "name": "Ada"},
    "tags": [],
    "created_datetime": "2026-10-20T09:00:00Z",
    "updated_datetime": "2026-10-25T09:00:00Z",
    "messages": [],
    "external_id": EXTERNAL_ID,
}
NOTE = {
    "id": 8002,
    "ticket_id": 7001,
    "channel": "internal-note",
    "via": "api",
    "public": False,
    "from_agent": True,
    "sender": {"id": 7},
    "body_text": "Triaged.",
    "created_datetime": "2026-10-20T09:00:00Z",
    "external_id": EXTERNAL_ID,
}


class AttemptContext:
    """Keep the last heartbeat across Execute attempts like Dex does, and count this attempt's writes."""

    def __init__(self) -> None:
        self.attempt = 1
        self.run_id = "run"
        self.deadline = datetime.now(UTC) + timedelta(seconds=30)
        self.heartbeat_value: object = None
        self.heartbeat_writes = 0

    def next_attempt(self) -> None:
        """Start the next Execute attempt with the last heartbeat kept."""
        self.attempt += 1
        self.heartbeat_writes = 0

    def has_last_heartbeat_value(self) -> bool:
        """Report whether an earlier attempt recorded a heartbeat."""
        return self.heartbeat_value is not None

    def get_last_heartbeat_value(self, value_type: object) -> object:
        """Return the last heartbeat."""
        return self.heartbeat_value

    async def heartbeat(self, value: object = None) -> None:
        """Record the heartbeat exactly as the SDK wrote it."""
        self.heartbeat_value = value
        self.heartbeat_writes += 1

    def marker(self) -> Marker:
        """Return CALL_ID's marker from a single-marker or per-call map heartbeat."""
        if isinstance(self.heartbeat_value, Marker):
            return self.heartbeat_value
        assert isinstance(self.heartbeat_value, dict)
        return Marker.from_wire(self.heartbeat_value["singleDispatchMarkers"][str(CALL_ID)])


class BrokenBodyStream(httpx.AsyncByteStream):
    """A 2xx body that fails after its first bytes, as a connection lost mid-body does."""

    async def __aiter__(self) -> AsyncIterator[bytes]:
        yield b'{"id":'
        raise httpx.ReadError("connection lost")


class FailingCredentials:
    """Fail every credential resolution with one error."""

    def __init__(self, error: Exception) -> None:
        self.error = error

    async def resolve(self, call: Call) -> Credentials:
        """Raise the configured error."""
        raise self.error


Answer = Callable[[httpx.Request], httpx.Response]
CREDENTIAL_FAILURES: list[tuple[str, Any, bool]] = [
    ("transient store failure", FailingCredentials(RuntimeError("credential store unavailable")), False),
    ("reauthorization required", FailingCredentials(ReauthorizationRequiredError()), True),
    (
        "unusable credentials",
        StaticCredentialProvider({REFERENCE: Credentials("not an address", SecretString("inert-key"))}),
        True,
    ),
]


def respond_json(status: int, body: object) -> httpx.Response:
    """Answer with a JSON body."""
    headers = {"Content-Type": "application/json"}
    return httpx.Response(status, content=json.dumps(body).encode(), headers=headers)


async def run(
    operation: str,
    answer: Answer,
    context: AttemptContext,
    requests: list[httpx.Request],
    credentials: Any = CREDENTIALS,
    max_response_bytes: int = 4_194_304,
) -> MutationResult[Any] | RetryError:
    """Run one Execute attempt of operation against answer."""

    def record(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return answer(request)

    async with httpx.AsyncClient(transport=httpx.MockTransport(record)) as transport:
        client = Client(Config("acme", max_response_bytes=max_response_bytes), credentials, transport)
        definition = CREATE_TICKET_DEFINITION if operation == "createTicket" else ADD_NOTE_DEFINITION
        reference = OperationRef("gorgias", operation)
        call = Call(CALL_ID, REFERENCE, reference, context=context, max_sends=definition.max_sends)
        try:
            if operation == "createTicket":
                return await run_mutation(
                    client.create_ticket(),
                    call,
                    CreateTicketInput(
                        subject="New ticket",
                        description="Please help",
                        requester=TicketRequesterInput(email="ada@example.com"),
                    ),
                )
            if operation == "addNote":
                note = AddNoteInput(ticket_id=7001, body="Triaged.")
                return await run_mutation(client.add_note(), call, note)
            return await run_mutation(
                client.update_ticket(), call, UpdateTicketInput(ticket_id=7001, status="closed")
            )
        except RetryError as error:
            return error


@pytest.mark.parametrize("is_found", [True, False])
async def test_create_ticket_whose_2xx_body_breaks_is_read_back_and_never_resent(is_found: bool) -> None:
    def answer(request: httpx.Request) -> httpx.Response:
        if request.method == "POST":
            return httpx.Response(201, stream=BrokenBodyStream())
        assert request.url.params["external_id"] == EXTERNAL_ID
        return respond_json(200, {"data": [TICKET] if is_found else [], "meta": {"next_cursor": None}})

    requests: list[httpx.Request] = []
    result = await run("createTicket", answer, AttemptContext(), requests)
    assert isinstance(result, MutationResult), "an accepted create is never retried without a saved ticket ID"
    assert [request.method for request in requests] == ["POST", "GET"]
    assert result.branch == ("created" if is_found else "uncertain")


async def test_create_ticket_undecodable_2xx_naming_the_ticket_is_read_back_by_id() -> None:
    def answer(request: httpx.Request) -> httpx.Response:
        if request.method == "POST":
            return respond_json(201, {"id": 7002, "status": 7})
        assert request.url.path == "/api/tickets/7002"
        return respond_json(200, TICKET)

    requests: list[httpx.Request] = []
    context = AttemptContext()
    result = await run("createTicket", answer, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "created"
    assert context.marker().provider_id == "7002"


async def test_add_note_whose_2xx_body_breaks_is_read_back_and_never_resent() -> None:
    def answer(request: httpx.Request) -> httpx.Response:
        if request.method == "POST":
            return httpx.Response(201, stream=BrokenBodyStream())
        return respond_json(200, {**TICKET, "id": 7001, "messages": [NOTE]})

    requests: list[httpx.Request] = []
    result = await run("addNote", answer, AttemptContext(), requests)
    assert isinstance(result, MutationResult) and result.branch == "added"
    assert [request.method for request in requests] == ["POST", "GET"]


@pytest.mark.parametrize("operation", ["createTicket", "addNote"])
@pytest.mark.parametrize(("name", "credentials", "is_terminal"), CREDENTIAL_FAILURES)
async def test_credential_failure_after_a_possible_send_retries_or_selects_uncertain(
    operation: str, name: str, credentials: Any, is_terminal: bool
) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    bad_gateway = {"error": {"msg": "bad_gateway"}}
    first = await run(operation, lambda _: respond_json(502, bad_gateway), context, requests)
    assert isinstance(first, RetryError)
    context.next_attempt()
    result = await run(operation, lambda _: respond_json(500, {}), context, requests, credentials)
    assert len(requests) == 1
    assert context.heartbeat_writes == 0, "notSent would hide the earlier possible send"
    if not is_terminal:
        assert isinstance(result, RetryError), name
        return
    assert isinstance(result, MutationResult) and result.branch == "uncertain", name


@pytest.mark.parametrize(("name", "credentials", "is_terminal"), CREDENTIAL_FAILURES)
async def test_credential_failure_on_a_first_send_records_not_sent(
    name: str, credentials: Any, is_terminal: bool
) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    result = await run("createTicket", lambda _: respond_json(500, {}), context, requests, credentials)
    assert not requests
    assert context.marker().state == State.NOT_SENT
    if not is_terminal:
        assert isinstance(result, RetryError), name
        return
    assert isinstance(result, MutationResult) and result.branch == "defect", name


async def test_create_ticket_refusal_with_an_oversized_body_is_an_unknown_outcome() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    body = {"error": {"msg": "x" * 4096}}
    result = await run(
        "createTicket", lambda _: respond_json(400, body), context, requests, max_response_bytes=2048
    )
    assert isinstance(result, RetryError), "an unread refusal body cannot prove nothing was applied"
    assert context.marker().state == State.CLAIMED


async def test_update_ticket_refusal_with_an_oversized_body_retries() -> None:
    def answer(request: httpx.Request) -> httpx.Response:
        if request.method == "GET":
            return respond_json(200, {**TICKET, "id": 7001})
        return respond_json(400, {"error": {"msg": "x" * 8192}})

    result = await run("updateTicket", answer, AttemptContext(), [], max_response_bytes=4096)
    assert isinstance(result, RetryError)


@pytest.mark.parametrize(("name", "credentials", "is_terminal"), CREDENTIAL_FAILURES)
async def test_update_ticket_credential_failure_retries_only_when_transient(
    name: str, credentials: Any, is_terminal: bool
) -> None:
    result = await run("updateTicket", lambda _: respond_json(500, {}), AttemptContext(), [], credentials)
    if not is_terminal:
        assert isinstance(result, RetryError), name
        return
    assert isinstance(result, MutationResult) and result.branch == "defect", name
