# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider parity scenarios for the Cohere Queries."""

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
    run_query,
)

from dex_connectors.cohere import (
    DEFECT,
    EMBED_DEFINITION,
    EMBEDDED,
    INVALID_RESPONSE,
    PROVIDER_REJECTED,
    RANKED,
    RERANK_DEFINITION,
    Client,
    Config,
    Credentials,
    EmbedInput,
    RerankInput,
)

CONNECTION = ConnectionRef("cohere", "cohere")
KEY = "co-test"
EMBED = EmbedInput("embed-v4.0", ("hello",), "search_query")
RERANK = RerankInput("rerank-v5.0-pro", "q", ("a", "b"), 1)


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


def embed_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, EMBED_DEFINITION.operation)


def rerank_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, RERANK_DEFINITION.operation)


@pytest.mark.asyncio
async def test_embed_requests_float_vectors() -> None:
    requests: list[httpx.Request] = []
    body = {"id": "e1", "embeddings": {"float": [[0.1, 2]]}}
    result = await run_query(
        make_client(httpx.Response(200, json=body), requests).embed(), embed_call(), EMBED
    )
    assert result.branch == EMBEDDED
    assert result.value.embeddings == ((0.1, 2.0),)
    assert requests[0].url.path == "/v2/embed"
    assert json.loads(requests[0].content) == {
        "model": "embed-v4.0",
        "texts": ["hello"],
        "input_type": "search_query",
        "embedding_types": ["float"],
    }
    assert requests[0].headers["Authorization"] == "Bearer " + KEY


@pytest.mark.asyncio
async def test_rerank_keeps_ranked_order() -> None:
    requests: list[httpx.Request] = []
    body = {"results": [{"index": 1, "relevance_score": 0.9}, {"index": 0, "relevance_score": 0.1}]}
    result = await run_query(
        make_client(httpx.Response(200, json=body), requests).rerank(), rerank_call(), RERANK
    )
    assert result.branch == RANKED
    assert [item.index for item in result.value.results] == [1, 0]
    assert json.loads(requests[0].content) == {
        "model": "rerank-v5.0-pro",
        "query": "q",
        "documents": ["a", "b"],
        "top_n": 1,
    }


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "body",
    [
        {"id": "e1", "embeddings": {"float": []}},
        {"id": "", "embeddings": {"float": [[1]]}},
        {"id": "e1", "embeddings": {"float": [[]]}},
    ],
)
async def test_incomplete_embeddings_are_invalid(body: object) -> None:
    result = await run_query(make_client(httpx.Response(200, json=body)).embed(), embed_call(), EMBED)
    assert result.branch == INVALID_RESPONSE


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "body", [{"results": [{"index": 2}]}, {"results": [{"index": 0}, {"index": 0}]}, {"ranking": []}]
)
async def test_invalid_rankings(body: object) -> None:
    result = await run_query(make_client(httpx.Response(200, json=body)).rerank(), rerank_call(), RERANK)
    assert result.branch == INVALID_RESPONSE


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("status", "kind"),
    [
        (401, FailureKind.AUTHENTICATION),
        (498, FailureKind.AUTHENTICATION),
        (403, FailureKind.AUTHORIZATION),
        (400, FailureKind.PROVIDER_REJECTION),
    ],
)
async def test_rejections(status: int, kind: FailureKind) -> None:
    result = await run_query(make_client(httpx.Response(status)).embed(), embed_call(), EMBED)
    assert result.branch == PROVIDER_REJECTED
    assert result.failure is not None and result.failure.kind == kind


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("status", "kind"),
    [(429, FailureKind.RATE_LIMIT), (409, FailureKind.AVAILABILITY), (500, FailureKind.AVAILABILITY)],
)
async def test_safe_failures_retry(status: int, kind: FailureKind) -> None:
    with pytest.raises(RetryError) as raised:
        await run_query(make_client(httpx.Response(status)).rerank(), rerank_call(), RERANK)
    assert raised.value.failure.kind == kind


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("operation", "value"),
    [
        ("embed", EmbedInput("m", (), "search_query")),
        ("embed", EmbedInput("m", (" ",), "search_query")),
        ("embed", EmbedInput("m", ("x",), "other")),
        ("rerank", RerankInput("m", "q", ("a",), 2)),
        ("rerank", RerankInput("m", " ", ("a",))),
    ],
)
async def test_invalid_input_is_defect_without_request(operation: str, value: object) -> None:
    requests: list[httpx.Request] = []
    client = make_client(httpx.Response(200), requests)
    if operation == "embed":
        result = await run_query(client.embed(), embed_call(), value)  # type: ignore[arg-type]
    else:
        result = await run_query(client.rerank(), rerank_call(), value)  # type: ignore[arg-type]
    assert result.branch == DEFECT
    assert requests == []
