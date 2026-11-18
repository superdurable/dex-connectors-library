# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""updateTicket and createTicket outcomes after a possible or accepted write."""

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
    run_mutation,
)
from dex_connectors.sdk.singledispatch import Marker

from dex_connectors.zendesk_support import Client, Config, Credentials
from dex_connectors.zendesk_support._generated.definitions import UPDATE_TICKET_DEFINITION
from dex_connectors.zendesk_support.models import (
    CreateTicketInput,
    CreateTicketOutput,
    TicketCommentInput,
    UpdateTicketInput,
    UpdateTicketOutput,
)

REFERENCE = ConnectionRef("zendesk", "desk")
CALL_ID = CallID("6a0e7f8c-2f0b-5d7e-9a51-2f4c8e1b3d77")
UPDATE = UpdateTicketInput(
    ticket_id=5512,
    status="open",
    add_tags=("escalated",),
    comment=TicketCommentInput(body="Customer called again.", is_internal_note=True),
)
EMPTY_AUDITS: dict[str, object] = {"audits": []}
KEYED_AUDITS: dict[str, object] = {
    "audits": [{"id": 10, "metadata": {"custom": {"dex_idempotency_key": str(CALL_ID)}}}]
}


def ticket(status: str = "pending", tags: list[str] | None = None) -> dict[str, object]:
    return {
        "ticket": {
            "id": 5512,
            "status": status,
            "tags": tags if tags is not None else ["billing"],
            "created_at": "2026-01-26T14:02:00Z",
            "updated_at": "2026-01-28T08:45:00Z",
        }
    }


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
        """Decode this call's marker from the last heartbeat."""
        value = self.heartbeat_value
        if isinstance(value, Marker):
            return value
        if not isinstance(value, dict):
            return None
        entry = value.get("singleDispatchMarkers", {}).get(str(CALL_ID))
        return None if entry is None else Marker.from_wire(entry)

    def has_last_heartbeat_value(self) -> bool:
        """Report whether an earlier attempt recorded a heartbeat."""
        return self.heartbeat_value is not None

    def get_last_heartbeat_value(self, value_type: object) -> object:
        """Return the last heartbeat, translating a seeded marker to the requested format."""
        if isinstance(self.heartbeat_value, dict) and value_type is Marker:
            return self.marker
        return self.heartbeat_value

    async def heartbeat(self, value: object = None) -> None:
        """Record the heartbeat exactly as the SDK wrote it."""
        self.heartbeat_value = value
        self.heartbeat_count += 1


class SwitchableCredentials(CredentialProvider[Credentials]):
    """Resolve the test token until error is set."""

    def __init__(self) -> None:
        self.error: Exception | None = None

    async def resolve(self, call: Call) -> Credentials:
        """Raise the configured error or return the inert test token."""
        if self.error is not None:
            raise self.error
        return Credentials("agent@example.com", SecretString("inert-token"))


class BrokenBody(httpx.AsyncByteStream):
    """Yield part of a body, then lose the connection."""

    async def __aiter__(self) -> AsyncIterator[bytes]:
        yield b'{"ticket":{"id":5512,'
        raise httpx.ReadError("connection lost")


Answer = Callable[[httpx.Request, int], httpx.Response]


def json_answer(status: int, body: dict[str, object]) -> httpx.Response:
    return httpx.Response(status, json=body)


def update_answers(
    write: Callable[[int], httpx.Response], audits: Callable[[int], dict[str, object]]
) -> Answer:
    """Answer each attempt's ticket read, audit page, and PUT, where reads are numbered per call."""
    counts = {"audits": 0, "writes": 0}

    def answer(request: httpx.Request, _: int) -> httpx.Response:
        if request.method == "PUT":
            counts["writes"] += 1
            return write(counts["writes"])
        if request.url.path.endswith("/audits"):
            counts["audits"] += 1
            return json_answer(200, audits(counts["audits"]))
        return json_answer(200, ticket())

    return answer


