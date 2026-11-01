# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider parity scenarios for the Bitbucket page Queries."""

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

from dex_connectors.atlassian_bitbucket import (
    DEFECT,
    INVALID_RESPONSE,
    LIST_PULL_REQUESTS_DEFINITION,
    LIST_REPOSITORIES_DEFINITION,
    LISTED,
    NOT_FOUND,
    PROVIDER_REJECTED,
    Client,
    Config,
    Credentials,
    ListPullRequestsInput,
    ListRepositoriesInput,
)

CONNECTION = ConnectionRef("bitbucket", "bitbucket")
TOKEN = "ATCTT_test"
REPOSITORY = {
    "uuid": "{a1}",
    "name": "Meridian API",
    "full_name": "meridian/api",
    "is_private": True,
    "size": 1,
}


def make_client(
    response: httpx.Response, requests: list[httpx.Request] | None = None, token: str = TOKEN
) -> Client:
    def respond(request: httpx.Request) -> httpx.Response:
        if requests is not None:
            requests.append(request)
        return response

    return Client(
        Config(base_url="http://localhost:8848/2.0", max_response_bytes=300),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString(token))}),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )


def repositories_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, LIST_REPOSITORIES_DEFINITION.operation)


def pulls_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, LIST_PULL_REQUESTS_DEFINITION.operation)


@pytest.mark.asyncio
async def test_repositories_page_with_next_link() -> None:
    requests: list[httpx.Request] = []
    body = {
        "values": [REPOSITORY],
        "next": "https://api.bitbucket.org/2.0/repositories/meridian?page=2",
        "page": 1,
        "pagelen": 10,
    }
    client = make_client(httpx.Response(200, json=body), requests)
    result = await run_query(
        client.list_repositories(), repositories_call(), ListRepositoriesInput("meridian")
    )
    assert result.branch == LISTED
    assert result.value.values[0].is_private is True
    assert result.value.next.endswith("page=2")
    assert requests[0].url.raw_path == b"/2.0/repositories/meridian"
    assert requests[0].headers["Authorization"] == "Bearer " + TOKEN


@pytest.mark.asyncio
async def test_pull_requests_path_escapes_segments() -> None:
    requests: list[httpx.Request] = []
    body = {"values": [{"id": 7, "title": "Add client tests", "state": "OPEN"}], "page": 1, "pagelen": 10}
    client = make_client(httpx.Response(200, json=body), requests)
    result = await run_query(
        client.list_pull_requests(), pulls_call(), ListPullRequestsInput("meri dian", "api")
    )
    assert result.branch == LISTED
    assert result.value.values[0].id == 7
    assert requests[0].url.raw_path == b"/2.0/repositories/meri%20dian/api/pullrequests"


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "branch", "kind"),
    [
        (httpx.Response(404), NOT_FOUND, FailureKind.PROVIDER_REJECTION),
        (httpx.Response(401), PROVIDER_REJECTED, FailureKind.AUTHENTICATION),
        (httpx.Response(403), PROVIDER_REJECTED, FailureKind.AUTHORIZATION),
        (httpx.Response(200, json={"page": 1}), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (
            httpx.Response(200, json={"values": [{"is_private": "yes"}]}),
            INVALID_RESPONSE,
            FailureKind.PROTOCOL,
        ),
        (httpx.Response(200, content=b"x" * 400), INVALID_RESPONSE, FailureKind.RESPONSE_TOO_LARGE),
    ],
)
async def test_repository_branches(response: httpx.Response, branch: str, kind: FailureKind) -> None:
    result = await run_query(
        make_client(response).list_repositories(), repositories_call(), ListRepositoriesInput("m")
    )
    assert result.branch == branch
    assert result.failure is not None and result.failure.kind == kind


@pytest.mark.asyncio
@pytest.mark.parametrize("status", [408, 429, 500])
async def test_safe_failures_retry(status: int) -> None:
    with pytest.raises(RetryError):
        await run_query(
            make_client(httpx.Response(status)).list_pull_requests(),
            pulls_call(),
            ListPullRequestsInput("m", "a"),
        )


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "value",
    [ListPullRequestsInput("", "api"), ListPullRequestsInput("m", "a/b"), ListPullRequestsInput("m", "..")],
)
async def test_invalid_segments_are_defects_without_request(value: ListPullRequestsInput) -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200), requests).list_pull_requests(), pulls_call(), value
    )
    assert result.branch == DEFECT
    assert requests == []
