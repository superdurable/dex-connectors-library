# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Freshdesk writes that were accepted, possibly sent, or cut off are never sent beyond the cap."""

import asyncio
from collections.abc import AsyncIterator, Awaitable, Callable
from datetime import UTC, datetime, timedelta
from typing import Any

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
from dex_connectors.sdk.singledispatch import Fence, Marker

from dex_connectors.freshworks_freshdesk import Client, Config, Credentials
from dex_connectors.freshworks_freshdesk._generated.definitions import (
    ADD_NOTE_DEFINITION,
    CREATE_TICKET_DEFINITION,
)
from dex_connectors.freshworks_freshdesk.models import (
    AddNoteInput,
    CreateTicketInput,
    TicketRequesterInput,
    UpdateTicketInput,
)

CONNECTION = ConnectionRef("freshdesk", "freshdesk-test")
CALL_ID = CallID("703d7ec0-692e-50db-a914-86d5fecf7dcc")
TICKET = {
    "id": 1207,
    "subject": "Billing issue",
    "description_text": "Charged twice",
    "status": 2,
    "priority": 1,
    "requester_id": 6007738334,
    "created_at": "2026-01-29T10:00:00Z",
    "updated_at": "2026-01-29T10:00:00Z",
}
Responder = Callable[[httpx.Request, int], httpx.Response]


class AttemptContext:
    """Keep the last heartbeat across Execute attempts like Dex does."""

    def __init__(self) -> None:
        self.attempt = 1
        self.run_id = "run"
        self.deadline = datetime.now(UTC) + timedelta(seconds=30)
        self.heartbeat_value: object = None
        self.heartbeat_count = 0

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
        return self.heartbeat_value is not None

    def get_last_heartbeat_value(self, value_type: type[object]) -> object:
        """Return the last heartbeat as the reading SDK expects it."""
        seeded = self.heartbeat_value
        if isinstance(seeded, Marker) and value_type is not Marker:
            return {"singleDispatchMarkers": {str(seeded.call_id): seeded.to_wire()}}
        if isinstance(seeded, dict) and value_type is Marker:
            return self.marker
        return seeded

    async def heartbeat(self, value: object = None) -> None:
        """Record the heartbeat exactly as the SDK wrote it."""
        self.heartbeat_value = value
        self.heartbeat_count += 1


class BrokenBodyStream(httpx.AsyncByteStream):
    """Send the first body bytes, then lose the connection."""

    async def __aiter__(self) -> AsyncIterator[bytes]:
        yield b'{"id":'
        raise httpx.ReadError("connection lost")


class FailingCredentials:
    """A credential provider that fails with one error."""

    def __init__(self, error: Exception) -> None:
        self.error = error

    async def resolve(self, call: Call) -> Credentials:
        """Raise the configured credential failure."""
        raise self.error


def static_credentials() -> StaticCredentialProvider[Credentials]:
    return StaticCredentialProvider({CONNECTION: Credentials(SecretString("inert-key"))})


async def run(
    operation_name: str,
    operation_input: Any,
    context: AttemptContext,
    requests: list[httpx.Request],
    respond: Responder,
    credentials: CredentialProvider[Credentials] | None = None,
    max_sends: int | None = None,
    max_response_bytes: int = 4_194_304,
) -> MutationResult[Any] | RetryError:
    """Run one Execute attempt of a Freshdesk write against a scripted provider."""

    def record(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return respond(request, len(requests) - 1)

    definition = CREATE_TICKET_DEFINITION if operation_name == "createTicket" else ADD_NOTE_DEFINITION
    async with httpx.AsyncClient(transport=httpx.MockTransport(record)) as transport:
        client = Client(
            Config("acme", max_response_bytes=max_response_bytes),
            credentials or static_credentials(),
            transport,
            api_base_url="http://127.0.0.1:22180/api/v2",
        )
        operation: Any = {
            "createTicket": client.create_ticket(),
            "addNote": client.add_note(),
            "updateTicket": client.update_ticket(),
        }[operation_name]
        call = Call(
            CALL_ID,
            CONNECTION,
            OperationRef("freshdesk", operation_name),
            context=context,
            max_sends=max_sends or definition.max_sends,
        )
        try:
            return await run_mutation(operation, call, operation_input)
        except RetryError as error:
            return error


def ticket_input() -> CreateTicketInput:
    return CreateTicketInput(
        subject="Billing issue",
        description="Charged twice",
        requester=TicketRequesterInput(email="jane@example.com"),
    )


def json_response(status: int, body: object, headers: dict[str, str] | None = None) -> httpx.Response:
    return httpx.Response(status, json=body, headers=headers)


@pytest.mark.asyncio
@pytest.mark.parametrize("operation_name", ["createTicket", "addNote"])
async def test_a_2xx_whose_body_breaks_is_accepted_and_never_resent(operation_name: str) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    operation_input: Any = (
        ticket_input() if operation_name == "createTicket" else AddNoteInput(ticket_id=1201, body="Hello")
    )
    result = await run(
        operation_name,
        operation_input,
        context,
        requests,
        lambda request, index: httpx.Response(201, stream=BrokenBodyStream()),
    )
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert result.failure is not None and result.failure.kind.value == "TRANSPORT"
    assert len(requests) == 1


@pytest.mark.asyncio
async def test_an_accepted_ticket_id_is_read_back_in_the_same_attempt() -> None:
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request, index: int) -> httpx.Response:
        if request.method == "POST":
            return json_response(201, {"id": 1207, "created_at": "SENTINEL"})
        return json_response(200, TICKET)

    result = await run("createTicket", ticket_input(), AttemptContext(), requests, respond)
    assert isinstance(result, MutationResult) and result.branch == "created"
    assert result.value.ticket.id == 1207
    assert [request.method for request in requests] == ["POST", "GET"]
    assert requests[1].url.path == "/api/v2/tickets/1207"


