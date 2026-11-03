# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Cover credential classification and an actual refused local connection."""

import asyncio
import socket
from datetime import UTC, datetime, timedelta
from types import SimpleNamespace
from typing import Any

import dex_connectors.sdk as sdk
import httpx
import pytest

from dex_connectors.doist_todoist import (
    Client,
    Config,
    CreateTaskInput,
    Credentials,
    GetTaskInput,
    UpdateTaskInput,
)

CONNECTION = sdk.ConnectionRef("todoist", "todoist")


class _UnavailableCredentials:
    async def resolve(self, call: sdk.Call) -> Credentials:
        raise OSError("inert credential store outage")


class _MissingCredentials:
    async def resolve(self, call: sdk.Call) -> Credentials:
        raise LookupError("inert missing connection")


def _call(operation: str) -> sdk.Call:
    return sdk.Call(
        sdk.CallID("call-1"),
        CONNECTION,
        sdk.OperationRef("todoist", operation),
    )


@pytest.mark.asyncio
async def test_credential_store_outage_retries_before_dispatch() -> None:
    """A recoverable credential read failure must not become a terminal defect."""
    client = Client(Config(), _UnavailableCredentials())
    try:
        answer = await client.get_task().invoke(_call("getTask"), GetTaskInput("task-123"))
        assert answer.kind == "retry"
        assert answer.failure is not None
        assert answer.failure.kind == sdk.FailureKind.AVAILABILITY
    finally:
        await client.aclose()


@pytest.mark.asyncio
async def test_missing_credential_is_terminal() -> None:
    """A missing connection cannot be repaired by retrying the same call."""
    client = Client(Config(), _MissingCredentials())
    try:
        answer = await client.get_task().invoke(_call("getTask"), GetTaskInput("task-123"))
        assert answer.kind == "branch" and answer.branch == "defect"
        assert answer.failure is not None
        assert answer.failure.kind == sdk.FailureKind.AUTHENTICATION
    finally:
        await client.aclose()


@pytest.mark.asyncio
async def test_refused_local_connection_retries_creation() -> None:
    """A traced connect refusal proves the create request never reached Todoist."""
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        free_port = listener.getsockname()[1]
    client = Client(
        Config(base_url=f"http://127.0.0.1:{free_port}/api/v1"),
        sdk.StaticCredentialProvider({CONNECTION: Credentials(sdk.SecretString("inert-token"))}),
    )
    try:
        operation = client.create_task()
        input = CreateTaskInput("Disposable task")
        call = _call("createTask")
        call = sdk.Call(call.id, call.connection, call.operation, operation.idempotency_key(call.id, input))
        answer = await operation.invoke(call, input)
        assert answer.kind == "retry"
        assert answer.failure is not None
        assert answer.failure.kind == sdk.FailureKind.TRANSPORT
    finally:
        await client.aclose()


@pytest.mark.asyncio
async def test_expired_attempt_does_not_send_creation() -> None:
    """A create attempt with no request budget must retry without a provider POST."""

    def reject_request(request: httpx.Request) -> httpx.Response:
        raise AssertionError(f"expired attempt sent {request.method}")

    async with httpx.AsyncClient(transport=httpx.MockTransport(reject_request)) as provider_http_client:
        client = Client(
            Config(base_url="http://localhost/api/v1"),
            sdk.StaticCredentialProvider({CONNECTION: Credentials(sdk.SecretString("inert-token"))}),
            provider_http_client,
        )
        operation = client.create_task()
        input = CreateTaskInput("Disposable task")
        base_call = _call("createTask")
        call = sdk.Call(
            base_call.id,
            base_call.connection,
            base_call.operation,
            operation.idempotency_key(base_call.id, input),
            context=SimpleNamespace(deadline=datetime.now(UTC) - timedelta(seconds=1)),
        )
        answer = await operation.invoke(call, input)
        assert answer.kind == "retry"
        assert answer.failure is not None
        assert answer.failure.kind == sdk.FailureKind.AVAILABILITY


