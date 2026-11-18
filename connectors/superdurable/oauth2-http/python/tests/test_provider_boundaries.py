# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Go-compatible compressed responses, query parsing, and timeout phases."""

import asyncio
import gzip
from collections.abc import AsyncIterator
from types import SimpleNamespace

import httpx
import pytest
from dex_connectors.sdk import FailureKind

from dex_connectors.superdurable_oauth2_http import Config
from dex_connectors.superdurable_oauth2_http.client import (
    Client,
    SendRequestOperation,
    _Exchange,
    _parse_go_query,
    _RequestPlan,
)


def _client(http_client: httpx.AsyncClient) -> Client:
    return Client(
        Config(
            authorization_endpoint="http://127.0.0.1/auth",
            token_endpoint="http://127.0.0.1/token",
            scopes=("profile",),
            api_base_url="http://127.0.0.1/v1",
        ),
        object(),  # type: ignore[arg-type]
        http_client,
    )


def test_go_query_discards_semicolon_and_invalid_escape_pairs() -> None:
    assert _parse_go_query("bad=1;evil=2&ok=a+b&broken=%GG") == [("ok", "a b")]


@pytest.mark.asyncio
async def test_gzip_body_is_decoded_before_bounding() -> None:
    payload = b"decoded provider response"
    async with httpx.AsyncClient(
        transport=httpx.MockTransport(
            lambda _: httpx.Response(
                200, headers={"Content-Encoding": "gzip"}, content=gzip.compress(payload)
            )
        )
    ) as http_client:
        answer = await _client(http_client)._exchange(
            SimpleNamespace(idempotency_key="call-id"),  # type: ignore[arg-type]
            _RequestPlan("GET", "http://127.0.0.1/v1/items", {}, "", ""),
            "synthetic-token",
            asyncio.get_running_loop().time() + 1,
        )
    assert answer.body == payload
    assert not answer.error


@pytest.mark.parametrize("status", [302, 303, 307])
def test_redirect_rejects_a_read_and_repeats_a_write(status: int) -> None:
    operation = SendRequestOperation(_client(httpx.AsyncClient()))
    call = SimpleNamespace(id="call-id", idempotency_key="call-id")
    location = httpx.Headers({"Location": "http://outside.example/"})
    answer = _Exchange(status, location, b"", is_dispatched=True)
    for method in ("GET", "HEAD", "OPTIONS"):
        plan = _RequestPlan(method, "http://127.0.0.1/v1/items", {}, "", "")
        attempt = operation._attempt_for(call, plan, answer, ["synthetic-token"])  # type: ignore[arg-type]
        assert attempt.branch == "rejected", method
    for method in ("POST", "PUT", "PATCH", "DELETE"):
        plan = _RequestPlan(method, "http://127.0.0.1/v1/items", {}, "{}", "")
        attempt = operation._attempt_for(call, plan, answer, ["synthetic-token"])  # type: ignore[arg-type]
        assert attempt.kind == "retry", method
        assert attempt.failure is not None and attempt.failure.kind == FailureKind.PROTOCOL


class _SlowBody(httpx.AsyncByteStream):
    async def __aiter__(self) -> AsyncIterator[bytes]:
        await asyncio.sleep(0.05)
        yield b"late"


@pytest.mark.asyncio
async def test_deadline_after_post_response_is_dispatched() -> None:
    async with httpx.AsyncClient(
        transport=httpx.MockTransport(lambda _: httpx.Response(200, stream=_SlowBody()))
    ) as http_client:
        answer = await _client(http_client)._exchange(
            SimpleNamespace(idempotency_key="call-id"),  # type: ignore[arg-type]
            _RequestPlan("POST", "http://127.0.0.1/v1/items", {}, "{}", ""),
            "synthetic-token",
            asyncio.get_running_loop().time() + 0.01,
        )
    assert answer.error and answer.is_dispatched


class _CancelledBody(httpx.AsyncByteStream):
    async def __aiter__(self) -> AsyncIterator[bytes]:
        raise asyncio.CancelledError
        yield b""  # pragma: no cover


@pytest.mark.asyncio
async def test_real_cancellation_propagates() -> None:
    async with httpx.AsyncClient(
        transport=httpx.MockTransport(lambda _: httpx.Response(200, stream=_CancelledBody()))
    ) as http_client:
        with pytest.raises(asyncio.CancelledError):
            await _client(http_client)._exchange(
                SimpleNamespace(idempotency_key="call-id"),  # type: ignore[arg-type]
                _RequestPlan("POST", "http://127.0.0.1/v1/items", {}, "{}", ""),
                "synthetic-token",
                asyncio.get_running_loop().time() + 1,
            )
