# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider parity scenarios for the Contentful Queries."""

from uuid import uuid4

import httpx
import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    FailureKind,
    QueryDefinition,
    RetryError,
    SecretString,
    StaticCredentialProvider,
    run_query,
)

from dex_connectors.contentful import (
    DEFECT,
    GET_ENTRY_DEFINITION,
    INVALID_RESPONSE,
    LIST_ENTRIES_DEFINITION,
    NOT_FOUND,
    PROVIDER_REJECTED,
    Client,
    Config,
    Credentials,
    GetEntryInput,
    ListEntriesInput,
)

CONNECTION = ConnectionRef("contentful", "contentful")
TOKEN = "test-token"


def make_client(
    response: httpx.Response, requests: list[httpx.Request] | None = None, token: str = TOKEN
) -> Client:
    def respond(request: httpx.Request) -> httpx.Response:
        if requests is not None:
            requests.append(request)
        return response

    return Client(
        Config(base_url="http://localhost:8848/base", max_response_bytes=300),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString(token))}),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )


def call(definition: QueryDefinition) -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, definition.operation)


@pytest.mark.asyncio
async def test_get_entry_success_and_request() -> None:
    requests: list[httpx.Request] = []
    body = {"sys": {"id": "x"}}
    result = await run_query(
        make_client(httpx.Response(200, json=body), requests).get_entry(),
        call(GET_ENTRY_DEFINITION),
        GetEntryInput("s1", "master", "e1"),
    )
    assert result.branch == "found"
    assert result.value.data == body
    assert requests[0].url.raw_path == b"/base/spaces/s1/environments/master/entries/e1"
    assert requests[0].headers["Authorization"] == "Bearer " + TOKEN


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "branch", "kind"),
    [
        (httpx.Response(404), NOT_FOUND, FailureKind.NOT_FOUND),
        (httpx.Response(401), PROVIDER_REJECTED, FailureKind.AUTHENTICATION),
        (httpx.Response(403), PROVIDER_REJECTED, FailureKind.AUTHORIZATION),
        (httpx.Response(422), PROVIDER_REJECTED, FailureKind.VALIDATION),
        (httpx.Response(200, json={"other": 1}), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (httpx.Response(200, content=b"{"), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (httpx.Response(200, content=b"x" * 400), INVALID_RESPONSE, FailureKind.RESPONSE_TOO_LARGE),
    ],
)
async def test_get_entry_terminal_branches(response: httpx.Response, branch: str, kind: FailureKind) -> None:
    result = await run_query(
        make_client(response).get_entry(), call(GET_ENTRY_DEFINITION), GetEntryInput("s1", "master", "e1")
    )
    assert result.branch == branch
    assert result.failure is not None and result.failure.kind == kind


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("status", "kind"),
    [(429, FailureKind.RATE_LIMIT), (409, FailureKind.AVAILABILITY), (502, FailureKind.AVAILABILITY)],
)
async def test_get_entry_retries(status: int, kind: FailureKind) -> None:
    with pytest.raises(RetryError) as raised:
        await run_query(
            make_client(httpx.Response(status)).get_entry(),
            call(GET_ENTRY_DEFINITION),
            GetEntryInput("s1", "master", "e1"),
        )
    assert raised.value.failure.kind == kind


@pytest.mark.asyncio
async def test_get_entry_invalid_input_and_token_are_defects_without_request() -> None:
    requests: list[httpx.Request] = []
    invalid = await run_query(
        make_client(httpx.Response(200), requests).get_entry(),
        call(GET_ENTRY_DEFINITION),
        GetEntryInput("s1", "master", "e/1"),
    )
    assert invalid.branch == DEFECT
    blank = await run_query(
        make_client(httpx.Response(200), requests, token="").get_entry(),
        call(GET_ENTRY_DEFINITION),
        GetEntryInput("s1", "master", "e1"),
    )
    assert blank.branch == DEFECT
    assert requests == []


@pytest.mark.asyncio
async def test_list_entries_success_and_request() -> None:
    requests: list[httpx.Request] = []
    body = {"items": [{"id": "x"}]}
    result = await run_query(
        make_client(httpx.Response(200, json=body), requests).list_entries(),
        call(LIST_ENTRIES_DEFINITION),
        ListEntriesInput("s1", "master", 10, 20),
    )
    assert result.branch == "listed"
    assert result.value.data == body
    assert requests[0].url.raw_path == b"/base/spaces/s1/environments/master/entries?limit=10&skip=20"
    assert requests[0].headers["Authorization"] == "Bearer " + TOKEN


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "branch", "kind"),
    [
        (httpx.Response(404), PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION),
        (httpx.Response(401), PROVIDER_REJECTED, FailureKind.AUTHENTICATION),
        (httpx.Response(403), PROVIDER_REJECTED, FailureKind.AUTHORIZATION),
        (httpx.Response(422), PROVIDER_REJECTED, FailureKind.VALIDATION),
        (httpx.Response(200, json={"other": 1}), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (httpx.Response(200, content=b"{"), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (httpx.Response(200, content=b"x" * 400), INVALID_RESPONSE, FailureKind.RESPONSE_TOO_LARGE),
    ],
)
async def test_list_entries_terminal_branches(
    response: httpx.Response, branch: str, kind: FailureKind
) -> None:
    result = await run_query(
        make_client(response).list_entries(),
        call(LIST_ENTRIES_DEFINITION),
        ListEntriesInput("s1", "master", 10, 20),
    )
    assert result.branch == branch
    assert result.failure is not None and result.failure.kind == kind


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("status", "kind"),
    [(429, FailureKind.RATE_LIMIT), (409, FailureKind.AVAILABILITY), (502, FailureKind.AVAILABILITY)],
)
async def test_list_entries_retries(status: int, kind: FailureKind) -> None:
    with pytest.raises(RetryError) as raised:
        await run_query(
            make_client(httpx.Response(status)).list_entries(),
            call(LIST_ENTRIES_DEFINITION),
            ListEntriesInput("s1", "master", 10, 20),
        )
    assert raised.value.failure.kind == kind


@pytest.mark.asyncio
async def test_list_entries_invalid_input_and_token_are_defects_without_request() -> None:
    requests: list[httpx.Request] = []
    invalid = await run_query(
        make_client(httpx.Response(200), requests).list_entries(),
        call(LIST_ENTRIES_DEFINITION),
        ListEntriesInput("s1", "master", 1001),
    )
    assert invalid.branch == DEFECT
    blank = await run_query(
        make_client(httpx.Response(200), requests, token="").list_entries(),
        call(LIST_ENTRIES_DEFINITION),
        ListEntriesInput("s1", "master", 10, 20),
    )
    assert blank.branch == DEFECT
    assert requests == []
