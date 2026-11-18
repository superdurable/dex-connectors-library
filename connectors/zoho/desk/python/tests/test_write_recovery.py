# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Recovery of createTicket, addComment, and updateTicket after unknown or accepted outcomes."""

import asyncio
import dataclasses
import json
from collections.abc import AsyncIterator, Callable
from datetime import UTC, datetime, timedelta

import dex_connectors.sdk as sdk
import httpx
import pytest
from dex_connectors.sdk import projectconfig
from dex_connectors.sdk.singledispatch import Marker

import dex_connectors.zoho_desk as desk
from dex_connectors.zoho_desk.client import ResponseObservation

CONNECTION = sdk.ConnectionRef("zoho", "zoho-desk-test")
CALL_ID = sdk.CallID("6b0f5c4e-3d1e-5c55-9d0e-6e8a7f0c2b11")
TICKET_ID = "1892000000099004"
COMMENT_ID = "4000000529001"
TICKET_BODY = json.dumps(
    {
        "id": TICKET_ID,
        "status": "Open",
        "statusType": "OPEN",
        "subject": "Billing question",
        "departmentId": "1892000000006907",
        "createdTime": "2026-01-28T09:00:00.000Z",
        "modifiedTime": "2026-01-28T09:03:00.000Z",
    }
)
COMMENT_BODY = json.dumps(
    {
        "id": COMMENT_ID,
        "isPublic": False,
        "content": "Triaged.",
        "contentType": "plainText",
        "commentedTime": "2026-01-28T09:04:00.000Z",
    }
)
Handler = Callable[[httpx.Request, int], httpx.Response]


class AttemptContext:
    """Keep the last heartbeat across Execute attempts like Dex does."""

    def __init__(self, attempt: int = 1, heartbeat_value: object = None) -> None:
        self.attempt = attempt
        self.run_id = "run"
        self.deadline = datetime.now(UTC) + timedelta(seconds=30)
        self.heartbeat_value = heartbeat_value
        self.heartbeat_writes = 0

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
        self.heartbeat_writes += 1
        self.heartbeat_value = value


class RefreshFailingCredentials:
    """Resolve a token, but fail the forced refresh after a provider rejection."""

    def __init__(self, error: Exception) -> None:
        self.error = error

    async def resolve(self, call: sdk.Call) -> desk.Credentials:
        """Return a usable token."""
        return desk.Credentials(access_token=sdk.SecretString("1000.rejected-token"))

    async def resolve_after_rejection(self, call: sdk.Call) -> desk.Credentials:
        """Fail the refresh."""
        raise self.error


class RevokedCredentials:
    """Report a revoked authorization before any request."""

    async def resolve(self, call: sdk.Call) -> desk.Credentials:
        """Raise the terminal credential error."""
        raise sdk.ReauthorizationRequiredError("invalid_code")


def response(status: int, body: str = "{}") -> httpx.Response:
    """Build a Zoho Desk JSON response."""
    return httpx.Response(status, content=body.encode(), headers={"Content-Type": "application/json"})


class BrokenAfterFirstChunk(httpx.AsyncByteStream):
    """A body whose connection breaks after the first chunk."""

    async def __aiter__(self) -> AsyncIterator[bytes]:
        yield b'{"id":"' + TICKET_ID.encode()
        raise httpx.ReadError("connection reset")


async def run(
    operation_name: str,
    handler: Handler,
    context: AttemptContext,
    requests: list[httpx.Request],
    credentials: object | None = None,
    is_public: bool = False,
) -> str:
    """Run one Execute attempt and return its branch or `retry`."""

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return handler(request, len(requests) - 1)

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as http_client:
        provider = credentials or sdk.StaticCredentialProvider(
            {CONNECTION: desk.Credentials(access_token=sdk.SecretString("1000.synthetic-access"))}
        )
        client = desk.Client(
            desk.Config(org_id="2389290"), provider, http_client, "http://127.0.0.1:22301/api/v1"  # type: ignore[arg-type]
        )
        definition = client.create_ticket().definition() if operation_name == "createTicket" else None
        operation = sdk.OperationRef("zoho-desk", operation_name)
        max_sends = definition.max_sends if definition else 3
        call = sdk.Call(CALL_ID, CONNECTION, operation, context=context, max_sends=max_sends)
        try:
            if operation_name == "createTicket":
                ticket = desk.CreateTicketInput(
                    subject="Billing question",
                    description="Invoice",
                    department_id="1892000000006907",
                    contact_id="1892000000042032",
                )
                return (await sdk.run_mutation(client.create_ticket(), call, ticket)).branch
            if operation_name == "addComment":
                comment = desk.AddCommentInput(ticket_id=TICKET_ID, content="Triaged.", is_public=is_public)
                return (await sdk.run_mutation(client.add_comment(), call, comment)).branch
            change = desk.UpdateTicketInput(ticket_id=TICKET_ID, status="Closed")
            return (await sdk.run_mutation(client.update_ticket(), call, change)).branch
        except sdk.RetryError:
            return "retry"


