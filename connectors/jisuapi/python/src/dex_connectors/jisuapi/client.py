# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""JisuAPI provider transport and Go-compatible Query decisions."""

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
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import LIST_PROVINCES_DEFINITION
from .models import ListProvincesInput, ListProvincesOutput, Province


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListProvincesOutput]:
    return QueryAttempt.branch_result(
        branch, ListProvincesOutput(), Receipt(), Failure(kind, "jisuapi", "listProvinces", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ListProvincesOutput]:
    return QueryAttempt.retry(Failure(kind, "jisuapi", "listProvinces", message), delay)


def _ascii_value(value: str, limit: int, allow_empty: bool = False) -> bool:
    return (
        isinstance(value, str)
        and (allow_empty or bool(value))
        and len(value.encode()) <= limit
        and all(0x20 < ord(c) < 0x7F for c in value)
    )


class Client:
    """Resolve credentials per listProvinces request and own a redirect-safe HTTP client."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("JisuAPI configuration and credentials are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("JisuAPI timeout or response limit is invalid")

        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def list_provinces(self) -> "ListProvincesOperation":
        """Bind the listProvinces Query to this client."""
        return ListProvincesOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client if this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class ListProvincesOperation:
    """Perform one listProvinces read and filter the provider response."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("JisuAPI client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Go-derived Query identity."""
        return LIST_PROVINCES_DEFINITION

    async def invoke(self, call: Call, input: ListProvincesInput) -> QueryAttempt[ListProvincesOutput]:
        """Classify transport and provider outcomes without exposing secrets."""
        try:
            credentials = await self.client.credentials.resolve(call)
            key = credentials.appkey.reveal()
            if not _ascii_value(key, 1024):
                return _branch(
                    "defect", FailureKind.AUTHENTICATION, "JisuAPI appkey is unavailable or invalid"
                )
            request = self.client.http_client.build_request(
                "GET",
                "https://api.jisuapi.com/area/province",
                params={"appkey": key},
                headers={"Accept": "application/json"},
            )
        except (ValueError, LookupError, OSError, httpx.HTTPError):
            return _branch("defect", FailureKind.AUTHENTICATION, "JisuAPI credentials or request are invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "JisuAPI request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "JisuAPI rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "JisuAPI is temporarily unavailable", delay)
            if status != 200:
                kind = FailureKind.PROVIDER_REJECTION
                if status == 401:
                    kind = FailureKind.AUTHENTICATION
                elif status == 403:
                    kind = FailureKind.AUTHORIZATION
                return _branch("providerRejected", kind, f"JisuAPI rejected request with HTTP {status}")
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "JisuAPI response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "JisuAPI response was interrupted")
        finally:
            await response.aclose()

        try:
            document: Any = json.loads(body)
        except (ValueError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "JisuAPI returned invalid JSON")
        if not isinstance(document, dict) or type(document.get("status")) is not int:
            return _branch("invalidResponse", FailureKind.PROTOCOL, "JisuAPI returned invalid province data")
        status = document["status"]
        if status in (104, 106):
            return _retry(FailureKind.RATE_LIMIT, "JisuAPI daily or IP request limit was reached")
        if status == 107:
            return _retry(FailureKind.AVAILABILITY, "JisuAPI is under maintenance")
        if status != 0:
            return _branch(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                f"JisuAPI rejected the province-list request with code {status}",
            )
        provinces = document.get("result")
        if not isinstance(provinces, list):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "JisuAPI returned incomplete province data"
            )
        result = []
        for province in provinces:
            if (
                not isinstance(province, dict)
                or not isinstance(province.get("id"), str)
                or not isinstance(province.get("name"), str)
                or not province["id"]
                or not province["name"]
            ):
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "JisuAPI returned an incomplete province"
                )
            result.append(Province(id=province["id"], name=province["name"]))
        return QueryAttempt.branch_result("listed", ListProvincesOutput(provinces=tuple(result)), Receipt())
