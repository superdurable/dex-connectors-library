# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Signed Winit warehouse catalog Query."""

import hashlib
import json
from datetime import UTC, datetime, timedelta, timezone

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


def _valid_secret(value: str) -> bool:
    return (
        isinstance(value, str)
        and 0 < len(value) <= 2048
        and value.strip() == value
        and all(0x20 < ord(c) < 0x7F for c in value)
    )


def _sign(parameters: dict[str, str], secret: str) -> str:
    joined = secret + "".join(key + value for key, value in sorted(parameters.items()) if value) + secret
    return hashlib.md5(joined.encode()).hexdigest().upper()


class Client:
    """Resolve seller and application credentials for each Query."""

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
            or not 1 <= len(config.platform) <= 128
            or not all(c.isascii() and (c.isalnum() or c in "_-") for c in config.platform)
        ):
            raise ValueError("Winit configuration and credentials are invalid")
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
    """Read bounded authorized overseas warehouses."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query contract."""
        return LIST_WAREHOUSES_DEFINITION

    def _failure(self, branch: str, kind: FailureKind) -> QueryAttempt[WarehousePage]:
        failure = Failure(kind, "winit", "listWarehouses", "Winit warehouse query failed")
        if branch == "retry":
            return QueryAttempt.retry(failure)
        return QueryAttempt.branch_result(branch, WarehousePage(), Receipt(), failure)

    async def invoke(self, call: Call, input: ListWarehousesInput) -> QueryAttempt[WarehousePage]:
        """Issue one signed warehouse query and classify provider results."""
        try:
            credentials = await self.client.credentials.resolve(call)
        except (LookupError, ValueError, OSError, AttributeError):
            return self._failure("defect", FailureKind.AUTHENTICATION)
        values = [
            credentials.app_key.reveal(),
            credentials.token.reveal(),
            credentials.client_id.reveal(),
            credentials.client_secret.reveal(),
        ]
        if not all(_valid_secret(value) for value in values):
            return self._failure("defect", FailureKind.AUTHENTICATION)
        stamp = (
            (self.client.now or datetime.now(UTC))
            .astimezone(timezone(timedelta(hours=8)))
            .strftime("%Y-%m-%d %H:%M:%S")
        )
        parameters = {
            "action": "queryWarehouse",
            "app_key": values[0],
            "format": "json",
            "platform": self.client.config.platform,
            "sign_method": "md5",
            "timestamp": stamp,
            "version": "1.0",
        }
        body = {
            "action": parameters["action"],
            "app_key": parameters["app_key"],
            "client_id": values[2],
            "client_sign": _sign(parameters, values[3]),
            "format": parameters["format"],
            "platform": parameters["platform"],
            "sign": _sign(parameters, values[1]),
            "sign_method": parameters["sign_method"],
            "timestamp": stamp,
            "version": "1.0",
        }
        try:
            request = self.client.http_client.build_request(
                "POST",
                self.client.config.base_url.rstrip("/") + "/openapi/service",
                json=body,
                headers={"Accept": "application/json", "Content-Type": "application/json"},
            )
            response, _ = await send_traced(self.client.http_client, request)
        except (httpx.HTTPError, ProviderTransportError):
            return self._failure("retry", FailureKind.AVAILABILITY)
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return QueryAttempt.retry(
                    Failure(
                        FailureKind.RATE_LIMIT, "winit", "listWarehouses", "Winit rate limited the query"
                    ),
                    retry_after,
                )
            if status == 408 or status >= 500:
                return QueryAttempt.retry(
                    Failure(FailureKind.AVAILABILITY, "winit", "listWarehouses", "Winit unavailable"),
                    retry_after,
                )
            response_body = await read_bounded_body(response, self.client.config.max_response_bytes)
        except BodyTooLargeError:
            return self._failure("invalidResponse", FailureKind.RESPONSE_TOO_LARGE)
        except (httpx.HTTPError, ProviderTransportError):
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
                if status in {400, 404}
                else FailureKind.PROVIDER_REJECTION
            )
            return self._failure("providerRejected", kind)
        try:
            wire = json.loads(
                response_body, object_pairs_hook=lambda pairs: {name.lower(): value for name, value in pairs}
            )
        except (UnicodeError, ValueError):
            return self._failure("invalidResponse", FailureKind.PROTOCOL)
        if not isinstance(wire, dict) or type(wire.get("code")) is not int:
            return self._failure("invalidResponse", FailureKind.PROTOCOL)
        if not -(1 << 63) <= wire["code"] < (1 << 63):
            return self._failure("invalidResponse", FailureKind.PROTOCOL)
        data = wire.get("data")
        if data is not None and not isinstance(data, list):
            return self._failure("invalidResponse", FailureKind.PROTOCOL)
        warehouses: list[Warehouse] = []
        for item in data or []:
            if item is None:
                item = {}
            if not isinstance(item, dict):
                return self._failure("invalidResponse", FailureKind.PROTOCOL)
            identifier = item.get("warehouseid")
            if identifier is not None and (
                type(identifier) is not int or not -(1 << 63) <= identifier < (1 << 63)
            ):
                return self._failure("invalidResponse", FailureKind.PROTOCOL)
            code, name = item.get("warehousecode"), item.get("warehousename")
            if any(value is not None and not isinstance(value, str) for value in (code, name)):
                return self._failure("invalidResponse", FailureKind.PROTOCOL)
            warehouses.append(Warehouse(identifier or 0, code or "", name or ""))
        if wire["code"] != 0:
            return self._failure("providerRejected", FailureKind.PROVIDER_REJECTION)
        if (
            data is None
            or len(warehouses) > 200
            or any(row.id <= 0 or not row.code or not row.name for row in warehouses)
        ):
            return self._failure("invalidResponse", FailureKind.PROTOCOL)
        return QueryAttempt.branch_result("listed", WarehousePage(tuple(warehouses)), Receipt(), None)
