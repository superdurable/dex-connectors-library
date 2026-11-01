# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider parity scenarios for the Pinecone operations."""

import json
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

from dex_connectors.pinecone import (
    API_VERSION,
    DEFECT,
    INVALID_RESPONSE,
    PROVIDER_REJECTED,
    QUERIED,
    QUERY_VECTORS_DEFINITION,
    UPSERT_VECTORS_DEFINITION,
    UPSERTED,
    Client,
    Config,
    Credentials,
    QueryVectorsInput,
    UpsertVectorsInput,
    Vector,
)

CONNECTION = ConnectionRef("pinecone", "pinecone")
KEY = "pinecone-test-key"
UPSERT = UpsertVectorsInput(
    "example", (Vector("vec-meridian", (0.1, 0.2), {"genre": "docs"}), Vector("vec-2", (1.0,)))
)
QUERY = QueryVectorsInput("example", (0.1, 0.2), 3, True)
MATCHES = {"matches": [{"id": "vec-meridian", "score": 0.98}], "namespace": "example"}


def make_client(
    response: httpx.Response, requests: list[httpx.Request] | None = None, key: str = KEY
) -> Client:
    def respond(request: httpx.Request) -> httpx.Response:
        if requests is not None:
            requests.append(request)
        return response

    return Client(
        Config("http://localhost:8848/", max_response_bytes=300),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString(key))}),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )


def upsert_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, UPSERT_VECTORS_DEFINITION.operation)


def query_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, QUERY_VECTORS_DEFINITION.operation)


@pytest.mark.asyncio
async def test_upsert_sends_go_shaped_body_and_headers() -> None:
    requests: list[httpx.Request] = []
    result = await run_mutation(
        make_client(httpx.Response(200, json={"upsertedCount": 2}), requests).upsert_vectors(),
        upsert_call(),
        UPSERT,
    )
    assert result.branch == UPSERTED
    assert result.value.upserted_count == 2
    request = requests[0]
    assert request.method == "POST"
    assert str(request.url) == "http://localhost:8848/vectors/upsert"
    assert request.headers["Api-Key"] == KEY
    assert request.headers["X-Pinecone-Api-Version"] == API_VERSION
    assert request.headers["User-Agent"] == "superdurable-pinecone/0.1"
    assert request.headers["Content-Type"] == "application/json"
    assert request.content == (
        b'{"namespace":"example","vectors":[{"id":"vec-meridian","values":[0.1,0.2],"metadata":{"genre":"docs"}},'
        b'{"id":"vec-2","values":[1]}]}'
    )


@pytest.mark.asyncio
async def test_query_sends_go_shaped_body_and_keeps_provider_json() -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200, json=MATCHES), requests).query_vectors(), query_call(), QUERY
    )
    assert result.branch == QUERIED
    assert result.value.data == MATCHES
    assert str(requests[0].url) == "http://localhost:8848/query"
    assert (
        requests[0].content == b'{"namespace":"example","vector":[0.1,0.2],"topK":3,"includeMetadata":true}'
    )


