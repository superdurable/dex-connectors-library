# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider parity scenarios for the Vercel Queries."""

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

from dex_connectors.vercel import (
    DEFECT,
    FOUND,
    GET_DEPLOYMENT_DEFINITION,
    INVALID_RESPONSE,
    LIST_DEPLOYMENTS_DEFINITION,
    LISTED,
    NOT_FOUND,
    PROVIDER_REJECTED,
    Client,
    Config,
    Credentials,
    GetDeploymentInput,
    ListDeploymentsInput,
)

CONNECTION = ConnectionRef("vercel", "vercel")
TOKEN = "vercel_test"
DEPLOYMENT = {
    "uid": "dpl_1",
    "name": "web",
    "url": "web.vercel.app",
    "state": "READY",
    "readyState": "READY",
    "created": 1,
}


def make_client(
    response: httpx.Response, requests: list[httpx.Request] | None = None, token: str = TOKEN
) -> Client:
    def respond(request: httpx.Request) -> httpx.Response:
        if requests is not None:
            requests.append(request)
        return response

    return Client(
        Config(base_url="http://localhost:8848", max_response_bytes=400),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString(token))}),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )


def list_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, LIST_DEPLOYMENTS_DEFINITION.operation)


def get_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, GET_DEPLOYMENT_DEFINITION.operation)


@pytest.mark.asyncio
async def test_list_passes_team_and_reads_pagination() -> None:
    requests: list[httpx.Request] = []
    body = {"deployments": [DEPLOYMENT], "pagination": {"next": 1761436700000}}
    client = make_client(httpx.Response(200, json=body), requests)
    result = await run_query(client.list_deployments(), list_call(), ListDeploymentsInput("team_1"))
    assert result.branch == LISTED
    assert result.value.pagination.next == 1761436700000
    assert result.value.deployments[0].ready_state == "READY"
    assert requests[0].url.raw_path == b"/v6/deployments?teamId=team_1"
    assert requests[0].headers["Authorization"] == "Bearer " + TOKEN
    await run_query(client.list_deployments(), list_call(), ListDeploymentsInput())
    assert requests[1].url.raw_path == b"/v6/deployments"


@pytest.mark.asyncio
async def test_get_escapes_hostname() -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200, json=DEPLOYMENT), requests).get_deployment(),
        get_call(),
        GetDeploymentInput("web.vercel.app/x"),
    )
    assert result.branch == FOUND
    assert requests[0].url.raw_path == b"/v13/deployments/web.vercel.app%2Fx"


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "branch"),
    [
        (httpx.Response(404), NOT_FOUND),
        (httpx.Response(403), PROVIDER_REJECTED),
        (httpx.Response(200, json={"name": "no id"}), INVALID_RESPONSE),
        (httpx.Response(200, json={**DEPLOYMENT, "created": "1"}), INVALID_RESPONSE),
    ],
)
async def test_get_branches(response: httpx.Response, branch: str) -> None:
    result = await run_query(make_client(response).get_deployment(), get_call(), GetDeploymentInput("dpl_1"))
    assert result.branch == branch


@pytest.mark.asyncio
async def test_list_requires_deployments_and_retries_safe_failures() -> None:
    invalid = await run_query(
        make_client(httpx.Response(200, json={})).list_deployments(), list_call(), ListDeploymentsInput()
    )
    assert invalid.branch == INVALID_RESPONSE
    rejected = await run_query(
        make_client(httpx.Response(404)).list_deployments(), list_call(), ListDeploymentsInput()
    )
    assert rejected.branch == PROVIDER_REJECTED
    assert rejected.failure is not None and rejected.failure.kind == FailureKind.NOT_FOUND
    for status in (409, 429, 503):
        with pytest.raises(RetryError):
            await run_query(
                make_client(httpx.Response(status)).list_deployments(), list_call(), ListDeploymentsInput()
            )


@pytest.mark.asyncio
async def test_blank_identifier_and_token_are_defects_without_request() -> None:
    requests: list[httpx.Request] = []
    blank_id = await run_query(
        make_client(httpx.Response(200), requests).get_deployment(), get_call(), GetDeploymentInput(" ")
    )
    assert blank_id.branch == DEFECT
    blank_token = await run_query(
        make_client(httpx.Response(200), requests, token="").list_deployments(),
        list_call(),
        ListDeploymentsInput(),
    )
    assert blank_token.branch == DEFECT
    assert requests == []
