# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded resend of Jira Service Management writes whose outcome is unknown."""

import asyncio
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


STALLED_BODY = "<stalled>"
"""A scripted body that sends the status and headers, then stalls past every deadline."""


class BrokenStream(httpx.AsyncByteStream):
    """A response body that fails after the status and headers arrived."""

    async def __aiter__(self):  # type: ignore[no-untyped-def]
        yield b'{"issueId":"100'
        raise httpx.ReadError("connection reset")


class StalledStream(httpx.AsyncByteStream):
    """A response body that stops sending after the status and headers arrived."""

    async def __aiter__(self):  # type: ignore[no-untyped-def]
        yield b'{"issueId":"100'
        await asyncio.sleep(60)
        yield b'42"}'


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
    credential_provider: object | None = None,
    transition: jsm.TransitionTicketInput | None = None,
) -> sdk.MutationResult[object] | sdk.RetryError:
    """Run one Execute attempt of a write against a scripted provider."""

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        status, body = answer(request)
        headers = {"Content-Type": "application/json"}
        if body == TRUNCATED_BODY:
            return httpx.Response(status, stream=BrokenStream(), headers=headers)
        if body == STALLED_BODY:
            return httpx.Response(status, stream=StalledStream(), headers=headers)
        return httpx.Response(status, content=body.encode(), headers=headers)

    credentials = jsm.Credentials(
        "client-id", sdk.SecretString("secret"), sdk.SecretString(credentials_token), sdk.SecretString("")
    )
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as transport:
        client = jsm.Client(
            jsm.Config(cloud_id="11223344-a1b2-4b33-8c44-def123456789", endpoint="http://localhost"),
            credential_provider or sdk.StaticCredentialProvider({CONNECTION: credentials}),  # type: ignore[arg-type]
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
            if transition is not None:
                moved = await sdk.run_mutation(client.transition_ticket(), call, transition)
                return moved  # type: ignore[return-value]
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


def context_with_deadline(seconds: float) -> AttemptContext:
    """Return a first attempt whose Step deadline is ``seconds`` away."""
    context = AttemptContext()
    context.deadline = datetime.now(UTC) + timedelta(seconds=seconds)
    return context


@pytest.mark.asyncio
async def test_success_headers_then_the_write_deadline_cuts_the_body_is_never_resent() -> None:
    requests: list[httpx.Request] = []
    # The Fence write window (7/12 of 13 s) ends before the 8 s operation deadline.
    context = context_with_deadline(13)
    result = await run("createTicket", always(201, STALLED_BODY), context, requests)
    assert branch_of(result) == "uncertain"
    assert context.marker is not None and context.marker.state == "claimed"
    assert len(requests) == 1


@pytest.mark.asyncio
async def test_refusal_status_with_an_unreadable_body_is_an_unknown_outcome() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert branch_of(await run("addComment", always(400, TRUNCATED_BODY), context, requests)) == "retry"
    assert context.marker is not None and context.marker.state == "claimed"


class ScriptedCredentials:
    """Credentials whose lookup fails transiently or terminally on demand."""

    def __init__(self, mode: str) -> None:
        self.mode = mode

    async def resolve(self, call: sdk.Call) -> jsm.Credentials:
        """Fail as the mode says."""
        if self.mode == "transient":
            raise OSError("credential broker unavailable")
        raise sdk.ReauthorizationRequiredError("reconnect")


@pytest.mark.asyncio
async def test_transient_credential_failure_before_any_send_retries_and_records_not_sent() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    result = await run(
        "createTicket", always(201, CREATED), context, requests,
        credential_provider=ScriptedCredentials("transient"),
    )
    assert branch_of(result) == "retry"
    assert context.marker is not None and context.marker.state == "notSent"
    terminal = await run(
        "createTicket", always(201, CREATED), AttemptContext(), requests,
        credential_provider=ScriptedCredentials("terminal"),
    )
    assert branch_of(terminal) == "defect"
    assert requests == []


TRANSITION_CALL_ISSUE_IN_PROGRESS = (
    '{"id":"10042","key":"ITH-42","fields":{"status":{"id":"3","name":"In progress",'
    '"statusCategory":{"key":"indeterminate"}}},"transitions":[{"id":"21","name":"Pending",'
    '"to":{"id":"1","name":"Waiting for support","statusCategory":{"key":"new"}}}]}'
)
TRANSITION_CALL_ISSUE_WAITING = (
    '{"id":"10042","key":"ITH-42","fields":{"status":{"id":"1","name":"Waiting for support",'
    '"statusCategory":{"key":"new"}}},"transitions":[{"id":"11","name":"Pending",'
    '"to":{"id":"3","name":"In progress","statusCategory":{"key":"indeterminate"}}}]}'
)
PENDING = jsm.TransitionTicketInput(issue_id_or_key="ITH-42", transition_name="Pending")


def issue_then_post(issue: list[str], post: tuple[int, str]) -> Answer:
    """Answer each issue read with the current issue and each transition with ``post``."""
    return lambda request: (200, issue[0]) if request.method == "GET" else post


def transition_posts(requests: list[httpx.Request]) -> int:
    return sum(1 for request in requests if request.method == "POST")


@pytest.mark.asyncio
async def test_transition_after_an_unknown_outcome_is_never_sent_again() -> None:
    requests: list[httpx.Request] = []
    issue = [TRANSITION_CALL_ISSUE_IN_PROGRESS]
    context = AttemptContext()
    answer = issue_then_post(issue, (502, "{}"))
    assert branch_of(await run("transitionTicket", answer, context, requests, transition=PENDING)) == "retry"
    # Jira applied the toggle late; a resend would move the request back.
    issue[0] = TRANSITION_CALL_ISSUE_WAITING
    for attempt in (2, 3):
        context.attempt = attempt
        repeated = await run("transitionTicket", answer, context, requests, transition=PENDING)
        assert branch_of(repeated) == "retry"
    assert transition_posts(requests) == 1


@pytest.mark.asyncio
async def test_repeated_transition_recognizes_the_destination_status() -> None:
    requests: list[httpx.Request] = []
    issue = [TRANSITION_CALL_ISSUE_WAITING]
    move = jsm.TransitionTicketInput(
        issue_id_or_key="ITH-42", transition_id="11", destination_status_name="In progress"
    )
    context = AttemptContext()
    answer = issue_then_post(issue, (504, ""))
    assert branch_of(await run("transitionTicket", answer, context, requests, transition=move)) == "retry"
    issue[0] = TRANSITION_CALL_ISSUE_IN_PROGRESS
    context.attempt = 2
    repeated = await run("transitionTicket", answer, context, requests, transition=move)
    assert branch_of(repeated) == "transitioned"
    assert transition_posts(requests) == 1


@pytest.mark.asyncio
@pytest.mark.parametrize(("status", "body"), [(404, "{}"), (200, '{"id":"x"}'), (403, "{}")])
async def test_repeated_transition_never_selects_a_failure_branch(status: int, body: str) -> None:
    requests: list[httpx.Request] = []
    result = await run(
        "transitionTicket", always(status, body), AttemptContext(2), requests, transition=PENDING
    )
    assert branch_of(result) == "retry"
    assert len(requests) == 1


@pytest.mark.asyncio
async def test_transition_rate_limit_allows_the_next_attempt_to_send() -> None:
    requests: list[httpx.Request] = []
    statuses = [(429, "{}"), (204, "")]

    def answer(request: httpx.Request) -> tuple[int, str]:
        return (200, TRANSITION_CALL_ISSUE_IN_PROGRESS) if request.method == "GET" else statuses.pop(0)

    context = AttemptContext()
    assert branch_of(await run("transitionTicket", answer, context, requests, transition=PENDING)) == "retry"
    assert context.marker is not None and context.marker.state == "canResend"
    context.attempt = 2
    repeated = await run("transitionTicket", answer, context, requests, transition=PENDING)
    assert branch_of(repeated) == "transitioned"
    assert transition_posts(requests) == 2


@pytest.mark.asyncio
async def test_transition_success_headers_then_the_deadline_cuts_the_body_is_transitioned() -> None:
    requests: list[httpx.Request] = []
    answer = issue_then_post([TRANSITION_CALL_ISSUE_IN_PROGRESS], (200, STALLED_BODY))
    result = await run("transitionTicket", answer, context_with_deadline(8.5), requests, transition=PENDING)
    assert branch_of(result) == "transitioned"
    assert transition_posts(requests) == 1


@pytest.mark.asyncio
async def test_transition_credential_failures_follow_the_send_history() -> None:
    requests: list[httpx.Request] = []
    terminal = ScriptedCredentials("terminal")
    first = await run(
        "transitionTicket", always(204, ""), AttemptContext(), requests,
        credential_provider=terminal, transition=PENDING,
    )
    assert branch_of(first) == "defect"
    later = await run(
        "transitionTicket", always(204, ""), AttemptContext(2), requests,
        credential_provider=terminal, transition=PENDING,
    )
    assert branch_of(later) == "retry", "a terminal failure after a possible send never selects defect"
    context = AttemptContext()
    transient = await run(
        "transitionTicket", always(204, ""), context, requests,
        credential_provider=ScriptedCredentials("transient"), transition=PENDING,
    )
    assert branch_of(transient) == "retry"
    assert context.marker is not None and context.marker.state == "notSent"
    assert requests == []
