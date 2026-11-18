# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read-back and bounded resend of ClickUp creates and comments whose outcome is unknown."""

import json
from collections.abc import Callable
from datetime import UTC, datetime, timedelta

import dex_connectors.sdk as sdk
import httpx
import pytest
from dex_connectors.sdk.singledispatch import Marker, State

from dex_connectors.clickup.client import Client, Config, Credentials
from dex_connectors.clickup.models import AddCommentInput, CreateTaskInput

CONNECTION = sdk.ConnectionRef("clickup", "clickup-test")
CALL_ID = sdk.CallID("bef06aa7-8066-5d69-a408-c3d4547c0405")
LIST_ID = "901410000001"
TASK_ID = "86b2x4k7q"
TASK_NAME = "Escalation: Fix login [86b2x4k00]"
FIRST_ATTEMPT_AT = datetime(2026, 1, 1, tzinfo=UTC)

Responder = Callable[[httpx.Request], httpx.Response]


class AttemptContext:
    """Keep the last heartbeat across Execute attempts like Dex does."""

    def __init__(self, attempt: int = 1, heartbeat_value: object = None) -> None:
        self.attempt = attempt
        self.run_id = "run"
        self.deadline = datetime.now(UTC) + timedelta(seconds=30)
        self.first_attempt_at = FIRST_ATTEMPT_AT
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


def task_body(
    identifier: str, created_at: datetime = FIRST_ATTEMPT_AT, parent: str | None = None
) -> dict[str, object]:
    """Render a ClickUp task."""
    return {
        "id": identifier,
        "name": TASK_NAME,
        "status": {"status": "open", "type": "open"},
        "date_created": str(int(created_at.timestamp() * 1000) + 1000),
        "list": {"id": LIST_ID},
        "parent": parent,
    }


def json_response(status: int, value: object) -> httpx.Response:
    """Answer with JSON."""
    return httpx.Response(
        status, content=json.dumps(value).encode(), headers={"Content-Type": "application/json"}
    )


async def run(
    operation: str,
    responder: Responder,
    context: AttemptContext,
    requests: list[httpx.Request],
    has_credentials: bool = True,
) -> sdk.MutationResult[object] | str:
    """Run one Execute attempt and return its result, or `retry`."""

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return responder(request)

    credentials = Credentials(sdk.SecretString("pk_4411723_TESTTOKEN"), sdk.SecretString("webhook-secret"))
    client = Client(
        Config(),
        sdk.StaticCredentialProvider({CONNECTION: credentials} if has_credentials else {}),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
        api_base_url="http://127.0.0.1:12345/api/v2",
    )
    call = sdk.Call(CALL_ID, CONNECTION, sdk.OperationRef("clickup", operation), context=context, max_sends=3)
    try:
        if operation == "createTask":
            return await sdk.run_mutation(client.create_task(), call, CreateTaskInput(LIST_ID, TASK_NAME))
        return await sdk.run_mutation(client.add_comment(), call, AddCommentInput(TASK_ID, "Escalated."))
    except sdk.RetryError:
        return "retry"


def branch_of(result: sdk.MutationResult[object] | str) -> str:
    """Return the selected branch or `retry`."""
    return result if isinstance(result, str) else result.branch


def marker_of(context: AttemptContext) -> Marker:
    """Return the attempt's single-dispatch marker."""
    marker = context.marker
    assert marker is not None
    return marker


def methods(requests: list[httpx.Request]) -> list[str]:
    """Return the request methods in order."""
    return [request.method for request in requests]


