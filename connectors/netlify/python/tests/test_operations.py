# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider parity scenarios for the Netlify Queries."""

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

from dex_connectors.netlify import (
    DEFECT,
    INVALID_RESPONSE,
    LIST_DEPLOYS_DEFINITION,
    LIST_SITES_DEFINITION,
    LISTED,
    NOT_FOUND,
    PROVIDER_REJECTED,
    Client,
    Config,
    Credentials,
    ListDeploysInput,
    ListSitesInput,
)

CONNECTION = ConnectionRef("netlify", "netlify")
TOKEN = "nfp_test"
SITE = {
    "id": "site-1",
    "name": "web",
    "url": "https://web.netlify.app",
    "admin_url": "https://app.netlify.com/sites/web",
}


def make_client(
    response: httpx.Response, requests: list[httpx.Request] | None = None, token: str = TOKEN
) -> Client:
    def respond(request: httpx.Request) -> httpx.Response:
        if requests is not None:
            requests.append(request)
        return response

    return Client(
        Config(base_url="http://localhost:8848/api/v1", max_response_bytes=300),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString(token))}),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )


def sites_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, LIST_SITES_DEFINITION.operation)


def deploys_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, LIST_DEPLOYS_DEFINITION.operation)


@pytest.mark.asyncio
async def test_sites_are_listed_with_bearer_token() -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200, json=[SITE]), requests).list_sites(), sites_call(), ListSitesInput()
    )
    assert result.branch == LISTED
    assert result.value.sites[0].admin_url.endswith("/web")
    assert requests[0].url.raw_path == b"/api/v1/sites"
    assert requests[0].headers["Authorization"] == "Bearer " + TOKEN


@pytest.mark.asyncio
async def test_deploys_escape_site_id() -> None:
    requests: list[httpx.Request] = []
    body = [{"id": "d1", "site_id": "site 1", "state": "ready", "url": "https://x"}]
    client = make_client(httpx.Response(200, json=body), requests)
    result = await run_query(client.list_deploys(), deploys_call(), ListDeploysInput("site 1"))
    assert result.branch == LISTED
    assert result.value.deploys[0].state == "ready"
    assert requests[0].url.raw_path == b"/api/v1/sites/site%201/deploys"


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("status", "branch", "kind"),
    [
        (404, NOT_FOUND, FailureKind.NOT_FOUND),
        (401, PROVIDER_REJECTED, FailureKind.AUTHENTICATION),
        (403, PROVIDER_REJECTED, FailureKind.AUTHORIZATION),
        (422, PROVIDER_REJECTED, FailureKind.VALIDATION),
        (418, PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION),
    ],
)
async def test_deploy_status_branches(status: int, branch: str, kind: FailureKind) -> None:
    result = await run_query(
        make_client(httpx.Response(status)).list_deploys(), deploys_call(), ListDeploysInput("s")
    )
    assert result.branch == branch
    assert result.failure is not None and result.failure.kind == kind


@pytest.mark.asyncio
async def test_sites_404_is_provider_rejection_with_not_found_kind() -> None:
    result = await run_query(make_client(httpx.Response(404)).list_sites(), sites_call(), ListSitesInput())
    assert result.branch == PROVIDER_REJECTED
    assert result.failure is not None and result.failure.kind == FailureKind.NOT_FOUND


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "kind"),
    [
        (httpx.Response(200, json={"sites": []}), FailureKind.PROTOCOL),
        (httpx.Response(200, json=[{"id": 1}]), FailureKind.PROTOCOL),
        (httpx.Response(200, content=b"x" * 400), FailureKind.RESPONSE_TOO_LARGE),
    ],
)
async def test_invalid_responses(response: httpx.Response, kind: FailureKind) -> None:
    result = await run_query(make_client(response).list_sites(), sites_call(), ListSitesInput())
    assert result.branch == INVALID_RESPONSE
    assert result.failure is not None and result.failure.kind == kind


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("status", "kind"),
    [(429, FailureKind.RATE_LIMIT), (409, FailureKind.AVAILABILITY), (500, FailureKind.AVAILABILITY)],
)
async def test_safe_failures_retry(status: int, kind: FailureKind) -> None:
    with pytest.raises(RetryError) as raised:
        await run_query(make_client(httpx.Response(status)).list_sites(), sites_call(), ListSitesInput())
    assert raised.value.failure.kind == kind


@pytest.mark.asyncio
async def test_blank_site_and_token_are_defects_without_request() -> None:
    requests: list[httpx.Request] = []
    blank_site = await run_query(
        make_client(httpx.Response(200), requests).list_deploys(), deploys_call(), ListDeploysInput(" ")
    )
    assert blank_site.branch == DEFECT
    blank_token = await run_query(
        make_client(httpx.Response(200), requests, token="").list_sites(), sites_call(), ListSitesInput()
    )
    assert blank_token.branch == DEFECT
    assert requests == []
