# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Onebound signed 1688 product search and provider decisions."""

from __future__ import annotations

import hashlib
import json
from collections.abc import Callable
from datetime import UTC, datetime, timedelta, timezone
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
from ._generated.definitions import SEARCH_PRODUCTS_DEFINITION
from .models import ProductSearchItem, ProductSearchPage, SearchProductsInput


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ProductSearchPage]:
    return QueryAttempt.branch_result(
        branch, ProductSearchPage(), Receipt(), Failure(kind, "onebound", "searchProducts", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ProductSearchPage]:
    return QueryAttempt.retry(Failure(kind, "onebound", "searchProducts", message), delay)


def _valid_credential(value: str) -> bool:
    return (
        bool(value)
        and len(value) <= 2048
        and value.strip() == value
        and all(32 < ord(c) < 127 for c in value)
    )


def _sign(parameters: dict[str, str], secret: str) -> str:
    if len(secret) != 32:
        secret = hashlib.md5(secret.encode()).hexdigest()
    pairs = [
        f"{name}={parameters[name]}"
        for name in sorted(parameters)
        if name not in ("secret", "sign") and parameters[name] and not parameters[name].startswith("@")
    ]
    return hashlib.md5(("&".join(pairs) + secret).encode()).hexdigest()


class Client:
    """Own Onebound configuration and resolve credentials per search."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        *,
        clock: Callable[[], datetime] | None = None,
    ) -> None:
        if (
            config is None
            or credentials is None
            or config.timeout_seconds <= 0
            or config.max_response_bytes <= 0
        ):
            raise ValueError("valid Onebound configuration and credentials are required")
        self.base_url = validate_base_url(config.base_url)
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None
        self.clock = clock or (lambda: datetime.now(UTC))

    def search_products(self) -> SearchProductsOperation:
        """Bind the product-search Query to this client."""
        return SearchProductsOperation(self)

    async def aclose(self) -> None:
        """Close the owned transport."""
        if self._owns_client:
            await self.http_client.aclose()


class SearchProductsOperation:
    """Search bounded public 1688 listings."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Onebound client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query identity and retry policy."""
        return SEARCH_PRODUCTS_DEFINITION

    async def invoke(self, call: Call, input: SearchProductsInput) -> QueryAttempt[ProductSearchPage]:
        """Sign a bounded search and classify the provider outcome."""
        keyword = input.keyword
        if (
            not keyword
            or len(keyword) > 100
            or keyword.strip() != keyword
            or any(ord(c) < 32 or c in "&=?#" for c in keyword)
            or input.page_number < 0
            or input.page_number > 100000
        ):
            return _branch("defect", FailureKind.VALIDATION, "Onebound keyword or page is invalid")
        page_number = input.page_number or 1
        try:
            credentials = await self.client.credentials.resolve(call)
            key, secret = credentials.api_key.reveal(), credentials.api_secret.reveal()
            if not _valid_credential(key) or not _valid_credential(secret):
                raise ValueError("invalid credentials")
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch("defect", FailureKind.AUTHENTICATION, "Onebound credentials are invalid")
        parameters = {
            "api_name": "item_search",
            "key": key,
            "q": keyword,
            "page": str(page_number),
            "page_size": "40",
            "dateline": self.client.clock()
            .astimezone(timezone(timedelta(hours=8)))
            .strftime("%Y-%m-%d %H:%M:%S"),
        }
        query = {name: value for name, value in parameters.items() if name != "api_name"}
        query["sign"] = _sign(parameters, secret)
        try:
            request = self.client.http_client.build_request(
                "GET",
                f"{self.client.base_url}/1688/item_search/",
                params=query,
                headers={"Accept": "application/json"},
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "Onebound request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "Onebound request failed")
        try:
            status = response.status_code
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Onebound rate limited search", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Onebound is unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse", FailureKind.RESPONSE_TOO_LARGE, "Onebound response exceeded the limit"
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Onebound response was interrupted")
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
            return _branch("providerRejected", kind, f"Onebound rejected HTTP {status}")
        try:
            wire: Any = json.loads(body)
        except (ValueError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Onebound returned invalid JSON")
        if (
            not isinstance(wire, dict)
            or not isinstance(wire.get("error_code"), str)
            or not wire["error_code"]
        ):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Onebound omitted response code")
        code = wire["error_code"]
        if code == "2000":
            return QueryAttempt.branch_result("listed", ProductSearchPage(page_number=page_number), Receipt())
        if code == "4008":
            return _retry(FailureKind.RATE_LIMIT, "Onebound concurrency limit exceeded")
        if code in ("4000", "4001", "4002", "4009", "4017", "5000"):
            return _retry(FailureKind.AVAILABILITY, "Onebound temporary error")
        if code != "0000":
            kind = (
                FailureKind.AUTHENTICATION
                if code in ("4004", "4005")
                else FailureKind.AUTHORIZATION
                if code in ("4006", "4007", "4012", "4013", "4016")
                else FailureKind.PROVIDER_REJECTION
            )
            return _branch("providerRejected", kind, "Onebound rejected search")
        items = wire.get("items")
        products = items.get("item") if isinstance(items, dict) else None
        if (
            not isinstance(items, dict)
            or type(items.get("total_results")) is not int
            or items["total_results"] < 0
            or not isinstance(products, list)
            or len(products) > 40
        ):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Onebound returned invalid page")
        try:
            returned_page = int(items.get("page", ""))
        except (ValueError, TypeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Onebound returned invalid page")
        if returned_page < 1:
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Onebound returned invalid page")
        result_items: list[ProductSearchItem] = []
        for product in products:
            if not isinstance(product, dict) or any(
                not isinstance(product.get(name), str) or not product[name]
                for name in ("num_iid", "title", "price", "detail_url")
            ):
                return _branch("invalidResponse", FailureKind.PROTOCOL, "Onebound returned invalid product")
            result_items.append(
                ProductSearchItem(
                    product_id=product["num_iid"],
                    title=product["title"],
                    price=product["price"],
                    detail_url=product["detail_url"],
                )
            )
        return QueryAttempt.branch_result(
            "listed",
            ProductSearchPage(
                page_number=returned_page, total=items["total_results"], products=tuple(result_items)
            ),
            Receipt(),
        )
