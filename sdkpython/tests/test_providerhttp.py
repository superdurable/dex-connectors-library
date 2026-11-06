# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded provider HTTP and positive request-dispatch evidence tests."""

import asyncio
import ssl
import subprocess
from collections.abc import AsyncIterator
from datetime import UTC, datetime, timedelta
from importlib.metadata import requires
from pathlib import Path
from typing import Any

import httpcore
import httpx
import pytest
from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import rsa
from cryptography.x509.oid import NameOID

from dex_connectors.sdk.providerhttp import (
    AddressPolicy,
    AddressRefusal,
    BodyTooLargeError,
    ProviderTransportError,
    RequestTrace,
    address_policy,
    encode_query_values,
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    read_error_tokens,
    send_traced,
    validate_base_url,
)


def test_query_values_match_go_url_values_encode() -> None:
    """Go-derived keys, repeated values, punctuation, Unicode, and empty lists match exactly."""
    assert encode_query_values({"z": ["~ * ! ' ( ) + / ? & = ☃"], "a": ["one", "two"]}) == (
        "a=one&a=two&z=~+%2A+%21+%27+%28+%29+%2B+%2F+%3F+%26+%3D+%E2%98%83"
    )
    assert encode_query_values({"é": ["café"], "a": ["雪"]}) == "a=%E9%9B%AA&%C3%A9=caf%C3%A9"
    assert encode_query_values({"empty": [], "x": [""]}) == "x="


def test_query_values_reject_plain_string_instead_of_iterating_characters() -> None:
    """A plain string is not a list of query values at runtime."""
    with pytest.raises(TypeError, match="sequence of strings"):
        encode_query_values({"x": "abc"})


@pytest.mark.parametrize(
    "addresses,private_http,private_https,public_https",
    [
        (["127.0.0.1"], True, True, False),
        (["::1"], True, True, False),
        (["10.1.2.3"], True, True, False),
        (["172.16.0.5"], True, True, False),
        (["172.31.255.255"], True, True, False),
        (["192.168.1.1"], True, True, False),
        (["fc00::1"], True, True, False),
        (["fd12:3456::1"], True, True, False),
        (["10.0.0.1", "192.168.0.2", "::1"], True, True, False),
        (["::ffff:10.0.0.1"], True, True, False),
        (["::ffff:8.8.8.8"], False, True, True),
        (["8.8.8.8"], False, True, True),
        (["2001:4860:4860::8888"], False, True, True),
        (["8.8.8.8", "1.1.1.1"], False, True, True),
        (["100.64.0.1"], False, True, False),
        (["10.0.0.1", "8.8.8.8"], False, True, False),
        (["8.8.8.8", "10.0.0.1"], False, True, False),
        (["169.254.169.254"], False, False, False),
        (["169.254.0.1"], False, False, False),
        (["fe80::1"], False, False, False),
        (["febf::1"], False, False, False),
        (["::ffff:169.254.169.254"], False, False, False),
        (["fd00:ec2::254"], False, False, False),
        (["127.0.0.1", "169.254.169.254"], False, False, False),
        (["8.8.8.8", "169.254.169.254"], False, False, False),
        (["0.0.0.0"], False, False, False),
        (["::"], False, False, False),
        (["224.0.0.1"], False, False, False),
        (["ff02::1"], False, False, False),
        (["255.255.255.255"], False, False, False),
    ],
)
def test_go_address_policy_table(
    addresses: list[str], private_http: bool, private_https: bool, public_https: bool
) -> None:
    """Replay Go's address sets plus the broadcast refusal shared with sdkts."""
    scenarios: tuple[tuple[str, AddressPolicy, bool], ...] = (
        ("http", "privateNetworkHTTP", private_http),
        ("https", "privateNetworkHTTP", private_https),
        ("http", "publicHTTPS", False),
        ("https", "publicHTTPS", public_https),
    )
    for scheme, policy, expected in scenarios:
        is_allowed = all(not address_policy._refusal_reason(address, scheme, policy) for address in addresses)
        assert is_allowed == expected


def test_supported_httpx_floor_excludes_legacy_proxy_mounts() -> None:
    requirements = requires("dex-connectors-sdk") or []
    assert any(requirement.startswith("httpx>=0.28") for requirement in requirements)


