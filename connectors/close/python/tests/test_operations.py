# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider parity scenarios for the Close lead Queries."""

import base64
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

from dex_connectors.close import (
    DEFECT,
    FOUND,
    GET_LEAD_DEFINITION,
    INVALID_RESPONSE,
    LIST_LEADS_DEFINITION,
    LISTED,
    NOT_FOUND,
    PROVIDER_REJECTED,
    Client,
    Config,
    Credentials,
    GetLeadInput,
    ListLeadsInput,
)

CONNECTION = ConnectionRef("close", "close")
KEY = "api_test_key"
LEAD = {"id": "lead_meridian1", "name": "Meridian Labs", "status_label": "Potential"}


def make_client(
    response: httpx.Response, requests: list[httpx.Request] | None = None, key: str = KEY
) -> Client:
    def respond(request: httpx.Request) -> httpx.Response:
        if requests is not None:
            requests.append(request)
        return response

    return Client(
        Config(base_url="http://localhost:8848/api/v1", max_response_bytes=256),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString(key))}),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )


def list_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, LIST_LEADS_DEFINITION.operation)


def get_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, GET_LEAD_DEFINITION.operation)


@pytest.mark.asyncio
async def test_list_defaults_limit_and_uses_basic_auth() -> None:
    requests: list[httpx.Request] = []
    client = make_client(httpx.Response(200, json={"data": [LEAD], "has_more": True}), requests)
    result = await run_query(client.list_leads(), list_call(), ListLeadsInput(skip=20))
    assert result.branch == LISTED
    assert result.value.leads[0].name == "Meridian Labs"
    assert result.value.has_more is True
    assert requests[0].url.raw_path == b"/api/v1/lead/?_skip=20&_limit=100"
    assert requests[0].headers["Authorization"] == "Basic " + base64.b64encode(b"api_test_key:").decode()


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "page", [ListLeadsInput(skip=-1), ListLeadsInput(limit=101), ListLeadsInput(limit=-1)]
)
async def test_list_rejects_invalid_window_without_request(page: ListLeadsInput) -> None:
    requests: list[httpx.Request] = []
    result = await run_query(make_client(httpx.Response(200), requests).list_leads(), list_call(), page)
    assert result.branch == DEFECT
    assert requests == []


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "branch", "kind"),
    [
        (httpx.Response(200, json={"has_more": False}), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (httpx.Response(200, json={"data": [], "has_more": "no"}), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (httpx.Response(200, content=b"x" * 300), INVALID_RESPONSE, FailureKind.RESPONSE_TOO_LARGE),
        (httpx.Response(404), PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION),
        (httpx.Response(401), PROVIDER_REJECTED, FailureKind.AUTHENTICATION),
        (httpx.Response(403), PROVIDER_REJECTED, FailureKind.AUTHORIZATION),
    ],
)
async def test_list_terminal_branches(response: httpx.Response, branch: str, kind: FailureKind) -> None:
    result = await run_query(make_client(response).list_leads(), list_call(), ListLeadsInput())
    assert result.branch == branch
    assert result.failure is not None and result.failure.kind == kind


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "kind"),
    [
        (httpx.Response(429, headers={"Retry-After": "3"}), FailureKind.RATE_LIMIT),
        (httpx.Response(409), FailureKind.AVAILABILITY),
        (httpx.Response(408), FailureKind.AVAILABILITY),
        (httpx.Response(502), FailureKind.AVAILABILITY),
    ],
)
async def test_list_retries_safe_failures(response: httpx.Response, kind: FailureKind) -> None:
    with pytest.raises(RetryError) as raised:
        await run_query(make_client(response).list_leads(), list_call(), ListLeadsInput())
    assert raised.value.failure.kind == kind


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "branch"),
    [
        (httpx.Response(200, json=LEAD), FOUND),
        (httpx.Response(404), NOT_FOUND),
        (httpx.Response(200, json={"id": "lead_meridian1"}), INVALID_RESPONSE),
    ],
)
async def test_get_branches(response: httpx.Response, branch: str) -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(response, requests).get_lead(), get_call(), GetLeadInput("lead_meridian1")
    )
    assert result.branch == branch
    assert requests[0].url.raw_path == b"/api/v1/lead/lead_meridian1/"


@pytest.mark.asyncio
@pytest.mark.parametrize("lead_id", ["", "cont_1", "lead_a/b"])
async def test_get_rejects_invalid_lead_id_without_request(lead_id: str) -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200), requests).get_lead(), get_call(), GetLeadInput(lead_id)
    )
    assert result.branch == DEFECT
    assert requests == []


@pytest.mark.asyncio
async def test_blank_key_is_defect_without_request() -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200), requests, key="").list_leads(), list_call(), ListLeadsInput()
    )
    assert result.branch == DEFECT
    assert result.failure is not None and result.failure.kind == FailureKind.AUTHENTICATION
    assert requests == []
