# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded resend of Jira Service Management writes whose outcome is unknown."""

from collections.abc import Callable
from datetime import UTC, datetime, timedelta

import dex_connectors.sdk as sdk
import httpx
import pytest
from dex_connectors.sdk.singledispatch import Marker

import dex_connectors.atlassian_jira_service_management as jsm
from dex_connectors.atlassian_jira_service_management._generated.definitions import (
    ADD_COMMENT_DEFINITION,
    CREATE_TICKET_DEFINITION,
)

CONNECTION = sdk.ConnectionRef("atlassian", "jsm-test")
CALL_ID = sdk.CallID("ab66bfde-d934-5d47-9e00-bdf172b83392")
TICKET_INPUT = jsm.CreateTicketInput(service_desk_id="10", request_type_id="25", summary="Laptop broken")
COMMENT_INPUT = jsm.AddCommentInput(issue_id_or_key="ITH-42", body="Check the warranty.", is_public=True)
CREATED = '{"issueId":"10042","issueKey":"ITH-42","serviceDeskId":"10","requestTypeId":"25"}'

CREATED_READ_BACK = CREATED[:-1] + ',"summary":"Laptop broken"}'
TRUNCATED_BODY = "<truncated>"
"""A scripted body that sends the status and headers, then breaks the connection."""

Answer = Callable[[httpx.Request], tuple[int, str]]


class BrokenStream(httpx.AsyncByteStream):
    """A response body that fails after the status and headers arrived."""

    async def __aiter__(self):  # type: ignore[no-untyped-def]
        yield b'{"issueId":"100'
        raise httpx.ReadError("connection reset")


def created_or_read_back(
    read_status: int, read_body: str, write_body: str = CREATED, write_status: int = 201
) -> Answer:
    """Answer the create POST and each read-back GET separately."""
    return lambda request: (read_status, read_body) if request.method == "GET" else (write_status, write_body)


class AttemptContext:
    """Keep the last heartbeat across Execute attempts like Dex does."""

    def __init__(self, attempt: int = 1, heartbeat_value: object = None) -> None:
        self.attempt = attempt
        self.run_id = "run"
        self.deadline = datetime.now(UTC) + timedelta(seconds=30)
        self.heartbeat_value = heartbeat_value
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


async def run(
    operation_name: str,
    answer: Answer,
    context: AttemptContext,
    requests: list[httpx.Request],
    *,
    max_sends: int = CREATE_TICKET_DEFINITION.max_sends,
    credentials_token: str = "jsm-access-token",
) -> sdk.MutationResult[object] | sdk.RetryError:
    """Run one Execute attempt of a write against a scripted provider."""

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        status, body = answer(request)
        if body == TRUNCATED_BODY:
            return httpx.Response(status, stream=BrokenStream(), headers={"Content-Type": "application/json"})
        return httpx.Response(status, content=body.encode(), headers={"Content-Type": "application/json"})

    credentials = jsm.Credentials(
        "client-id", sdk.SecretString("secret"), sdk.SecretString(credentials_token), sdk.SecretString("")
    )
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as transport:
        client = jsm.Client(
            jsm.Config(cloud_id="11223344-a1b2-4b33-8c44-def123456789", endpoint="http://localhost"),
            sdk.StaticCredentialProvider({CONNECTION: credentials}),
            transport,
        )
        call = sdk.Call(
            CALL_ID,
            CONNECTION,
            sdk.OperationRef("jira-service-management", operation_name),
            context=context,
            max_sends=max_sends,
        )
        try:
            if operation_name == "createTicket":
                return await sdk.run_mutation(client.create_ticket(), call, TICKET_INPUT)  # type: ignore[return-value]
            return await sdk.run_mutation(client.add_comment(), call, COMMENT_INPUT)  # type: ignore[return-value]
        except sdk.RetryError as error:
            return error


def always(status: int, body: str = "{}") -> Answer:
    return lambda _: (status, body)


def branch_of(result: sdk.MutationResult[object] | sdk.RetryError) -> str:
    return "retry" if isinstance(result, sdk.RetryError) else result.branch


@pytest.mark.asyncio
async def test_unknown_outcome_resends_below_the_cap_then_is_uncertain() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    for attempt in range(1, CREATE_TICKET_DEFINITION.max_sends):
        context.attempt = attempt
        assert branch_of(await run("createTicket", always(500), context, requests)) == "retry"
        assert context.marker is not None and context.marker.send_count == attempt
    context.attempt = CREATE_TICKET_DEFINITION.max_sends
    assert branch_of(await run("createTicket", always(500), context, requests)) == "uncertain"
    context.attempt += 1
    assert branch_of(await run("createTicket", always(500), context, requests)) == "uncertain"
    assert len(requests) == 3


