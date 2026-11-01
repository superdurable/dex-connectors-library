# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider parity scenarios for the retrieveResponse Query."""

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

from dex_connectors.openai import (
    DEFECT,
    FOUND,
    INVALID_RESPONSE,
    NOT_FOUND,
    PROVIDER_REJECTED,
    RETRIEVE_RESPONSE_DEFINITION,
    Client,
    Config,
    Credentials,
    RetrieveRequest,
)

CONNECTION = ConnectionRef("openai", "openai")


def make_client(response: httpx.Response, requests: list[httpx.Request] | None = None) -> Client:
    def respond(request: httpx.Request) -> httpx.Response:
        if requests is not None:
            requests.append(request)
        return response

    return Client(
        Config(endpoint="http://127.0.0.1:8848/v1/", max_response_bytes=300),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString("sk-test"))}),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )


def call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, RETRIEVE_RESPONSE_DEFINITION.operation)


@pytest.mark.asyncio
async def test_found_escapes_id_and_records_receipt() -> None:
    requests: list[httpx.Request] = []
    body = {"id": "resp_1", "status": "completed", "output": [], "usage": {"total_tokens": 5}}
    client = make_client(
        httpx.Response(
            200, json=body, headers={"X-Request-Id": "req_9", "x-ratelimit-remaining-requests": "7"}
        ),
        requests,
    )
    result = await run_query(client.retrieve_response(), call(), RetrieveRequest("resp_1"))
    assert result.branch == FOUND
    assert result.value.usage.total_tokens == 5
    assert result.receipt.provider_request_id == "req_9"
    assert result.receipt.metadata == {"x-ratelimit-remaining-requests": "7"}
    await run_query(client.retrieve_response(), call(), RetrieveRequest("a/b"))
    assert requests[1].url.raw_path == b"/v1/responses/a%2Fb"


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "branch", "kind"),
    [
        (httpx.Response(404), NOT_FOUND, FailureKind.NOT_FOUND),
        (httpx.Response(401), PROVIDER_REJECTED, FailureKind.AUTHENTICATION),
        (httpx.Response(409), PROVIDER_REJECTED, FailureKind.CONFLICT),
        (httpx.Response(200, content=b"[1]"), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (httpx.Response(200, json={"id": 1}), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (httpx.Response(200, content=b"x" * 400), INVALID_RESPONSE, FailureKind.RESPONSE_TOO_LARGE),
    ],
)
async def test_terminal_branches(response: httpx.Response, branch: str, kind: FailureKind) -> None:
    result = await run_query(make_client(response).retrieve_response(), call(), RetrieveRequest("resp_1"))
    assert result.branch == branch
    assert result.failure is not None and result.failure.kind == kind


@pytest.mark.asyncio
@pytest.mark.parametrize("status", [408, 429, 500, 502, 503])
async def test_safe_failures_retry(status: int) -> None:
    with pytest.raises(RetryError):
        await run_query(
            make_client(httpx.Response(status)).retrieve_response(), call(), RetrieveRequest("resp_1")
        )


@pytest.mark.asyncio
async def test_blank_id_is_defect_without_request() -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200), requests).retrieve_response(), call(), RetrieveRequest()
    )
    assert result.branch == DEFECT
    assert requests == []


@pytest.mark.parametrize(
    "config",
    [
        Config(endpoint="http://example.com/v1"),
        Config(endpoint="/v1"),
        Config(model="bad model"),
        Config(max_sse_event_bytes=-1),
    ],
)
def test_client_rejects_unsafe_configuration(config: Config) -> None:
    with pytest.raises(ValueError):
        Client(config, StaticCredentialProvider({CONNECTION: Credentials(SecretString("sk-test"))}))
