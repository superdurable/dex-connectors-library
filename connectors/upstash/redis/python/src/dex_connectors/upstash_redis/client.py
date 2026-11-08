# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Upstash Redis GET commands with Go-compatible outcomes."""

from __future__ import annotations

import json
from dataclasses import dataclass
from datetime import UTC, datetime

import httpx
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    QueryAttempt,
    QueryDefinition,
    Receipt,
)
from dex_connectors.sdk.providerhttp import (
    BodyTooLargeError,
    ProviderTransportError,
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import GET_KEY_DEFINITION
from .models import GetKeyInput, KeyLookup


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One provider response or local failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "upstash-redis", "getKey", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[KeyLookup]:
    return QueryAttempt.branch_result(branch, KeyLookup(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[KeyLookup]:
    return QueryAttempt.retry(_failure(kind, message), delay)


class Client:
    """Resolve a REST token for each GET command."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Upstash configuration and credential source are required")
        if config.timeout_seconds <= 0 or config.max_response_bytes < 1:
            raise ValueError("Upstash request limits must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_key(self) -> GetKeyOperation:
        """Bind the getKey Query to this client."""
        return GetKeyOperation(self)

    async def aclose(self) -> None:
        """Close an owned HTTP transport after Worker shutdown."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _read_key(self, call: Call, key: str) -> _Exchange:
        try:
            credentials = await self.credentials.resolve(call)
            token = credentials.rest_token.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return _Exchange(error="credential")
        if not is_header_safe_credential(token):
            return _Exchange(error="credential")
        try:
            request = self.http_client.build_request(
                "POST",
                self.base_url + "/",
                json=["GET", key],
                headers={"Accept": "application/json", "Authorization": "Bearer " + token},
            )
            response, _ = await send_traced(self.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _Exchange(error="request")
        try:
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _Exchange(status=response.status_code, retry_after=retry_after, error="too_large")
            except ProviderTransportError:
                return _Exchange(status=response.status_code, retry_after=retry_after, error="interrupted")
            if token.encode() in body:
                return _Exchange(status=response.status_code, retry_after=retry_after, error="credential")
            return _Exchange(status=response.status_code, body=body, retry_after=retry_after)
        finally:
            await response.aclose()


class GetKeyOperation:
    """Read a Redis string and distinguish a missing key."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query contract."""
        return GET_KEY_DEFINITION

    async def invoke(self, call: Call, input: GetKeyInput) -> QueryAttempt[KeyLookup]:
        """Execute one command and classify provider outcomes."""
        if (
            not isinstance(input, GetKeyInput)
            or not input.key
            or len(input.key.encode()) > 512
            or "\x00" in input.key
        ):
            return _branch("defect", FailureKind.VALIDATION, "Redis key is invalid")
        exchange = await self.client._read_key(call, input.key)
        if exchange.error == "credential":
            return _branch("defect", FailureKind.AUTHENTICATION, "Upstash token is unavailable or invalid")
        if exchange.error == "request":
            return _retry(FailureKind.TRANSPORT, "Upstash request failed")
        if exchange.status == 429:
            return _retry(FailureKind.RATE_LIMIT, "Upstash rate limited the request", exchange.retry_after)
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(
                FailureKind.AVAILABILITY, "Upstash is temporarily unavailable", exchange.retry_after
            )
        if exchange.status != 200:
            return _branch(
                "providerRejected", FailureKind.PROVIDER_REJECTION, f"Upstash rejected HTTP {exchange.status}"
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "Upstash response exceeded the configured limit",
            )
        if exchange.error:
            return _retry(FailureKind.TRANSPORT, "Upstash response was interrupted")
        try:
            value = json.loads(exchange.body)
        except (ValueError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Upstash returned invalid JSON")
        if not isinstance(value, dict) or len(value) != 1:
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Upstash returned invalid JSON")
        if isinstance(value.get("error"), str):
            return _branch(
                "providerRejected", FailureKind.PROVIDER_REJECTION, "Upstash rejected the GET command"
            )
        if "result" not in value or (value["result"] is not None and not isinstance(value["result"], str)):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Upstash returned invalid JSON")
        if value["result"] is None:
            return QueryAttempt.branch_result("missing", KeyLookup(key=input.key), Receipt())
        return QueryAttempt.branch_result(
            "found", KeyLookup(key=input.key, value=value["result"], exists=True), Receipt()
        )