@pytest.mark.asyncio
async def test_send_cap_of_one_never_resends() -> None:
    requests: list[httpx.Request] = []
    result = await run("addComment", always(409), AttemptContext(), requests, max_sends=1)
    assert branch_of(result) == "uncertain"
    assert len(requests) == 1


@pytest.mark.asyncio
async def test_later_attempt_resends_and_saved_identity_is_never_resent() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext(attempt=2)
    answer = created_or_read_back(200, CREATED_READ_BACK)
    assert branch_of(await run("createTicket", answer, context, requests)) == "created"
    assert context.marker is not None and context.marker.provider_id == "10042"
    context.attempt = 3
    assert branch_of(await run("createTicket", answer, context, requests)) == "created"
    assert [request.method for request in requests] == ["POST", "GET"], (
        "the saved ID is read back, never resent"
    )
    requests.clear()
    at_cap = AttemptContext(attempt=ADD_COMMENT_DEFINITION.max_sends + 1)
    assert branch_of(await run("addComment", always(201, '{"id":"1001"}'), at_cap, requests)) == "uncertain"
    assert requests == []


@pytest.mark.asyncio
async def test_transient_read_back_of_a_saved_identity_retries_without_resending() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    partial = '{"issueId":"10042","issueKey":"bad key"}'
    assert (
        branch_of(await run("createTicket", created_or_read_back(503, "{}", partial), context, requests))
        == "retry"
    )
    context.attempt = 2
    assert (
        branch_of(await run("createTicket", created_or_read_back(404, "{}"), context, requests))
        == "uncertain"
    )
    assert [request.method for request in requests] == ["POST", "GET", "GET"]


@pytest.mark.asyncio
@pytest.mark.parametrize("operation_name", ["createTicket", "addComment"])
@pytest.mark.parametrize("body", [TRUNCATED_BODY, '{"issueKey":"SENTINEL","id":"SENTINEL"}'])
async def test_accepted_write_with_unusable_body_is_never_resent(operation_name: str, body: str) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert branch_of(await run(operation_name, always(201, body), context, requests)) == "uncertain"
    assert len(requests) == 1


@pytest.mark.asyncio
async def test_confirmed_conflict_saves_the_provider_identity() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    answer = created_or_read_back(200, CREATED_READ_BACK, '{"issueId":"10042"}', 409)
    assert branch_of(await run("createTicket", answer, context, requests)) == "created"
    assert context.marker is not None and context.marker.provider_id == "10042"


@pytest.mark.asyncio
async def test_credential_failure_after_a_possible_send_records_no_not_sent_marker() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    statuses = iter([500, 429])
    assert (
        branch_of(await run("createTicket", lambda _: (next(statuses), "{}"), context, requests)) == "retry"
    )
    context.attempt = 2
    assert (
        branch_of(await run("createTicket", lambda _: (next(statuses), "{}"), context, requests)) == "retry"
    )
    marker = context.marker
    assert marker is not None and marker.state == "canResend"
    context.attempt = 3
    result = await run("createTicket", always(201, CREATED), context, requests, credentials_token="")
    assert branch_of(result) == "uncertain", "the first send may have created the request"
    assert context.marker == marker, "notSent would hide the earlier possible send"
    assert len(requests) == 2


@pytest.mark.asyncio
async def test_definite_refusal_records_can_resend_and_is_never_resent() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert branch_of(await run("addComment", always(403), context, requests)) == "providerRejected"
    assert context.marker is not None and context.marker.state == "canResend"
    assert len(requests) == 1


@pytest.mark.asyncio
async def test_refusal_of_a_resend_is_uncertain() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert branch_of(await run("createTicket", always(502), context, requests)) == "retry"
    context.attempt = 2
    assert branch_of(await run("createTicket", always(400), context, requests)) == "uncertain"
    assert len(requests) == 2


@pytest.mark.asyncio
async def test_unreadable_checkpoint_retries_without_sending() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext(2)
    context.heartbeat_read_error = RuntimeError("heartbeat store unavailable")
    result = await run("createTicket", always(201, CREATED), context, requests)
    assert branch_of(result) == "retry"
    assert requests == []
