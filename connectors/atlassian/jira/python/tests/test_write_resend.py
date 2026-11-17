# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded resend of Jira creates and comments whose outcome is unknown."""

from collections.abc import Callable
from datetime import UTC, datetime, timedelta

import dex_connectors.sdk as sdk
import httpx
import pytest
from dex_connectors.sdk.singledispatch import Marker

import dex_connectors.atlassian_jira as jira
from dex_connectors.atlassian_jira._generated.definitions import (
    ADD_COMMENT_DEFINITION,
    CREATE_ISSUE_DEFINITION,
)

CONNECTION = sdk.ConnectionRef("atlassian", "jira-test")
CALL_ID = sdk.CallID("62d8b09d-d5a4-53c5-991a-4adc7a1ae5cd")
CLOUD_ID = "11223344-a1b2-4b33-8c44-def123456789"
ISSUE_INPUT = jira.CreateIssueInput(project_key="OPS", issue_type_name="Task", summary="Fire panel wiring")
COMMENT_INPUT = jira.AddCommentInput(issue_id_or_key="OPS-43", body="Triage started.")

Answer = Callable[[httpx.Request], tuple[int, str]]


class AttemptContext:
    """Keep the last heartbeat across Execute attempts like Dex does."""

    def __init__(self, attempt: int = 1, marker: Marker | None = None, read_error: bool = False) -> None:
        self.attempt = attempt
        self.run_id = "run"
        self.deadline = datetime.now(UTC) + timedelta(seconds=30)
        self.marker = marker
        self.read_error = read_error

    def has_last_heartbeat_value(self) -> bool:
        """Report whether an earlier attempt recorded a heartbeat."""
        if self.read_error:
            raise RuntimeError("heartbeat unavailable")
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
    max_sends: int = CREATE_ISSUE_DEFINITION.max_sends,
) -> sdk.MutationResult[object] | sdk.RetryError:
    """Run one Execute attempt of a Jira write against a scripted provider."""

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        status, body = answer(request)
        return httpx.Response(status, content=body.encode(), headers={"Content-Type": "application/json"})

    credentials = jira.Credentials(
        "client-id", sdk.SecretString("secret"), sdk.SecretString("jira-access-token"), sdk.SecretString("")
    )
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as transport:
        client = jira.Client(
            jira.Config(cloud_id=CLOUD_ID, endpoint="http://127.0.0.1:12345"),
            sdk.StaticCredentialProvider({CONNECTION: credentials}),
            transport,
        )
        call = sdk.Call(
            CALL_ID,
            CONNECTION,
            sdk.OperationRef("jira", operation_name),
            context=context,
            max_sends=max_sends,
        )
        try:
            if operation_name == "createIssue":
                return await sdk.run_mutation(client.create_issue(), call, ISSUE_INPUT)  # type: ignore[return-value]
            return await sdk.run_mutation(client.add_comment(), call, COMMENT_INPUT)  # type: ignore[return-value]
        except sdk.RetryError as error:
            return error


def posts(requests: list[httpx.Request], suffix: str) -> int:
    return sum(1 for request in requests if request.method == "POST" and request.url.path.endswith(suffix))


def empty_search_or(status: int, body: str) -> Answer:
    def answer(request: httpx.Request) -> tuple[int, str]:
        if request.url.path.endswith("/search/jql"):
            return 200, '{"issues":[]}'
        if request.method == "GET":
            return 200, '{"comments":[]}'
        return status, body

    return answer


@pytest.mark.asyncio
async def test_unknown_issue_outcome_resends_below_the_cap_then_is_uncertain() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    for attempt in range(1, CREATE_ISSUE_DEFINITION.max_sends):
        context.attempt = attempt
        retried = await run("createIssue", empty_search_or(500, "{}"), context, requests)
        assert isinstance(retried, sdk.RetryError)
        assert context.marker is not None and context.marker.send_count == attempt
    context.attempt = CREATE_ISSUE_DEFINITION.max_sends
    result = await run("createIssue", empty_search_or(500, "{}"), context, requests)
    assert isinstance(result, sdk.MutationResult) and result.branch == "uncertain"
    context.attempt += 1
    result = await run("createIssue", empty_search_or(500, "{}"), context, requests)
    assert isinstance(result, sdk.MutationResult) and result.branch == "uncertain"
    assert posts(requests, "/issue") == 3


@pytest.mark.asyncio
async def test_send_cap_of_one_never_resends_a_comment() -> None:
    requests: list[httpx.Request] = []
    result = await run("addComment", empty_search_or(409, "{}"), AttemptContext(), requests, max_sends=1)
    assert isinstance(result, sdk.MutationResult) and result.branch == "uncertain"
    assert posts(requests, "/comment") == 1


@pytest.mark.asyncio
async def test_later_attempt_reads_back_before_resending() -> None:
    requests: list[httpx.Request] = []
    created = empty_search_or(201, '{"id":"10043","key":"OPS-43"}')
    result = await run("createIssue", created, AttemptContext(attempt=2), requests)
    assert isinstance(result, sdk.MutationResult) and result.branch == "created"
    assert [request.url.path.rsplit("/", 1)[-1] for request in requests] == ["jql", "issue"]
    requests.clear()
    at_cap = AttemptContext(attempt=ADD_COMMENT_DEFINITION.max_sends + 1)
    result = await run("addComment", empty_search_or(201, '{"id":"10500"}'), at_cap, requests)
    assert isinstance(result, sdk.MutationResult) and result.branch == "uncertain"
    assert posts(requests, "/comment") == 0


@pytest.mark.asyncio
async def test_definite_refusal_records_can_resend_and_is_never_resent() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    refusal = empty_search_or(400, '{"errors":{"summary":"Summary is required."}}')
    result = await run("createIssue", refusal, context, requests)
    assert isinstance(result, sdk.MutationResult) and result.branch == "providerRejected"
    assert context.marker is not None and context.marker.state == "canResend"
    assert posts(requests, "/issue") == 1


@pytest.mark.asyncio
async def test_refusal_of_a_resend_is_uncertain() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert isinstance(await run("createIssue", empty_search_or(502, "{}"), context, requests), sdk.RetryError)
    context.attempt = 2
    result = await run("createIssue", empty_search_or(403, "{}"), context, requests)
    assert isinstance(result, sdk.MutationResult) and result.branch == "uncertain"
    assert posts(requests, "/issue") == 2


@pytest.mark.asyncio
async def test_untrusted_checkpoint_retries_without_sending() -> None:
    requests: list[httpx.Request] = []
    foreign = Marker(call_id=sdk.CallID("another-call"), attempt=1, state="claimed")
    result = await run("createIssue", empty_search_or(201, "{}"), AttemptContext(2, foreign), requests)
    assert isinstance(result, sdk.RetryError)
    result = await run("addComment", empty_search_or(201, "{}"), AttemptContext(2, read_error=True), requests)
    assert isinstance(result, sdk.RetryError)
    assert requests == []
