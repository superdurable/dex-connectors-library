# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded resend of Asana creates and comments whose outcome is unknown."""

from datetime import UTC, datetime, timedelta

import dex_connectors.sdk as sdk
import httpx
import pytest
from dex_connectors.sdk.singledispatch import Marker

import dex_connectors.asana as asana
from dex_connectors.asana._generated.definitions import ADD_COMMENT_DEFINITION, CREATE_TASK_DEFINITION

CONNECTION = sdk.ConnectionRef("asana", "asana-test")
CALL_ID = sdk.CallID("6072a0ce-9ddb-5937-8998-675e48cfa406")
TASK_ID = "1204567890123456"
CREATED = f'{{"data":{{"gid":"{TASK_ID}"}}}}'


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
    status: int,
    body: str,
    context: AttemptContext,
    requests: list[httpx.Request],
    *,
    max_sends: int = CREATE_TASK_DEFINITION.max_sends,
) -> str:
    """Run one Execute attempt of an Asana write and return its branch or `retry`."""

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(status, content=body.encode(), headers={"Content-Type": "application/json"})

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as http_client:
        client = asana.Client(
            asana.Config(endpoint="http://127.0.0.1:22201/api/1.0"),
            sdk.StaticCredentialProvider({CONNECTION: asana.Credentials(sdk.SecretString("token"))}),
            http_client,
        )
        operation = sdk.OperationRef("asana", operation_name)
        call = sdk.Call(CALL_ID, CONNECTION, operation, context=context, max_sends=max_sends)
        try:
            if operation_name == "createTask":
                task = asana.CreateTaskInput(name="Replace badge reader", project_id="1201000000000001")
                return (await sdk.run_mutation(client.create_task(), call, task)).branch
            comment = asana.AddCommentInput(task_id=TASK_ID, text="Approved.")
            return (await sdk.run_mutation(client.add_comment(), call, comment)).branch
        except sdk.RetryError:
            return "retry"


@pytest.mark.asyncio
async def test_unknown_outcome_resends_below_the_cap_then_is_uncertain() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    for attempt in range(1, CREATE_TASK_DEFINITION.max_sends):
        context.attempt = attempt
        assert await run("createTask", 500, "{}", context, requests) == "retry"
        assert context.marker is not None and context.marker.send_count == attempt
    context.attempt = CREATE_TASK_DEFINITION.max_sends
    assert await run("createTask", 500, "{}", context, requests) == "uncertain"
    context.attempt += 1
    assert await run("createTask", 500, "{}", context, requests) == "uncertain"
    assert len(requests) == 3


@pytest.mark.asyncio
async def test_send_cap_of_one_never_resends() -> None:
    requests: list[httpx.Request] = []
    assert await run("addComment", 409, "{}", AttemptContext(), requests, max_sends=1) == "uncertain"
    assert len(requests) == 1


@pytest.mark.asyncio
async def test_saved_identity_is_read_back_instead_of_resent() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext(attempt=2)
    assert await run("createTask", 201, CREATED, context, requests) == "created"
    assert context.marker is not None and context.marker.provider_id == TASK_ID
    context.attempt = 3
    assert await run("createTask", 200, CREATED, context, requests) == "created"
    assert [request.method for request in requests] == ["POST", "GET"]
    at_cap = AttemptContext(attempt=ADD_COMMENT_DEFINITION.max_sends + 1)
    assert await run("addComment", 201, CREATED, at_cap, requests) == "uncertain"
    assert len(requests) == 2


@pytest.mark.asyncio
async def test_definite_refusal_records_can_resend_and_is_never_resent() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    refusal = '{"errors":[{"message":"bad"}]}'
    assert await run("createTask", 400, refusal, context, requests) == "providerRejected"
    assert context.marker is not None and context.marker.state == "canResend"
    assert len(requests) == 1


@pytest.mark.asyncio
async def test_refusal_of_a_resend_is_uncertain() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert await run("addComment", 502, "{}", context, requests) == "retry"
    context.attempt = 2
    assert await run("addComment", 403, "{}", context, requests) == "uncertain"
    assert len(requests) == 2


@pytest.mark.asyncio
async def test_untrusted_checkpoint_retries_without_sending() -> None:
    requests: list[httpx.Request] = []
    foreign = Marker(call_id=sdk.CallID("another-call"), attempt=1, state="claimed")
    assert await run("createTask", 201, CREATED, AttemptContext(2, foreign), requests) == "retry"
    assert requests == []