@pytest.mark.asyncio
@pytest.mark.parametrize("headers_started", [False, True])
async def test_budget_timeout_distinguishes_dispatch(headers_started: bool) -> None:
    """Only a cancelled connection attempt is safe to retry as undispatched."""

    async def connect_until_cancelled(request: httpx.Request) -> httpx.Response:
        await request.extensions["trace"]("connection.connect_tcp.started", {})
        if headers_started:
            await request.extensions["trace"]("http11.send_request_headers.started", {})
        await asyncio.Future()
        raise AssertionError("connection unexpectedly completed")

    transport = httpx.MockTransport(connect_until_cancelled)
    async with httpx.AsyncClient(transport=transport) as provider_http_client:
        client = Client(
            Config(base_url="http://localhost/api/v1"),
            sdk.StaticCredentialProvider({CONNECTION: Credentials(sdk.SecretString("inert-token"))}),
            provider_http_client,
        )
        operation = client.create_task()
        input = CreateTaskInput("Disposable task")
        base_call = _call("createTask")
        call = sdk.Call(
            base_call.id,
            base_call.connection,
            base_call.operation,
            operation.idempotency_key(base_call.id, input),
            context=SimpleNamespace(deadline=datetime.now(UTC) + timedelta(seconds=0.25)),
        )
        answer = await operation.invoke(call, input)
        assert answer.kind == ("uncertain" if headers_started else "retry")
        assert answer.failure is not None
        assert answer.failure.kind == sdk.FailureKind.TRANSPORT


@pytest.mark.asyncio
async def test_update_read_back_request_construction_defect() -> None:
    """A local GET construction failure after an accepted write is terminal."""

    def answer_update(request: httpx.Request) -> httpx.Response:
        assert request.method == "POST"
        return httpx.Response(
            200,
            json={
                "id": "task-123",
                "project_id": "project-123",
                "content": "old",
                "description": "",
                "checked": False,
            },
        )

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer_update)) as provider_http_client:
        original_build_request = provider_http_client.build_request

        def fail_read_back(method: str, url: str, **kwargs: Any) -> httpx.Request:
            if method == "GET":
                raise ValueError("inert invalid request")
            return original_build_request(method, url, **kwargs)

        provider_http_client.build_request = fail_read_back  # type: ignore[assignment]
        client = Client(
            Config(base_url="http://localhost/api/v1"),
            sdk.StaticCredentialProvider({CONNECTION: Credentials(sdk.SecretString("inert-token"))}),
            provider_http_client,
        )
        answer = await client.update_task_content().invoke(
            _call("updateTaskContent"), UpdateTaskInput("task-123", content="new")
        )
        assert answer.kind == "branch" and answer.branch == "defect"
        assert answer.failure is not None
        assert answer.failure.kind == sdk.FailureKind.LOCAL_DEFECT


@pytest.mark.asyncio
@pytest.mark.parametrize("content", ["\x1c", "\x1d", "\x1e", "\x1f"])
async def test_go_trim_space_accepts_ascii_information_separators(content: str) -> None:
    """Go treats these control characters as content, not whitespace."""

    def answer_request(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            200,
            json={
                "id": "task-123",
                "project_id": "project-123",
                "content": content,
                "description": "",
                "checked": False,
            },
        )

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer_request)) as provider_http_client:
        client = Client(
            Config(base_url="http://localhost/api/v1"),
            sdk.StaticCredentialProvider({CONNECTION: Credentials(sdk.SecretString("inert-token"))}),
            provider_http_client,
        )
        create_operation = client.create_task()
        create_input = CreateTaskInput(content)
        base_call = _call("createTask")
        create_call = sdk.Call(
            base_call.id,
            base_call.connection,
            base_call.operation,
            create_operation.idempotency_key(base_call.id, create_input),
        )
        created = await create_operation.invoke(create_call, create_input)
        updated = await client.update_task_content().invoke(
            _call("updateTaskContent"), UpdateTaskInput("task-123", content=content)
        )
        assert created.kind == "branch" and created.branch == "created"
        assert updated.kind == "branch" and updated.branch == "updated"