@pytest.mark.asyncio
async def test_unknown_create_reads_back_then_resends_below_the_cap_and_is_uncertain_at_the_cap() -> None:
    def responder(request: httpx.Request) -> httpx.Response:
        if request.method == "POST":
            return json_response(500, {"ECODE": "SERVER_001"})
        return json_response(200, {"tasks": [], "last_page": True})

    context, requests = AttemptContext(), []
    for attempt in range(1, 4):
        context.attempt = attempt
        assert await run("createTask", responder, context, requests) == "retry"
        assert marker_of(context).send_count == attempt
    context.attempt = 4
    assert branch_of(await run("createTask", responder, context, requests)) == "uncertain"
    assert methods(requests) == ["POST", "GET", "POST", "GET", "POST", "GET"]
    assert requests[1].url.params["date_created_gt"] == str(
        int((FIRST_ATTEMPT_AT - timedelta(minutes=5)).timestamp() * 1000)
    )


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("tasks", "expected_branch", "expected_methods"),
    [
        ([task_body("86b2x4k7q")], "created", ["POST", "GET"]),
        ([task_body("86b2x4k7q"), task_body("86b2x4k7r")], "uncertain", ["POST", "GET"]),
        ([task_body("86b2x4k7q", parent="86b2x4k99")], "created", ["POST", "GET", "POST"]),
    ],
)
async def test_unknown_create_reports_one_match_and_never_resends_beside_several(
    tasks: list[dict[str, object]], expected_branch: str, expected_methods: list[str]
) -> None:
    def responder(request: httpx.Request) -> httpx.Response:
        if request.method == "GET":
            return json_response(200, {"tasks": tasks, "last_page": True})
        if len(requests) == 1:
            raise httpx.ReadError("connection lost after the request was sent")
        return json_response(200, task_body("86b2x4k7s"))

    context, requests = AttemptContext(), []
    assert await run("createTask", responder, context, requests) == "retry"
    context.attempt = 2
    result = await run("createTask", responder, context, requests)
    assert branch_of(result) == expected_branch
    assert methods(requests) == expected_methods
    if expected_methods == ["POST", "GET"] and expected_branch == "created":
        assert not isinstance(result, str) and result.value.was_already_applied


@pytest.mark.asyncio
async def test_definite_refusal_of_a_first_send_records_can_resend_and_is_never_resent() -> None:
    context, requests = AttemptContext(), []
    result = await run("createTask", lambda _: json_response(400, {"ECODE": "ITEM_015"}), context, requests)
    assert branch_of(result) == "providerRejected"
    assert marker_of(context).state == State.CAN_RESEND
    assert len(requests) == 1


@pytest.mark.asyncio
@pytest.mark.parametrize("status", [409, 410, 425, 308])
async def test_status_that_proves_no_refusal_is_unknown(status: int) -> None:
    context, requests = AttemptContext(), []
    assert (
        await run("createTask", lambda _: json_response(status, {"ECODE": "ITEM_027"}), context, requests)
        == "retry"
    )
    assert marker_of(context).state == State.CLAIMED


@pytest.mark.asyncio
@pytest.mark.parametrize("status", [429, 503])
async def test_status_proving_nothing_applied_resends_without_read_back(status: int) -> None:
    def responder(_: httpx.Request) -> httpx.Response:
        return json_response(status if len(requests) == 1 else 200, task_body("86b2x4k7q"))

    context, requests = AttemptContext(), []
    assert await run("createTask", responder, context, requests) == "retry"
    assert marker_of(context).state == State.CAN_RESEND
    context.attempt = 2
    assert branch_of(await run("createTask", responder, context, requests)) == "created"
    assert methods(requests) == ["POST", "POST"]


@pytest.mark.asyncio
async def test_unreadable_checkpoint_retries_without_sending() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext(2)
    context.heartbeat_read_error = RuntimeError("heartbeat store unavailable")
    assert await run("createTask", lambda _: pytest.fail("request sent"), context, requests) == "retry"
    assert requests == []


@pytest.mark.asyncio
async def test_accepted_create_is_read_back_by_id_and_never_resent() -> None:
    marker = Marker(CALL_ID, 1, State.CLAIMED, provider_id=TASK_ID, send_count=1)
    for status, expected_branch in ((200, "created"), (404, "uncertain")):
        context, requests = AttemptContext(2, marker), []
        result = await run(
            "createTask", lambda _, code=status: json_response(code, task_body(TASK_ID)), context, requests
        )
        assert branch_of(result) == expected_branch
        assert [(request.method, request.url.path) for request in requests] == [
            ("GET", f"/api/v2/task/{TASK_ID}")
        ]


