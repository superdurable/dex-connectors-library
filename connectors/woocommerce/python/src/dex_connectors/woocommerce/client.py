# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""WooCommerce provider reads and Go-compatible branch classification."""

import base64
import json
from collections.abc import Callable
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from typing import Any, TypeVar
from urllib.parse import urlencode

import httpx
from dex_connectors.sdk import (
    BranchDefinition,
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    OperationRef,
    QueryAttempt,
    QueryDefinition,
    ReauthorizationRequiredError,
    Receipt,
    SecretString,
    StepDefaults,
)
from dex_connectors.sdk.providerhttp import (
    BodyTooLargeError,
    ProviderTransportError,
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
    validate_base_url,
)
from dex_connectors.sdk.retry_policy import default_connector_step_execute_retry_policy

from .models import GetOrderInput, ListOrdersInput, Order, OrderPage

CONNECTOR_ID = "woocommerce"
LIST_ORDERS_OPERATION_ID = "listOrders"
GET_ORDER_OPERATION_ID = "getOrder"
LISTED = "listed"
FOUND = "found"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
INVALID_RESPONSE = "invalidResponse"
DEFECT = "defect"
_API_PATH = "/wp-json/wc/v3"
_DEFAULT_TIMEOUT_SECONDS = 15
_DEFAULT_MAX_RESPONSE_BYTES = 2_097_152
_STEP_DEFAULTS = StepDefaults(
    timedelta(seconds=30), 2, execute_retry=default_connector_step_execute_retry_policy()
)
_SHARED_BRANCHES = (
    BranchDefinition(NOT_FOUND, "The requested resource was not found.", True),
    BranchDefinition(PROVIDER_REJECTED, "The provider conclusively rejected the request.", True),
    BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized JSON.", True),
    BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
)
LIST_ORDERS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_ORDERS_OPERATION_ID),
    (BranchDefinition(LISTED, "List one page of WooCommerce orders."), *_SHARED_BRANCHES),
    _STEP_DEFAULTS,
)
GET_ORDER_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_ORDER_OPERATION_ID),
    (BranchDefinition(FOUND, "Read one WooCommerce order."), *_SHARED_BRANCHES),
    _STEP_DEFAULTS,
)

Output = TypeVar("Output")


@dataclass(frozen=True, slots=True)
class Config:
    """WooCommerce settings; store_url is the WordPress Site Address, other zero values use defaults."""

    store_url: str = ""
    timeout_seconds: float = _DEFAULT_TIMEOUT_SECONDS
    max_response_bytes: int = _DEFAULT_MAX_RESPONSE_BYTES


@dataclass(frozen=True, slots=True)
class Credentials:
    """WooCommerce REST API consumer key pair, resolved anew for every provider attempt."""

    consumer_key: SecretString
    consumer_secret: SecretString