async def context_after(
    operation_name: str, handler: Handler, attempt: int, send_count: int
) -> AttemptContext:
    """Run a first attempt, then move its SDK-written claim to `attempt` and `send_count`."""
    context = AttemptContext()
    await run(operation_name, handler, context, [])
    value = context.heartbeat_value
    if isinstance(value, Marker):
        context.heartbeat_value = dataclasses.replace(value, attempt=attempt, send_count=send_count)
    else:
        assert isinstance(value, dict)
        value["singleDispatchMarkers"][str(CALL_ID)].update({"attempt": attempt, "sendCount": send_count})
    context.attempt = attempt + 1
    return context


@pytest.mark.asyncio
async def test_prior_sends_count_after_a_can_resend_marker() -> None:
    statuses = [500, 429, 422]
    requests: list[httpx.Request] = []
    context = AttemptContext()
    for attempt, expected in ((1, "retry"), (2, "retry")):
        context.attempt = attempt
        assert await run("createTicket", lambda _r, i: response(statuses[i]), context, requests) == expected
    assert context.marker is not None and context.marker.state == "canResend"
    assert context.marker.send_count == 1
    context.attempt = 3
    branch = await run("createTicket", lambda _r, i: response(statuses[i]), context, requests)
    assert branch == "uncertain", "the first send may have created the ticket"
    assert len(requests) == 3


@pytest.mark.asyncio
async def test_credential_failures_after_a_possible_send_never_select_a_terminal_refusal() -> None:
    requests: list[httpx.Request] = []
    context = await context_after("createTicket", lambda _r, _i: response(500), 1, 1)
    writes = context.heartbeat_writes
    revoked_branch = await run(
        "createTicket", lambda _r, _i: response(500), context, requests, RevokedCredentials()
    )
    assert revoked_branch == "uncertain"
    assert context.heartbeat_writes == writes, "notSent would hide the earlier possible send"

    outage = RefreshFailingCredentials(RuntimeError("Zoho token endpoint returned HTTP 503"))
    assert await run("createTicket", lambda _r, _i: response(401), context, requests, outage) == "retry"
    assert context.marker is not None and context.marker.state == "claimed", "the claim keeps counting"
    assert len(requests) == 1

    revoked = RefreshFailingCredentials(sdk.ReauthorizationRequiredError("invalid_code"))
    context = await context_after("createTicket", lambda _r, _i: response(500), 1, 1)
    assert await run("createTicket", lambda _r, _i: response(401), context, requests, revoked) == "uncertain"


@pytest.mark.asyncio
async def test_first_401_with_a_transient_refresh_failure_records_can_resend_and_retries() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    outage = RefreshFailingCredentials(RuntimeError("Zoho token endpoint returned HTTP 503"))
    assert await run("createTicket", lambda _r, _i: response(401), context, requests, outage) == "retry"
    assert context.marker is not None and context.marker.state == "canResend"


@pytest.mark.asyncio
async def test_a_2xx_whose_body_breaks_is_never_resent() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    handler: Handler = lambda _r, _i: httpx.Response(200, stream=BrokenAfterFirstChunk())  # noqa: E731
    assert await run("createTicket", handler, context, requests) == "uncertain"
    assert len(requests) == 1
    assert context.marker is not None and context.marker.state == "claimed"


@pytest.mark.asyncio
async def test_a_2xx_without_an_id_is_never_resent() -> None:
    requests: list[httpx.Request] = []
    branch = await run("addComment", lambda _r, _i: response(200, "{}"), AttemptContext(), requests)
    assert branch == "uncertain"
    assert len(requests) == 1


@pytest.mark.asyncio
async def test_a_timeout_after_2xx_headers_keeps_the_status() -> None:
    async def slow_body() -> AsyncIterator[bytes]:
        yield b"{"
        await asyncio.sleep(5)

    class SlowStream(httpx.AsyncByteStream):
        def __aiter__(self) -> AsyncIterator[bytes]:
            return slow_body()

    async with httpx.AsyncClient(
        transport=httpx.MockTransport(lambda _r: httpx.Response(200, stream=SlowStream()))
    ) as http_client:
        client = desk.Client(desk.Config(org_id="2389290"), RevokedCredentials(), http_client)  # type: ignore[arg-type]
        call = sdk.Call(CALL_ID, CONNECTION, sdk.OperationRef("zoho-desk", "createTicket"))
        client._operation_deadlines[str(call.id)] = asyncio.get_running_loop().time() + 0.2
        observation = ResponseObservation()
        result = await client.exchange(
            call,
            "createTicket",
            desk.Credentials(access_token=sdk.SecretString("1000.synthetic-access")),
            "https://desk.zoho.com/api/v1",
            "POST",
            "/tickets",
            payload={},
            resend_after_unauthorized=False,
            observation=observation,
        )
    assert (result.outcome, result.status, observation.status) == ("unavailable", 200, 200)


