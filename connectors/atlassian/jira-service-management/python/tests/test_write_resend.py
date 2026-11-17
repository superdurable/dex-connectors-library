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

Answer = Callable[[httpx.Request], tuple[int, str]]


class AttemptContext:
    """Keep the last heartbeat across Execute attempts like Dex does."""

    def __init__(self, attempt: int = 1, marker: Marker | None = None) -> None:
        self.attempt = attempt
        self.run_id = "run"
        self.deadline = datetime.now(UTC) + timedelta(seconds=30)
        self.marker = marker

    def has_last_heartbeat_value(self) -> bool:
        """Report whether an earlier attempt recorded a heartbeat."""
        return self.marker is not None

    def get_last_heartbeat_value(self, value_type: type[object]) -> object:
        """Return the last marker."""
        return self.marker

    async def heartbeat(self, value: object = None) -> None:
        """Record the attempt's marker."""
        self.marker = value if isinstance(value, Marker) else None


async def run(
    operation_name: str,
    answer: Answer,
    context: AttemptContext,
    requests: list[httpx.Request],
    *,
    max_sends: int = CREATE_TICKET_DEFINITION.max_sends,
) -> sdk.MutationResult[object] | sdk.RetryError:
    """Run one Execute attempt of a write against a scripted provider."""

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        status, body = answer(request)
        return httpx.Response(status, content=body.encode(), headers={"Content-Type": "application/json"})

    credentials = jsm.Credentials(
        "client-id", sdk.SecretString("secret"), sdk.SecretString("jsm-access-token"), sdk.SecretString("")
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
    assert branch_of(await run("createTicket", always(201, CREATED), context, requests)) == "created"
    assert context.marker is not None and context.marker.provider_id == "10042"
    context.attempt = 3
    assert branch_of(await run("createTicket", always(201, CREATED), context, requests)) == "uncertain"
    at_cap = AttemptContext(attempt=ADD_COMMENT_DEFINITION.max_sends + 1)
    assert branch_of(await run("addComment", always(201, '{"id":"1001"}'), at_cap, requests)) == "uncertain"
    assert len(requests) == 1


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
async def test_untrusted_checkpoint_retries_without_sending() -> None:
    requests: list[httpx.Request] = []
    foreign = Marker(call_id=sdk.CallID("another-call"), attempt=1, state="claimed")
    result = await run("createTicket", always(201, CREATED), AttemptContext(2, foreign), requests)
    assert branch_of(result) == "retry"
    assert requests == []