class Client:
    """One redirect-safe HTTP client and call-time credential source."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None:
            raise ValueError("WooCommerce configuration is required")
        if credentials is None:
            raise ValueError("WooCommerce credential source is required")
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("timeout and maxResponseBytes must be positive")
        if not config.store_url:
            raise ValueError("configuration storeUrl is required")
        store_url = validate_base_url(config.store_url)
        if store_url.lower().endswith(_API_PATH):
            raise ValueError("storeUrl must be the WordPress Site Address without /wp-json/wc/v3")
        self.base_url = store_url + _API_PATH
        self.max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(
            config.timeout_seconds or _DEFAULT_TIMEOUT_SECONDS
        )

    def list_orders(self) -> "ListOrdersOperation":
        """Bind the order page Query to this client."""
        return ListOrdersOperation(self)

    def get_order(self) -> "GetOrderOperation":
        """Bind the order lookup Query to this client."""
        return GetOrderOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider HTTP client after application shutdown."""
        await self.http_client.aclose()

    async def authorize(self, call: Call) -> dict[str, str] | None:
        """Return the authentication headers, or None when credentials are unusable."""
        try:
            credentials = await self.credentials.resolve(call)
        except (ValueError, LookupError, OSError, ReauthorizationRequiredError):
            return None
        key = credentials.consumer_key.reveal()
        secret = credentials.consumer_secret.reveal()
        if (
            not key
            or not secret
            or not is_header_safe_credential(key)
            or not is_header_safe_credential(secret)
        ):
            return None
        return {"Authorization": "Basic " + base64.b64encode(f"{key}:{secret}".encode()).decode()}

    async def execute_read(
        self,
        call: Call,
        operation: str,
        path: str,
        query: dict[str, str],
        empty: Output,
        success: str,
        decode: Callable[[Any], Output],
    ) -> QueryAttempt[Output]:
        """Send one GET and classify it exactly like the Go executeRead helper."""
        endpoint = self.base_url + path
        if query:
            endpoint += "?" + urlencode(sorted(query.items()))
        headers = await self.authorize(call)
        if headers is None:
            return _failed(
                operation,
                DEFECT,
                empty,
                FailureKind.AUTHENTICATION,
                "WooCommerce credentials are unavailable",
            )
        try:
            request = self.http_client.build_request(
                "GET", endpoint, headers={"Accept": "application/json", **headers}
            )
        except (httpx.HTTPError, ValueError):
            return _failed(
                operation, DEFECT, empty, FailureKind.LOCAL_DEFECT, "provider request could not be built"
            )
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError:
            return _retry(operation, FailureKind.AVAILABILITY, "provider read failed", 0)
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _retry(
                    operation, FailureKind.RATE_LIMIT, "provider rate limited the read", retry_after
                )
            if status == 408 or status >= 500:
                return _retry(
                    operation,
                    FailureKind.AVAILABILITY,
                    "provider read is temporarily unavailable",
                    retry_after,
                )
            if status == 404:
                return _failed(
                    operation, NOT_FOUND, empty, FailureKind.PROVIDER_REJECTION, "resource was not found"
                )
            if status == 401:
                return _failed(
                    operation,
                    PROVIDER_REJECTED,
                    empty,
                    FailureKind.AUTHENTICATION,
                    "provider rejected credentials",
                )
            if status == 403:
                return _failed(
                    operation, PROVIDER_REJECTED, empty, FailureKind.AUTHORIZATION, "provider denied access"
                )
            if status != 200:
                return _failed(
                    operation,
                    PROVIDER_REJECTED,
                    empty,
                    FailureKind.PROVIDER_REJECTION,
                    f"provider rejected read with HTTP {status}",
                )
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _failed(
                    operation,
                    INVALID_RESPONSE,
                    empty,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "provider response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(operation, FailureKind.AVAILABILITY, "provider response was interrupted", 0)
            try:
                value = decode(json.loads(body))
            except (ValueError, TypeError, UnicodeDecodeError):
                return _failed(
                    operation, INVALID_RESPONSE, empty, FailureKind.PROTOCOL, "provider returned invalid JSON"
                )
            return QueryAttempt.branch_result(success, value, Receipt())
        finally:
            await response.aclose()


class ListOrdersOperation:
    """Read one page of WooCommerce orders."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("WooCommerce client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return LIST_ORDERS_DEFINITION

    async def invoke(self, call: Call, input: ListOrdersInput) -> QueryAttempt[OrderPage]:
        """Validate the page, read once, then select a branch or safe retry."""
        operation = LIST_ORDERS_OPERATION_ID
        if (
            not isinstance(input, ListOrdersInput)
            or type(input.page) is not int
            or type(input.per_page) is not int
            or input.page < 0
            or not 0 <= input.per_page <= 100
        ):
            return _failed(
                operation,
                DEFECT,
                OrderPage(),
                FailureKind.VALIDATION,
                "page must be positive and perPage at most 100",
            )
        query: dict[str, str] = {}
        if input.page > 0:
            query["page"] = str(input.page)
        if input.per_page > 0:
            query["per_page"] = str(input.per_page)
        return await self.client.execute_read(
            call, operation, "/orders", query, OrderPage(), LISTED, _decode_order_page
        )


class GetOrderOperation:
    """Read one WooCommerce order by ID."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("WooCommerce client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return GET_ORDER_DEFINITION

    async def invoke(self, call: Call, input: GetOrderInput) -> QueryAttempt[Order]:
        """Validate the order ID, read once, then select a branch or safe retry."""
        operation = GET_ORDER_OPERATION_ID
        if not isinstance(input, GetOrderInput) or type(input.order_id) is not int or input.order_id <= 0:
            return _failed(operation, DEFECT, Order(), FailureKind.VALIDATION, "orderId must be positive")
        return await self.client.execute_read(
            call, operation, f"/orders/{input.order_id}", {}, Order(), FOUND, _decode_complete_order
        )


def _failed(
    operation: str, branch: str, value: Output, kind: FailureKind, message: str
) -> QueryAttempt[Output]:
    return QueryAttempt.branch_result(
        branch, value, Receipt(), Failure(kind, CONNECTOR_ID, operation, message)
    )


def _retry(operation: str, kind: FailureKind, message: str, after_seconds: float) -> QueryAttempt[Any]:
    return QueryAttempt.retry(Failure(kind, CONNECTOR_ID, operation, message), after_seconds)


def _decode_order_page(document: Any) -> OrderPage:
    if not isinstance(document, list):
        raise ValueError
    return OrderPage(tuple(_decode_order(order) for order in document))


def _decode_complete_order(document: Any) -> Order:
    order = _decode_order(document)
    if order.id <= 0:
        raise ValueError
    return order


def _decode_order(document: Any) -> Order:
    document = _object(document)
    order_id = document.get("id")
    if order_id is None:
        order_id = 0
    if type(order_id) is not int:
        raise ValueError
    return Order(
        order_id, _string(document, "status"), _string(document, "total"), _string(document, "currency")
    )


def _object(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ValueError
    return value


def _string(document: dict[str, Any], name: str) -> str:
    value = document.get(name)
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError
    return value
