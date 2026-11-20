# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""WeCom HTTP transport, bounded responses, and application token cache."""

import asyncio
import json
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from typing import Any

import httpx
from dex_connectors.sdk import CredentialProvider, FailureKind
from dex_connectors.sdk.providerhttp import (
    BodyTooLargeError,
    ProviderTransportError,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials

_DEFINITE_REFUSAL_STATUSES = frozenset({400, 401, 403, 404, 411, 413, 414, 415, 422})
_RETRYABLE_CODES = frozenset({-1, 45009, 45033})


@dataclass(frozen=True, slots=True)
class Exchange:
    """A bounded provider reply without a secret-bearing request URL or error string."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""
    request_sent: bool = False


def provider_code(body: bytes) -> tuple[int, dict[str, Any]] | None:
    """Decode a WeCom object with a numeric errcode, excluding booleans."""
    try:
        value = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return None
    if not isinstance(value, dict) or type(value.get("errcode")) is not int:
        return None
    return value["errcode"], value


def failure_kind_for_code(code: int) -> FailureKind:
    """Classify documented WeCom errors without returning provider text."""
    if code in {40001, 40013, 40014, 41001, 42001}:
        return FailureKind.AUTHENTICATION
    if code in {60020, 48002, 301002}:
        return FailureKind.AUTHORIZATION
    if code in {45009, 45033}:
        return FailureKind.RATE_LIMIT
    if code == -1:
        return FailureKind.AVAILABILITY
    return FailureKind.PROVIDER_REJECTION


class Client:
    """Resolve credentials per call and cache short-lived application access tokens."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if (
            config is None
            or credentials is None
            or config.timeout_seconds <= 0
            or not 0 < config.max_response_bytes <= 16 << 20
        ):
            raise ValueError("WeCom configuration and credentials are invalid")
        self.base_url = validate_base_url(config.base_url)
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None
        self._token_slots: dict[tuple[str, str], tuple[str, datetime]] = {}
        self._token_lock = asyncio.Lock()

    def send_group_robot_message(self) -> "SendGroupRobotMessageOperation":
        """Bind the fenced group robot Mutation."""
        from .group_robot import SendGroupRobotMessageOperation

        return SendGroupRobotMessageOperation(self)

    def send_application_message(self) -> "SendApplicationMessageOperation":
        """Bind the fenced application Mutation."""
        from .application import SendApplicationMessageOperation

        return SendApplicationMessageOperation(self)

    async def aclose(self) -> None:
        """Close the owned transport after Worker shutdown."""
        if self._owns_client:
            await self.http_client.aclose()

    async def request(self, request: httpx.Request) -> Exchange:
        """Send without reflecting query secrets in errors or Flow results."""
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError as error:
            return Exchange(error="request", request_sent=not error.provably_not_sent)
        try:
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if not (200 <= response.status_code < 300 or response.status_code in _DEFINITE_REFUSAL_STATUSES):
                return Exchange(response.status_code, retry_after=retry_after, request_sent=True)
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
                return Exchange(response.status_code, body, retry_after, request_sent=True)
            except BodyTooLargeError:
                return Exchange(
                    response.status_code, retry_after=retry_after, error="too_large", request_sent=True
                )
            except ProviderTransportError:
                return Exchange(
                    response.status_code, retry_after=retry_after, error="read", request_sent=True
                )
        finally:
            await response.aclose()

    async def access_token(self, corp_id: str, corp_secret: str) -> tuple[str, FailureKind | None, str]:
        """Get a cached token, or return a safe failure kind and decision category."""
        import hashlib

        fingerprint = hashlib.sha256(corp_secret.encode()).hexdigest()
        key = (corp_id, fingerprint)
        async with self._token_lock:
            cached = self._token_slots.get(key)
            if cached and datetime.now(UTC) < cached[1]:
                return cached[0], None, ""
            try:
                request = self.http_client.build_request(
                    "GET",
                    self.base_url + "/cgi-bin/gettoken",
                    params={"corpid": corp_id, "corpsecret": corp_secret},
                    headers={"Accept": "application/json"},
                )
            except (httpx.HTTPError, ValueError):
                return "", FailureKind.LOCAL_DEFECT, "defect"
            exchange = await self.request(request)
            if exchange.error:
                return (
                    "",
                    FailureKind.TRANSPORT
                    if exchange.error != "too_large"
                    else FailureKind.RESPONSE_TOO_LARGE,
                    "retry",
                )
            if exchange.status == 429:
                return "", FailureKind.RATE_LIMIT, "retry"
            if exchange.status == 408 or exchange.status >= 500:
                return "", FailureKind.AVAILABILITY, "retry"
            if exchange.status in _DEFINITE_REFUSAL_STATUSES:
                return "", FailureKind.PROVIDER_REJECTION, "providerRejected"
            if exchange.status != 200:
                return "", FailureKind.AVAILABILITY, "retry"
            decoded = provider_code(exchange.body)
            if decoded is None:
                return "", FailureKind.PROTOCOL, "retry"
            code, data = decoded
            if code in _RETRYABLE_CODES:
                return "", failure_kind_for_code(code), "retry"
            if code != 0:
                return "", failure_kind_for_code(code), "providerRejected"
            token = data.get("access_token")
            expires_in = data.get("expires_in")
            if (
                not isinstance(token, str)
                or not token
                or len(token) > 512
                or any(ord(c) <= 32 or ord(c) >= 127 for c in token)
                or type(expires_in) is not int
                or expires_in <= 0
            ):
                return "", FailureKind.PROTOCOL, "retry"
            lifetime = expires_in - 300 if expires_in > 600 else expires_in // 2
            self._token_slots[key] = token, datetime.now(UTC) + timedelta(seconds=lifetime)
            return token, None, ""

    async def invalidate_token(self, corp_id: str, corp_secret: str, token: str) -> None:
        """Discard only the cached token WeCom rejected."""
        import hashlib

        key = (corp_id, hashlib.sha256(corp_secret.encode()).hexdigest())
        async with self._token_lock:
            cached = self._token_slots.get(key)
            if cached and cached[0] == token:
                self._token_slots.pop(key, None)


from .application import SendApplicationMessageOperation as SendApplicationMessageOperation  # noqa: E402
from .group_robot import SendGroupRobotMessageOperation as SendGroupRobotMessageOperation  # noqa: E402