@pytest.mark.asyncio
async def test_policy_checks_all_dns_answers_and_pins_checked_address(monkeypatch: Any) -> None:
    """Mixed DNS results refuse before dial; allowed results dial an IP, never the hostname."""
    answers = ["127.0.0.1", "169.254.169.254"]
    dialed: list[str] = []

    async def resolve(host: str, port: int) -> list[str]:
        assert host == "provider.test"
        return answers

    async def connect(self: Any, host: str, port: int, **kwargs: Any) -> Any:
        dialed.append(host)
        raise httpcore.ConnectError("local test dial")

    monkeypatch.setattr(address_policy, "_resolve_addresses", resolve)
    monkeypatch.setattr(httpcore.AnyIOBackend, "connect_tcp", connect)
    async with new_provider_http_client(address_policy="privateNetworkHTTP") as client:
        with pytest.raises(AddressRefusal):
            await client.get("http://provider.test/")
        assert not dialed
        answers[:] = ["::ffff:127.0.0.1"]
        with pytest.raises(httpx.ConnectError):
            await client.get("http://provider.test/")
        assert dialed == ["127.0.0.1"]


@pytest.mark.asyncio
async def test_policy_dns_failure_is_provably_not_sent(monkeypatch: Any) -> None:
    async def fail(host: str, port: int) -> list[str]:
        raise httpcore.ConnectError("provider address resolution failed")

    monkeypatch.setattr(address_policy, "_resolve_addresses", fail)
    async with new_provider_http_client(address_policy="publicHTTPS") as client:
        with pytest.raises(ProviderTransportError) as caught:
            request = client.build_request("POST", "https://provider.test/", content=b"write")
            await send_traced(client, request)
        assert caught.value.provably_not_sent


@pytest.mark.asyncio
async def test_restricted_policy_accepts_only_safe_client_options() -> None:
    with pytest.raises(ValueError, match="address policy is invalid"):
        new_provider_http_client(address_policy="unknown")  # type: ignore[arg-type]
    unsafe_options = {
        "transport": httpx.MockTransport(lambda request: httpx.Response(200)),
        "proxy": "http://127.0.0.1:3128",
        "proxies": "http://127.0.0.1:3128",
        "mounts": {},
        "app": object(),
    }
    for name, value in unsafe_options.items():
        unsafe_option: dict[str, Any] = {name: value}
        with pytest.raises(ValueError, match="unsupported client option"):
            new_provider_http_client(address_policy="publicHTTPS", **unsafe_option)
    with pytest.raises(ValueError, match="environment proxies"):
        new_provider_http_client(address_policy="publicHTTPS", trust_env=True)
    client = new_provider_http_client(address_policy="publicHTTPS", headers={"X-Test": "yes"})
    assert client.headers["X-Test"] == "yes"
    await client.aclose()


@pytest.mark.asyncio
async def test_policy_dns_resolution_obeys_connect_timeout(monkeypatch: Any) -> None:
    """A slow resolver exhausts the connect budget before any HTTP bytes are sent."""
    dialed = False

    async def resolve(host: str, port: int) -> list[str]:
        await asyncio.sleep(1)
        return ["127.0.0.1"]

    async def connect(self: Any, host: str, port: int, **kwargs: Any) -> Any:
        nonlocal dialed
        dialed = True
        raise AssertionError("resolver timeout should precede dial")

    monkeypatch.setattr(address_policy, "_resolve_addresses", resolve)
    monkeypatch.setattr(httpcore.AnyIOBackend, "connect_tcp", connect)
    async with new_provider_http_client(0.05, address_policy="publicHTTPS") as client:
        with pytest.raises(ProviderTransportError) as caught:
            await send_traced(client, client.build_request("POST", "https://provider.test/"))
        assert caught.value.provably_not_sent
        assert not dialed


@pytest.mark.asyncio
async def test_policy_dial_attempts_share_connect_timeout(monkeypatch: Any) -> None:
    """A second address gets only the connect budget left after the first dial."""
    dial_timeouts: list[float] = []

    async def resolve(host: str, port: int) -> list[str]:
        return ["127.0.0.1", "127.0.0.2"]

    async def connect(self: Any, host: str, port: int, **kwargs: Any) -> Any:
        dial_timeouts.append(kwargs["timeout"])
        await asyncio.sleep(0.04)
        raise httpcore.ConnectError("local test dial")

    monkeypatch.setattr(address_policy, "_resolve_addresses", resolve)
    monkeypatch.setattr(httpcore.AnyIOBackend, "connect_tcp", connect)
    async with new_provider_http_client(0.06, address_policy="privateNetworkHTTP") as client:
        with pytest.raises(ProviderTransportError) as caught:
            await send_traced(client, client.build_request("POST", "http://provider.test/"))
        assert caught.value.provably_not_sent
    assert len(dial_timeouts) == 2
    assert 0 < dial_timeouts[1] < dial_timeouts[0]


