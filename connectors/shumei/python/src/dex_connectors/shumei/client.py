# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Shumei provider Query with bounded HTTP responses."""

import json
from datetime import UTC, datetime
from urllib.parse import urlsplit

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
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import GET_BALANCE_DEFINITION
from .models import Balance, GetBalanceInput


class ProviderRejection(Exception):
    """Represent a definite provider refusal inside a successful HTTP exchange."""

    def __init__(self, kind: FailureKind) -> None:
        self.kind = kind


class InputDefect(Exception):
    """Represent invalid application input before any provider request."""


def _valid_secret(value: str, limit: int) -> bool:
    return (
        bool(value)
        and len(value) <= limit
        and value.strip() == value
        and all(0x20 < ord(ch) < 0x7F for ch in value)
    )


def _valid_base_url(value: str) -> str:
    parsed = urlsplit(value)
    if (
        parsed.scheme != "https"
        or not parsed.hostname
        or parsed.username
        or parsed.password
        or parsed.query
        or parsed.fragment
        or parsed.path not in ("", "/")
    ):
        raise ValueError("invalid HTTPS provider base URL")
    return value.rstrip("/")


def _json_object(body: bytes) -> dict[str, object]:
    wire = json.loads(body, parse_constant=lambda _: (_ for _ in ()).throw(ValueError()))
    if not isinstance(wire, dict):
        raise ValueError("invalid JSON object")
    return wire


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[Balance]:
    return QueryAttempt.branch_result(
        branch, Balance(), Receipt(), Failure(kind, "shumei", "getBalance", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[Balance]:
    return QueryAttempt.retry(Failure(kind, "shumei", "getBalance", message), delay)


class Client:
    """Own provider HTTP limits and resolve credentials for each attempt."""

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
            raise ValueError("Shumei configuration and credential source are required")

        self.base_url = _valid_base_url(config.base_url)

        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_balance(self) -> "GetBalanceOperation":
        """Bind the provider Query to this client."""
        return GetBalanceOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP transport only when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class GetBalanceOperation:
    """Read provider data without changing provider state."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Shumei client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated provider Query contract."""
        return GET_BALANCE_DEFINITION

    async def invoke(self, call: Call, input: GetBalanceInput) -> QueryAttempt[Balance]:
        """Make one provider request and classify definite and unknown outcomes."""
        try:
            credentials = await self.client.credentials.resolve(call)
            request = self._request(credentials, input)
        except InputDefect:
            return _branch("defect", FailureKind.VALIDATION, "invalid Query input")
        except (ValueError, LookupError, OSError, AttributeError, httpx.HTTPError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
            )
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "provider request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if response.status_code == 429:
                return _retry(FailureKind.RATE_LIMIT, "provider rate limited the request", delay)
            if response.status_code == 408 or response.status_code >= 500:
                return _retry(FailureKind.AVAILABILITY, "provider is temporarily unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse", FailureKind.RESPONSE_TOO_LARGE, "provider response exceeded the limit"
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "provider response was interrupted")
            if response.status_code != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if response.status_code == 401
                    else FailureKind.AUTHORIZATION
                    if response.status_code == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return _branch(
                    "providerRejected", kind, "provider rejected HTTP " + str(response.status_code)
                )
            try:
                output = self._parse(body)
            except ProviderRejection as rejection:
                return _branch("providerRejected", rejection.kind, "provider rejected the query")
            except (ValueError, KeyError, TypeError, UnicodeDecodeError, OverflowError):
                return _branch("invalidResponse", FailureKind.PROTOCOL, "provider returned invalid data")
            return QueryAttempt.branch_result("retrieved", output, Receipt())
        finally:
            await response.aclose()

    def _request(self, credentials: Credentials, input: GetBalanceInput) -> httpx.Request:
        """Build the provider request after credential and input validation."""

        key = credentials.access_key.reveal()
        if not _valid_secret(key, 1024):
            raise ValueError("invalid access key")
        return self.client.http_client.build_request(
            "POST",
            self.client.base_url + "/saas/balance/v1",
            json={"accessKey": key},
            headers={"Accept": "application/json", "Content-Type": "application/json; charset=utf-8"},
        )

    def _parse(self, body: bytes) -> Balance:
        """Validate the provider envelope and return only declared fields."""

        envelope = _json_object(body)
        code = envelope["code"]
        if not isinstance(code, int):
            raise ValueError("invalid code")
        if code != 1100:
            raise ProviderRejection(FailureKind.PROVIDER_REJECTION)
        content = envelope["content"]
        if not isinstance(content, dict) or not isinstance(content.get("balance"), (int, float)):
            raise ValueError("invalid balance")
        return Balance(float(content["balance"]))
