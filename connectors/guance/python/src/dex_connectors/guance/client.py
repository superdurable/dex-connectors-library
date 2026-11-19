# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Guance dashboard-page transport and Go-compatible decisions."""

import json
from datetime import UTC, datetime
from typing import Any
from urllib.parse import urlencode

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
from ._generated.definitions import LIST_DASHBOARDS_PAGE_DEFINITION
from .models import DashboardMetadata, ListDashboardsPageInput, ListDashboardsPageOutput

_ENDPOINT = "https://openapi.guance.com/api/v1/dashboards/list"


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListDashboardsPageOutput]:
    return QueryAttempt.branch_result(
        branch,
        ListDashboardsPageOutput(),
        Receipt(),
        Failure(kind, "guance", "listDashboardsPage", message),
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ListDashboardsPageOutput]:
    return QueryAttempt.retry(Failure(kind, "guance", "listDashboardsPage", message), delay)


def _valid_api_key(value: str) -> bool:
    return bool(value) and len(value) <= 1024 and all(0x20 < ord(character) < 0x7F for character in value)


class Client:
    """Own the redirect-safe HTTP transport and resolve Guance credentials per Query."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Guance configuration and credential source are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("Guance timeout or response limit is invalid")
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def list_dashboards_page(self) -> "ListDashboardsPageOperation":
        """Bind the dashboard-list Query to this client."""
        return ListDashboardsPageOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP transport when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class ListDashboardsPageOperation:
    """List one page of dashboard IDs and names without user details."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Guance client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated dashboard-list Query identity."""
        return LIST_DASHBOARDS_PAGE_DEFINITION

    async def invoke(
        self, call: Call, input: ListDashboardsPageInput
    ) -> QueryAttempt[ListDashboardsPageOutput]:
        """Make one dashboard-list request and classify its provider outcome."""
        if input.page_index < 0 or input.page_size < 0 or input.page_size > 100:
            return _branch("defect", FailureKind.VALIDATION, "Guance pagination is invalid")
        page_index = input.page_index or 1
        page_size = input.page_size or 10
        try:
            credentials = await self.client.credentials.resolve(call)
            api_key = credentials.api_key.reveal()
            if not _valid_api_key(api_key):
                raise ValueError("invalid API Key")
        except (ValueError, LookupError, OSError):
            return _branch("defect", FailureKind.AUTHENTICATION, "Guance API Key is unavailable or invalid")
        try:
            query = urlencode({"pageIndex": page_index, "pageSize": page_size})
            request = self.client.http_client.build_request(
                "GET",
                _ENDPOINT + "?" + query,
                headers={"DF-API-KEY": api_key, "Accept": "application/json"},
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "Guance dashboard-list request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "Guance dashboard-list request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Guance rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(
                    FailureKind.AVAILABILITY, "Guance dashboard list is temporarily unavailable", delay
                )
            if status != 200:
                kind = FailureKind.PROVIDER_REJECTION
                if status == 401:
                    kind = FailureKind.AUTHENTICATION
                elif status == 403:
                    kind = FailureKind.AUTHORIZATION
                return _branch("providerRejected", kind, f"Guance rejected dashboard list with HTTP {status}")
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Guance response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Guance response was interrupted")
        finally:
            await response.aclose()
        return _decode_page(body, page_index)


def _decode_page(body: bytes, page_index: int) -> QueryAttempt[ListDashboardsPageOutput]:
    try:
        document: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "Guance returned invalid dashboard metadata")
    if (
        not isinstance(document, dict)
        or type(document.get("code")) is not int
        or type(document.get("success")) is not bool
    ):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "Guance returned invalid dashboard metadata")
    if not document["success"] or document["code"] != 200:
        return _branch(
            "providerRejected", FailureKind.PROVIDER_REJECTION, "Guance rejected the dashboard-list request"
        )
    content = document.get("content")
    page_info = document.get("pageInfo")
    if not isinstance(content, list) or not isinstance(page_info, dict):
        return _branch(
            "invalidResponse", FailureKind.PROTOCOL, "Guance returned invalid dashboard pagination"
        )
    for field in ("count", "pageIndex", "pageSize", "totalCount"):
        if type(page_info.get(field)) is not int:
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Guance returned invalid dashboard pagination"
            )
    if (
        page_info["count"] != len(content)
        or page_info["count"] < 0
        or page_info["pageIndex"] != page_index
        or not 1 <= page_info["pageSize"] <= 100
        or page_info["totalCount"] < page_info["count"]
    ):
        return _branch(
            "invalidResponse", FailureKind.PROTOCOL, "Guance returned invalid dashboard pagination"
        )
    dashboards: list[DashboardMetadata] = []
    for item in content:
        if (
            not isinstance(item, dict)
            or not isinstance(item.get("uuid"), str)
            or not isinstance(item.get("name"), str)
        ):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Guance returned incomplete dashboard metadata"
            )
        if not item["uuid"] or not item["name"]:
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Guance returned incomplete dashboard metadata"
            )
        dashboards.append(DashboardMetadata(uuid=item["uuid"], name=item["name"]))
    return QueryAttempt.branch_result(
        "listed",
        ListDashboardsPageOutput(
            total_count=page_info["totalCount"],
            page_index=page_info["pageIndex"],
            page_size=page_info["pageSize"],
            dashboards=tuple(dashboards),
        ),
        Receipt(),
    )