class StalledAfterFirstChunk(httpx.AsyncByteStream):
    """A body that sends its first chunk, then never finishes."""

    async def __aiter__(self) -> AsyncIterator[bytes]:
        yield b"{"
        await asyncio.sleep(60)


@pytest.mark.asyncio
@pytest.mark.parametrize("operation_name", ["createTicket", "addComment"])
async def test_the_write_deadline_cutting_a_2xx_body_is_never_resent(operation_name: str) -> None:
    """The Fence write window cancels the body read after 2xx headers; the status proves acceptance."""
    requests: list[httpx.Request] = []
    context = AttemptContext()
    context.deadline = datetime.now(UTC) + timedelta(milliseconds=1200)
    handler: Handler = lambda _r, _i: httpx.Response(201, stream=StalledAfterFirstChunk())  # noqa: E731
    assert await run(operation_name, handler, context, requests) == "uncertain"
    assert context.marker is not None and context.marker.state == "claimed"
    assert len(requests) == 1


@pytest.mark.asyncio
async def test_the_write_deadline_cutting_a_refusal_body_resends_below_the_cap() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    context.deadline = datetime.now(UTC) + timedelta(milliseconds=1200)
    handler: Handler = lambda _r, _i: httpx.Response(422, stream=StalledAfterFirstChunk())  # noqa: E731
    assert await run("createTicket", handler, context, requests) == "retry"
    assert context.marker is not None and context.marker.state == "claimed"


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "reply",
    [
        lambda: response(400, json.dumps({"errorCode": "INVALID_DATA", "message": "x" * 70_000})),
        lambda: response(404, json.dumps({"errorCode": "URL_NOT_FOUND", "message": "1000.synthetic-access"})),
        lambda: httpx.Response(422, stream=BrokenAfterFirstChunk()),
    ],
    ids=["oversized", "reflects the token", "broken"],
)
async def test_a_refusal_with_an_unusable_body_resends_below_the_cap_and_is_uncertain_at_it(
    reply: Callable[[], httpx.Response],
) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    for attempt in (1, 2):
        context.attempt = attempt
        assert await run("addComment", lambda _r, _i: reply(), context, requests) == "retry"
        assert context.marker is not None and context.marker.state == "claimed"
    context.attempt = 3
    assert await run("addComment", lambda _r, _i: reply(), context, requests) == "uncertain"
    assert len(requests) == 3


@pytest.mark.asyncio
async def test_a_rate_limit_whose_body_breaks_records_can_resend_and_retries() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    handler: Handler = lambda _r, _i: httpx.Response(429, stream=BrokenAfterFirstChunk())  # noqa: E731
    assert await run("createTicket", handler, context, requests) == "retry"
    assert context.marker is not None and context.marker.state == "canResend"


@pytest.mark.asyncio
async def test_a_refusal_that_carries_a_created_id_is_read_back_by_that_id() -> None:
    for operation_name, created, body, path, success in (
        ("createTicket", TICKET_ID, TICKET_BODY, f"/api/v1/tickets/{TICKET_ID}", "created"),
        (
            "addComment",
            COMMENT_ID,
            COMMENT_BODY,
            f"/api/v1/tickets/{TICKET_ID}/comments/{COMMENT_ID}",
            "added",
        ),
    ):
        requests: list[httpx.Request] = []
        context = AttemptContext()

        def handler(
            request: httpx.Request, _index: int, created: str = created, body: str = body
        ) -> httpx.Response:
            if request.method == "POST":
                return response(422, json.dumps({"id": created, "errorCode": "INVALID_DATA"}))
            return response(200, body)

        assert await run(operation_name, handler, context, requests) == success
        assert context.marker is not None and context.marker.provider_id == created
        assert [(request.method, request.url.path) for request in requests][1] == ("GET", path)
        assert len(requests) == 2


@pytest.mark.asyncio
async def test_update_ticket_retries_a_revoked_credential_after_the_first_attempt() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    handler: Handler = lambda _r, _i: response(200, TICKET_BODY)  # noqa: E731
    assert await run("updateTicket", handler, context, requests, RevokedCredentials()) == "providerRejected"
    context.attempt = 2
    assert await run("updateTicket", handler, context, requests, RevokedCredentials()) == "retry"
    assert requests == []


