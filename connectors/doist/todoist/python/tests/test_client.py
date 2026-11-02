# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Cover credential classification and an actual refused local connection."""

import socket
from datetime import UTC, datetime, timedelta
from types import SimpleNamespace

import dex_connectors.sdk as sdk
import httpx
import pytest

from dex_connectors.doist_todoist import Client, Config, CreateTaskInput, Credentials, GetTaskInput

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