async def run(
    operation_name: str,
    answer: Answer,
    context: AttemptContext,
    requests: list[httpx.Request],
    credentials: SwitchableCredentials | None = None,
    max_response_bytes: int = 4_194_304,
) -> MutationResult[UpdateTicketOutput] | MutationResult[CreateTicketOutput] | RetryError:
    """Run one Execute attempt of one Zendesk Mutation against answer."""

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return answer(request, len(requests) - 1)

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as transport:
        client = Client(
            Config("acme", max_response_bytes=max_response_bytes),
            credentials or SwitchableCredentials(),
            transport,
            api_base_url="http://127.0.0.1:37001/api/v2",
        )
        # MutationStep passes the definition's send cap.
        call = Call(
            CALL_ID,
            REFERENCE,
            OperationRef("zendesk-support", operation_name),
            context=context,
            max_sends=UPDATE_TICKET_DEFINITION.max_sends if operation_name == "updateTicket" else 1,
        )
        try:
            if operation_name == "createTicket":
                return await run_mutation(
                    client.create_ticket(),
                    call,
                    CreateTicketInput(
                        subject="Double charge", comment=TicketCommentInput(body="Charged twice")
                    ),
                )
            return await run_mutation(client.update_ticket(), call, UPDATE)
        except RetryError as error:
            return error


@pytest.mark.asyncio
async def test_reads_back_before_resending_after_a_rate_limited_resend() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    answer = update_answers(
        lambda write: json_answer(500 if write == 1 else 429, {"error": "x"}),
        lambda audit: KEYED_AUDITS if audit == 3 else EMPTY_AUDITS,
    )
    for attempt in (1, 2):
        context.attempt = attempt
        assert isinstance(await run("updateTicket", answer, context, requests), RetryError)
    assert context.marker is not None and context.marker.state == "canResend"
    context.attempt = 3
    result = await run("updateTicket", answer, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "updated"
    assert isinstance(result.value, UpdateTicketOutput) and result.value.was_already_applied
    assert len(requests) == 8
    assert [request.method for request in requests[6:]] == ["GET", "GET"]


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("error", "is_uncertain"),
    [(ReauthorizationRequiredError(), True), (RuntimeError("broker unavailable"), False)],
)
async def test_credential_failure_after_a_possible_send_never_selects_defect(
    error: Exception, is_uncertain: bool
) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    credentials = SwitchableCredentials()
    answer = update_answers(lambda _: json_answer(500, {"error": "x"}), lambda _: EMPTY_AUDITS)
    assert isinstance(await run("updateTicket", answer, context, requests, credentials), RetryError)
    heartbeats = context.heartbeat_count
    context.attempt = 2
    credentials.error = error
    result = await run("updateTicket", answer, context, requests, credentials)
    if is_uncertain:
        assert isinstance(result, MutationResult) and result.branch == "uncertain"
    else:
        assert isinstance(result, RetryError)
    assert context.heartbeat_count == heartbeats, "notSent would hide the earlier possible send"
    assert len(requests) == 3


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("error", "is_defect"),
    [(ReauthorizationRequiredError(), True), (RuntimeError("broker unavailable"), False)],
)
async def test_credential_failure_on_a_first_send(error: Exception, is_defect: bool) -> None:
    credentials = SwitchableCredentials()
    credentials.error = error
    context = AttemptContext()
    requests: list[httpx.Request] = []
    result = await run(
        "updateTicket",
        update_answers(lambda _: json_answer(200, ticket()), lambda _: EMPTY_AUDITS),
        context,
        requests,
        credentials,
    )
    if is_defect:
        assert isinstance(result, MutationResult) and result.branch == "defect"
    else:
        assert isinstance(result, RetryError)
    assert context.marker is not None and context.marker.state == "notSent"
    assert not requests


def broken_body(_: int) -> httpx.Response:
    return httpx.Response(200, stream=BrokenBody())


