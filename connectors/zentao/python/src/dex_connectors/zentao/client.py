# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""ZenTao REST v1 product catalog Query with Go-compatible outcomes."""

import ipaddress
import json
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
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import LIST_PRODUCTS_DEFINITION
from .models import ListProductsInput, Product, ProductList


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ProductList]:
    return QueryAttempt.branch_result(
        branch, ProductList(), Receipt(), Failure(kind, "zentao", "listProducts", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ProductList]:
    return QueryAttempt.retry(Failure(kind, "zentao", "listProducts", message), delay)


def _validated_base_url(value: str) -> str:
    if not value or any(ord(character) <= 0x20 or ord(character) > 0x7E for character in value):
        raise ValueError("ZenTao base URL must be printable ASCII")
    try:
        parsed = urlsplit(value)
        host = parsed.hostname
    except ValueError as error:
        raise ValueError("ZenTao base URL is invalid") from error
    if not host or parsed.username or parsed.password or parsed.query or parsed.fragment:
        raise ValueError("ZenTao base URL is invalid")
    if parsed.scheme != "https":
        try:
            is_loopback = host == "localhost" or ipaddress.ip_address(host).is_loopback
        except ValueError:
            is_loopback = host == "localhost"
        if parsed.scheme != "http" or not is_loopback:
            raise ValueError("ZenTao base URL must use HTTPS")
    return value.rstrip("/")


class Client:
    """Own ZenTao settings and resolve credentials for each product list."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("configuration and credentials are required")
        if config.timeout_seconds <= 0 or config.max_response_bytes <= 0:
            raise ValueError("ZenTao timeout and maxResponseBytes must be positive")
        self.base_url = _validated_base_url(config.base_url)
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def list_products(self) -> "ListProductsOperation":
        """Bind the listProducts Query."""
        return ListProductsOperation(self)

    async def aclose(self) -> None:
        """Close only an HTTP client this instance created."""
        if self._owns_client:
            await self.http_client.aclose()


class ListProductsOperation:
    """Read one ZenTao product page and classify provider outcomes."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("provider client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query identity and retry policy."""
        return LIST_PRODUCTS_DEFINITION

    async def invoke(self, call: Call, input: ListProductsInput) -> QueryAttempt[ProductList]:
        """Make one read-only provider request."""
        try:
            credentials = await self.client.credentials.resolve(call)
            token = credentials.token.reveal()
            if not token or any(ord(character) < 0x21 or ord(character) > 0x7E for character in token):
                raise ValueError("invalid token")
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "ZenTao REST v1 token is unavailable or invalid"
            )
        try:
            request = self.client.http_client.build_request(
                "GET",
                self.client.base_url + "/api.php/v1/products",
                headers={"Accept": "application/json", "Token": token},
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "ZenTao product-list request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "ZenTao product-list request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "ZenTao rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "ZenTao is temporarily unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "ZenTao response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "ZenTao response was interrupted")
        finally:
            await response.aclose()
        if status != 200:
            kind = (
                FailureKind.AUTHENTICATION
                if status == 401
                else FailureKind.AUTHORIZATION
                if status == 403
                else FailureKind.VALIDATION
                if status in (400, 404)
                else FailureKind.PROVIDER_REJECTION
            )
            return _branch("providerRejected", kind, f"ZenTao rejected the query with HTTP {status}")
        return _decode_products(body)


def _decode_products(body: bytes) -> QueryAttempt[ProductList]:
    try:
        document: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return _branch(
            "invalidResponse", FailureKind.PROTOCOL, "ZenTao returned an invalid product-list response"
        )
    if not isinstance(document, dict):
        return _branch(
            "invalidResponse", FailureKind.PROTOCOL, "ZenTao returned an invalid product-list response"
        )
    total, entries = document.get("total"), document.get("products")
    if type(total) is not int or total < 0 or not isinstance(entries, list) or len(entries) > 1000:
        return _branch(
            "invalidResponse", FailureKind.PROTOCOL, "ZenTao returned an invalid product-list response"
        )
    products = []
    for entry in entries:
        if not isinstance(entry, dict):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "ZenTao returned an invalid product-list response"
            )
        identifier, name = entry.get("id"), entry.get("name")
        product_type, access_control = entry.get("type"), entry.get("acl")
        if (
            type(identifier) is not int
            or identifier <= 0
            or not isinstance(name, str)
            or not name
            or product_type not in ("normal", "branch", "platform")
            or access_control not in ("open", "private")
        ):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "ZenTao returned an invalid product-list response"
            )
        code = entry.get("code", "")
        if not isinstance(code, str):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "ZenTao returned an invalid product-list response"
            )
        products.append(Product(identifier, name, code, product_type, access_control))
    return QueryAttempt.branch_result("listed", ProductList(total, tuple(products)), Receipt())
