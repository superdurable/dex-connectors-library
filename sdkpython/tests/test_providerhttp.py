# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded provider HTTP and positive request-dispatch evidence tests."""

import asyncio
from collections.abc import AsyncIterator
from datetime import UTC, datetime, timedelta

import httpx
import pytest

from dex_connectors.sdk.providerhttp import (
    BodyTooLargeError,
    ProviderTransportError,
    RequestTrace,
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    read_error_tokens,
    send_traced,
    validate_base_url,
)


@pytest.mark.asyncio
async def test_redirect_not_followed_and_body_bounded() -> None:
    """A credential-bearing request never follows a provider redirect."""
    requests: list[httpx.Request] = []

    def response(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(302, headers={"Location": "https://other.example/steal"}, content=b"12345")

    async with new_provider_http_client(transport=httpx.MockTransport(response)) as client:
        observed = await client.get(
            "https://provider.example/resource", headers={"Authorization": "Bearer fake"}
        )
        assert observed.status_code == 302
        assert len(requests) == 1
        with pytest.raises(BodyTooLargeError) as oversized:
            await read_bounded_body(observed, 4)
        assert oversized.value.status_code == 302
        assert oversized.value.headers["Location"] == "https://other.example/steal"
        assert "https://other.example/steal" not in str(oversized.value)


@pytest.mark.asyncio
async def test_tracing_fails_closed_without_positive_connection_evidence() -> None:
    """Only a traced connection failure before send qualifies as not sent."""
    trace = RequestTrace()
    await trace.observe("connection.connect_tcp.failed", {})
    assert trace.provably_not_sent
    await trace.observe("http11.send_request_headers.started", {})
    assert not trace.provably_not_sent
    tls_trace = RequestTrace()
    await tls_trace.observe("connection.start_tls.failed", {})
    assert tls_trace.provably_not_sent
    await tls_trace.observe("http11.send_request_headers.started", {})
    assert not tls_trace.provably_not_sent

    def fail(_: httpx.Request) -> httpx.Response:
        raise httpx.ConnectError("secret-bearing upstream URL")

    async with new_provider_http_client(transport=httpx.MockTransport(fail)) as client:
        with pytest.raises(ProviderTransportError) as caught:
            await send_traced(client, client.build_request("POST", "https://provider.example", json={"x": 1}))
        assert not caught.value.provably_not_sent
        assert "secret-bearing" not in str(caught.value)


@pytest.mark.asyncio
async def test_real_connection_failures_are_provably_not_sent() -> None:
    """Refused TCP, DNS, and failed TLS handshakes cannot carry HTTP bytes."""
    refused_server = await asyncio.start_server(lambda _reader, _writer: None, "127.0.0.1", 0)
    refused_port = refused_server.sockets[0].getsockname()[1]
    refused_server.close()
    await refused_server.wait_closed()

    async def reject_tls(reader: asyncio.StreamReader, writer: asyncio.StreamWriter) -> None:
        await reader.read(1024)
        writer.write(b"HTTP/1.1 400 Bad Request\r\nContent-Length: 0\r\n\r\n")
        await writer.drain()
        writer.close()
        await writer.wait_closed()

    tls_server = await asyncio.start_server(reject_tls, "127.0.0.1", 0)
    tls_port = tls_server.sockets[0].getsockname()[1]
    try:
        async with new_provider_http_client(2, trust_env=False) as client:
            for url in (
                f"http://127.0.0.1:{refused_port}/",
                "https://nonexistent.invalid/",
                f"https://127.0.0.1:{tls_port}/",
            ):
                with pytest.raises(ProviderTransportError) as caught:
                    await send_traced(client, client.build_request("POST", url, content=b"write"))
                assert caught.value.provably_not_sent
    finally:
        tls_server.close()
        await tls_server.wait_closed()


@pytest.mark.asyncio
async def test_reset_after_request_headers_is_possibly_sent() -> None:
    """A server reset after receiving headers cannot prove an unkeyed write safe."""

    async def reset_after_headers(reader: asyncio.StreamReader, writer: asyncio.StreamWriter) -> None:
        await reader.readuntil(b"\r\n\r\n")
        writer.transport.abort()

    server = await asyncio.start_server(reset_after_headers, "127.0.0.1", 0)
    port = server.sockets[0].getsockname()[1]
    try:
        async with new_provider_http_client(2, trust_env=False) as client:
            with pytest.raises(ProviderTransportError) as caught:
                request = client.build_request("POST", f"http://127.0.0.1:{port}/", content=b"write")
                await send_traced(client, request)
            assert not caught.value.provably_not_sent
    finally:
        server.close()
        await server.wait_closed()


def test_error_tokens_retry_after_and_addresses() -> None:
    """Parse safe tokens and bounded retry hints without leaking response text."""
    assert read_error_tokens(
        b'{"error":{"code":"rate_limit","message":"secret text"}}', ("/error/code", "/error/message")
    ) == ("rate_limit",)
    now = datetime(2026, 10, 29, tzinfo=UTC)
    assert parse_retry_after("99999", now) == 3600
    assert parse_retry_after("bad", now) == 0
    assert parse_retry_after((now + timedelta(seconds=7)).strftime("%a, %d %b %Y %H:%M:%S GMT"), now) == 7
    assert validate_base_url("http://127.0.0.1:8080/") == "http://127.0.0.1:8080"
    with pytest.raises(ValueError):
        validate_base_url("http://provider.example")
    with pytest.raises(ValueError):
        validate_base_url("https://provider.example?token=")
    with pytest.raises(ValueError):
        validate_base_url("https://@provider.example")
    assert is_header_safe_credential("abc|123")
    assert not is_header_safe_credential("abc\n123")


def test_retry_after_and_token_parser_reject_ambiguous_values() -> None:
    """Invalid and past hints fall back to Step policy; error prose is discarded."""
    now = datetime(2026, 10, 29, tzinfo=UTC)
    assert parse_retry_after("-1", now) == 0
    assert parse_retry_after("1.5", now) == 0
    assert parse_retry_after("Thu, 28 Oct 2026 00:00:00 GMT", now) == 0
    assert read_error_tokens(b"not-json", ("/error/code",)) == ()
    assert read_error_tokens(b'{"error":{"code":"SECRET TOKEN WITH SPACES"}}', ("/error/code",)) == ()
    assert read_error_tokens(b'{"errors":[{"code":"busy"}]}', ("/errors/0/code",)) == ("busy",)
    assert read_error_tokens(b'{"value":1.50,"power":1e3}', ("/value", "/power")) == ("1.50", "1e3")


@pytest.mark.asyncio
async def test_bounded_reader_closes_stream_on_overflow_and_transport_error() -> None:
    """A failed bounded read releases its streaming connection."""

    class Stream(httpx.AsyncByteStream):
        def __init__(self, fail: bool) -> None:
            self.fail = fail
            self.closed = False

        async def __aiter__(self) -> AsyncIterator[bytes]:
            if self.fail:
                raise httpx.ReadError("private provider details")
            yield b"12345"

        async def aclose(self) -> None:
            self.closed = True

    overflow = Stream(False)
    with pytest.raises(BodyTooLargeError):
        await read_bounded_body(httpx.Response(200, stream=overflow), 4)
    assert overflow.closed
    failed = Stream(True)
    with pytest.raises(ProviderTransportError) as caught:
        await read_bounded_body(httpx.Response(200, stream=failed), 4)
    assert failed.closed
    assert "private provider" not in str(caught.value)
