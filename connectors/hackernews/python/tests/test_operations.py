# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider parity scenarios for the Hacker News Queries."""

from uuid import uuid4

import httpx
import pytest
from dex_connectors.sdk import Call, CallID, ConnectionRef, FailureKind, RetryError, run_query
from dex_connectors.sdk.projectconfig import (
    Configuration,
    ConfigurationStore,
    ConnectionConfiguration,
    ConnectionStore,
    InMemoryObjectStore,
    LoadedProject,
    Scope,
    SnapshotRef,
)

from dex_connectors.hackernews import (
    DEFECT,
    FOUND,
    GET_ITEM_DEFINITION,
    INVALID_RESPONSE,
    LIST_STORY_IDS_DEFINITION,
    LISTED,
    NOT_FOUND,
    PROVIDER_REJECTED,
    Client,
    Config,
    GetItemInput,
    ListStoryIDsInput,
    new_project_connection,
)

CONNECTION = ConnectionRef("hacker-news", "hackernews")
STORY = {
    "id": 42001,
    "type": "story",
    "by": "dev",
    "time": 1,
    "title": "A release",
    "kids": [7, 8],
    "score": 5,
}


def make_client(response: httpx.Response, requests: list[httpx.Request] | None = None) -> Client:
    def respond(request: httpx.Request) -> httpx.Response:
        if requests is not None:
            requests.append(request)
        return response

    return Client(
        Config(base_url="http://localhost:8848/v0", max_response_bytes=300),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )


def list_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, LIST_STORY_IDS_DEFINITION.operation)


def get_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, GET_ITEM_DEFINITION.operation)


@pytest.mark.asyncio
async def test_feed_snapshot_is_bounded() -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200, json=[3, 2, 1]), requests).list_story_ids(),
        list_call(),
        ListStoryIDsInput("best", 2),
    )
    assert result.branch == LISTED
    assert (result.value.ids, result.value.has_more) == ((3, 2), True)
    assert requests[0].url.raw_path == b"/v0/beststories.json"
    default = await run_query(
        make_client(httpx.Response(200, json=[1]), requests).list_story_ids(),
        list_call(),
        ListStoryIDsInput(),
    )
    assert default.value.has_more is False
    assert requests[1].url.raw_path == b"/v0/topstories.json"


@pytest.mark.asyncio
async def test_item_gets_discussion_url() -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200, json=STORY), requests).get_item(), get_call(), GetItemInput(42001)
    )
    assert result.branch == FOUND
    assert result.value.discussion_url == "https://news.ycombinator.com/item?id=42001"
    assert result.value.kids == (7, 8)
    assert requests[0].url.raw_path == b"/v0/item/42001.json"


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "branch"),
    [
        (httpx.Response(200, content=b"null"), NOT_FOUND),
        (httpx.Response(200, json={**STORY, "dead": True}), NOT_FOUND),
        (httpx.Response(404), NOT_FOUND),
        (httpx.Response(200, json={**STORY, "id": 1}), INVALID_RESPONSE),
        (httpx.Response(200, json={**STORY, "type": "unknown"}), INVALID_RESPONSE),
        (httpx.Response(200, json={**STORY, "kids": [0]}), INVALID_RESPONSE),
        (httpx.Response(200, content=b"x" * 400), INVALID_RESPONSE),
        (httpx.Response(403), PROVIDER_REJECTED),
    ],
)
async def test_item_branches(response: httpx.Response, branch: str) -> None:
    result = await run_query(make_client(response).get_item(), get_call(), GetItemInput(42001))
    assert result.branch == branch


@pytest.mark.asyncio
async def test_feed_errors() -> None:
    for body, branch in (([0], INVALID_RESPONSE), ({"ids": []}, INVALID_RESPONSE)):
        result = await run_query(
            make_client(httpx.Response(200, json=body)).list_story_ids(), list_call(), ListStoryIDsInput()
        )
        assert result.branch == branch
    rejected = await run_query(
        make_client(httpx.Response(404)).list_story_ids(), list_call(), ListStoryIDsInput()
    )
    assert rejected.branch == PROVIDER_REJECTED


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("status", "kind"),
    [(429, FailureKind.RATE_LIMIT), (408, FailureKind.AVAILABILITY), (503, FailureKind.AVAILABILITY)],
)
async def test_safe_failures_retry(status: int, kind: FailureKind) -> None:
    with pytest.raises(RetryError) as raised:
        await run_query(make_client(httpx.Response(status)).get_item(), get_call(), GetItemInput(1))
    assert raised.value.failure.kind == kind


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("operation", "value"),
    [("list", ListStoryIDsInput("jobs")), ("list", ListStoryIDsInput(limit=501)), ("get", GetItemInput(0))],
)
async def test_invalid_input_is_defect_without_request(operation: str, value: object) -> None:
    requests: list[httpx.Request] = []
    client = make_client(httpx.Response(200), requests)
    if operation == "list":
        result = await run_query(client.list_story_ids(), list_call(), value)  # type: ignore[arg-type]
    else:
        result = await run_query(client.get_item(), get_call(), value)  # type: ignore[arg-type]
    assert result.branch == DEFECT
    assert requests == []


def make_project(connector_id: str, provider: str) -> LoadedProject:
    scope = Scope("project-news", "preview")
    objects = InMemoryObjectStore()
    entry = ConnectionConfiguration(
        connector_id,
        "hacker-news-public",
        "github.com/superdurable/dex-connectors-library/connectors/hackernews/go",
        provider,
        {"baseUrl": "http://localhost:8848/v0", "timeout": 5_000_000_000},
    )
    return LoadedProject(
        Configuration(scope, 1, (entry,)),
        SnapshotRef("", "", ""),
        ConnectionStore(objects, scope),
        ConfigurationStore(objects, scope),
    )


@pytest.mark.asyncio
async def test_project_connection_matches_dex_web_connection_identity() -> None:
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200, json=STORY)

    connection = new_project_connection(
        make_project("hacker-news-daily", "hacker-news"),
        "hacker-news-public",
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )
    assert connection.reference == ConnectionRef("hacker-news", "hacker-news-public")
    operation = connection.get_item()
    assert operation.definition().operation.connector_id == "hacker-news-daily"
    call = Call(CallID(str(uuid4())), connection.reference, operation.definition().operation)
    assert (await run_query(operation, call, GetItemInput(42001))).branch == FOUND
    assert [str(request.url) for request in requests] == ["http://localhost:8848/v0/item/42001.json"]
    await connection.aclose()


@pytest.mark.parametrize(
    ("connector_id", "provider", "message"),
    [
        ("hackernews", "hacker-news", "Hacker News project connection is missing or duplicated"),
        ("hacker-news-daily", "hackernews", "Hacker News project connection provider differs"),
    ],
)
def test_project_connection_rejects_other_identities(connector_id: str, provider: str, message: str) -> None:
    with pytest.raises(ValueError) as error:
        new_project_connection(make_project(connector_id, provider), "hacker-news-public")
    assert str(error.value) == message
