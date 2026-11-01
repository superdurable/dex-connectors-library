# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider parity scenarios for the Algolia operations."""

import json
from urllib.parse import parse_qs
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

from dex_connectors.algolia import (
    DEFECT,
    INVALID_RESPONSE,
    PROVIDER_REJECTED,
    SAVE_OBJECT_DEFINITION,
    SAVED,
    SEARCH_DEFINITION,
    SEARCHED,
    Client,
    Config,
    Credentials,
    SavedObject,
    SaveObjectInput,
    SearchInput,
)

CONNECTION = ConnectionRef("algolia", "algolia")
KEY = "algolia-test-key"
PAGE = {"nbHits": 1, "hits": [{"objectID": "product-1", "name": "Meridian notebook"}]}
SAVED_BODY = {"objectID": "product-1", "taskID": 1514562690001}
SEARCH = SearchInput("products", "notebook")
RECORD = SaveObjectInput("products", "product-1", {"name": "Meridian notebook"})


def make_client(
    response: httpx.Response,
    requests: list[httpx.Request] | None = None,
    application_id: str = "TESTAPP",
    base_url: str = "http://localhost:8848/1",
) -> Client:
    def respond(request: httpx.Request) -> httpx.Response:
        if requests is not None:
            requests.append(request)
        return response

    return Client(
        Config(base_url=base_url, max_response_bytes=300),
        StaticCredentialProvider({CONNECTION: Credentials(application_id, SecretString(KEY))}),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )


def search_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, SEARCH_DEFINITION.operation)


def save_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, SAVE_OBJECT_DEFINITION.operation)


@pytest.mark.asyncio
async def test_search_uses_headers_and_decodes_hits() -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200, json=PAGE), requests).search(),
        search_call(),
        SearchInput("products", "notebook bag", 5),
    )
    assert result.branch == SEARCHED
    assert result.value.nb_hits == 1
    assert [(hit.object_id, hit.name) for hit in result.value.hits] == [("product-1", "Meridian notebook")]
    request = requests[0]
    assert request.method == "POST"
    assert request.url.path == "/1/indexes/products/query"
    assert request.headers["x-algolia-application-id"] == "TESTAPP"
    assert request.headers["x-algolia-api-key"] == KEY
    assert KEY not in str(request.url)
    params = json.loads(request.content)["params"]
    assert params == "hitsPerPage=5&query=notebook+bag"
    assert parse_qs(params) == {"hitsPerPage": ["5"], "query": ["notebook bag"]}


@pytest.mark.asyncio
async def test_public_host_template_prefixes_application_id() -> None:
    requests: list[httpx.Request] = []
    await run_query(
        make_client(httpx.Response(200, json=PAGE), requests, base_url="").search(), search_call(), SEARCH
    )
    assert str(requests[0].url) == "https://testapp.algolia.net/1/indexes/products/query"


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "value",
    [
        SearchInput("../bad"),
        SearchInput(" "),
        SearchInput("a?b"),
        SearchInput("p", "", -1),
        SearchInput("p", "", 1001),
    ],
)
async def test_search_rejects_bad_input_before_dispatch(value: SearchInput) -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200, json=PAGE), requests).search(), search_call(), value
    )
    assert result.branch == DEFECT
    assert requests == []


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "branch", "kind"),
    [
        (httpx.Response(400), PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION),
        (httpx.Response(401), PROVIDER_REJECTED, FailureKind.AUTHENTICATION),
        (httpx.Response(403), PROVIDER_REJECTED, FailureKind.AUTHORIZATION),
        (httpx.Response(200, json={"nbHits": 0}), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (httpx.Response(200, json={"nbHits": -1, "hits": []}), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (
            httpx.Response(200, json={"nbHits": 1, "hits": [{"name": "x"}]}),
            INVALID_RESPONSE,
            FailureKind.PROTOCOL,
        ),
        (httpx.Response(200, content=b"x" * 400), INVALID_RESPONSE, FailureKind.RESPONSE_TOO_LARGE),
    ],
)
async def test_search_branches(response: httpx.Response, branch: str, kind: FailureKind) -> None:
    result = await run_query(make_client(response).search(), search_call(), SEARCH)
    assert result.branch == branch
    assert result.failure is not None and result.failure.kind == kind


@pytest.mark.asyncio
async def test_save_object_replaces_by_stable_id() -> None:
    requests: list[httpx.Request] = []
    result = await run_mutation(
        make_client(httpx.Response(200, json=SAVED_BODY), requests).save_object(), save_call(), RECORD
    )
    assert result.branch == SAVED
    assert result.value == SavedObject("product-1", 1514562690001)
    request = requests[0]
    assert request.method == "PUT"
    assert request.url.path == "/1/indexes/products/product-1"
    assert json.loads(request.content) == {"name": "Meridian notebook", "objectID": "product-1"}


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "value",
    [
        SaveObjectInput("products", "product-1", {"objectID": "other"}),
        SaveObjectInput("products", "product-1", {" ": "x"}),
        SaveObjectInput("products", "a/b", {}),
        SaveObjectInput("", "product-1", {}),
        SaveObjectInput("products", "product-1", {"big": "x" * (1 << 20)}),
    ],
)
async def test_save_object_rejects_bad_input_before_dispatch(value: SaveObjectInput) -> None:
    requests: list[httpx.Request] = []
    result = await run_mutation(
        make_client(httpx.Response(200, json=SAVED_BODY), requests).save_object(), save_call(), value
    )
    assert result.branch == DEFECT
    assert requests == []


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "body",
    [{"objectID": "other", "taskID": 1}, {"objectID": "product-1", "taskID": 0}, {"objectID": "product-1"}],
)
async def test_save_object_requires_matching_task(body: dict[str, object]) -> None:
    result = await run_mutation(
        make_client(httpx.Response(200, json=body)).save_object(), save_call(), RECORD
    )
    assert result.branch == INVALID_RESPONSE
    assert result.failure is not None and result.failure.kind == FailureKind.PROTOCOL


@pytest.mark.asyncio
@pytest.mark.parametrize("status", [408, 409, 429, 500, 503])
async def test_safe_failures_retry(status: int) -> None:
    with pytest.raises(RetryError):
        await run_query(make_client(httpx.Response(status)).search(), search_call(), SEARCH)
    with pytest.raises(RetryError):
        await run_mutation(make_client(httpx.Response(status)).save_object(), save_call(), RECORD)


@pytest.mark.asyncio
async def test_transport_failure_retries() -> None:
    def fail(request: httpx.Request) -> httpx.Response:
        raise httpx.ConnectError("refused", request=request)

    client = Client(
        Config(base_url="http://localhost:8848/1"),
        StaticCredentialProvider({CONNECTION: Credentials("TESTAPP", SecretString(KEY))}),
        httpx.AsyncClient(transport=httpx.MockTransport(fail)),
    )
    with pytest.raises(RetryError) as raised:
        await run_mutation(client.save_object(), save_call(), RECORD)
    assert raised.value.failure.kind == FailureKind.TRANSPORT


@pytest.mark.asyncio
@pytest.mark.parametrize("application_id", ["", "lowercase", "TEST-APP"])
async def test_invalid_application_id_is_defect_without_request(application_id: str) -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200, json=PAGE), requests, application_id=application_id).search(),
        search_call(),
        SEARCH,
    )
    assert result.branch == DEFECT
    assert result.failure is not None and result.failure.kind == FailureKind.AUTHENTICATION
    assert requests == []
