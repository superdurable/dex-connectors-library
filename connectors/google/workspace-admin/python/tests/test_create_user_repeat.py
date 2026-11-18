# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""createUser repeats converge on the account an earlier attempt may have created."""

import json
from datetime import UTC, datetime, timedelta
from types import SimpleNamespace
from typing import Any, cast

import httpx
import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    IdempotencyKey,
    OperationRef,
    SecretString,
    StaticCredentialProvider,
)

from dex_connectors.google_workspace_admin import Client, Config, CreateUserInput, Credentials

CONNECTION = ConnectionRef("google", "workspace")
INPUT = CreateUserInput(primary_email="ada@example.com", given_name="Ada", family_name="Lovelace")
ACCOUNT = {
    "id": "1001",
    "primaryEmail": "ada@example.com",
    "externalIds": [{"type": "custom", "customType": "dexIdempotencyKey", "value": "hire-42"}],
}


def call_at(attempt: int, first_attempt_at: datetime) -> Call:
    """Build a createUser Call for one attempt of a Step."""
    context = SimpleNamespace(attempt=attempt, first_attempt_at=first_attempt_at)
    return Call(
        CallID("6f1c9c1e-5d8c-4f1b-9b8e-0c8c8f7f2a10"),
        CONNECTION,
        OperationRef("google-workspace-admin", "createUser"),
        IdempotencyKey("hire-42"),
        cast(Any, context),
    )


def client_answering(insert_status: int, requests: list[httpx.Request]) -> Client:
    """Answer the insert with insert_status and the read with this Step's account."""

    def handle(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if request.method == "POST":
            body = {"error": {"errors": [{"reason": "forbidden"}]}}
            return httpx.Response(insert_status, content=json.dumps(body).encode())
        return httpx.Response(200, content=json.dumps(ACCOUNT).encode())

    return Client(Config(), static_credentials(), httpx.AsyncClient(transport=httpx.MockTransport(handle)))


def static_credentials() -> StaticCredentialProvider[Credentials]:
    """Serve one bearer token for the test connection."""
    return StaticCredentialProvider({CONNECTION: Credentials(access_token=SecretString("token"))})


@pytest.mark.asyncio
async def test_first_attempt_refusal_is_definite() -> None:
    """A refusal with no earlier attempt selects providerRejected after one request."""
    requests: list[httpx.Request] = []
    result = await client_answering(403, requests).create_user().invoke(call_at(1, datetime.now(UTC)), INPUT)
    assert result.kind == "branch" and result.branch == "providerRejected"
    assert len(requests) == 1


@pytest.mark.asyncio
async def test_later_attempt_refusal_reads_back_this_steps_account() -> None:
    """After an earlier attempt, a refusal reads the address and finds this Step's account."""
    requests: list[httpx.Request] = []
    result = await client_answering(403, requests).create_user().invoke(call_at(2, datetime.now(UTC)), INPUT)
    assert result.kind == "branch" and result.branch == "created"
    assert result.value is not None and result.value.was_already_created
    assert [request.method for request in requests] == ["POST", "GET"]


@pytest.mark.asyncio
async def test_later_attempt_keeps_waiting_for_an_unreadable_conflicting_address() -> None:
    """A 409 whose account is not readable yet retries after an earlier attempt, even late."""

    def handle(request: httpx.Request) -> httpx.Response:
        return httpx.Response(409 if request.method == "POST" else 404, content=b"{}")

    client = Client(Config(), static_credentials(), httpx.AsyncClient(transport=httpx.MockTransport(handle)))
    late = datetime.now(UTC) - timedelta(minutes=2)
    result = await client.create_user().invoke(call_at(2, late), INPUT)
    assert result.kind == "retry"
