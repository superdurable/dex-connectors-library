# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Signed Dada status Query and Go-compatible provider outcome classification."""

from __future__ import annotations

import hashlib
import json
from collections.abc import Callable
from datetime import UTC, datetime
from typing import Any
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
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import CONNECTOR_ID as CONNECTOR_ID
from ._generated.definitions import DEFECT as DEFECT
from ._generated.definitions import FOUND as FOUND
from ._generated.definitions import INVALID_RESPONSE as INVALID_RESPONSE
from ._generated.definitions import PROVIDER_REJECTED as PROVIDER_REJECTED
from ._generated.definitions import QUERY_ORDER_STATUS_DEFINITION as QUERY_ORDER_STATUS_DEFINITION
from ._generated.definitions import QUERY_ORDER_STATUS_OPERATION_ID as QUERY_ORDER_STATUS_OPERATION_ID
from .models import OrderStatus, QueryOrderStatusInput

_DEFAULT_BASE_URL = "https://newopen.imdada.cn"
_DEFAULT_TIMEOUT_SECONDS = 15
_DEFAULT_MAX_RESPONSE_BYTES = 65_536
_ORDER_STATUS_PATH = "/api/order/status/query"
_INT64_MIN = -(2**63)
_INT64_MAX = 2**63 - 1


def _is_opaque_credential(value: str) -> bool:
    return bool(value) and len(value.encode()) <= 4096 and value.strip() == value and all(
        33 <= ord(character) <= 126 for character in value
    )


def _is_source_id(value: str) -> bool:
    return bool(value) and len(value.encode()) <= 20 and value.isascii() and value.isdecimal()


def _is_order_id(value: str) -> bool:
    return (
        bool(value)
        and len(value.encode()) <= 128
        and value.strip() == value
        and all(ord(character) >= 32 and ord(character) != 127 for character in value)
    )


def _sign_request(secret: str, app_key: str, body: str, source_id: str, timestamp: str) -> str:
    fields = (
        "app_key" + app_key + "body" + body + "formatjson" + "source_id" + source_id
        + "timestamp" + timestamp + "v1.0"
    )
    return hashlib.md5((secret + fields + secret).encode()).hexdigest().upper()  # noqa: S324


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, CONNECTOR_ID, QUERY_ORDER_STATUS_OPERATION_ID, message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[OrderStatus]:
    return QueryAttempt.branch_result(branch, OrderStatus(), Receipt(), _failure(kind, message))


def _wire_field(document: dict[str, Any], name: str) -> Any:
    if name in document:
        return document[name]
    return next((value for key, value in document.items() if key.casefold() == name.casefold()), None)


