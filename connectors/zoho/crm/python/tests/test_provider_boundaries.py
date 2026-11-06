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
    COQLValue,
    Credentials,
    FindRecordsInput,
    GetRecordInput,
    ListModifiedRecordsInput,
    UpsertRecordInput,
    new_project_connection,
)
from dex_connectors.zoho_crm.client import Exchange
from dex_connectors.zoho_crm.operations import (
    GetRecordOperation,
    UpsertRecordOperation,
    _condition_value,
    _write_result,
    build_list_modified_records_query,
)

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


@pytest.mark.parametrize(
    "value", (COQLValue("dateTime", "2024-01-05T10:00:00Z"), COQLValue("date", "2024-1-5"))
)
def test_coql_rejects_non_go_date_spellings(value: COQLValue) -> None:
    with pytest.raises(ValueError):
        _condition_value(value)


def test_coql_rejects_c1_control_characters() -> None:
    with pytest.raises(ValueError):
        _condition_value(COQLValue("text", "bad\u0085value"))


def test_write_result_preserves_offset_and_fraction() -> None:
    response = (
        b'{"data":[{"code":"SUCCESS","details":{"id":"123",'
        b'"Created_Time":"2026-01-28T13:00:05.123456789+05:30",'
        b'"Modified_Time":"2026-01-28T13:00:05+05:30"}}]}'
    )
    result = _write_result(Exchange("succeeded", body=response))
    assert result[3:] == ("2026-01-28T13:00:05.123456789+05:30", "2026-01-28T13:00:05+05:30")


@pytest.mark.asyncio
async def test_whitespace_duplicate_check_sends_nothing() -> None:
    async with httpx.AsyncClient(
        transport=httpx.MockTransport(lambda _: pytest.fail("unexpected request"))
    ) as http_client:
        result = await UpsertRecordOperation(_client(http_client)).invoke(
            _call("upsertRecord"),
            UpsertRecordInput("Contacts", ("Email",), {"Email": "   "}),
        )
    assert result.branch == "defect"


def test_modified_time_counts_within_fifty_selected_fields() -> None:
    fields = ("Modified_Time", *(f"Field_{number}" for number in range(49)))
    query, _ = build_list_modified_records_query(
        ListModifiedRecordsInput("Deals", fields, "2026-01-28T13:00:00Z")
    )
    assert "Field_48" in query