@pytest.mark.asyncio
async def test_a_saved_ticket_id_is_read_back_by_a_later_attempt_and_never_resent() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()

    def respond(request: httpx.Request, index: int) -> httpx.Response:
        if request.method == "POST":
            return json_response(201, {"id": 1207})
        if index == 1:
            return json_response(503, {"code": "service_unavailable"})
        return json_response(200, TICKET)

    first = await run("createTicket", ticket_input(), context, requests, respond)
    assert isinstance(first, RetryError), "a transient read-back failure retries once the ID is saved"
    assert context.marker is not None and context.marker.provider_id == "1207"
    context.attempt = 2
    second = await run("createTicket", ticket_input(), context, requests, respond)
    assert isinstance(second, MutationResult) and second.branch == "created"
    assert [request.method for request in requests] == ["POST", "GET", "GET"]


@pytest.mark.asyncio
async def test_a_read_back_404_after_acceptance_selects_uncertain() -> None:
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request, index: int) -> httpx.Response:
        if request.method == "POST":
            return json_response(201, {"id": 1207})
        return json_response(404, {})

    result = await run("createTicket", ticket_input(), AttemptContext(), requests, respond)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert result.failure is not None and result.failure.kind.value == "NOT_FOUND"


@pytest.mark.asyncio
async def test_a_saved_note_id_is_found_across_conversation_pages() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    note = AddNoteInput(ticket_id=1201, body="Hello")

    def respond(request: httpx.Request, index: int) -> httpx.Response:
        if request.method == "POST":
            return json_response(201, {"id": 9003, "created_at": "SENTINEL"})
        if index == 1:
            return json_response(500, {"code": "internal_error"})
        if request.url.params["page"] == "1":
            return json_response(
                200,
                [{"id": 9001, "ticket_id": 1201, "created_at": "2026-01-27T10:00:00Z"}],
                {"Link": '<https://acme.freshdesk.com/api/v2/tickets/1201/conversations?page=2>; rel="next"'},
            )
        return json_response(
            200,
            [
                {
                    "id": 9003,
                    "ticket_id": 1201,
                    "source": 2,
                    "private": True,
                    "body_text": "Hello",
                    "created_at": "2026-01-27T10:00:00Z",
                }
            ],
        )

    first = await run("addNote", note, context, requests, respond)
    assert isinstance(first, RetryError)
    context.attempt = 2
    second = await run("addNote", note, context, requests, respond)
    assert isinstance(second, MutationResult) and second.branch == "added"
    assert second.value.conversation.id == 9003 and second.value.conversation.is_private
    assert [request.method for request in requests] == ["POST", "GET", "GET", "GET"]
    assert requests[2].url.params["per_page"] == "100" and requests[3].url.params["page"] == "2"


def unknown_then_rate_limited(third: httpx.Response) -> Responder:
    def respond(request: httpx.Request, index: int) -> httpx.Response:
        if index == 0:
            return json_response(500, {"code": "internal_error"})
        if index == 1:
            return json_response(429, {"code": "rate_limited"})
        return third

    return respond


async def unknown_send_then_rate_limited_resend(
    context: AttemptContext, requests: list[httpx.Request], respond: Responder
) -> None:
    assert isinstance(await run("createTicket", ticket_input(), context, requests, respond), RetryError)
    context.attempt = 2
    assert isinstance(await run("createTicket", ticket_input(), context, requests, respond), RetryError)
    assert context.marker is not None and context.marker.state == "canResend"
    assert context.marker.send_count == 1, "the 429 does not erase the unknown first send"
    context.attempt = 3


