# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded resend of Todoist creates whose outcome is unknown, and retries of safe writes."""

from collections.abc import AsyncIterator, Callable

import dex_connectors.sdk as sdk
import httpx
import pytest

import dex_connectors.doist_todoist as todoist
from dex_connectors.doist_todoist._generated.definitions import CREATE_TASK_DEFINITION
from tests.attempt_context import AttemptContext

CONNECTION = sdk.ConnectionRef("todoist", "todoist")
CALL_ID = sdk.CallID("51afb24a-372a-5f2f-927c-a0493cb99a0b")
TASK = {
    "id": "task-123",
    "project_id": "project-456",
    "content": "Title",
    "description": "",
    "checked": False,
}
Responder = Callable[[httpx.Request], httpx.Response]


async def run(
    operation_name: str,
    respond: Responder,
    context: AttemptContext,
    requests: list[httpx.Request],
    *,
    max_sends: int = CREATE_TASK_DEFINITION.max_sends,
    token: str = "todoist-test-token",
) -> str:
    """Run one Execute attempt of a Todoist write and return its branch or `retry`."""

    def record(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return respond(request)

    client = todoist.Client(
        todoist.Config(base_url="http://127.0.0.1:12345/api/v1"),
        sdk.StaticCredentialProvider({CONNECTION: todoist.Credentials(sdk.SecretString(token))}),
        httpx.AsyncClient(transport=httpx.MockTransport(record)),
    )
    call = sdk.Call(
        CALL_ID, CONNECTION, sdk.OperationRef("todoist", operation_name), context=context, max_sends=max_sends
    )
    try:
        if operation_name == "createTask":
            result = await sdk.run_mutation(client.create_task(), call, todoist.CreateTaskInput("Title"))
        elif operation_name == "updateTaskContent":
            update = todoist.UpdateTaskInput("task-123", content="Title")
            result = await sdk.run_mutation(client.update_task_content(), call, update)
        else:
            result = await sdk.run_mutation(client.delete_task(), call, todoist.DeleteTaskInput("task-123"))
        return result.branch
    except sdk.RetryError:
        return "retry"
    finally:
        await client.aclose()


def status(code: int, body: object = None) -> Responder:
    """Answer every request with one status and optional JSON body."""
    return lambda request: httpx.Response(code, json=body) if body is not None else httpx.Response(code)


@pytest.mark.asyncio
async def test_unknown_outcome_resends_below_the_cap_then_is_uncertain() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    for attempt in range(1, CREATE_TASK_DEFINITION.max_sends):
        context.attempt = attempt
        assert await run("createTask", status(500), context, requests) == "retry"
        assert context.marker is not None and context.marker.send_count == attempt
    context.attempt = CREATE_TASK_DEFINITION.max_sends
    assert await run("createTask", status(409), context, requests) == "uncertain"
    context.attempt += 1
    assert await run("createTask", status(500), context, requests) == "uncertain"
    assert len(requests) == CREATE_TASK_DEFINITION.max_sends
    assert len({request.headers["X-Request-ID"] for request in requests}) == 1


@pytest.mark.asyncio
async def test_send_cap_of_one_never_resends() -> None:
    requests: list[httpx.Request] = []
    assert await run("createTask", status(502), AttemptContext(), requests, max_sends=1) == "uncertain"
    assert len(requests) == 1


@pytest.mark.asyncio
async def test_two_xx_without_task_id_is_uncertain_and_never_resent() -> None:
    requests: list[httpx.Request] = []
    assert await run("createTask", status(200, {"ok": True}), AttemptContext(), requests) == "uncertain"
    assert len(requests) == 1


class _BrokenBody(httpx.AsyncByteStream):
    async def __aiter__(self) -> AsyncIterator[bytes]:
        yield b'{"id":'
        raise httpx.ReadError("connection reset")


@pytest.mark.asyncio
async def test_two_xx_whose_body_breaks_is_uncertain_and_never_resent() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    broken = await run("createTask", lambda _: httpx.Response(200, stream=_BrokenBody()), context, requests)
    assert broken == "uncertain"
    assert context.marker is not None and context.marker.state == "claimed"
    assert len(requests) == 1


@pytest.mark.asyncio
@pytest.mark.parametrize("code", [429, 500, 503])
async def test_transient_read_back_after_acceptance_retries_the_read_back(code: int) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    reads = iter([status(code), status(200, TASK)])

    def respond(request: httpx.Request) -> httpx.Response:
        return status(200, {"id": "task-123"})(request) if request.method == "POST" else next(reads)(request)

    assert await run("createTask", respond, context, requests) == "retry"
    assert context.marker is not None and context.marker.provider_id == "task-123"
    context.attempt = 2
    assert await run("createTask", respond, context, requests) == "created"
    assert [request.method for request in requests] == ["POST", "GET", "GET"]


class _ProviderIDLossContext(AttemptContext):
    async def heartbeat(self, value: object = None) -> None:
        if "task-123" in repr(value):
            raise RuntimeError("heartbeat lost")
        await super().heartbeat(value)


@pytest.mark.asyncio
async def test_transient_read_back_with_an_unsaved_task_id_is_uncertain() -> None:
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        return status(200, {"id": "task-123"})(request) if request.method == "POST" else status(503)(request)

    assert await run("createTask", respond, _ProviderIDLossContext(), requests) == "uncertain"
    assert [request.method for request in requests] == ["POST", "GET"]


@pytest.mark.asyncio
async def test_rate_limit_after_an_unknown_outcome_keeps_counting_toward_the_cap() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    for attempt, code in enumerate([502, 429, 502], start=1):
        context.attempt = attempt
        assert await run("createTask", status(code), context, requests) == "retry"
    context.attempt = 4
    assert await run("createTask", status(502), context, requests) == "uncertain"
    assert len(requests) == 4


@pytest.mark.asyncio
async def test_credential_failure_after_a_rate_limited_resend_never_records_not_sent() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    for attempt, code in enumerate([502, 429], start=1):
        context.attempt = attempt
        assert await run("createTask", status(code), context, requests) == "retry"
    before = context.heartbeat_value
    context.attempt = 3
    assert await run("createTask", status(200, TASK), context, requests, token="bad\ntoken") == "uncertain"
    assert context.heartbeat_value is before
    assert len(requests) == 2


@pytest.mark.asyncio
async def test_saved_task_id_is_read_back_and_never_resent() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert await run("createTask", status(200, TASK), context, requests) == "created"
    assert context.marker is not None and context.marker.provider_id == "task-123"
    context.attempt = 2
    assert await run("createTask", status(200, TASK), context, requests) == "created"
    context.attempt = 3
    assert await run("createTask", status(500), context, requests) == "retry"
    assert await run("createTask", status(404), context, requests) == "uncertain"
    assert [request.method for request in requests] == ["POST", "GET", "GET", "GET"]


@pytest.mark.asyncio
async def test_definite_refusal_records_can_resend_and_is_never_resent() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert await run("createTask", status(400), context, requests) == "providerRejected"
    assert context.marker is not None and context.marker.state == "canResend"
    assert len(requests) == 1


@pytest.mark.asyncio
async def test_refusal_or_defect_after_a_possible_send_is_uncertain() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert await run("createTask", status(502), context, requests) == "retry"
    context.attempt = 2
    assert await run("createTask", status(403), context, requests) == "uncertain"
    context.attempt = 3
    assert await run("createTask", status(200, TASK), context, requests, token="bad\ntoken") == "uncertain"
    assert len(requests) == 2


@pytest.mark.asyncio
async def test_unreadable_checkpoint_retries_without_sending() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext(2)
    context.heartbeat_read_error = RuntimeError("heartbeat store unavailable")
    assert await run("createTask", status(200, TASK), context, requests) == "retry"
    assert requests == []


@pytest.mark.asyncio
@pytest.mark.parametrize("operation_name", ["updateTaskContent", "deleteTask"])
@pytest.mark.parametrize("code", [302, 405, 409, 410])
async def test_safe_writes_retry_any_status_but_a_definite_refusal(operation_name: str, code: int) -> None:
    requests: list[httpx.Request] = []
    assert await run(operation_name, status(code), AttemptContext(), requests) == "retry"
    assert await run(operation_name, status(422), AttemptContext(), requests) == "providerRejected"
