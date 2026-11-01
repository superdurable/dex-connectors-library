# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider parity scenarios for the TMDB movie Queries."""

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

from dex_connectors.tmdb import (
    DEFECT,
    FOUND,
    GET_MOVIE_DEFINITION,
    INVALID_RESPONSE,
    NOT_FOUND,
    PROVIDER_REJECTED,
    SEARCH_MOVIES_DEFINITION,
    SEARCHED,
    Client,
    Config,
    Credentials,
    GetMovieInput,
    SearchMoviesInput,
)

CONNECTION = ConnectionRef("tmdb", "tmdb")
TOKEN = "eyJhbGciOiJIUzI1NiJ9.test"
MOVIE = {
    "id": 343611,
    "title": "Jack Reacher",
    "overview": "A thriller.",
    "release_date": "2016-10-19",
    "adult": False,
}


def make_client(
    response: httpx.Response, requests: list[httpx.Request] | None = None, token: str = TOKEN
) -> Client:
    def respond(request: httpx.Request) -> httpx.Response:
        if requests is not None:
            requests.append(request)
        return response

    return Client(
        Config(base_url="http://localhost:8848/3", max_response_bytes=256),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString(token))}),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )


def search_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, SEARCH_MOVIES_DEFINITION.operation)


def get_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, GET_MOVIE_DEFINITION.operation)


@pytest.mark.asyncio
async def test_search_encodes_query_and_sends_bearer_token() -> None:
    requests: list[httpx.Request] = []
    body = {"page": 1, "total_pages": 3, "total_results": 41, "results": [MOVIE]}
    client = make_client(httpx.Response(200, json=body), requests)
    result = await run_query(client.search_movies(), search_call(), SearchMoviesInput("Jack & Reacher"))
    assert result.branch == SEARCHED
    assert (result.value.total_pages, result.value.total_results) == (3, 41)
    assert result.value.results[0].id == 343611
    request = requests[0]
    assert request.url.path == "/3/search/movie"
    assert dict(request.url.params) == {"query": "Jack & Reacher"}
    assert request.headers["Authorization"] == "Bearer " + TOKEN
    await run_query(client.search_movies(), search_call(), SearchMoviesInput("x", 2))
    assert dict(requests[1].url.params) == {"query": "x", "page": "2"}


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "search", [SearchMoviesInput(" "), SearchMoviesInput("x", -1), SearchMoviesInput("x", 501)]
)
async def test_search_rejects_invalid_input_without_request(search: SearchMoviesInput) -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200), requests).search_movies(), search_call(), search
    )
    assert result.branch == DEFECT
    assert requests == []


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "branch", "kind"),
    [
        (httpx.Response(200, json={"page": 1}), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (httpx.Response(200, json={"page": 0, "results": []}), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (
            httpx.Response(200, json={"page": 1, "results": [{"id": "x"}]}),
            INVALID_RESPONSE,
            FailureKind.PROTOCOL,
        ),
        (httpx.Response(200, content=b"x" * 300), INVALID_RESPONSE, FailureKind.RESPONSE_TOO_LARGE),
        (httpx.Response(404), PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION),
        (httpx.Response(401), PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION),
    ],
)
async def test_search_terminal_branches(response: httpx.Response, branch: str, kind: FailureKind) -> None:
    result = await run_query(make_client(response).search_movies(), search_call(), SearchMoviesInput("x"))
    assert result.branch == branch
    assert result.failure is not None and result.failure.kind == kind


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "kind"),
    [
        (httpx.Response(429, headers={"Retry-After": "1"}), FailureKind.RATE_LIMIT),
        (httpx.Response(503), FailureKind.AVAILABILITY),
        (httpx.Response(408), FailureKind.AVAILABILITY),
    ],
)
async def test_search_retries_safe_failures(response: httpx.Response, kind: FailureKind) -> None:
    with pytest.raises(RetryError) as raised:
        await run_query(make_client(response).search_movies(), search_call(), SearchMoviesInput("x"))
    assert raised.value.failure.kind == kind


@pytest.mark.asyncio
async def test_get_reads_movie_path() -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200, json=MOVIE), requests).get_movie(), get_call(), GetMovieInput(343611)
    )
    assert result.branch == FOUND
    assert result.value.release_date == "2016-10-19"
    assert requests[0].url.path == "/3/movie/343611"


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "branch"),
    [
        (httpx.Response(404), NOT_FOUND),
        (httpx.Response(401), PROVIDER_REJECTED),
        (httpx.Response(200, json={**MOVIE, "id": 1}), INVALID_RESPONSE),
        (httpx.Response(200, json={"title": "no id"}), INVALID_RESPONSE),
    ],
)
async def test_get_branches(response: httpx.Response, branch: str) -> None:
    result = await run_query(make_client(response).get_movie(), get_call(), GetMovieInput(343611))
    assert result.branch == branch


@pytest.mark.asyncio
async def test_invalid_id_and_blank_token_are_defects_without_request() -> None:
    requests: list[httpx.Request] = []
    invalid_id = await run_query(
        make_client(httpx.Response(200), requests).get_movie(), get_call(), GetMovieInput(0)
    )
    assert invalid_id.branch == DEFECT
    blank_token = await run_query(
        make_client(httpx.Response(200), requests, token=" ").get_movie(), get_call(), GetMovieInput(1)
    )
    assert blank_token.branch == DEFECT
    assert requests == []