@pytest.mark.asyncio
async def test_a_refusal_after_an_unknown_send_and_a_rate_limited_resend_selects_uncertain() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    respond = unknown_then_rate_limited(json_response(400, {"errors": [{"field": "group_id"}]}))
    await unknown_send_then_rate_limited_resend(context, requests, respond)
    result = await run("createTicket", ticket_input(), context, requests, respond)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 3


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("error", "branch"),
    [(ReauthorizationRequiredError("reconnect"), "uncertain"), (RuntimeError("store unavailable"), None)],
)
async def test_a_credential_failure_after_a_possible_send_records_nothing(
    error: Exception, branch: str | None
) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    respond = unknown_then_rate_limited(json_response(201, TICKET))
    await unknown_send_then_rate_limited_resend(context, requests, respond)
    heartbeats = context.heartbeat_count
    result = await run(
        "createTicket", ticket_input(), context, requests, respond, credentials=FailingCredentials(error)
    )
    if branch is None:
        assert isinstance(result, RetryError)
    else:
        assert isinstance(result, MutationResult) and result.branch == branch
    assert context.heartbeat_count == heartbeats, "notSent would hide the unknown first send"
    assert len(requests) == 2


@pytest.mark.asyncio
@pytest.mark.parametrize(("max_sends", "branch"), [(3, None), (1, "uncertain")])
async def test_the_write_window_expiring_after_dispatch_is_an_unknown_outcome(
    monkeypatch: pytest.MonkeyPatch, max_sends: int, branch: str | None
) -> None:
    original_dispatch = Fence.dispatch

    async def time_out_after_the_claim(
        fence: Fence, send: Callable[[datetime], Awaitable[object]]
    ) -> object:
        async def expire(deadline: datetime) -> object:
            raise TimeoutError

        return await original_dispatch(fence, expire)

    monkeypatch.setattr(Fence, "dispatch", time_out_after_the_claim)
    context = AttemptContext()
    result = await run(
        "createTicket",
        ticket_input(),
        context,
        [],
        lambda request, index: json_response(201, TICKET),
        max_sends=max_sends,
    )
    assert context.marker is not None and context.marker.state == "claimed"
    if branch is None:
        assert isinstance(result, RetryError)
    else:
        assert isinstance(result, MutationResult) and result.branch == branch


class StalledBodyStream(httpx.AsyncByteStream):
    """Send the first body bytes, then never send the rest."""

    async def __aiter__(self) -> AsyncIterator[bytes]:
        yield b'{"id":'
        await asyncio.sleep(30)


@pytest.mark.asyncio
@pytest.mark.parametrize("operation_name", ["createTicket", "addNote"])
@pytest.mark.parametrize("max_sends", [3, 1])
async def test_2xx_headers_then_the_write_deadline_cuts_the_body_is_never_resent(
    monkeypatch: pytest.MonkeyPatch, operation_name: str, max_sends: int
) -> None:
    original_dispatch = Fence.dispatch

    async def cut_at_the_write_deadline(
        fence: Fence, send: Callable[[datetime], Awaitable[object]]
    ) -> object:
        async def send_until_the_window_closes(deadline: datetime) -> object:
            return await asyncio.wait_for(send(deadline), 0.3)

        return await original_dispatch(fence, send_until_the_window_closes)

    monkeypatch.setattr(Fence, "dispatch", cut_at_the_write_deadline)
    requests: list[httpx.Request] = []
    operation_input: Any = (
        ticket_input() if operation_name == "createTicket" else AddNoteInput(ticket_id=1201, body="Hello")
    )
    result = await run(
        operation_name,
        operation_input,
        AttemptContext(),
        requests,
        lambda request, index: httpx.Response(201, stream=StalledBodyStream()),
        max_sends=max_sends,
    )
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 1


@pytest.mark.asyncio
async def test_a_terminal_credential_failure_selects_update_defect_only_on_the_first_attempt() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    credentials = FailingCredentials(ReauthorizationRequiredError("reconnect"))
    update = UpdateTicketInput(ticket_id=1207, status=2)
    first = await run(
        "updateTicket", update, context, requests, lambda request, index: json_response(200, TICKET),
        credentials=credentials,
    )
    assert isinstance(first, MutationResult) and first.branch == "defect"
    context.attempt = 2
    second = await run(
        "updateTicket", update, context, requests, lambda request, index: json_response(200, TICKET),
        credentials=credentials,
    )
    assert isinstance(second, RetryError), "an earlier attempt may have sent the change"
    assert not requests


@pytest.mark.asyncio
async def test_an_oversized_refusal_is_an_unknown_outcome() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    oversized = json_response(400, {"errors": "x" * 4096})
    created = await run(
        "createTicket", ticket_input(), context, requests, lambda request, index: oversized,
        max_response_bytes=2048,
    )
    assert isinstance(created, RetryError), "an unread refusal may hide an accepted ticket"
    assert context.marker is not None and context.marker.state == "claimed"

    def update(request: httpx.Request, index: int) -> httpx.Response:
        if request.method == "GET":
            return json_response(200, {**TICKET, "status": 3})
        return oversized

    updated = await run(
        "updateTicket", UpdateTicketInput(ticket_id=1207, status=2), AttemptContext(), [], update,
        max_response_bytes=2048,
    )
    assert isinstance(updated, RetryError), "the absolute update simply repeats"
