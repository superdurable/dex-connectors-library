# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider parity scenarios for the Mailgun Queries."""

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

from dex_connectors.mailgun import (
    DEFECT,
    GET_EVENTS_DEFINITION,
    INVALID_RESPONSE,
    LIST_DOMAINS_DEFINITION,
    LISTED,
    PROVIDER_REJECTED,
    Client,
    Config,
    Credentials,
    GetEventsInput,
    ListDomainsInput,
)

CONNECTION = ConnectionRef("mailgun", "mailgun")
KEY = "key-test"


def make_client(
    response: httpx.Response, requests: list[httpx.Request] | None = None, key: str = KEY
) -> Client:
    def respond(request: httpx.Request) -> httpx.Response:
        if requests is not None:
            requests.append(request)
        return response

    return Client(
        Config(base_url="http://localhost:8848", max_response_bytes=300),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString(key))}),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )


def events_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, GET_EVENTS_DEFINITION.operation)


def domains_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, LIST_DOMAINS_DEFINITION.operation)


@pytest.mark.asyncio
async def test_events_use_basic_auth_and_domain_path() -> None:
    requests: list[httpx.Request] = []
    body = {"items": [{"id": "e1", "event": "delivered", "timestamp": 1793100000.5}], "paging": {}}
    result = await run_query(
        make_client(httpx.Response(200, json=body), requests).get_events(),
        events_call(),
        GetEventsInput("mg.example.test", 25),
    )
    assert result.branch == LISTED
    assert result.value.events[0].timestamp == 1793100000.5
    assert requests[0].url.raw_path == b"/v3/mg.example.test/events?limit=25"
    assert requests[0].headers["Authorization"] == "Basic " + base64.b64encode(b"api:key-test").decode()
    assert KEY not in str(requests[0].url)


@pytest.mark.asyncio
async def test_domains_page_requires_total_count() -> None:
    requests: list[httpx.Request] = []
    body = {"items": [{"name": "example.test", "state": "active"}], "total_count": 1}
    client = make_client(httpx.Response(200, json=body), requests)
    result = await run_query(client.list_domains(), domains_call(), ListDomainsInput(limit=10, skip=5))
    assert result.branch == LISTED
    assert result.value.total_count == 1
    assert requests[0].url.raw_path == b"/v4/domains?limit=10&skip=5"
    missing = await run_query(
        make_client(httpx.Response(200, json={"items": []})).list_domains(),
        domains_call(),
        ListDomainsInput(),
    )
    assert missing.branch == INVALID_RESPONSE


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "branch", "kind"),
    [
        (httpx.Response(404), PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION),
        (httpx.Response(401), PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION),
        (httpx.Response(200, json={"paging": {}}), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (httpx.Response(200, content=b"x" * 400), INVALID_RESPONSE, FailureKind.RESPONSE_TOO_LARGE),
    ],
)
async def test_event_branches(response: httpx.Response, branch: str, kind: FailureKind) -> None:
    result = await run_query(
        make_client(response).get_events(), events_call(), GetEventsInput("example.test")
    )
    assert result.branch == branch
    assert result.failure is not None and result.failure.kind == kind


@pytest.mark.asyncio
@pytest.mark.parametrize("status", [408, 429, 500])
async def test_safe_failures_retry(status: int) -> None:
    with pytest.raises(RetryError):
        await run_query(
            make_client(httpx.Response(status)).list_domains(), domains_call(), ListDomainsInput()
        )


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("operation", "value"),
    [
        ("events", GetEventsInput("-bad.example")),
        ("events", GetEventsInput("a/b")),
        ("events", GetEventsInput("example.test", 301)),
        ("domains", ListDomainsInput(limit=1001)),
        ("domains", ListDomainsInput(skip=-1)),
    ],
)
async def test_invalid_input_is_defect_without_request(operation: str, value: object) -> None:
    requests: list[httpx.Request] = []
    client = make_client(httpx.Response(200), requests)
    if operation == "events":
        result = await run_query(client.get_events(), events_call(), value)  # type: ignore[arg-type]
    else:
        result = await run_query(client.list_domains(), domains_call(), value)  # type: ignore[arg-type]
    assert result.branch == DEFECT
    assert requests == []


@pytest.mark.asyncio
async def test_blank_key_is_defect_without_request() -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200), requests, key="").list_domains(), domains_call(), ListDomainsInput()
    )
    assert result.branch == DEFECT
    assert result.failure is not None and result.failure.kind == FailureKind.AUTHENTICATION
    assert requests == []
