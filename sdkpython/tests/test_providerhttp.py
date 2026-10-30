# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded provider HTTP and positive request-dispatch evidence tests."""

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
        with pytest.raises(BodyTooLargeError):
            await read_bounded_body(observed, 4)


@pytest.mark.asyncio
async def test_tracing_fails_closed_without_positive_connection_evidence() -> None:
    """Only a traced connection failure before send qualifies as not sent."""
    trace = RequestTrace()
    await trace.observe("connection.connect_tcp.failed", {})
    assert trace.provably_not_sent
    await trace.observe("http11.send_request_headers.started", {})
    assert not trace.provably_not_sent

    def fail(_: httpx.Request) -> httpx.Response:
        raise httpx.ConnectError("secret-bearing upstream URL")

    async with new_provider_http_client(transport=httpx.MockTransport(fail)) as client:
        with pytest.raises(ProviderTransportError) as caught:
            await send_traced(client, client.build_request("POST", "https://provider.example", json={"x": 1}))
        assert not caught.value.provably_not_sent
        assert "secret-bearing" not in str(caught.value)


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
