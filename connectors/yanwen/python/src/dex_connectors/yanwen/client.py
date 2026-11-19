# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Signed Yanwen warehouse catalog Query."""

import hashlib
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
from dex_connectors.sdk.providerhttp import new_provider_http_client, parse_retry_after

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import LIST_WAREHOUSES_DEFINITION
from .models import ListWarehousesInput, Warehouse, WarehouseList


class Client:
    """Resolve Yanwen credentials per call and sign read requests."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        *,
        now: datetime | None = None,
    ) -> None:
        if (
            config is None
            or credentials is None
            or config.timeout_seconds <= 0
            or config.max_response_bytes <= 0
        ):
            raise ValueError("Yanwen configuration and credentials are required")
        self.config = config
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_http_client = http_client is None
        self.now = now

    def list_warehouses(self) -> "ListWarehousesOperation":
        """Bind the generated warehouse Query."""
        return ListWarehousesOperation(self)

    async def aclose(self) -> None:
        """Close the owned HTTP client."""
        if self._owns_http_client:
            await self.http_client.aclose()


class ListWarehousesOperation:
    """Read bounded handover warehouse metadata."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query contract."""
        return LIST_WAREHOUSES_DEFINITION

    def _failure(self, branch: str, kind: FailureKind) -> QueryAttempt[WarehouseList]:
        failure = Failure(kind, "yanwen", "listWarehouses", "Yanwen warehouse query failed")
        if branch == "retry":
            return QueryAttempt.retry(failure)
        return QueryAttempt.branch_result(branch, WarehouseList(), Receipt(), failure)

    async def invoke(self, call: Call, input: ListWarehousesInput) -> QueryAttempt[WarehouseList]:
        """Make one signed request and classify provider outcomes."""
        try:
            credentials = await self.client.credentials.resolve(call)
        except (LookupError, ValueError, OSError, AttributeError):
            return self._failure("defect", FailureKind.AUTHENTICATION)
        user_id, token = credentials.user_id, credentials.api_token.reveal()
        if (
            not user_id
            or len(user_id) > 64
            or user_id.strip() != user_id
            or not all(0x20 < ord(c) < 0x7F for c in user_id)
            or not token
            or len(token) > 2048
            or token.strip() != token
        ):
            return self._failure("defect", FailureKind.AUTHENTICATION)
        timestamp = str(int((self.client.now or datetime.now(UTC)).timestamp() * 1000))
        digest = hashlib.md5(
            (token + user_id + "{}jsoncommon.warehouse.getlist" + timestamp + "V1.0" + token).encode()
        ).hexdigest()
        params = {
            "user_id": user_id,
            "format": "json",
            "method": "common.warehouse.getlist",
            "timestamp": timestamp,
            "version": "V1.0",
            "sign": digest,
        }
        try:
            response = await self.client.http_client.post(
                self.client.config.base_url.rstrip("/") + "/api/order",
                params=params,
                content=b"{}",
                headers={"Content-Type": "application/json; charset=UTF-8", "Accept": "application/json"},
            )
        except httpx.HTTPError:
            return self._failure("retry", FailureKind.AVAILABILITY)
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return QueryAttempt.retry(
                    Failure(
                        FailureKind.RATE_LIMIT, "yanwen", "listWarehouses", "Yanwen rate limited the query"
                    ),
                    retry_after,
                )
            if status == 408 or status >= 500:
                return QueryAttempt.retry(
                    Failure(
                        FailureKind.AVAILABILITY,
                        "yanwen",
                        "listWarehouses",
                        "Yanwen warehouse API unavailable",
                    ),
                    retry_after,
                )
            body = await response.aread()
            if len(body) > self.client.config.max_response_bytes:
                return self._failure("invalidResponse", FailureKind.RESPONSE_TOO_LARGE)
        except httpx.HTTPError:
            return self._failure("retry", FailureKind.AVAILABILITY)
        finally:
            await response.aclose()
        if status != 200:
            kind = (
                FailureKind.AUTHENTICATION
                if status == 401
                else FailureKind.AUTHORIZATION
                if status == 403
                else FailureKind.VALIDATION
                if status in {400, 404, 412}
                else FailureKind.PROVIDER_REJECTION
            )
            return self._failure("providerRejected", kind)
        try:
            wire = json.loads(body)
        except (ValueError, UnicodeError):
            return self._failure("invalidResponse", FailureKind.PROTOCOL)
        if (
            not isinstance(wire, dict)
            or type(wire.get("success")) is not bool
            or not isinstance(wire.get("code"), str)
            or not wire["code"]
        ):
            return self._failure("invalidResponse", FailureKind.PROTOCOL)
        if not wire["success"] or wire["code"] != "0":
            if wire["code"].startswith("-"):
                return self._failure("retry", FailureKind.AVAILABILITY)
            return self._failure("providerRejected", FailureKind.PROVIDER_REJECTION)
        data = wire.get("data")
        if not isinstance(data, list) or len(data) > 500:
            return self._failure("invalidResponse", FailureKind.PROTOCOL)
        warehouses: list[Warehouse] = []
        for item in data:
            if not isinstance(item, dict) or any(
                not isinstance(item.get(field), str) or not item[field] for field in ("code", "name", "area")
            ):
                return self._failure("invalidResponse", FailureKind.PROTOCOL)
            warehouses.append(Warehouse(item["code"], item["name"], item["area"]))
        return QueryAttempt.branch_result("listed", WarehouseList(tuple(warehouses)), Receipt(), None)
