# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""iP138 token usage transport and Go-compatible Query decisions."""

import json
from datetime import UTC, datetime
from typing import Any, cast

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
from ._generated.definitions import GET_TOKEN_USAGE_DEFINITION
from .models import GetTokenUsageInput, GetTokenUsageOutput

_ENDPOINT = "https://api.ip138.com/status/"


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[GetTokenUsageOutput]:
    return QueryAttempt.branch_result(
        branch, GetTokenUsageOutput(), Receipt(), Failure(kind, "ip138", "getTokenUsage", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[GetTokenUsageOutput]:
    return QueryAttempt.retry(Failure(kind, "ip138", "getTokenUsage", message), delay)


def _valid_token(value: str) -> bool:
    return bool(value) and len(value) <= 1024 and all(0x20 < ord(character) < 0x7F for character in value)


class Client:
    """Own the redirect-safe HTTP transport and resolve credentials per token usage Query."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("iP138 configuration and credential source are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("iP138 timeout or response limit is invalid")
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_token_usage(self) -> "GetTokenUsageOperation":
        """Bind the token usage Query to this client."""
        return GetTokenUsageOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP transport when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class GetTokenUsageOperation:
    """Read the token's current-hour use and purchased quota."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("iP138 client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated token usage Query identity."""
        return GET_TOKEN_USAGE_DEFINITION

    async def invoke(self, call: Call, input: GetTokenUsageInput) -> QueryAttempt[GetTokenUsageOutput]:
        """Make one usage request and classify its provider outcome."""
        try:
            credentials = await self.client.credentials.resolve(call)
            token = credentials.token.reveal()
            if not _valid_token(token):
                raise ValueError("invalid token")
        except (ValueError, LookupError, OSError):
            return _branch("defect", FailureKind.AUTHENTICATION, "iP138 token is unavailable or invalid")
        try:
            request = self.client.http_client.build_request(
                "GET",
                _ENDPOINT,
                params={"token": token},
                headers={"Accept": "application/json"},
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "iP138 usage request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "iP138 usage request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "iP138 rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(
                    FailureKind.AVAILABILITY, "iP138 usage service is temporarily unavailable", delay
                )
            if status != 200:
                kind = FailureKind.PROVIDER_REJECTION
                if status == 401:
                    kind = FailureKind.AUTHENTICATION
                elif status == 403:
                    kind = FailureKind.AUTHORIZATION
                return _branch("providerRejected", kind, f"iP138 rejected usage request with HTTP {status}")
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "iP138 response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "iP138 response was interrupted")
        finally:
            await response.aclose()
        return _decode_usage(body)


def _decode_usage(body: bytes) -> QueryAttempt[GetTokenUsageOutput]:
    try:
        document: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "iP138 returned invalid usage data")
    if not isinstance(document, dict) or not isinstance(document.get("ret"), str) or not document["ret"]:
        return _branch("invalidResponse", FailureKind.PROTOCOL, "iP138 returned invalid usage data")
    if document["ret"] == "err":
        return _branch("providerRejected", FailureKind.PROVIDER_REJECTION, "iP138 rejected the usage request")
    data = document.get("data")
    if document["ret"] != "ok" or not isinstance(data, dict):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "iP138 returned incomplete usage data")
    values = (data.get("reqs"), data.get("hour"), data.get("package"))
    if any(type(value) is not int or value < 0 for value in values):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "iP138 returned incomplete usage data")
    return QueryAttempt.branch_result(
        "found",
        GetTokenUsageOutput(
            current_hour_requests=cast(int, values[0]),
            hourly_allowance=cast(int, values[1]),
            package_quota=cast(int, values[2]),
        ),
        Receipt(),
    )
