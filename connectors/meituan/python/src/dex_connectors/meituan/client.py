# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Meituan Delivery signed order-status Query."""

from __future__ import annotations

import hashlib
import json
from collections.abc import Callable
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
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import QUERY_ORDER_STATUS_DEFINITION
from .models import OrderStatusResult, QueryOrderStatusInput


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[OrderStatusResult]:
    return QueryAttempt.branch_result(
        branch, OrderStatusResult(), Receipt(), Failure(kind, "meituan", "queryOrderStatus", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[OrderStatusResult]:
    return QueryAttempt.retry(Failure(kind, "meituan", "queryOrderStatus", message), delay)


def _valid_order_id(value: str) -> bool:
    return (
        bool(value)
        and len(value) <= 32
        and value.strip() == value
        and all(33 <= ord(c) <= 126 for c in value)
    )


def _valid_credential(value: str) -> bool:
    return bool(value) and len(value) <= 2048 and value.strip() == value


class Client:
    """Own Meituan settings and resolve credentials for each order query."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        *,
        clock: Callable[[], datetime] | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Meituan configuration and credential source are required")
        if config.timeout_seconds <= 0 or config.max_response_bytes <= 0:
            raise ValueError("Meituan timeout and response limit must be positive")
        self.base_url = validate_base_url(config.base_url)
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None
        self.clock = clock or (lambda: datetime.now(UTC))

    def query_order_status(self) -> QueryOrderStatusOperation:
        """Bind the order-status Query to this client."""
        return QueryOrderStatusOperation(self)

    async def aclose(self) -> None:
        """Close the transport if this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class QueryOrderStatusOperation:
    """Read delivery status while discarding rider information."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Meituan client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query identity and retry policy."""
        return QUERY_ORDER_STATUS_DEFINITION

    async def invoke(self, call: Call, input: QueryOrderStatusInput) -> QueryAttempt[OrderStatusResult]:
        """Sign one order request and classify the provider response."""
        if (
            not isinstance(input, QueryOrderStatusInput)
            or input.delivery_id <= 0
            or not _valid_order_id(input.meituan_order_id)
        ):
            return _branch("defect", FailureKind.VALIDATION, "Meituan delivery order identifiers are invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
            app_key, secret = credentials.app_key, credentials.secret.reveal()
            if not _valid_credential(app_key) or not _valid_credential(secret):
                raise ValueError("invalid credentials")
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch(
                "defect",
                FailureKind.AUTHENTICATION,
                "Meituan Delivery credentials are unavailable or invalid",
            )
        form = {
            "appkey": app_key,
            "timestamp": str(int(self.client.clock().timestamp())),
            "version": "1.0",
            "delivery_id": str(input.delivery_id),
            "mt_peisong_id": input.meituan_order_id,
        }
        payload = secret + "".join(key + form[key] for key in sorted(form) if form[key])
        form["sign"] = hashlib.sha1(payload.encode()).hexdigest()
        try:
            request = self.client.http_client.build_request(
                "POST",
                f"{self.client.base_url}/api/order/status/query",
                content=urlencode(sorted(form.items())),
                headers={
                    "Content-Type": "application/x-www-form-urlencoded; charset=UTF-8",
                    "Accept": "application/json",
                },
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "Meituan Delivery order request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "Meituan Delivery order request failed")
        try:
            status = response.status_code
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Meituan Delivery rate limited order query", delay)
            if status == 408 or status >= 500:
                return _retry(
                    FailureKind.AVAILABILITY, "Meituan Delivery order API is temporarily unavailable", delay
                )
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Meituan Delivery response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Meituan Delivery order response was interrupted")
        finally:
            await response.aclose()
        if status != 200:
            kind = FailureKind.PROVIDER_REJECTION
            if status == 401:
                kind = FailureKind.AUTHENTICATION
            elif status == 403:
                kind = FailureKind.AUTHORIZATION
            elif status in (400, 404, 412):
                kind = FailureKind.VALIDATION
            return _branch(
                "providerRejected", kind, f"Meituan Delivery rejected order query with HTTP {status}"
            )
        try:
            wire: Any = json.loads(body)
        except (ValueError, UnicodeDecodeError):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Meituan Delivery returned an invalid order response"
            )
        if not isinstance(wire, dict) or type(wire.get("code")) is not int:
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Meituan Delivery returned an invalid order response"
            )
        code = wire["code"]
        if code == 11:
            return _retry(FailureKind.RATE_LIMIT, "Meituan Delivery rate limited order query", delay)
        if code != 0:
            return _branch(
                "providerRejected", FailureKind.PROVIDER_REJECTION, "Meituan Delivery rejected order query"
            )
        data = wire.get("data")
        if (
            not isinstance(data, dict)
            or type(data.get("delivery_id")) is not int
            or data["delivery_id"] != input.delivery_id
            or data.get("mt_peisong_id") != input.meituan_order_id
            or not _valid_order_id(data.get("order_id", ""))
            or type(data.get("status")) is not int
            or data["status"] not in (0, 20, 30, 50, 99)
            or type(data.get("operate_time")) is not int
            or data["operate_time"] <= 0
        ):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Meituan Delivery returned an invalid order response"
            )
        return QueryAttempt.branch_result(
            "found",
            OrderStatusResult(
                delivery_id=data["delivery_id"],
                meituan_order_id=data["mt_peisong_id"],
                merchant_order_id=data["order_id"],
                status=data["status"],
                operated_at=data["operate_time"],
            ),
            Receipt(),
        )
