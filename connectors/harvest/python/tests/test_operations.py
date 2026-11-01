# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider parity scenarios for the Harvest page Queries."""

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

from dex_connectors.harvest import (
    DEFECT,
    INVALID_RESPONSE,
    LIST_PROJECTS_DEFINITION,
    LIST_TIME_ENTRIES_DEFINITION,
    LISTED,
    PROVIDER_REJECTED,
    Client,
    Config,
    Credentials,
    ListProjectsInput,
    ListTimeEntriesInput,
)

CONNECTION = ConnectionRef("harvest", "harvest")
TOKEN = "1234.pt.test"
NEXT = "https://api.harvestapp.com/v2/projects?cursor=abc_DEF-1&per_page=100"


def make_client(
    response: httpx.Response, requests: list[httpx.Request] | None = None, token: str = TOKEN
) -> Client:
    def respond(request: httpx.Request) -> httpx.Response:
        if requests is not None:
            requests.append(request)
        return response

    return Client(
        Config(account_id="987654", base_url="http://localhost:8848/v2", max_response_bytes=400),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString(token))}),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )


def projects_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, LIST_PROJECTS_DEFINITION.operation)


def entries_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, LIST_TIME_ENTRIES_DEFINITION.operation)


@pytest.mark.asyncio
async def test_projects_page_sends_account_and_reads_next_cursor() -> None:
    requests: list[httpx.Request] = []
    body = {
        "projects": [{"id": 3001, "name": "Meridian Website", "is_active": True}],
        "links": {"next": NEXT},
    }
    client = make_client(httpx.Response(200, json=body), requests)
    result = await run_query(
        client.list_projects(), projects_call(), ListProjectsInput(cursor="prev_1", per_page=50)
    )
    assert result.branch == LISTED
    assert result.value.projects[0].is_active is True
    assert result.value.next_cursor == "abc_DEF-1"
    request = requests[0]
    assert request.url.raw_path == b"/v2/projects?cursor=prev_1&per_page=50"
    assert request.headers["Authorization"] == "Bearer " + TOKEN
    assert request.headers["Harvest-Account-Id"] == "987654"


@pytest.mark.asyncio
async def test_time_entries_page_defaults_size_and_decodes_hours() -> None:
    requests: list[httpx.Request] = []
    body = {
        "time_entries": [{"id": 4001, "project": {"id": 3001}, "hours": 2.5, "spent_date": "2026-10-28"}],
        "links": {"next": None},
    }
    client = make_client(httpx.Response(200, json=body), requests)
    result = await run_query(client.list_time_entries(), entries_call(), ListTimeEntriesInput())
    assert result.branch == LISTED
    entry = result.value.time_entries[0]
    assert (entry.project.id, entry.hours, entry.spent_date) == (3001, 2.5, "2026-10-28")
    assert result.value.next_cursor == ""
    assert requests[0].url.raw_path == b"/v2/time_entries?per_page=100"


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "body",
    [
        {"links": {}},
        {"projects": [], "links": {"next": "https://evil.example/v2/projects?cursor=x"}},
        {"projects": [], "links": {"next": "https://api.harvestapp.com/v2/time_entries?cursor=x"}},
        {"projects": [], "links": {"next": "https://api.harvestapp.com/v2/projects?cursor=bad cursor"}},
        {"projects": [{"id": "3001"}]},
    ],
)
async def test_unusable_pages_are_invalid_responses(body: object) -> None:
    result = await run_query(
        make_client(httpx.Response(200, json=body)).list_projects(), projects_call(), ListProjectsInput()
    )
    assert result.branch == INVALID_RESPONSE


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "page",
    [ListProjectsInput(per_page=2001), ListProjectsInput(per_page=-1), ListProjectsInput(cursor="a/b")],
)
async def test_invalid_page_input_is_defect_without_request(page: ListProjectsInput) -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200), requests).list_projects(), projects_call(), page
    )
    assert result.branch == DEFECT
    assert requests == []


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "kind"),
    [
        (httpx.Response(401), FailureKind.AUTHENTICATION),
        (httpx.Response(403), FailureKind.AUTHORIZATION),
        (httpx.Response(404), FailureKind.PROVIDER_REJECTION),
    ],
)
async def test_rejections(response: httpx.Response, kind: FailureKind) -> None:
    result = await run_query(
        make_client(response).list_time_entries(), entries_call(), ListTimeEntriesInput()
    )
    assert result.branch == PROVIDER_REJECTED
    assert result.failure is not None and result.failure.kind == kind


@pytest.mark.asyncio
@pytest.mark.parametrize("status", [408, 409, 429, 500])
async def test_safe_failures_retry(status: int) -> None:
    with pytest.raises(RetryError):
        await run_query(
            make_client(httpx.Response(status)).list_projects(), projects_call(), ListProjectsInput()
        )


def test_account_id_must_be_decimal() -> None:
    for account_id in ("", "0123", "acct"):
        with pytest.raises(ValueError):
            Client(Config(account_id=account_id), StaticCredentialProvider({}))
