# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider parity scenarios for the Unsplash Queries."""

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

from dex_connectors.unsplash import (
    DEFECT,
    FOUND,
    GET_PHOTO_DEFINITION,
    INVALID_RESPONSE,
    NOT_FOUND,
    PROVIDER_REJECTED,
    SEARCH_PHOTOS_DEFINITION,
    SEARCHED,
    Client,
    Config,
    Credentials,
    GetPhotoInput,
    SearchPhotosInput,
)

CONNECTION = ConnectionRef("unsplash", "unsplash")
KEY = "access-key"
PHOTO = {
    "id": "p1",
    "description": None,
    "alt_description": "desk",
    "urls": {"regular": "https://images.example/p1"},
    "links": {"html": "https://unsplash.com/photos/p1"},
    "user": {"name": "Ada", "links": {"html": "https://unsplash.com/@ada"}},
}


def make_client(response: httpx.Response, requests: list[httpx.Request] | None = None) -> Client:
    def respond(request: httpx.Request) -> httpx.Response:
        if requests is not None:
            requests.append(request)
        return response

    return Client(
        Config(base_url="http://localhost:8848", max_response_bytes=600),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString(KEY))}),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )


def search_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, SEARCH_PHOTOS_DEFINITION.operation)


def get_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, GET_PHOTO_DEFINITION.operation)


@pytest.mark.asyncio
async def test_search_sends_client_id_and_maps_photos() -> None:
    requests: list[httpx.Request] = []
    body = {"total": 1, "total_pages": 1, "results": [PHOTO]}
    result = await run_query(
        make_client(httpx.Response(200, json=body), requests).search_photos(),
        search_call(),
        SearchPhotosInput("desk", 2, 5),
    )
    assert result.branch == SEARCHED
    photo = result.value.photos[0]
    assert (photo.description, photo.alt_description, photo.photographer_name) == (None, "desk", "Ada")
    assert dict(requests[0].url.params) == {"page": "2", "per_page": "5", "query": "desk"}
    assert requests[0].headers["Authorization"] == "Client-ID " + KEY


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "body",
    [
        {"total": -1, "total_pages": 0, "results": []},
        {"total": 0, "total_pages": 0},
        {"total": 1, "total_pages": 1, "results": [{**PHOTO, "links": {"html": "http://insecure"}}]},
    ],
)
async def test_invalid_search_pages(body: object) -> None:
    result = await run_query(
        make_client(httpx.Response(200, json=body)).search_photos(), search_call(), SearchPhotosInput("x")
    )
    assert result.branch == INVALID_RESPONSE


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "branch", "kind"),
    [
        (httpx.Response(200, json=PHOTO), FOUND, None),
        (httpx.Response(404), NOT_FOUND, FailureKind.NOT_FOUND),
        (httpx.Response(401), PROVIDER_REJECTED, FailureKind.AUTHENTICATION),
        (httpx.Response(403), PROVIDER_REJECTED, FailureKind.AUTHORIZATION),
        (httpx.Response(200, json={**PHOTO, "user": {"name": ""}}), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (httpx.Response(200, content=b"x" * 700), INVALID_RESPONSE, FailureKind.RESPONSE_TOO_LARGE),
    ],
)
async def test_photo_branches(response: httpx.Response, branch: str, kind: FailureKind | None) -> None:
    requests: list[httpx.Request] = []
    result = await run_query(make_client(response, requests).get_photo(), get_call(), GetPhotoInput("p 1"))
    assert result.branch == branch
    assert (result.failure.kind if result.failure else None) == kind
    assert requests[0].url.raw_path == b"/photos/p%201"


@pytest.mark.asyncio
async def test_search_404_is_rejection_and_safe_failures_retry() -> None:
    rejected = await run_query(
        make_client(httpx.Response(404)).search_photos(), search_call(), SearchPhotosInput("x")
    )
    assert rejected.branch == PROVIDER_REJECTED
    for status in (408, 429, 503):
        with pytest.raises(RetryError):
            await run_query(
                make_client(httpx.Response(status)).search_photos(), search_call(), SearchPhotosInput("x")
            )


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("operation", "value"),
    [
        ("search", SearchPhotosInput(" ")),
        ("search", SearchPhotosInput("x", per_page=31)),
        ("get", GetPhotoInput("a/b")),
    ],
)
async def test_invalid_input_is_defect_without_request(operation: str, value: object) -> None:
    requests: list[httpx.Request] = []
    client = make_client(httpx.Response(200), requests)
    if operation == "search":
        result = await run_query(client.search_photos(), search_call(), value)  # type: ignore[arg-type]
    else:
        result = await run_query(client.get_photo(), get_call(), value)  # type: ignore[arg-type]
    assert result.branch == DEFECT
    assert requests == []
