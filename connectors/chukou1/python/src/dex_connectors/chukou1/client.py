# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Chukou1 warehouse listing with Go-compatible provider outcome classification."""

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
from ._generated.definitions import CONNECTOR_ID as CONNECTOR_ID
from ._generated.definitions import DEFECT as DEFECT
from ._generated.definitions import INVALID_RESPONSE as INVALID_RESPONSE
from ._generated.definitions import LIST_WAREHOUSES_DEFINITION as LIST_WAREHOUSES_DEFINITION
from ._generated.definitions import LIST_WAREHOUSES_OPERATION_ID as LIST_WAREHOUSES_OPERATION_ID
from ._generated.definitions import LISTED as LISTED
from ._generated.definitions import PROVIDER_REJECTED as PROVIDER_REJECTED
from .models import ListWarehousesInput, Warehouse, WarehousePage

_OPERATION = LIST_WAREHOUSES_OPERATION_ID


class Client:
    """Read a bounded overseas warehouse catalog with credentials resolved per Query attempt."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        """Validate the connection and own an HTTP client unless one is provided."""
        if config is None or credentials is None:
            raise ValueError("Chukou1 configuration and credential source are required")
        self.base_url = validate_base_url(config.base_url)
        if httpx.URL(self.base_url).path not in ("", "/"):
            raise ValueError("Chukou1 API URL must be a host root")
        if config.timeout_seconds <= 0 or config.max_response_bytes <= 0:
            raise ValueError("Chukou1 timeout and response limit must be positive")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self.owns_http_client = http_client is None

    def list_warehouses(self) -> "ListWarehousesOperation":
        """Bind the authorized warehouse catalog Query."""
        return ListWarehousesOperation(self)

    async def aclose(self) -> None:
        """Close the connector-owned transport after the Worker stops."""
        if self.owns_http_client:
            await self.http_client.aclose()


class ListWarehousesOperation:
    """Read Chukou1's at-most-200 overseas warehouse rows."""

    def __init__(self, client: Client) -> None:
        """Bind one validated client."""
        if client is None:
            raise ValueError("Chukou1 client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest operation identity and branches."""
        return LIST_WAREHOUSES_DEFINITION

    async def invoke(self, call: Call, input: ListWarehousesInput) -> QueryAttempt[WarehousePage]:
        """Send one GET and classify provider, protocol, and transport outcomes."""
        if not isinstance(input, ListWarehousesInput):
            return _branch(DEFECT, FailureKind.VALIDATION, "warehouse input is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
            token = credentials.access_token.reveal()
        except Exception:
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "Chukou1 access token is unavailable or invalid"
            )
        if not _valid_access_token(token):
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "Chukou1 access token is unavailable or invalid"
            )
        try:
            request = self.client.http_client.build_request(
                "GET",
                self.client.base_url + "/v1/warehouses",
                headers={
                    "Accept": "application/json",
                    "Content-Type": "application/json; charset=utf-8",
                    "Authorization": "Bearer " + token,
                },
            )
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError):
            return _retry(FailureKind.AVAILABILITY, "Chukou1 warehouse request failed")
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Chukou1 rate limited warehouse query", retry_after)
            if status == 408 or status >= 500:
                return _retry(
                    FailureKind.AVAILABILITY, "Chukou1 warehouse API is temporarily unavailable", retry_after
                )
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    INVALID_RESPONSE,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Chukou1 response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Chukou1 response was interrupted")
            if status != 200:
                return _branch(
                    PROVIDER_REJECTED,
                    _classify_http(status),
                    f"Chukou1 rejected warehouse query with HTTP {status}",
                )
            try:
                page = _decode_warehouse_page(body)
            except (UnicodeDecodeError, ValueError, TypeError):
                return _branch(
                    INVALID_RESPONSE, FailureKind.PROTOCOL, "Chukou1 returned an invalid warehouse response"
                )
            return QueryAttempt.branch_result(LISTED, page, Receipt())
        finally:
            await response.aclose()


def _valid_access_token(value: object) -> bool:
    return (
        isinstance(value, str)
        and 0 < len(value) <= 2048
        and not value.lower().startswith("bearer ")
        and all(0x21 <= ord(character) <= 0x7E for character in value)
    )


def _decode_warehouse_page(body: bytes) -> WarehousePage:
    document: Any = json.loads(body.decode("utf-8"))
    if not isinstance(document, list) or len(document) > 200:
        raise ValueError("invalid Chukou1 warehouses")
    warehouses: list[Warehouse] = []
    for row in document:
        if not isinstance(row, dict):
            raise ValueError("invalid Chukou1 warehouse")
        id, name, country, country_code = (
            _json_string(row.get("WarehouseId")),
            _json_string(row.get("WarehouseName")),
            _json_string(row.get("Country")),
            _json_string(row.get("CountryCode")),
        )
        if not id or not name or not country_code:
            raise ValueError("incomplete Chukou1 warehouse")
        warehouses.append(Warehouse(id, name, country, country_code))
    return WarehousePage(tuple(warehouses))


def _json_string(value: object) -> str:
    if value is None:
        return ""
    if not isinstance(value, str):
        raise TypeError("Chukou1 warehouse field must be a string")
    return value


def _classify_http(status: int) -> FailureKind:
    if status == 401:
        return FailureKind.AUTHENTICATION
    if status == 403:
        return FailureKind.AUTHORIZATION
    if status in (400, 404, 412):
        return FailureKind.VALIDATION
    return FailureKind.PROVIDER_REJECTION


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[WarehousePage]:
    return QueryAttempt.branch_result(
        branch, WarehousePage(), Receipt(), Failure(kind, CONNECTOR_ID, _OPERATION, message)
    )


def _retry(kind: FailureKind, message: str, after: float = 0) -> QueryAttempt[WarehousePage]:
    return QueryAttempt.retry(Failure(kind, CONNECTOR_ID, _OPERATION, message), after)