class Client:
    """Own the Dada provider settings and resolve credentials for each Query."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        clock: Callable[[], datetime] | None = None,
    ) -> None:
        """Validate the host root and inject an optional deterministic test clock."""
        if config is None or credentials is None:
            raise ValueError("Dada settings and credential source are required")
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("Dada timeout and maxResponseBytes must be positive")
        base_url = validate_base_url(config.base_url or _DEFAULT_BASE_URL)
        if urlsplit(base_url).path:
            raise ValueError("Dada API URL must be a host root without a path")
        self.base_url = base_url
        self.max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(
            config.timeout_seconds or _DEFAULT_TIMEOUT_SECONDS
        )
        self.clock = clock or (lambda: datetime.now(UTC))

    def query_order_status(self) -> QueryOrderStatusOperation:
        """Bind the read-only order status Query."""
        return QueryOrderStatusOperation(self)

    async def aclose(self) -> None:
        """Close the provider HTTP client after Worker shutdown."""
        await self.http_client.aclose()


class QueryOrderStatusOperation:
    """Read one delivery order's status without rider personal data."""

    def __init__(self, client: Client) -> None:
        """Bind a validated Dada client."""
        if client is None:
            raise ValueError("Dada client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return generated Query identity and branches."""
        return QUERY_ORDER_STATUS_DEFINITION

    async def invoke(self, call: Call, input: QueryOrderStatusInput) -> QueryAttempt[OrderStatus]:
        """Sign a POST and classify Dada's definite and retryable outcomes."""
        if not _is_order_id(input.order_id):
            return _branch(DEFECT, FailureKind.VALIDATION, "Dada order ID is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
            app_key = credentials.app_key
            app_secret = credentials.app_secret.reveal()
            source_id = credentials.source_id
        except Exception:  # noqa: BLE001
            return _branch(DEFECT, FailureKind.AUTHENTICATION, "Dada credentials are unavailable or invalid")
        if not (
            _is_opaque_credential(app_key)
            and _is_opaque_credential(app_secret)
            and _is_source_id(source_id)
        ):
            return _branch(DEFECT, FailureKind.AUTHENTICATION, "Dada credentials are unavailable or invalid")
        body = json.dumps({"order_id": input.order_id}, ensure_ascii=False, separators=(",", ":"))
        timestamp = str(int(self.client.clock().timestamp()))
        signature = _sign_request(app_secret, app_key, body, source_id, timestamp)
        envelope = {
            "app_key": app_key,
            "body": body,
            "format": "json",
            "signature": signature,
            "source_id": source_id,
            "timestamp": timestamp,
            "v": "1.0",
        }
        try:
            request = self.client.http_client.build_request(
                "POST",
                self.client.base_url + _ORDER_STATUS_PATH,
                content=json.dumps(envelope, ensure_ascii=False, separators=(",", ":")).encode(),
                headers={"Content-Type": "application/json; charset=utf-8", "Accept": "application/json"},
            )
        except (httpx.HTTPError, ValueError, TypeError, OverflowError):
            return QueryAttempt.retry(_failure(FailureKind.TRANSPORT, "Dada request failed"))
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return QueryAttempt.retry(_failure(FailureKind.TRANSPORT, "Dada request failed"))
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), self.client.clock())
            if status == 429:
                return QueryAttempt.retry(
                    _failure(FailureKind.RATE_LIMIT, "Dada rate limited the request"), retry_after
                )
            if status == 408 or status >= 500:
                return QueryAttempt.retry(
                    _failure(FailureKind.AVAILABILITY, "Dada is temporarily unavailable"), retry_after
                )
            try:
                content = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    INVALID_RESPONSE, FailureKind.RESPONSE_TOO_LARGE,
                    "Dada response exceeded the configured limit",
                )
            except ProviderTransportError:
                return QueryAttempt.retry(_failure(FailureKind.TRANSPORT, "Dada response was interrupted"))
            if status != 200:
                return _branch(
                    PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION, f"Dada rejected HTTP {status}"
                )
            try:
                document = json.loads(content)
                if not isinstance(document, dict):
                    raise ValueError("status response is not an object")
            except (ValueError, UnicodeDecodeError):
                return _branch(INVALID_RESPONSE, FailureKind.PROTOCOL, "Dada returned invalid status JSON")
            provider_status = _wire_field(document, "status")
            provider_code = _wire_field(document, "code")
            if (
                provider_status == "fail"
                and type(provider_code) is int
                and _INT64_MIN <= provider_code <= _INT64_MAX
            ):
                return _branch(
                    PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION,
                    f"Dada rejected code {provider_code}",
                )
            result = _wire_field(document, "result")
            if not isinstance(result, dict):
                return _branch(
                    INVALID_RESPONSE, FailureKind.PROTOCOL,
                    "Dada returned incomplete or inconsistent order status",
                )
            order_id = _wire_field(result, "orderId")
            status_code = _wire_field(result, "statusCode")
            status_message = _wire_field(result, "statusMsg")
            if not (
                provider_status == "success"
                and type(provider_code) is int and provider_code == 0
                and order_id == input.order_id
                and type(status_code) is int and _INT64_MIN <= status_code <= _INT64_MAX
                and status_code != 0
                and isinstance(status_message, str) and bool(status_message)
            ):
                return _branch(
                    INVALID_RESPONSE, FailureKind.PROTOCOL,
                    "Dada returned incomplete or inconsistent order status",
                )
            return QueryAttempt.branch_result(
                FOUND, OrderStatus(order_id, status_code, status_message), Receipt()
            )
        finally:
            await response.aclose()


Config.__doc__ = "Dada HTTPS API root and response bounds; blank or zero use manifest defaults."
Credentials.__doc__ = "Application key, private signing secret, and merchant source ID."
