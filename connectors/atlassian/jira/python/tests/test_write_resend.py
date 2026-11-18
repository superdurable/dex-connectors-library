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
TRUNCATED_BODY = "<truncated>"
"""A scripted body that sends the status and headers, then breaks the connection."""


class BrokenStream(httpx.AsyncByteStream):
    """A response body that fails after the status and headers arrived."""

    async def __aiter__(self):  # type: ignore[no-untyped-def]
        yield b'{"id":"100'
        raise httpx.ReadError("connection reset")


class ScriptedCredentials:
    """Credentials whose lookup fails transiently or terminally on demand."""

    def __init__(self) -> None:
        self.mode = "ready"

    async def resolve(self, call: sdk.Call) -> jira.Credentials:
        """Return the test token, or fail as the current mode says."""
        if self.mode == "transient":
            raise RuntimeError("credential broker unavailable")
        token = "" if self.mode == "invalid" else "jira-access-token"
        return jira.Credentials(
            "client-id", sdk.SecretString("secret"), sdk.SecretString(token), sdk.SecretString("")
        )


class AttemptContext:
    """Keep the last heartbeat across Execute attempts like Dex does."""

    def __init__(self, attempt: int = 1, heartbeat_value: object = None, read_error: bool = False) -> None:
        self.attempt = attempt
        self.run_id = "run"
        self.deadline = datetime.now(UTC) + timedelta(seconds=30)
        self.heartbeat_value = heartbeat_value
        self.read_error = read_error
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
        if self.read_error:
            raise RuntimeError("heartbeat unavailable")
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
    max_sends: int = CREATE_ISSUE_DEFINITION.max_sends,
    credential_provider: object | None = None,
) -> sdk.MutationResult[object] | sdk.RetryError:
    """Run one Execute attempt of a Jira write against a scripted provider."""

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        status, body = answer(request)
        if body == TRUNCATED_BODY:
            return httpx.Response(status, stream=BrokenStream(), headers={"Content-Type": "application/json"})
        return httpx.Response(status, content=body.encode(), headers={"Content-Type": "application/json"})

    credentials = jira.Credentials(
        "client-id", sdk.SecretString("secret"), sdk.SecretString("jira-access-token"), sdk.SecretString("")
    )
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as transport:
        client = jira.Client(
            jira.Config(cloud_id=CLOUD_ID, endpoint="http://127.0.0.1:12345"),
            credential_provider or sdk.StaticCredentialProvider({CONNECTION: credentials}),  # type: ignore[arg-type]
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
async def test_unreadable_checkpoint_retries_without_sending() -> None:
    requests: list[httpx.Request] = []
    unreadable = AttemptContext(2)
    unreadable.heartbeat_read_error = RuntimeError("heartbeat store unavailable")
    result = await run("createIssue", empty_search_or(201, "{}"), unreadable, requests)
    assert isinstance(result, sdk.RetryError)
    result = await run("addComment", empty_search_or(201, "{}"), AttemptContext(2, read_error=True), requests)
    assert isinstance(result, sdk.RetryError)
    assert requests == []


def scripted_writes(statuses: list[tuple[int, str]]) -> Answer:
    """Answer read-backs with nothing and each write with the next scripted response."""
    remaining = list(statuses)

    def answer(request: httpx.Request) -> tuple[int, str]:
        if request.url.path.endswith("/search/jql"):
            return 200, '{"issues":[]}'
        if request.method == "GET":
            return 200, '{"comments":[]}'
        return remaining.pop(0)

    return answer


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("operation_name", "created"),
    [("createIssue", '{"id":"10043","key":"OPS-43"}'), ("addComment", '{"id":"10500"}')],
)
async def test_resend_after_can_resend_reads_back_before_the_third_send(
    operation_name: str, created: str
) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    answer = scripted_writes([(500, "{}"), (429, "{}"), (201, created)])
    assert isinstance(await run(operation_name, answer, context, requests), sdk.RetryError)
    context.attempt = 2
    assert isinstance(await run(operation_name, answer, context, requests), sdk.RetryError)
    assert context.marker is not None and context.marker.state == "canResend"
    context.attempt = 3
    requests.clear()
    result = await run(operation_name, answer, context, requests)
    assert isinstance(result, sdk.MutationResult) and result.branch in ("created", "added")
    assert [request.method for request in requests][-1] == "POST"
    assert len(requests) == 2, "attempt three reads back the possible first send before resending"


@pytest.mark.asyncio
@pytest.mark.parametrize("body", [TRUNCATED_BODY, "{}"])
@pytest.mark.parametrize("operation_name", ["createIssue", "addComment"])
async def test_accepted_write_with_broken_body_is_never_resent(operation_name: str, body: str) -> None:
    requests: list[httpx.Request] = []
    result = await run(operation_name, empty_search_or(201, body), AttemptContext(), requests)
    assert isinstance(result, sdk.MutationResult) and result.branch == "uncertain"
    assert sum(1 for request in requests if request.method == "POST" and "jql" not in request.url.path) == 1


@pytest.mark.asyncio
async def test_accepted_issue_without_id_is_uncertain_when_read_back_is_rate_limited() -> None:
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> tuple[int, str]:
        if request.url.path.endswith("/search/jql"):
            return 429, "{}"
        return 201, "{}"

    result = await run("createIssue", answer, AttemptContext(), requests)
    assert isinstance(result, sdk.MutationResult) and result.branch == "uncertain"
    assert posts(requests, "/issue") == 1


@pytest.mark.asyncio
async def test_credential_failure_after_a_possible_send_records_no_not_sent_marker() -> None:
    requests: list[httpx.Request] = []
    credentials = ScriptedCredentials()
    context = AttemptContext()
    answer = scripted_writes([(500, "{}"), (429, "{}")])
    for attempt in (1, 2):
        context.attempt = attempt
        retried = await run("createIssue", answer, context, requests, credential_provider=credentials)
        assert isinstance(retried, sdk.RetryError)
    marker = context.marker
    credentials.mode = "transient"
    context.attempt = 3
    assert isinstance(
        await run("createIssue", answer, context, requests, credential_provider=credentials), sdk.RetryError
    )
    assert context.marker == marker, "notSent would hide the earlier possible send"
    credentials.mode = "invalid"
    context.attempt = 4
    result = await run("createIssue", answer, context, requests, credential_provider=credentials)
    assert isinstance(result, sdk.MutationResult) and result.branch == "uncertain"
    assert posts(requests, "/issue") == 2