@pytest.mark.asyncio
async def test_update_ticket_repeats_a_change_whose_refusal_body_is_unusable() -> None:
    requests: list[httpx.Request] = []

    def handler(request: httpx.Request, _index: int) -> httpx.Response:
        if request.method == "GET":
            return response(200, TICKET_BODY)
        return response(400, json.dumps({"errorCode": "INVALID_DATA", "message": "x" * 70_000}))

    assert await run("updateTicket", handler, AttemptContext(), requests) == "retry"
    assert len(requests) == 2


@pytest.mark.asyncio
async def test_an_accepted_ticket_id_is_read_back_in_the_same_attempt() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()

    def handler(request: httpx.Request, index: int) -> httpx.Response:
        if request.method == "POST":
            return response(200, json.dumps({"id": TICKET_ID, "subject": "Billing question"}))
        return response(500) if index == 1 else response(200, TICKET_BODY)

    assert await run("createTicket", handler, context, requests) == "retry"
    assert context.marker is not None and context.marker.provider_id == TICKET_ID
    context.attempt = 2
    assert await run("createTicket", handler, context, requests) == "created"
    assert [request.method for request in requests] == ["POST", "GET", "GET"]
    assert requests[2].url.path == f"/api/v1/tickets/{TICKET_ID}"


@pytest.mark.asyncio
async def test_a_saved_id_is_read_back_at_the_send_cap() -> None:
    for operation_name, body, path, success in (
        ("createTicket", TICKET_BODY, f"/api/v1/tickets/{TICKET_ID}", "created"),
        ("addComment", COMMENT_BODY, f"/api/v1/tickets/{TICKET_ID}/comments/{COMMENT_ID}", "added"),
    ):
        for status, expected in ((200, success), (503, "retry"), (404, "uncertain"), (403, "uncertain")):
            requests: list[httpx.Request] = []
            accepting: Handler = lambda _r, _i, accepted=body: response(200, accepted)  # noqa: E731
            context = await context_after(operation_name, accepting, 3, 3)
            reply = response(status, body if status == 200 else "{}")
            branch = await run(operation_name, lambda _r, _i, answer=reply: answer, context, requests)
            assert branch == expected, (operation_name, status)
            assert [(request.method, request.url.path) for request in requests] == [("GET", path)]


@pytest.mark.asyncio
async def test_an_unsaved_provider_id_never_retries_a_failed_read_back() -> None:
    requests: list[httpx.Request] = []

    class ClaimOnlyContext(AttemptContext):
        async def heartbeat(self, value: object = None) -> None:
            if self.heartbeat_writes >= 1:
                raise RuntimeError("heartbeat stream closed")
            await super().heartbeat(value)

    def handler(request: httpx.Request, _index: int) -> httpx.Response:
        if request.method == "POST":
            return response(201, json.dumps({"id": COMMENT_ID, "isPublic": "maybe"}))
        return response(500)

    assert await run("addComment", handler, ClaimOnlyContext(), requests) == "uncertain"
    assert len(requests) == 2


@pytest.mark.asyncio
async def test_update_ticket_retries_an_unusable_read() -> None:
    requests: list[httpx.Request] = []
    branch = await run("updateTicket", lambda _r, _i: response(200, "{}"), AttemptContext(), requests)
    assert branch == "retry"
    assert len(requests) == 1


class FailingCredentials:
    """Fail every resolution with one error."""

    def __init__(self, error: Exception) -> None:
        self.error = error

    async def resolve(self, call: sdk.Call) -> desk.Credentials:
        """Raise the configured error."""
        raise self.error


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("error", "branch"),
    [
        (projectconfig.ObjectNotFoundError("connection"), "uncertain"),
        (projectconfig.ConnectionNotSelectedError("connection"), "uncertain"),
        (projectconfig.CredentialBrokerRejectedError("broker"), "uncertain"),
        (projectconfig.CredentialBrokerUnavailableError("broker"), "retry"),
        (ValueError("connection is not configured"), "retry"),
    ],
)
async def test_every_credential_class_after_a_possible_send(error: Exception, branch: str) -> None:
    requests: list[httpx.Request] = []
    context = await context_after("createTicket", lambda _r, _i: response(500), 1, 1)
    writes = context.heartbeat_writes
    credentials = FailingCredentials(error)
    result = await run("createTicket", lambda _r, _i: response(500), context, requests, credentials)
    assert result == branch
    assert context.heartbeat_writes == writes, "notSent would hide the earlier possible send"
    assert not requests