def undecodable_body(_: int) -> httpx.Response:
    return json_answer(200, {"wrong": True})


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "write", [broken_body, undecodable_body], ids=["body breaks after 2xx headers", "undecodable 2xx"]
)
async def test_accepted_write_with_an_unusable_answer_reads_back_in_the_same_attempt(
    write: Callable[[int], httpx.Response],
) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    answer = update_answers(write, lambda audit: KEYED_AUDITS if audit == 2 else EMPTY_AUDITS)
    result = await run("updateTicket", answer, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "updated"
    assert isinstance(result.value, UpdateTicketOutput) and not result.value.was_already_applied
    assert len(requests) == 5
    assert context.marker is not None and context.marker.provider_id == "5512"


@pytest.mark.asyncio
async def test_accepted_write_that_cannot_be_confirmed_selects_uncertain_without_resending() -> None:
    requests: list[httpx.Request] = []
    answer = update_answers(undecodable_body, lambda _: EMPTY_AUDITS)
    result = await run("updateTicket", answer, AttemptContext(), requests)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert [request.method for request in requests].count("PUT") == 1


@pytest.mark.asyncio
async def test_accepted_write_retries_a_transient_read_back_and_later_only_reads_back() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    base = update_answers(undecodable_body, lambda audit: KEYED_AUDITS if audit > 1 else EMPTY_AUDITS)

    def answer(request: httpx.Request, index: int) -> httpx.Response:
        if index == 3:
            return json_answer(503, {"error": "Unavailable"})
        return base(request, index)

    assert isinstance(await run("updateTicket", answer, context, requests), RetryError)
    assert context.marker is not None and context.marker.provider_id == "5512"
    heartbeats = context.heartbeat_count
    context.attempt = 2
    result = await run("updateTicket", answer, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "updated"
    assert isinstance(result.value, UpdateTicketOutput) and result.value.was_already_applied
    assert len(requests) == 6
    assert [request.method for request in requests].count("PUT") == 1
    assert context.heartbeat_count == heartbeats


@pytest.mark.asyncio
async def test_saved_ticket_id_with_a_missing_ticket_selects_uncertain() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    base = update_answers(undecodable_body, lambda _: EMPTY_AUDITS)

    def answer(request: httpx.Request, index: int) -> httpx.Response:
        if index == 3:
            return json_answer(502, {"error": "BadGateway"})
        if index > 3:
            return json_answer(404, {"error": "RecordNotFound"})
        return base(request, index)

    assert isinstance(await run("updateTicket", answer, context, requests), RetryError)
    context.attempt = 2
    result = await run("updateTicket", answer, context, requests)
    assert isinstance(result, MutationResult) and result.branch == "uncertain"
    assert len(requests) == 5


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "body",
    [
        {"error": "RecordInvalid", "description": "x" * 4096},
        {"error": "RecordInvalid", "ticket": {"id": 5512}},
    ],
    ids=["oversized", "carries a ticket"],
)
@pytest.mark.parametrize("operation_name", ["updateTicket", "createTicket"])
async def test_refusal_with_an_unreadable_or_contradictory_body_is_an_unknown_outcome(
    body: dict[str, object], operation_name: str
) -> None:
    def answer(request: httpx.Request, _: int) -> httpx.Response:
        if request.method in ("PUT", "POST"):
            return json_answer(422, body)
        if request.url.path.endswith("/audits"):
            return json_answer(200, EMPTY_AUDITS)
        return json_answer(200, ticket())

    context = AttemptContext()
    result = await run(operation_name, answer, context, [], max_response_bytes=2048)
    assert isinstance(result, RetryError)
    if operation_name == "updateTicket":
        assert context.marker is not None and context.marker.state == "claimed"


@pytest.mark.asyncio
@pytest.mark.parametrize(("prior_sends", "branch"), [(0, None), (2, "uncertain")])
async def test_write_window_timeout_is_an_unknown_outcome(prior_sends: int, branch: str | None) -> None:
    def answer(request: httpx.Request, _: int) -> httpx.Response:
        if request.method == "PUT":
            raise TimeoutError
        if request.url.path.endswith("/audits"):
            return json_answer(200, EMPTY_AUDITS)
        return json_answer(200, ticket())

    context = AttemptContext()
    if prior_sends:
        context.attempt = prior_sends + 1
        context.heartbeat_value = Marker(CALL_ID, prior_sends, "claimed", "", prior_sends)
    result = await run("updateTicket", answer, context, [])
    if branch is None:
        assert isinstance(result, RetryError), "the claim stays, so the next attempt reads back first"
        assert context.marker is not None and context.marker.state == "claimed"
    else:
        assert isinstance(result, MutationResult) and result.branch == branch


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("error", "attempt", "is_defect"),
    [
        (ReauthorizationRequiredError(), 1, True),
        (RuntimeError("broker unavailable"), 1, False),
        (ReauthorizationRequiredError(), 2, False),
    ],
)
async def test_create_ticket_credential_failure_selects_defect_only_when_terminal_on_the_first_attempt(
    error: Exception, attempt: int, is_defect: bool
) -> None:
    credentials = SwitchableCredentials()
    credentials.error = error
    context = AttemptContext()
    context.attempt = attempt
    requests: list[httpx.Request] = []
    result = await run(
        "createTicket", lambda _request, _index: json_answer(201, ticket()), context, requests, credentials
    )
    if is_defect:
        assert isinstance(result, MutationResult) and result.branch == "defect"
    else:
        assert isinstance(result, RetryError)
    assert not requests