@pytest.mark.asyncio
async def test_unknown_comment_is_uncertain_only_at_the_cap() -> None:
    def responder(request: httpx.Request) -> httpx.Response:
        if request.method == "POST":
            raise httpx.ReadError("connection lost after the request was sent")
        return json_response(200, {"comments": []})

    context, requests = AttemptContext(), []
    for attempt in range(1, 4):
        context.attempt = attempt
        assert await run("addComment", responder, context, requests) == "retry"
    context.attempt = 4
    assert branch_of(await run("addComment", responder, context, requests)) == "uncertain"
    assert methods(requests).count("POST") == 3


@pytest.mark.asyncio
async def test_accepted_comment_is_matched_only_by_its_saved_id() -> None:
    marker = Marker(CALL_ID, 1, State.CLAIMED, provider_id="700001", send_count=1)
    comments = {"comments": [{"id": "700001", "comment_text": "Edited text", "date": "1767225603000"}]}
    context, requests = AttemptContext(2, marker), []
    result = await run("addComment", lambda _: json_response(200, comments), context, requests)
    assert branch_of(result) == "added"
    assert methods(requests) == ["GET"]


class BrokenBodyStream(httpx.AsyncByteStream):
    """Fail after the response headers arrived."""

    async def __aiter__(self):  # type: ignore[no-untyped-def]
        raise httpx.ReadError("connection lost while reading the body")
        yield b""


@pytest.mark.asyncio
@pytest.mark.parametrize("operation", ["createTask", "addComment"])
async def test_resend_after_a_can_resend_marker_reads_back_first(operation: str) -> None:
    empty = {"tasks": []} if operation == "createTask" else {"comments": []}
    accepted = task_body(TASK_ID) if operation == "createTask" else {"id": "700002", "date": "1767225603000"}

    def responder(request: httpx.Request) -> httpx.Response:
        if request.method == "GET":
            return json_response(200, empty)
        posts = methods(requests).count("POST")
        if posts == 1:
            raise httpx.ReadError("connection lost after the request was sent")
        return json_response(429 if posts == 2 else 200, accepted)

    context, requests = AttemptContext(), []
    for attempt in (1, 2):
        context.attempt = attempt
        assert await run(operation, responder, context, requests) == "retry"
    assert marker_of(context).state == State.CAN_RESEND
    context.attempt = 3
    result = await run(operation, responder, context, requests)
    assert branch_of(result) in ("created", "added")
    assert methods(requests) == ["POST", "GET", "POST", "GET", "POST"]


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("operation", "status", "error_code"),
    [("createTask", 404, "ITEM_013"), ("createTask", 401, "OAUTH_027"), ("addComment", 404, "ITEM_013")],
)
async def test_read_back_of_a_missing_target_after_a_possible_send_is_uncertain(
    operation: str, status: int, error_code: str
) -> None:
    marker = Marker(CALL_ID, 1, State.CLAIMED, send_count=1)
    context, requests = AttemptContext(2, marker), []
    result = await run(operation, lambda _: json_response(status, {"ECODE": error_code}), context, requests)
    assert branch_of(result) == "uncertain"
    assert methods(requests) == ["GET"]


@pytest.mark.asyncio
@pytest.mark.parametrize("operation", ["createTask", "addComment"])
@pytest.mark.parametrize(
    "response",
    [
        lambda: httpx.Response(200, stream=BrokenBodyStream()),
        lambda: json_response(200, {"name": "no identity"}),
    ],
    ids=["body breaks after 2xx headers", "no identity"],
)
async def test_accepted_write_without_an_identity_is_uncertain_and_never_resent(
    operation: str, response: Callable[[], httpx.Response]
) -> None:
    context, requests = AttemptContext(), []
    result = await run(operation, lambda _: response(), context, requests)
    assert branch_of(result) == "uncertain"
    assert methods(requests) == ["POST"]


@pytest.mark.asyncio
async def test_credential_failure_after_a_possible_send_retries_without_not_sent() -> None:
    marker = Marker(CALL_ID, 1, State.CLAIMED, send_count=1)
    context, requests = AttemptContext(2, marker), []
    result = await run(
        "createTask", lambda _: pytest.fail("request sent"), context, requests, has_credentials=False
    )
    assert result == "retry"
    assert marker_of(context).state == State.CLAIMED
    assert requests == []
