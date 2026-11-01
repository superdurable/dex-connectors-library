# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider parity scenarios for the Cloudflare operations."""

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

from dex_connectors.cloudflare import (
    DEFECT,
    INVALID_RESPONSE,
    LIST_ZONES_DEFINITION,
    LISTED,
    NOT_FOUND,
    PROVIDER_REJECTED,
    PURGE_CACHE_BY_URLS_DEFINITION,
    PURGED,
    CachePurge,
    Client,
    Config,
    Credentials,
    ListZonesInput,
    PurgeCacheByURLsInput,
    PurgeRequest,
    Zone,
)

CONNECTION = ConnectionRef("cloudflare", "cloudflare")
TOKEN = "cloudflare-test-token"
ZONES = {"success": True, "result": [{"id": "zone-meridian", "name": "meridian.example", "status": "active"}]}
PURGE = PurgeCacheByURLsInput(
    "zone-meridian", ("https://meridian.example/a", "http://meridian.example/b?x=1")
)


def make_client(
    response: httpx.Response, requests: list[httpx.Request] | None = None, token: str = TOKEN
) -> Client:
    def respond(request: httpx.Request) -> httpx.Response:
        if requests is not None:
            requests.append(request)
        return response

    return Client(
        Config("http://localhost:8848/client/v4", max_response_bytes=300),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString(token))}),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )


def list_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, LIST_ZONES_DEFINITION.operation)


def purge_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, PURGE_CACHE_BY_URLS_DEFINITION.operation)


@pytest.mark.asyncio
async def test_list_zones_sends_bearer_token_and_decodes_page() -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200, json=ZONES), requests).list_zones(), list_call(), ListZonesInput()
    )
    assert result.branch == LISTED
    assert result.value.success is True
    assert result.value.result == (Zone("zone-meridian", "meridian.example", "active"),)
    request = requests[0]
    assert request.method == "GET"
    assert str(request.url) == "http://localhost:8848/client/v4/zones"
    assert request.headers["Authorization"] == "Bearer " + TOKEN
    assert "Content-Type" not in request.headers


@pytest.mark.asyncio
async def test_purge_posts_files_to_escaped_zone_path() -> None:
    requests: list[httpx.Request] = []
    body = {"success": True, "result": {"id": "purge-meridian"}}
    result = await run_mutation(
        make_client(httpx.Response(200, json=body), requests).purge_cache_by_urls(), purge_call(), PURGE
    )
    assert result.branch == PURGED
    assert result.value == CachePurge(True, PurgeRequest("purge-meridian"))
    request = requests[0]
    assert request.method == "POST"
    assert request.url.raw_path == b"/client/v4/zones/zone-meridian/purge_cache"
    assert request.headers["Content-Type"] == "application/json"
    assert request.content == b'{"files":["https://meridian.example/a","http://meridian.example/b?x=1"]}'


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "value",
    [
        PurgeCacheByURLsInput(" ", ("https://a.example/",)),
        PurgeCacheByURLsInput("zone", ()),
        PurgeCacheByURLsInput("zone", tuple(f"https://a.example/{index}" for index in range(31))),
        PurgeCacheByURLsInput("zone", ("ftp://a.example/",)),
        PurgeCacheByURLsInput("zone", ("/relative",)),
        PurgeCacheByURLsInput("zone", ("https://user@a.example/",)),
        PurgeCacheByURLsInput("zone", ("https://a.example/\n",)),
    ],
)
async def test_invalid_purge_is_defect_without_request(value: PurgeCacheByURLsInput) -> None:
    requests: list[httpx.Request] = []
    result = await run_mutation(
        make_client(httpx.Response(200, json={"success": True}), requests).purge_cache_by_urls(),
        purge_call(),
        value,
    )
    assert result.branch == DEFECT
    assert result.failure is not None and result.failure.kind == FailureKind.VALIDATION
    assert requests == []


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "branch", "kind"),
    [
        (httpx.Response(404), NOT_FOUND, FailureKind.NOT_FOUND),
        (httpx.Response(400), PROVIDER_REJECTED, FailureKind.VALIDATION),
        (httpx.Response(401), PROVIDER_REJECTED, FailureKind.AUTHENTICATION),
        (httpx.Response(403), PROVIDER_REJECTED, FailureKind.AUTHORIZATION),
        (
            httpx.Response(200, json={"success": False, "errors": []}),
            PROVIDER_REJECTED,
            FailureKind.PROVIDER_REJECTION,
        ),
        (httpx.Response(200, json={"result": {"id": "x"}}), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (
            httpx.Response(200, json={"success": True, "result": {"id": 1}}),
            INVALID_RESPONSE,
            FailureKind.PROTOCOL,
        ),
        (httpx.Response(200, content=b"{"), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (httpx.Response(200, content=b"x" * 400), INVALID_RESPONSE, FailureKind.RESPONSE_TOO_LARGE),
    ],
)
async def test_purge_branches(response: httpx.Response, branch: str, kind: FailureKind) -> None:
    result = await run_mutation(make_client(response).purge_cache_by_urls(), purge_call(), PURGE)
    assert result.branch == branch
    assert result.failure is not None and result.failure.kind == kind


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "branch", "kind"),
    [
        (httpx.Response(404), PROVIDER_REJECTED, FailureKind.NOT_FOUND),
        (httpx.Response(200, json={"success": True}), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (
            httpx.Response(200, json={"success": True, "result": [{"id": 1}]}),
            INVALID_RESPONSE,
            FailureKind.PROTOCOL,
        ),
        (httpx.Response(200, json=[]), INVALID_RESPONSE, FailureKind.PROTOCOL),
    ],
)
async def test_list_zones_branches(response: httpx.Response, branch: str, kind: FailureKind) -> None:
    result = await run_query(make_client(response).list_zones(), list_call(), ListZonesInput())
    assert result.branch == branch
    assert result.failure is not None and result.failure.kind == kind


@pytest.mark.asyncio
async def test_null_zone_fields_decode_as_blank() -> None:
    body = {"success": True, "result": [{"id": "z", "name": None}, None]}
    result = await run_query(
        make_client(httpx.Response(200, json=body)).list_zones(), list_call(), ListZonesInput()
    )
    assert result.branch == LISTED
    assert result.value.result == (Zone("z", "", ""), Zone())


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("status", "kind"),
    [
        (408, FailureKind.AVAILABILITY),
        (409, FailureKind.AVAILABILITY),
        (429, FailureKind.RATE_LIMIT),
        (502, FailureKind.AVAILABILITY),
    ],
)
async def test_safe_failures_retry(status: int, kind: FailureKind) -> None:
    with pytest.raises(RetryError) as raised:
        await run_mutation(make_client(httpx.Response(status)).purge_cache_by_urls(), purge_call(), PURGE)
    assert raised.value.failure.kind == kind


@pytest.mark.asyncio
async def test_transport_failure_retries() -> None:
    def fail(request: httpx.Request) -> httpx.Response:
        raise httpx.ConnectError("refused", request=request)

    client = Client(
        Config(),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString(TOKEN))}),
        httpx.AsyncClient(transport=httpx.MockTransport(fail)),
    )
    with pytest.raises(RetryError) as raised:
        await run_query(client.list_zones(), list_call(), ListZonesInput())
    assert raised.value.failure.kind == FailureKind.AVAILABILITY


@pytest.mark.asyncio
async def test_blank_token_is_defect_without_request() -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200, json=ZONES), requests, token="").list_zones(),
        list_call(),
        ListZonesInput(),
    )
    assert result.branch == DEFECT
    assert result.failure is not None and result.failure.kind == FailureKind.AUTHENTICATION
    assert requests == []
