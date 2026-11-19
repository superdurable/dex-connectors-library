# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""ZoomEye provider transport and Go-compatible Query decisions."""

import json
from datetime import UTC, datetime
from typing import Any

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
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import GET_ACCOUNT_QUOTA_DEFINITION
from .models import GetAccountQuotaInput, GetAccountQuotaOutput


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[GetAccountQuotaOutput]:
    return QueryAttempt.branch_result(
        branch, GetAccountQuotaOutput(), Receipt(), Failure(kind, "zoomeye", "getAccountQuota", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[GetAccountQuotaOutput]:
    return QueryAttempt.retry(Failure(kind, "zoomeye", "getAccountQuota", message), delay)


def _ascii_value(value: str, limit: int, allow_empty: bool = False) -> bool:
    return (
        isinstance(value, str)
        and (allow_empty or bool(value))
        and len(value.encode()) <= limit
        and all(0x20 < ord(c) < 0x7F for c in value)
    )


class Client:
    """Resolve credentials per getAccountQuota request and own a redirect-safe HTTP client."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("ZoomEye configuration and credentials are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("ZoomEye timeout or response limit is invalid")
        self.base_url = validate_base_url(config.base_url)
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_account_quota(self) -> "GetAccountQuotaOperation":
        """Bind the getAccountQuota Query to this client."""
        return GetAccountQuotaOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client if this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class GetAccountQuotaOperation:
    """Perform one getAccountQuota read and filter the provider response."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("ZoomEye client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Go-derived Query identity."""
        return GET_ACCOUNT_QUOTA_DEFINITION

    async def invoke(self, call: Call, input: GetAccountQuotaInput) -> QueryAttempt[GetAccountQuotaOutput]:
        """Classify transport and provider outcomes without exposing secrets."""
        try:
            credentials = await self.client.credentials.resolve(call)
            key = credentials.api_key.reveal()
            if not _ascii_value(key, 512):
                return _branch(
                    "defect", FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
                )
            request = self.client.http_client.build_request(
                "POST",
                self.client.base_url + "/v2/userinfo",
                headers={"Accept": "application/json", "API-KEY": key},
            )
        except (ValueError, LookupError, OSError, httpx.HTTPError):
            return _branch("defect", FailureKind.AUTHENTICATION, "ZoomEye credentials or request are invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "ZoomEye request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "ZoomEye rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "ZoomEye is temporarily unavailable", delay)

            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "ZoomEye response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "ZoomEye response was interrupted")
        finally:
            await response.aclose()
        if status != 200:
            kind = FailureKind.PROVIDER_REJECTION
            if status == 401:
                kind = FailureKind.AUTHENTICATION
            elif status == 403:
                kind = FailureKind.AUTHORIZATION
            return _branch("providerRejected", kind, f"ZoomEye rejected request with HTTP {status}")
        try:
            document: Any = json.loads(body)
        except (ValueError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "ZoomEye returned invalid JSON")
        if not isinstance(document, dict) or type(document.get("code")) is not int:
            return _branch("invalidResponse", FailureKind.PROTOCOL, "ZoomEye returned an invalid response")
        if document["code"] != 60000:
            return _branch(
                "providerRejected", FailureKind.PROVIDER_REJECTION, "ZoomEye rejected the account request"
            )
        data = document.get("data")
        subscription = data.get("subscription") if isinstance(data, dict) else None
        if (
            not isinstance(subscription, dict)
            or not isinstance(subscription.get("plan"), str)
            or not subscription["plan"]
            or not all(
                isinstance(subscription.get(k), str)
                and 0 < len(subscription[k]) <= 30
                and subscription[k].isascii()
                and subscription[k].isdigit()
                for k in ("points", "zoomeye_points")
            )
        ):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "ZoomEye response lacks subscription fields"
            )
        return QueryAttempt.branch_result(
            "fetched",
            GetAccountQuotaOutput(
                plan=subscription["plan"],
                general_points=subscription["points"],
                zoomeye_points=subscription["zoomeye_points"],
            ),
            Receipt(),
        )
