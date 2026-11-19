# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Yijingtong warehouse transport and Go-compatible query decisions."""

import json
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
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import LIST_WAREHOUSES_DEFINITION
from .models import ListWarehousesInput, Warehouse, WarehousePage


def _valid_token(value: str) -> bool:
    return isinstance(value, str) and 0 < len(value) <= 2048 and all(0x20 < ord(c) < 0x7F for c in value)


class Client:
    """Resolve Yijingtong credentials for each warehouse query."""

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
            raise ValueError("Yijingtong configuration and credentials are required")
        parsed = httpx.URL(config.base_url)
        if parsed.scheme != "https" or parsed.path not in ("", "/") or parsed.query or parsed.fragment:
            raise ValueError("Yijingtong API URL must be an HTTPS origin")
        self.config = config
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_http_client = http_client is None

    def list_warehouses(self) -> "ListWarehousesOperation":
        """Bind the warehouse-list query."""
        return ListWarehousesOperation(self)

    async def aclose(self) -> None:
        """Close the owned transport."""
        if self._owns_http_client:
            await self.http_client.aclose()


class ListWarehousesOperation:
    """Query Yijingtong and classify the provider outcome."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated query definition."""
        return LIST_WAREHOUSES_DEFINITION

    def _failure(self, kind: FailureKind) -> Failure:
        return Failure(kind, "yijingtong", "listWarehouses", "Yijingtong request failed")

    def _branch(self, branch: str, kind: FailureKind) -> QueryAttempt[WarehousePage]:
        return QueryAttempt.branch_result(branch, WarehousePage(), Receipt(), self._failure(kind))

    def _retry(self, kind: FailureKind, delay: float = 0) -> QueryAttempt[WarehousePage]:
        return QueryAttempt.retry(self._failure(kind), delay)

    async def invoke(self, call: Call, input: ListWarehousesInput) -> QueryAttempt[WarehousePage]:
        """Read a bounded, authorized overseas warehouse catalog."""
        if not isinstance(input, ListWarehousesInput):
            return self._branch("defect", FailureKind.VALIDATION)
        try:
            credentials = await self.client.credentials.resolve(call)
        except (ValueError, LookupError, OSError, AttributeError):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        token = credentials.token.reveal()
        if not _valid_token(token):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        body = json.dumps({"token": token}, separators=(",", ":")).encode()
        request = self.client.http_client.build_request(
            "POST",
            self.client.config.base_url.rstrip("/") + "/hwc_api/hwc_storage_list.php",
            content=body,
            headers={"Accept": "application/json", "Content-Type": "application/json; charset=utf-8"},
        )
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return self._retry(FailureKind.AVAILABILITY)
        try:
            status = response.status_code
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return self._retry(FailureKind.RATE_LIMIT, delay)
            if status == 408 or status >= 500:
                return self._retry(FailureKind.AVAILABILITY, delay)
            try:
                body = await read_bounded_body(response, self.client.config.max_response_bytes)
            except BodyTooLargeError:
                return self._branch("invalidResponse", FailureKind.RESPONSE_TOO_LARGE)
            except ProviderTransportError:
                return self._retry(FailureKind.AVAILABILITY)
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.VALIDATION
                    if status in (400, 404, 412)
                    else FailureKind.PROVIDER_REJECTION
                )
                return self._branch("providerRejected", kind)
        finally:
            await response.aclose()
        try:
            wire = json.loads(body)
        except (UnicodeDecodeError, ValueError):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        if not isinstance(wire, dict) or wire.get("error") not in ("T", "F"):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        if wire["error"] == "F":
            return self._branch("providerRejected", FailureKind.PROVIDER_REJECTION)
        data = wire.get("data")
        if not isinstance(data, list) or len(data) > 200:
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        warehouses = []
        for item in data:
            if (
                not isinstance(item, dict)
                or not isinstance(item.get("nums"), str)
                or not item["nums"].strip()
                or not isinstance(item.get("pname"), str)
                or not item["pname"].strip()
                or not isinstance(item.get("country", ""), str)
            ):
                return self._branch("invalidResponse", FailureKind.PROTOCOL)
            warehouses.append(Warehouse(id=item["nums"], name=item["pname"], country=item.get("country", "")))
        return QueryAttempt.branch_result("listed", WarehousePage(warehouses=tuple(warehouses)), Receipt())
