# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider parity scenarios for the Smartsheet sheet Queries."""

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
    run_query,
)

from dex_connectors.smartsheet import (
    DEFECT,
    FOUND,
    GET_SHEET_DEFINITION,
    INVALID_RESPONSE,
    LIST_SHEETS_DEFINITION,
    LISTED,
    NOT_FOUND,
    PROVIDER_REJECTED,
    Client,
    Config,
    Credentials,
    GetSheetInput,
    ListSheetsInput,
)

CONNECTION = ConnectionRef("smartsheet", "smartsheet")
TOKEN = "ss_test_token"
CELL = {"columnId": 2001, "value": "Plan", "displayValue": "Plan"}
SHEET = {
    "id": 1001,
    "name": "Meridian Planning",
    "columns": [{"id": 2001, "title": "Task"}],
    "rows": [{"id": 3001, "cells": [CELL]}],
}


def make_client(response: httpx.Response, requests: list[httpx.Request] | None = None) -> Client:
    def respond(request: httpx.Request) -> httpx.Response:
        if requests is not None:
            requests.append(request)
        return response

    return Client(
        Config(base_url="http://localhost:8848/2.0", max_response_bytes=400),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString(TOKEN))}),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )


def list_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, LIST_SHEETS_DEFINITION.operation)


def get_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, GET_SHEET_DEFINITION.operation)


@pytest.mark.asyncio
async def test_list_defaults_page_and_size() -> None:
    requests: list[httpx.Request] = []
    body = {"data": [{"id": 1001, "name": "Meridian Planning"}], "pageNumber": 1, "totalCount": 1}
    result = await run_query(
        make_client(httpx.Response(200, json=body), requests).list_sheets(), list_call(), ListSheetsInput()
    )
    assert result.branch == LISTED
    assert result.value.sheets[0].id == 1001
    assert requests[0].url.raw_path == b"/2.0/sheets?page=1&pageSize=100"
    assert requests[0].headers["Authorization"] == "Bearer " + TOKEN


@pytest.mark.asyncio
async def test_get_keeps_cells_as_json() -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200, json=SHEET), requests).get_sheet(), get_call(), GetSheetInput(1001)
    )
    assert result.branch == FOUND
    assert result.value.rows[0].cells == (CELL,)
    assert requests[0].url.raw_path == b"/2.0/sheets/1001"


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "body",
    [
        {"data": [], "pageNumber": 0, "totalCount": 0},
        {"pageNumber": 1, "totalCount": 0},
        {"data": [], "pageNumber": 1, "totalCount": -1},
    ],
)
async def test_incomplete_pages_are_invalid(body: object) -> None:
    result = await run_query(
        make_client(httpx.Response(200, json=body)).list_sheets(), list_call(), ListSheetsInput()
    )
    assert result.branch == INVALID_RESPONSE


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "branch"),
    [
        (httpx.Response(404), NOT_FOUND),
        (httpx.Response(403), PROVIDER_REJECTED),
        (httpx.Response(200, json={**SHEET, "rows": None}), INVALID_RESPONSE),
        (httpx.Response(200, json={**SHEET, "name": ""}), INVALID_RESPONSE),
        (httpx.Response(200, content=b"x" * 500), INVALID_RESPONSE),
    ],
)
async def test_get_branches(response: httpx.Response, branch: str) -> None:
    result = await run_query(make_client(response).get_sheet(), get_call(), GetSheetInput(1001))
    assert result.branch == branch


@pytest.mark.asyncio
async def test_list_404_is_provider_rejection_and_safe_failures_retry() -> None:
    rejected = await run_query(make_client(httpx.Response(404)).list_sheets(), list_call(), ListSheetsInput())
    assert rejected.branch == PROVIDER_REJECTED
    assert rejected.failure is not None and rejected.failure.kind == FailureKind.PROVIDER_REJECTION
    for status in (408, 409, 429, 500):
        with pytest.raises(RetryError):
            await run_query(make_client(httpx.Response(status)).list_sheets(), list_call(), ListSheetsInput())


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("operation", "value"),
    [
        ("list", ListSheetsInput(page=-1)),
        ("list", ListSheetsInput(page_size=1001)),
        ("get", GetSheetInput(0)),
    ],
)
async def test_invalid_input_is_defect_without_request(operation: str, value: object) -> None:
    requests: list[httpx.Request] = []
    client = make_client(httpx.Response(200), requests)
    if operation == "list":
        result = await run_query(client.list_sheets(), list_call(), value)  # type: ignore[arg-type]
    else:
        result = await run_query(client.get_sheet(), get_call(), value)  # type: ignore[arg-type]
    assert result.branch == DEFECT
    assert requests == []
