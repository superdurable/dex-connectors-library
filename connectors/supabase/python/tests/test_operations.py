# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider parity scenarios for the Supabase operations."""

from uuid import uuid4

import httpx
import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    FailureKind,
    RetryError,
    SecretString,
    StaticCredentialProvider,
    run_mutation,
    run_query,
)

from dex_connectors.supabase import (
    DEFECT,
    INVALID_RESPONSE,
    PROVIDER_REJECTED,
    SELECT_ROWS_DEFINITION,
    SELECTED,
    UPSERT_PREFERENCE,
    UPSERT_ROWS_DEFINITION,
    UPSERTED,
    Client,
    Config,
    Credentials,
    SelectRowsInput,
    UpsertRowsInput,
)

CONNECTION = ConnectionRef("supabase", "supabase")
KEY = "supabase-test-key"
ROWS = [{"id": 1, "name": "Meridian"}]
UPSERT = UpsertRowsInput("members", "id", ({"name": "Meridian", "id": 1, "tags": {"z": 1, "a": 2}},))


def make_client(
    response: httpx.Response, requests: list[httpx.Request] | None = None, key: str = KEY
) -> Client:
    def respond(request: httpx.Request) -> httpx.Response:
        if requests is not None:
            requests.append(request)
        return response

    return Client(
        Config("http://localhost:8848/rest/v1/", max_response_bytes=300),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString(key))}),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )


def select_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, SELECT_ROWS_DEFINITION.operation)


def upsert_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, UPSERT_ROWS_DEFINITION.operation)


@pytest.mark.asyncio
async def test_select_rows_sends_go_query_and_keeps_rows() -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200, json=ROWS), requests).select_rows(),
        select_call(),
        SelectRowsInput("members", "id,name", 0, 20),
    )
    assert result.branch == SELECTED
    assert result.value.data == ROWS
    request = requests[0]
    assert request.method == "GET"
    assert str(request.url) == "http://localhost:8848/rest/v1/members?limit=100&offset=20&select=id%2Cname"
    assert request.headers["apikey"] == KEY
    assert "Prefer" not in request.headers
    assert "Content-Type" not in request.headers


@pytest.mark.asyncio
async def test_select_rows_defaults_to_all_columns() -> None:
    requests: list[httpx.Request] = []
    await run_query(
        make_client(httpx.Response(200, json=[]), requests).select_rows(),
        select_call(),
        SelectRowsInput("members", limit=5),
    )
    assert requests[0].url.query == b"limit=5&select=%2A"


@pytest.mark.asyncio
@pytest.mark.parametrize("status", [200, 201])
async def test_upsert_rows_merges_duplicates_with_sorted_json(status: int) -> None:
    requests: list[httpx.Request] = []
    result = await run_mutation(
        make_client(httpx.Response(status, json=ROWS), requests).upsert_rows(), upsert_call(), UPSERT
    )
    assert result.branch == UPSERTED
    assert result.value.data == ROWS
    request = requests[0]
    assert request.method == "POST"
    assert str(request.url) == "http://localhost:8848/rest/v1/members?on_conflict=id"
    assert request.headers["Prefer"] == UPSERT_PREFERENCE
    assert request.headers["Content-Type"] == "application/json"
    assert request.content == b'[{"id":1,"name":"Meridian","tags":{"a":2,"z":1}}]'


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "value",
    [
        SelectRowsInput("bad/table"),
        SelectRowsInput("members", limit=1001),
        SelectRowsInput("members", offset=-1),
        SelectRowsInput("members", columns="id,"),
        SelectRowsInput("members", columns="id;drop"),
    ],
)
async def test_invalid_select_is_defect_without_request(value: SelectRowsInput) -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200, json=ROWS), requests).select_rows(), select_call(), value
    )
    assert result.branch == DEFECT
    assert result.failure is not None and result.failure.kind == FailureKind.VALIDATION
    assert requests == []


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "value",
    [
        UpsertRowsInput("members", "id", ()),
        UpsertRowsInput("members", "", ({"id": 1},)),
        UpsertRowsInput("members", "id", tuple({"id": index} for index in range(101))),
        UpsertRowsInput("members", "id", ({},)),
        UpsertRowsInput("members", "id", ({"name": "x"},)),
        UpsertRowsInput("members", "id", ({"id": None},)),
        UpsertRowsInput("members", "id", ({"id": 1}, {"id": 1.0})),
        UpsertRowsInput("members", "id,org", ({"id": 1, "org": "a"}, {"id": 1, "org": "a"})),
    ],
)
async def test_invalid_upsert_is_defect_without_request(value: UpsertRowsInput) -> None:
    requests: list[httpx.Request] = []
    result = await run_mutation(
        make_client(httpx.Response(201, json=ROWS), requests).upsert_rows(), upsert_call(), value
    )
    assert result.branch == DEFECT
    assert requests == []


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "branch", "kind"),
    [
        (httpx.Response(400), PROVIDER_REJECTED, FailureKind.VALIDATION),
        (httpx.Response(401), PROVIDER_REJECTED, FailureKind.AUTHENTICATION),
        (httpx.Response(403), PROVIDER_REJECTED, FailureKind.AUTHORIZATION),
        (httpx.Response(409), PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION),
        (httpx.Response(201, json=ROWS), PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION),
        (httpx.Response(200, json={"rows": []}), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (httpx.Response(200, content=b"null"), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (httpx.Response(200, content=b"["), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (httpx.Response(200, content=b"x" * 400), INVALID_RESPONSE, FailureKind.RESPONSE_TOO_LARGE),
    ],
)
async def test_select_rows_branches(response: httpx.Response, branch: str, kind: FailureKind) -> None:
    result = await run_query(make_client(response).select_rows(), select_call(), SelectRowsInput("members"))
    assert result.branch == branch
    assert result.failure is not None and result.failure.kind == kind


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("status", "kind"),
    [(408, FailureKind.AVAILABILITY), (429, FailureKind.RATE_LIMIT), (503, FailureKind.AVAILABILITY)],
)
async def test_safe_failures_retry(status: int, kind: FailureKind) -> None:
    with pytest.raises(RetryError) as raised:
        await run_mutation(make_client(httpx.Response(status)).upsert_rows(), upsert_call(), UPSERT)
    assert raised.value.failure.kind == kind


@pytest.mark.asyncio
async def test_transport_failure_retries() -> None:
    def fail(request: httpx.Request) -> httpx.Response:
        raise httpx.ConnectError("refused", request=request)

    client = Client(
        Config("http://localhost:8848/rest/v1"),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString(KEY))}),
        httpx.AsyncClient(transport=httpx.MockTransport(fail)),
    )
    with pytest.raises(RetryError) as raised:
        await run_query(client.select_rows(), select_call(), SelectRowsInput("members"))
    assert raised.value.failure.kind == FailureKind.AVAILABILITY


@pytest.mark.asyncio
async def test_blank_key_is_defect_without_request() -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200, json=ROWS), requests, key="").select_rows(),
        select_call(),
        SelectRowsInput("members"),
    )
    assert result.branch == DEFECT
    assert result.failure is not None and result.failure.kind == FailureKind.AUTHENTICATION
    assert requests == []


def test_base_url_is_required() -> None:
    with pytest.raises(ValueError):
        Client(Config(""), StaticCredentialProvider({}))
