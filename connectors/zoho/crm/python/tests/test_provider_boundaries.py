# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider body, numeric, and timeout boundaries from Zoho's Go behavior."""

import asyncio
from collections.abc import AsyncIterator

import httpx
import pytest
from dex import CodecRegistry
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    OperationRef,
    SecretString,
    StaticCredentialProvider,
)

from dex_connectors.zoho_crm import (
    Client,
    Config,
    Credentials,
    FindRecordsInput,
    GetRecordInput,
    new_project_connection,
)
from dex_connectors.zoho_crm.operations import GetRecordOperation

REFERENCE = ConnectionRef("zoho", "zoho-crm")
CREDENTIALS = Credentials(
    auth_method_id="zoho-us-oauth",
    oauth_client_id="synthetic-client",
    oauth_client_secret=SecretString("synthetic-secret"),
    access_token=SecretString("synthetic-access"),
    refresh_token=SecretString("synthetic-refresh"),
    api_domain="https://www.zohoapis.com",
)


def _client(http_client: httpx.AsyncClient) -> Client:
    return Client(
        Config(),
        StaticCredentialProvider({REFERENCE: CREDENTIALS}),
        http_client,
        api_base_url="http://127.0.0.1:12345/crm/v8",
    )


def _call(operation: str) -> Call:
    return Call(
        CallID("11111111-1111-5111-8111-111111111111"),
        REFERENCE,
        OperationRef("zoho-crm", operation),
    )


class _BrokenStream(httpx.AsyncByteStream):
    async def __aiter__(self) -> AsyncIterator[bytes]:
        yield b'{"data":['
        raise httpx.ReadError("synthetic body interruption")


@pytest.mark.asyncio
async def test_mid_body_failure_after_200_retries() -> None:
    """A partially read successful response is never accepted as a record."""
    async with httpx.AsyncClient(
        transport=httpx.MockTransport(lambda _: httpx.Response(200, stream=_BrokenStream()))
    ) as http_client:
        result = await GetRecordOperation(_client(http_client)).invoke(
            _call("getRecord"), GetRecordInput("Deals", "123")
        )
    assert result.kind == "retry"
    assert result.failure is not None and result.failure.kind.value == "TRANSPORT"


@pytest.mark.parametrize(
    "body",
    [
        b'{"data":[{"id":"123","Amount":NaN}]}',
        b'{"data":[{"id":"123","Amount":Infinity}]}',
        b'{"data":[{"id":"123","Amount":1e999}]}',
        b'{"data":[{"id":"123","Amount":0.123456789123456789}]}',
    ],
)
@pytest.mark.asyncio
async def test_nonfinite_or_lossy_provider_number_is_invalid_response(body: bytes) -> None:
    """Reject a number that Go would reject or Python would round silently."""
    async with httpx.AsyncClient(
        transport=httpx.MockTransport(lambda _: httpx.Response(200, content=body))
    ) as http_client:
        result = await GetRecordOperation(_client(http_client)).invoke(
            _call("getRecord"), GetRecordInput("Deals", "123")
        )
    assert result.kind == "branch" and result.branch == "invalidResponse"


@pytest.mark.asyncio
async def test_large_integer_field_survives_sdk_encoding() -> None:
    """Keep a CRM integer above JavaScript's exact range without rounding it."""
    body = b'{"data":[{"id":"123","Amount":9007199254740993}]}'
    async with httpx.AsyncClient(
        transport=httpx.MockTransport(lambda _: httpx.Response(200, content=body))
    ) as http_client:
        result = await GetRecordOperation(_client(http_client)).invoke(
            _call("getRecord"), GetRecordInput("Deals", "123")
        )
    assert result.branch == "found"
    assert result.value is not None
    encoded = CodecRegistry().resolve(type(result.value)).encode(result.value)
    assert isinstance(encoded.data, str)
    assert "9007199254740993" in encoded.data


def test_boolean_response_limit_is_not_an_integer() -> None:
    """Configuration cannot treat bool as a byte count."""
    with pytest.raises(ValueError):
        Client(Config(max_response_bytes=True), StaticCredentialProvider({REFERENCE: CREDENTIALS}))


def test_invalid_project_connection_closes_default_http_client() -> None:
    """A failed synchronous connection build preserves its validation error."""
    with pytest.raises(ValueError, match="loaded project"):
        new_project_connection(None, "zoho-crm")  # type: ignore[arg-type]


@pytest.mark.asyncio
async def test_boolean_page_limit_is_not_an_integer() -> None:
    """A Python boolean cannot turn into Go's integer page size."""
    async with httpx.AsyncClient(
        transport=httpx.MockTransport(lambda _: pytest.fail("unexpected request"))
    ) as http_client:
        result = (
            await _client(http_client)
            .find_records()
            .invoke(_call("findRecords"), FindRecordsInput("Contacts", ("Email",), (), limit=True))
        )
    assert result.kind == "branch" and result.branch == "defect"


@pytest.mark.asyncio
async def test_expired_operation_deadline_retries_without_a_request() -> None:
    """The total Execute budget prevents a request after it has expired."""
    async with httpx.AsyncClient(
        transport=httpx.MockTransport(lambda _: pytest.fail("unexpected request"))
    ) as http_client:
        result = await _client(http_client).exchange(
            _call("getRecord"),
            "getRecord",
            "GET",
            "/Deals/123",
            deadline_at=asyncio.get_running_loop().time() - 1,
        )
    assert result.outcome == "retry"
    assert result.failure is not None and result.failure.kind.value == "TRANSPORT"
