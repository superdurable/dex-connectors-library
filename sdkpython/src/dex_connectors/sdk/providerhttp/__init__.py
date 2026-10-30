# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded, redirect-safe async provider HTTP helpers."""

import ipaddress
import json
import re
from dataclasses import dataclass
from datetime import UTC, datetime
from email.utils import parsedate_to_datetime
from typing import Any
from urllib.parse import urlsplit, urlunsplit

import httpx

MAX_ERROR_BODY_BYTES = 64 << 10
MAX_RETRY_AFTER_DELAY_SECONDS = 3600
_ERROR_TOKEN = re.compile(r"^[A-Za-z0-9_.:-]{1,64}$")


class BodyTooLargeError(ValueError):
    """Provider body exceeded its configured byte limit."""


class ProviderTransportError(OSError):
    """Secret-safe transport failure with positive not-sent evidence when available."""

    def __init__(self, provably_not_sent: bool) -> None:
        self.provably_not_sent = provably_not_sent
        super().__init__("provider request failed")


@dataclass(slots=True)
class RequestTrace:
    """Trace an HTTP request and fail closed when dispatch evidence is incomplete."""

    connection_failed_before_send: bool = False
    request_headers_started: bool = False

    async def observe(self, event: str, info: dict[str, Any]) -> None:
        """Record httpcore trace events without retaining request data."""
        del info
        if event.endswith("send_request_headers.started"):
            self.request_headers_started = True
        if event.endswith("connect_tcp.failed") or event.endswith("connect_unix_socket.failed"):
            if not self.request_headers_started:
                self.connection_failed_before_send = True

    @property
    def provably_not_sent(self) -> bool:
        """Report only a traced pre-dispatch connection failure as not sent."""
        return self.connection_failed_before_send and not self.request_headers_started


def new_provider_http_client(request_timeout_seconds: float = 30, **options: Any) -> httpx.AsyncClient:
    """Create an async client that returns redirects without replaying credentials."""
    if request_timeout_seconds <= 0:
        raise ValueError("provider request timeout must be positive")
    if "follow_redirects" in options or "timeout" in options:
        raise ValueError("redirect and timeout policy are owned by providerhttp")
    return httpx.AsyncClient(follow_redirects=False, timeout=request_timeout_seconds, **options)


async def send_traced(
    client: httpx.AsyncClient, request: httpx.Request
) -> tuple[httpx.Response, RequestTrace]:
    """Send once and expose positive pre-dispatch evidence on transport errors."""
    trace = RequestTrace()
    request.extensions["trace"] = trace.observe
    try:
        return await client.send(request, follow_redirects=False, stream=True), trace
    except httpx.TransportError:
        raise ProviderTransportError(trace.provably_not_sent) from None


async def read_bounded_body(response: httpx.Response, maximum_bytes: int) -> bytes:
    """Read at most limit plus one byte, then reject an oversized provider body."""
    if maximum_bytes < 0:
        maximum_bytes = 0
    chunks: list[bytes] = []
    count = 0
    try:
        async for chunk in response.aiter_bytes():
            count += len(chunk)
            if count > maximum_bytes:
                raise BodyTooLargeError("provider response body exceeds its size limit")
            chunks.append(chunk)
    except httpx.HTTPError:
        raise ProviderTransportError(False) from None
    return b"".join(chunks)


def read_error_tokens(body: bytes, pointers: tuple[str, ...]) -> tuple[str, ...]:
    """Read safe scalar tokens at RFC 6901 pointers from a bounded JSON error body."""
    try:
        document = json.loads(body)
    except (UnicodeDecodeError, json.JSONDecodeError):
        return ()
    tokens: list[str] = []
    for pointer in pointers:
        current: object = document
        if pointer and not pointer.startswith("/"):
            continue
        for escaped in pointer[1:].split("/") if pointer else ():
            segment = escaped.replace("~1", "/").replace("~0", "~")
            if isinstance(current, dict):
                current = current.get(segment)
            elif (
                isinstance(current, list)
                and segment.isdecimal()
                and str(int(segment)) == segment
                and int(segment) < len(current)
            ):
                current = current[int(segment)]
            else:
                current = None
                break
        if isinstance(current, str):
            token = current
        elif isinstance(current, (int, float)) and not isinstance(current, bool):
            token = str(current)
        else:
            continue
        if _ERROR_TOKEN.fullmatch(token):
            tokens.append(token)
    return tuple(tokens)


def parse_retry_after(value: str, now: datetime) -> float:
    """Parse delta-seconds or HTTP date, capped at one hour; invalid values give zero."""
    value = value.strip()
    if value.isascii() and value.isdecimal():
        return float(min(int(value), MAX_RETRY_AFTER_DELAY_SECONDS))
    try:
        retry_at = parsedate_to_datetime(value)
    except (ValueError, TypeError, IndexError):
        return 0
    if retry_at is None:
        return 0
    current = now if now.tzinfo else now.replace(tzinfo=UTC)
    return max(0.0, min((retry_at - current).total_seconds(), MAX_RETRY_AFTER_DELAY_SECONDS))


def validate_base_url(value: str) -> str:
    """Require printable HTTPS, allowing HTTP only on localhost or a loopback IP."""
    if not value or any(ord(character) < 33 or ord(character) > 126 for character in value):
        raise ValueError("base URL must be printable ASCII without spaces")
    parsed = urlsplit(value)
    if (
        not parsed.hostname
        or "@" in parsed.netloc
        or "?" in value
        or "#" in value
        or parsed.query
        or parsed.fragment
    ):
        raise ValueError("base URL must be absolute without user information, query, or fragment")
    try:
        if parsed.port == 0:
            raise ValueError("zero port")
    except ValueError:
        raise ValueError("base URL port is invalid") from None
    is_loopback = parsed.hostname.lower() == "localhost"
    try:
        is_loopback = is_loopback or ipaddress.ip_address(parsed.hostname).is_loopback
    except ValueError:
        pass
    if parsed.scheme != "https" and not (parsed.scheme == "http" and is_loopback):
        raise ValueError("base URL must use HTTPS unless its host is loopback")
    return urlunsplit((parsed.scheme, parsed.netloc, parsed.path.rstrip("/"), "", ""))


def is_header_safe_credential(value: str) -> bool:
    """Require a nonempty printable ASCII value without spaces or controls."""
    return bool(value) and all(33 <= ord(character) <= 126 for character in value)
