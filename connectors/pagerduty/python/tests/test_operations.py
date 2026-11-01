# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider parity scenarios for the PagerDuty incident Queries."""

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

from dex_connectors.pagerduty import (
    DEFECT,
    FOUND,
    GET_INCIDENT_DEFINITION,
    INVALID_RESPONSE,
    LIST_INCIDENTS_DEFINITION,
    LISTED,
    NOT_FOUND,
    PROVIDER_REJECTED,
    Client,
    Config,
    Credentials,
    GetIncidentInput,
    ListIncidentsInput,
)

CONNECTION = ConnectionRef("pagerduty", "pagerduty")
TOKEN = "u+abcdefghijklmnop"
INCIDENT = {"id": "P123456", "title": "API latency", "status": "triggered", "urgency": "high"}


def make_client(
    response: httpx.Response, requests: list[httpx.Request] | None = None, token: str = TOKEN
) -> Client:
    def respond(request: httpx.Request) -> httpx.Response:
        if requests is not None:
            requests.append(request)
        return response

    return Client(
        Config(base_url="http://localhost:8848", max_response_bytes=256),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString(token))}),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )


def list_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, LIST_INCIDENTS_DEFINITION.operation)


def get_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, GET_INCIDENT_DEFINITION.operation)


@pytest.mark.asyncio
async def test_list_defaults_limit_and_sends_token_authorization() -> None:
    requests: list[httpx.Request] = []
    body = {"incidents": [INCIDENT], "limit": 25, "offset": 0, "more": True}
    client = make_client(httpx.Response(200, json=body), requests)
    result = await run_query(client.list_incidents(), list_call(), ListIncidentsInput())
    assert result.branch == LISTED
    assert result.value.incidents[0].id == "P123456"
    assert result.value.more is True
    request = requests[0]
    assert request.url.path == "/incidents"
    assert dict(request.url.params) == {"limit": "25", "offset": "0"}
    assert request.headers["Authorization"] == "Token token=" + TOKEN
    assert request.headers["Accept"] == "application/vnd.pagerduty+json;version=2"


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "page", [ListIncidentsInput(limit=101), ListIncidentsInput(limit=-1), ListIncidentsInput(offset=-1)]
)
async def test_list_rejects_invalid_page_without_request(page: ListIncidentsInput) -> None:
    requests: list[httpx.Request] = []
    client = make_client(httpx.Response(200, json={"incidents": []}), requests)
    result = await run_query(client.list_incidents(), list_call(), page)
    assert result.branch == DEFECT
    assert requests == []


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "branch", "kind"),
    [
        (httpx.Response(200, json={"limit": 25}), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (httpx.Response(200, json={"incidents": [], "more": "no"}), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (httpx.Response(200, content=b"x" * 300), INVALID_RESPONSE, FailureKind.RESPONSE_TOO_LARGE),
        (httpx.Response(404), PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION),
        (httpx.Response(403), PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION),
    ],
)
async def test_list_terminal_branches(response: httpx.Response, branch: str, kind: FailureKind) -> None:
    result = await run_query(make_client(response).list_incidents(), list_call(), ListIncidentsInput())
    assert result.branch == branch
    assert result.failure is not None and result.failure.kind == kind


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "kind"),
    [
        (httpx.Response(429, headers={"Retry-After": "2"}), FailureKind.RATE_LIMIT),
        (httpx.Response(502), FailureKind.AVAILABILITY),
        (httpx.Response(408), FailureKind.AVAILABILITY),
    ],
)
async def test_list_retries_safe_failures(response: httpx.Response, kind: FailureKind) -> None:
    with pytest.raises(RetryError) as raised:
        await run_query(make_client(response).list_incidents(), list_call(), ListIncidentsInput())
    assert raised.value.failure.kind == kind


@pytest.mark.asyncio
async def test_get_unwraps_incident_and_escapes_id() -> None:
    requests: list[httpx.Request] = []
    client = make_client(httpx.Response(200, json={"incident": INCIDENT}), requests)
    result = await run_query(client.get_incident(), get_call(), GetIncidentInput("P123456"))
    assert result.branch == FOUND
    assert result.value.title == "API latency"
    await run_query(client.get_incident(), get_call(), GetIncidentInput("a/b"))
    assert requests[1].url.raw_path == b"/incidents/a%2Fb"


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "branch"),
    [
        (httpx.Response(404), NOT_FOUND),
        (httpx.Response(401), PROVIDER_REJECTED),
        (httpx.Response(200, json={"incident": {"title": "no id"}}), INVALID_RESPONSE),
        (httpx.Response(200, json={}), INVALID_RESPONSE),
    ],
)
async def test_get_branches(response: httpx.Response, branch: str) -> None:
    result = await run_query(make_client(response).get_incident(), get_call(), GetIncidentInput("P123456"))
    assert result.branch == branch


@pytest.mark.asyncio
async def test_blank_id_and_token_are_defects_without_request() -> None:
    requests: list[httpx.Request] = []
    blank_id = await run_query(
        make_client(httpx.Response(200), requests).get_incident(), get_call(), GetIncidentInput("")
    )
    assert blank_id.branch == DEFECT
    blank_token = await run_query(
        make_client(httpx.Response(200), requests, token=" ").get_incident(),
        get_call(),
        GetIncidentInput("P1"),
    )
    assert blank_token.branch == DEFECT
    assert requests == []
