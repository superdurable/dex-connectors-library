# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded resend of Jira creates and comments whose outcome is unknown."""

import asyncio
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


STALLED_BODY = "<stalled>"
"""A scripted body that sends the status and headers, then stalls past the write window."""


class BrokenStream(httpx.AsyncByteStream):
    """A response body that fails after the status and headers arrived."""

    async def __aiter__(self):  # type: ignore[no-untyped-def]
        yield b'{"id":"100'
        raise httpx.ReadError("connection reset")


class StalledStream(httpx.AsyncByteStream):
    """A response body that stops sending after the status and headers arrived."""

    async def __aiter__(self):  # type: ignore[no-untyped-def]
        yield b'{"id":"100'
        await asyncio.sleep(30)
        yield b'43"}'


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
    transition: jira.TransitionIssueInput | None = None,
) -> sdk.MutationResult[object] | sdk.RetryError:
    """Run one Execute attempt of a Jira write against a scripted provider."""

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        status, body = answer(request)
        if body == TRUNCATED_BODY:
            return httpx.Response(status, stream=BrokenStream(), headers={"Content-Type": "application/json"})
        if body == STALLED_BODY:
            headers = {"Content-Type": "application/json"}
            return httpx.Response(status, stream=StalledStream(), headers=headers)
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
            if transition is not None:
                moved = await sdk.run_mutation(client.transition_issue(), call, transition)
                return moved  # type: ignore[return-value]
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


def short_write_window_context() -> AttemptContext:
    """Return a first attempt whose Fence write window ends well before a stalled body finishes."""
    context = AttemptContext()
    context.deadline = datetime.now(UTC) + timedelta(seconds=1.5)
    return context


@pytest.mark.asyncio
@pytest.mark.parametrize("operation_name", ["createIssue", "addComment"])
async def test_success_headers_then_the_write_deadline_cuts_the_body_is_never_resent(
    operation_name: str,
) -> None:
    requests: list[httpx.Request] = []
    context = short_write_window_context()
    result = await run(operation_name, empty_search_or(201, STALLED_BODY), context, requests)
    assert isinstance(result, sdk.MutationResult) and result.branch == "uncertain"
    assert context.marker is not None and context.marker.state == "claimed"
    assert sum(1 for request in requests if request.method == "POST" and "jql" not in request.url.path) == 1


@pytest.mark.asyncio
async def test_refusal_status_with_an_unreadable_body_is_an_unknown_outcome() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    result = await run("createIssue", empty_search_or(400, TRUNCATED_BODY), context, requests)
    assert isinstance(result, sdk.RetryError), "a cut refusal body may hide an accepted create"
    assert context.marker is not None and context.marker.state == "claimed"


TRANSITION = jira.TransitionIssueInput(issue_id_or_key="OPS-42", transition_name="Stop Progress")
IN_PROGRESS_ISSUE = (
    '{"id":"10042","key":"OPS-42","fields":{"status":{"id":"3","name":"In Progress",'
    '"statusCategory":{"key":"indeterminate"}}},"transitions":[{"id":"21","name":"Stop Progress",'
    '"to":{"id":"1","name":"To Do","statusCategory":{"key":"new"}}}]}'
)
TO_DO_ISSUE = (
    '{"id":"10042","key":"OPS-42","fields":{"status":{"id":"1","name":"To Do",'
    '"statusCategory":{"key":"new"}}},"transitions":[{"id":"21","name":"Stop Progress",'
    '"to":{"id":"3","name":"In Progress","statusCategory":{"key":"indeterminate"}}}]}'
)


def issue_then_post(issue: list[str], post: tuple[int, str]) -> Answer:
    """Answer each issue read with the current issue and each transition with ``post``."""

    def answer(request: httpx.Request) -> tuple[int, str]:
        if request.method == "GET":
            return 200, issue[0]
        return post

    return answer