@pytest.mark.asyncio
async def test_restricted_pool_uses_default_limits_and_checked_backend(monkeypatch: Any) -> None:
    client = new_provider_http_client(address_policy="publicHTTPS")
    transport = client._transport
    assert isinstance(transport, address_policy._AddressPolicyTransport)
    pool = transport._transport._pool
    assert pool._max_connections == 100
    assert pool._max_keepalive_connections == 20
    assert isinstance(pool._network_backend, address_policy._AddressPolicyBackend)
    await client.aclose()

    class IncompatibleTransport:
        def __init__(self, **kwargs: Any) -> None:
            self._pool = object()

    monkeypatch.setattr(httpx, "AsyncHTTPTransport", IncompatibleTransport)
    with pytest.raises(RuntimeError, match="compatible httpcore pool"):
        new_provider_http_client(address_policy="publicHTTPS")


@pytest.mark.asyncio
@pytest.mark.parametrize("certificate_env", [None, "SSL_CERT_FILE", "SSL_CERT_DIR"])
async def test_tls_uses_original_hostname_for_sni_and_certificate(
    monkeypatch: Any, tmp_path: Path, certificate_env: str | None
) -> None:
    """The checked loopback IP is dialed while SNI and certificate checks use provider.test."""
    key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    subject = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, "provider.test")])
    certificate = (
        x509.CertificateBuilder()
        .subject_name(subject)
        .issuer_name(subject)
        .public_key(key.public_key())
        .serial_number(x509.random_serial_number())
        .not_valid_before(datetime.now(UTC) - timedelta(days=1))
        .not_valid_after(datetime.now(UTC) + timedelta(days=1))
        .add_extension(x509.SubjectAlternativeName([x509.DNSName("provider.test")]), critical=False)
        .sign(key, hashes.SHA256())
    )
    certificate_pem = certificate.public_bytes(serialization.Encoding.PEM)
    certificate_path = tmp_path / "server.crt"
    key_path = tmp_path / "server.key"
    certificate_path.write_bytes(certificate_pem)
    key_path.write_bytes(key.private_bytes(
        serialization.Encoding.PEM, serialization.PrivateFormat.TraditionalOpenSSL,
        serialization.NoEncryption(),
    ))
    observed_sni: list[str | None] = []
    server_context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    server_context.load_cert_chain(str(certificate_path), str(key_path))
    server_context.set_servername_callback(lambda _socket, name, _context: observed_sni.append(name))
    observed_host: list[str] = []

    async def reply(reader: asyncio.StreamReader, writer: asyncio.StreamWriter) -> None:
        headers = await reader.readuntil(b"\r\n\r\n")
        observed_host.append(headers.decode("ascii").split("Host: ", 1)[1].split("\r\n", 1)[0])
        writer.write(b"HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
        await writer.drain()
        writer.close()
        await writer.wait_closed()

    server = await asyncio.start_server(reply, "127.0.0.1", 0, ssl=server_context)
    port = server.sockets[0].getsockname()[1]

    async def resolve(host: str, requested_port: int) -> list[str]:
        assert host == "provider.test" and requested_port == port
        return ["127.0.0.1"]

    monkeypatch.setattr(address_policy, "_resolve_addresses", resolve)
    monkeypatch.delenv("SSL_CERT_FILE", raising=False)
    monkeypatch.delenv("SSL_CERT_DIR", raising=False)
    monkeypatch.setenv("HTTPS_PROXY", "http://127.0.0.1:1")
    monkeypatch.setenv("NO_PROXY", "")
    if certificate_env == "SSL_CERT_FILE":
        monkeypatch.setenv(certificate_env, str(certificate_path))
    elif certificate_env == "SSL_CERT_DIR":
        certificate_directory = tmp_path / "trust"
        certificate_directory.mkdir()
        subject_hash = subprocess.check_output(
            ["openssl", "x509", "-hash", "-noout", "-in", str(certificate_path)], text=True
        ).strip()
        (certificate_directory / f"{subject_hash}.0").symlink_to(certificate_path)
        monkeypatch.setenv(certificate_env, str(certificate_directory))
    client_context = (
        ssl.create_default_context(cafile=str(certificate_path)) if certificate_env is None else True
    )
    try:
        async with new_provider_http_client(
            address_policy="privateNetworkHTTP", verify=client_context
        ) as client:
            response = await client.get(f"https://provider.test:{port}/")
            assert response.text == "ok"
        assert observed_sni == ["provider.test"]
        assert observed_host == [f"provider.test:{port}"]
    finally:
        server.close()
        await server.wait_closed()


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
        await read_bounded_body(httpx.Response(200, headers={"X-Request-ID": "received"}, stream=failed), 4)
    assert failed.closed
    assert caught.value.status_code == 200
    assert caught.value.headers is not None
    assert caught.value.headers["X-Request-ID"] == "received"
    assert "private provider" not in str(caught.value)