@pytest.mark.asyncio
async def test_query_omits_blank_namespace_and_false_metadata() -> None:
    requests: list[httpx.Request] = []
    await run_query(
        make_client(httpx.Response(200, json=MATCHES), requests).query_vectors(),
        query_call(),
        QueryVectorsInput(vector=(0.5,), top_k=1),
    )
    assert json.loads(requests[0].content) == {"vector": [0.5], "topK": 1}


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "value",
    [
        UpsertVectorsInput(),
        UpsertVectorsInput(vectors=tuple(Vector(str(index), (1.0,)) for index in range(1001))),
        UpsertVectorsInput(vectors=(Vector("", (1.0,)),)),
        UpsertVectorsInput(vectors=(Vector("a", ()),)),
        UpsertVectorsInput(vectors=(Vector("a", (1.0,)), Vector("a", (2.0,)))),
        UpsertVectorsInput(vectors=(Vector("a", (float("nan"),)),)),
        UpsertVectorsInput(vectors=(Vector("a", (1e39,)),)),
    ],
)
async def test_invalid_upsert_is_defect_without_request(value: UpsertVectorsInput) -> None:
    requests: list[httpx.Request] = []
    result = await run_mutation(
        make_client(httpx.Response(200, json={"upsertedCount": 1}), requests).upsert_vectors(),
        upsert_call(),
        value,
    )
    assert result.branch == DEFECT
    assert result.failure is not None and result.failure.kind == FailureKind.VALIDATION
    assert requests == []


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "value",
    [
        QueryVectorsInput(vector=(), top_k=1),
        QueryVectorsInput(vector=(1.0,)),
        QueryVectorsInput(vector=(1.0,), top_k=10001),
    ],
)
async def test_invalid_query_is_defect_without_request(value: QueryVectorsInput) -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200, json=MATCHES), requests).query_vectors(), query_call(), value
    )
    assert result.branch == DEFECT
    assert requests == []


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "branch", "kind"),
    [
        (httpx.Response(400), PROVIDER_REJECTED, FailureKind.VALIDATION),
        (httpx.Response(401), PROVIDER_REJECTED, FailureKind.AUTHENTICATION),
        (httpx.Response(403), PROVIDER_REJECTED, FailureKind.AUTHORIZATION),
        (httpx.Response(404), PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION),
        (httpx.Response(200, json={}), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (httpx.Response(200, json={"upsertedCount": 3}), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (httpx.Response(200, json={"upsertedCount": 1.5}), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (httpx.Response(200, content=b"{"), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (httpx.Response(200, content=b"x" * 400), INVALID_RESPONSE, FailureKind.RESPONSE_TOO_LARGE),
    ],
)
async def test_upsert_branches(response: httpx.Response, branch: str, kind: FailureKind) -> None:
    result = await run_mutation(make_client(response).upsert_vectors(), upsert_call(), UPSERT)
    assert result.branch == branch
    assert result.failure is not None and result.failure.kind == kind


@pytest.mark.asyncio
@pytest.mark.parametrize("body", [{"matches": None}, {"matches": {}}, [1]])
async def test_query_requires_matches_list(body: object) -> None:
    result = await run_query(make_client(httpx.Response(200, json=body)).query_vectors(), query_call(), QUERY)
    assert result.branch == INVALID_RESPONSE


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("status", "kind"),
    [
        (408, FailureKind.AVAILABILITY),
        (409, FailureKind.AVAILABILITY),
        (429, FailureKind.RATE_LIMIT),
        (503, FailureKind.AVAILABILITY),
    ],
)
async def test_safe_failures_retry(status: int, kind: FailureKind) -> None:
    with pytest.raises(RetryError) as raised:
        await run_mutation(make_client(httpx.Response(status)).upsert_vectors(), upsert_call(), UPSERT)
    assert raised.value.failure.kind == kind


@pytest.mark.asyncio
async def test_transport_failure_retries() -> None:
    def fail(request: httpx.Request) -> httpx.Response:
        raise httpx.ConnectError("refused", request=request)

    client = Client(
        Config("http://localhost:8848"),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString(KEY))}),
        httpx.AsyncClient(transport=httpx.MockTransport(fail)),
    )
    with pytest.raises(RetryError) as raised:
        await run_query(client.query_vectors(), query_call(), QUERY)
    assert raised.value.failure.kind == FailureKind.AVAILABILITY


@pytest.mark.asyncio
async def test_blank_key_is_defect_without_request() -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200, json=MATCHES), requests, key="").query_vectors(), query_call(), QUERY
    )
    assert result.branch == DEFECT
    assert result.failure is not None and result.failure.kind == FailureKind.AUTHENTICATION
    assert requests == []


def test_base_url_is_required() -> None:
    with pytest.raises(ValueError):
        Client(Config(""), StaticCredentialProvider({}))