@pytest.mark.asyncio
async def test_transition_after_an_unknown_outcome_is_never_sent_again() -> None:
    requests: list[httpx.Request] = []
    issue = [IN_PROGRESS_ISSUE]
    context = AttemptContext()
    answer = issue_then_post(issue, (502, "{}"))
    first = await run("transitionIssue", answer, context, requests, transition=TRANSITION)
    assert isinstance(first, sdk.RetryError)
    # Jira applied the toggle late; a resend would move the issue back.
    issue[0] = TO_DO_ISSUE
    for attempt in (2, 3):
        context.attempt = attempt
        retried = await run("transitionIssue", answer, context, requests, transition=TRANSITION)
        assert isinstance(retried, sdk.RetryError)
    assert posts(requests, "/transitions") == 1


@pytest.mark.asyncio
async def test_repeated_transition_recognizes_the_destination_status() -> None:
    requests: list[httpx.Request] = []
    issue = [TO_DO_ISSUE]
    context = AttemptContext()
    move = jira.TransitionIssueInput(
        issue_id_or_key="OPS-42", transition_id="21", destination_status_name="In Progress"
    )
    answer = issue_then_post(issue, (504, ""))
    first = await run("transitionIssue", answer, context, requests, transition=move)
    assert isinstance(first, sdk.RetryError)
    issue[0] = IN_PROGRESS_ISSUE
    context.attempt = 2
    result = await run("transitionIssue", answer, context, requests, transition=move)
    assert isinstance(result, sdk.MutationResult) and result.branch == "transitioned"
    assert posts(requests, "/transitions") == 1


@pytest.mark.asyncio
@pytest.mark.parametrize(("status", "body"), [(404, "{}"), (200, '{"id":"x"}'), (403, "{}")])
async def test_repeated_transition_never_selects_a_failure_branch(status: int, body: str) -> None:
    requests: list[httpx.Request] = []
    result = await run(
        "transitionIssue", lambda _request: (status, body), AttemptContext(attempt=2), requests,
        transition=TRANSITION,
    )
    assert isinstance(result, sdk.RetryError)
    assert len(requests) == 1


@pytest.mark.asyncio
async def test_transition_rate_limit_allows_the_next_attempt_to_send() -> None:
    requests: list[httpx.Request] = []
    issue = [IN_PROGRESS_ISSUE]
    statuses = [(429, "{}"), (204, "")]

    def answer(request: httpx.Request) -> tuple[int, str]:
        if request.method == "GET":
            return 200, issue[0]
        return statuses.pop(0)

    context = AttemptContext()
    first = await run("transitionIssue", answer, context, requests, transition=TRANSITION)
    assert isinstance(first, sdk.RetryError)
    assert context.marker is not None and context.marker.state == "canResend"
    context.attempt = 2
    result = await run("transitionIssue", answer, context, requests, transition=TRANSITION)
    assert isinstance(result, sdk.MutationResult) and result.branch == "transitioned"
    assert posts(requests, "/transitions") == 2


@pytest.mark.asyncio
async def test_transition_success_headers_then_the_write_deadline_cuts_the_body_is_transitioned() -> None:
    requests: list[httpx.Request] = []
    result = await run(
        "transitionIssue", issue_then_post([IN_PROGRESS_ISSUE], (200, STALLED_BODY)),
        short_write_window_context(), requests, transition=TRANSITION,
    )
    assert isinstance(result, sdk.MutationResult) and result.branch == "transitioned"
    assert posts(requests, "/transitions") == 1


@pytest.mark.asyncio
async def test_transition_credential_failures_follow_the_send_history() -> None:
    requests: list[httpx.Request] = []
    credentials = ScriptedCredentials()
    credentials.mode = "invalid"
    answer = issue_then_post([IN_PROGRESS_ISSUE], (204, ""))
    result = await run(
        "transitionIssue", answer, AttemptContext(), requests,
        credential_provider=credentials, transition=TRANSITION,
    )
    assert isinstance(result, sdk.MutationResult) and result.branch == "defect"
    later = await run(
        "transitionIssue", answer, AttemptContext(attempt=2), requests,
        credential_provider=credentials, transition=TRANSITION,
    )
    assert isinstance(later, sdk.RetryError), "a terminal failure after a possible send never selects defect"
    credentials.mode = "transient"
    context = AttemptContext()
    transient = await run(
        "transitionIssue", answer, context, requests,
        credential_provider=credentials, transition=TRANSITION,
    )
    assert isinstance(transient, sdk.RetryError)
    assert context.marker is not None and context.marker.state == "notSent"
    assert requests == []
