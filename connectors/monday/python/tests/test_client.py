# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider validation and failure classification beyond recorded scenarios."""

import httpx
import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    OperationRef,
    SecretString,
    StaticCredentialProvider,
)
from dex_connectors.sdk.projectconfig.provider import RefreshFailedError

from dex_connectors.monday import Client, ColumnValue, Config, Credentials, GetItemInput
from dex_connectors.monday.item import decode_item, encode_column_values

CONNECTION = ConnectionRef("monday", "monday-test")


def _client(http_client: httpx.AsyncClient, token: str = "inert-token") -> Client:
    return Client(
        Config(),
        StaticCredentialProvider({CONNECTION: Credentials("personal-api-token", SecretString(token))}),
        http_client,
        endpoint="http://127.0.0.1:45101/v2",
    )


@pytest.mark.asyncio
async def test_connection_refusal_is_a_safe_retry() -> None:
    """A local dial refusal must stay retryable, matching Go's no-send evidence."""

    def refuse(_: httpx.Request) -> httpx.Response:
        raise httpx.ConnectError("refused")

    async with httpx.AsyncClient(transport=httpx.MockTransport(refuse)) as http_client:
        call = Call(CallID("monday-refusal"), CONNECTION, OperationRef("monday", "getItem"))
        attempt = await _client(http_client).get_item().invoke(call, GetItemInput(item_id="1234567890"))
    assert attempt.kind == "retry"
    assert attempt.failure is not None and attempt.failure.kind == "TRANSPORT"


@pytest.mark.asyncio
async def test_invalid_credential_is_terminal_without_a_request() -> None:
    """Reject a header-unsafe personal token before provider dispatch."""
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200, json={"data": {"items": []}})

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        call = Call(CallID("monday-credential"), CONNECTION, OperationRef("monday", "getItem"))
        attempt = (
            await _client(http_client, "two words")
            .get_item()
            .invoke(call, GetItemInput(item_id="1234567890"))
        )
    assert attempt.kind == "branch" and attempt.branch == "defect"
    assert attempt.failure is not None and attempt.failure.kind == "AUTHENTICATION"
    assert not requests


def test_column_values_have_provider_exact_json_shape() -> None:
    """Keep typed fields and Go's canonical nested JSON string."""
    encoded = encode_column_values(
        {
            "status": ColumnValue(type="status", status_label="Done"),
            "text": ColumnValue(type="text", text="A&B"),
        }
    )
    assert encoded == '{"status":{"label":"Done"},"text":"A\\u0026B"}'


def test_invalid_calendar_day_fails_before_dispatch() -> None:
    """Reject impossible dates instead of silently sending them to monday.com."""
    with pytest.raises(ValueError, match="real calendar date"):
        encode_column_values({"date4": ColumnValue(type="date", date="2026-02-30")})


def test_wrong_typed_provider_item_fails_as_invalid_response() -> None:
    """Do not coerce a numeric item name into a persisted string."""
    with pytest.raises(ValueError, match="name is not a string"):
        decode_item({"id": "1234567890", "name": 42})


@pytest.mark.asyncio
async def test_temporary_oauth_refresh_failure_retries() -> None:
    """A durable project refresh failure needs Retry, as Go's refresh driver does."""

    class UnavailableCredentials:
        async def resolve(self, call: Call) -> Credentials:
            raise RefreshFailedError("inert transient failure")

    async with httpx.AsyncClient(transport=httpx.MockTransport(lambda _: httpx.Response(599))) as http_client:
        client = Client(Config(), UnavailableCredentials(), http_client, endpoint="http://127.0.0.1:45101/v2")
        call = Call(CallID("monday-refresh"), CONNECTION, OperationRef("monday", "getItem"))
        attempt = await client.get_item().invoke(call, GetItemInput(item_id="1234567890"))
    assert attempt.kind == "retry"
    assert attempt.failure is not None and attempt.failure.kind == "AVAILABILITY"
