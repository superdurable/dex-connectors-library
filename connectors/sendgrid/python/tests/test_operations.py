# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider parity scenarios for the SendGrid operations."""

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

from dex_connectors.sendgrid import (
    DEFECT,
    FOUND,
    GET_STATS_DEFINITION,
    INVALID_RESPONSE,
    LIST_TEMPLATES_DEFINITION,
    LISTED,
    PROVIDER_REJECTED,
    Client,
    Config,
    Credentials,
    GetStatsInput,
    ListTemplatesInput,
    StatsDay,
    Template,
)

CONNECTION = ConnectionRef("sendgrid", "sendgrid")
KEY = "SG.test-key"
STATS = [
    {
        "date": "2026-10-27",
        "stats": [{"metrics": {"requests": 5, "delivered": 4}}, {"metrics": {"requests": 3, "delivered": 3}}],
    }
]
TEMPLATES = {
    "result": [{"id": "d-meridian", "name": "Meridian receipt", "generation": "dynamic", "updated_at": "x"}],
    "_metadata": {"count": 7, "next": "https://api.sendgrid.com/v3/templates?page_size=1&page_token=abc"},
}


def make_client(
    response: httpx.Response, requests: list[httpx.Request] | None = None, key: str = KEY
) -> Client:
    def respond(request: httpx.Request) -> httpx.Response:
        if requests is not None:
            requests.append(request)
        return response

    return Client(
        Config(base_url="http://localhost:8848", max_response_bytes=500),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString(key))}),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )


def stats_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, GET_STATS_DEFINITION.operation)


def templates_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, LIST_TEMPLATES_DEFINITION.operation)


@pytest.mark.asyncio
async def test_get_stats_sums_category_metrics_per_day() -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200, json=STATS), requests).get_stats(),
        stats_call(),
        GetStatsInput("2026-10-27", "2026-10-28"),
    )
    assert result.branch == FOUND
    assert result.value.days == (StatsDay("2026-10-27", 8, 7),)
    request = requests[0]
    assert request.method == "GET"
    assert str(request.url) == "http://localhost:8848/v3/stats?end_date=2026-10-28&start_date=2026-10-27"
    assert request.headers["Authorization"] == "Bearer " + KEY
    assert request.headers["Accept"] == "application/json"


@pytest.mark.asyncio
async def test_list_templates_defaults_generation_and_page_size_and_extracts_token() -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200, json=TEMPLATES), requests).list_templates(),
        templates_call(),
        ListTemplatesInput(page_token="prev"),
    )
    assert result.branch == LISTED
    assert result.value.templates == (Template("d-meridian", "Meridian receipt", "dynamic"),)
    assert result.value.count == 7
    assert result.value.next_page_token == "abc"
    assert requests[0].url.query == b"generations=dynamic&page_size=100&page_token=prev"


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "value",
    [
        GetStatsInput(""),
        GetStatsInput("2026-13-01"),
        GetStatsInput("20261027"),
        GetStatsInput("2026-10-27", "2026-10-26"),
    ],
)
async def test_invalid_stats_range_is_defect_without_request(value: GetStatsInput) -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200, json=STATS), requests).get_stats(), stats_call(), value
    )
    assert result.branch == DEFECT
    assert result.failure is not None and result.failure.kind == FailureKind.VALIDATION
    assert requests == []


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "value",
    [ListTemplatesInput("modern"), ListTemplatesInput(page_size=201), ListTemplatesInput(page_size=-1)],
)
async def test_invalid_template_page_is_defect_without_request(value: ListTemplatesInput) -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200, json=TEMPLATES), requests).list_templates(), templates_call(), value
    )
    assert result.branch == DEFECT
    assert requests == []


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("body", "branch"),
    [
        ([], FOUND),
        ({"date": "x"}, INVALID_RESPONSE),
        ([{"date": "2026-10-27", "stats": []}], INVALID_RESPONSE),
        ([{"stats": [{"metrics": {}}]}], INVALID_RESPONSE),
        ([{"date": "2026-10-27", "stats": [{"metrics": {"requests": 1.5}}]}], INVALID_RESPONSE),
    ],
)
async def test_stats_decoding(body: object, branch: str) -> None:
    result = await run_query(
        make_client(httpx.Response(200, json=body)).get_stats(), stats_call(), GetStatsInput("2026-10-27")
    )
    assert result.branch == branch


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "body",
    [
        {"_metadata": {}},
        {"result": [], "_metadata": {"next": "ftp://x/?page_token=a"}},
        {"result": [], "_metadata": {"next": "https://x/v3/templates"}},
        {"result": [{"id": 1}]},
    ],
)
async def test_invalid_template_page_is_invalid_response(body: object) -> None:
    result = await run_query(
        make_client(httpx.Response(200, json=body)).list_templates(), templates_call(), ListTemplatesInput()
    )
    assert result.branch == INVALID_RESPONSE
    assert result.failure is not None and result.failure.kind == FailureKind.PROTOCOL


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "kind"),
    [
        (httpx.Response(401), FailureKind.PROVIDER_REJECTION),
        (httpx.Response(200, content=b"x" * 600), FailureKind.RESPONSE_TOO_LARGE),
    ],
)
async def test_terminal_failures(response: httpx.Response, kind: FailureKind) -> None:
    result = await run_query(make_client(response).list_templates(), templates_call(), ListTemplatesInput())
    assert result.failure is not None and result.failure.kind == kind
    assert result.branch == (
        PROVIDER_REJECTED if kind == FailureKind.PROVIDER_REJECTION else INVALID_RESPONSE
    )


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("status", "kind"),
    [(408, FailureKind.AVAILABILITY), (429, FailureKind.RATE_LIMIT), (500, FailureKind.AVAILABILITY)],
)
async def test_safe_failures_retry(status: int, kind: FailureKind) -> None:
    with pytest.raises(RetryError) as raised:
        await run_query(
            make_client(httpx.Response(status)).get_stats(), stats_call(), GetStatsInput("2026-10-27")
        )
    assert raised.value.failure.kind == kind


@pytest.mark.asyncio
async def test_blank_key_is_defect_without_request() -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200, json=STATS), requests, key="").get_stats(),
        stats_call(),
        GetStatsInput("2026-10-27"),
    )
    assert result.branch == DEFECT
    assert result.failure is not None and result.failure.kind == FailureKind.AUTHENTICATION
    assert requests == []
