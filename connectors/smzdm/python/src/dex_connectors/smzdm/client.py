# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""SMZDM signed public product search Query."""

from __future__ import annotations

import hashlib
import json
import math
import unicodedata
from collections.abc import Callable
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
from ._generated.definitions import SEARCH_PRODUCTS_DEFINITION
from .models import Product, ProductPage, SearchProductsInput


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ProductPage]:
    return QueryAttempt.branch_result(
        branch, ProductPage(), Receipt(), Failure(kind, "smzdm", "searchProducts", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ProductPage]:
    return QueryAttempt.retry(Failure(kind, "smzdm", "searchProducts", message), delay)


def _valid_credential(value: str) -> bool:
    return (
        bool(value)
        and len(value.encode()) <= 2048
        and value.strip() == value
        and all(32 < ord(c) < 127 for c in value)
    )


def _valid_text(value: str, maximum: int) -> bool:
    return (
        bool(value)
        and len(value) <= maximum
        and value.strip() == value
        and all(unicodedata.category(c) != "Cc" for c in value)
    )


def _classify_status(status: int) -> FailureKind:
    return {
        401: FailureKind.AUTHENTICATION,
        403: FailureKind.AUTHORIZATION,
        400: FailureKind.VALIDATION,
        404: FailureKind.VALIDATION,
    }.get(status, FailureKind.PROVIDER_REJECTION)


class Client:
    """Own SMZDM request settings and resolve app credentials per attempt."""

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
            raise ValueError("SMZDM configuration and credential source are required")
        if not _valid_text(config.request_from, 128):
            raise ValueError("SMZDM request source is invalid")
        self.base_url = validate_base_url(config.base_url)
        self.request_from = config.request_from
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None
        self.clock = clock or (lambda: datetime.now(UTC))

    def search_products(self) -> SearchProductsOperation:
        """Bind the first-page product search Query to this client."""
        return SearchProductsOperation(self)

    async def aclose(self) -> None:
        """Close the transport if this client owns it."""
        if self._owns_client:
            await self.http_client.aclose()


class SearchProductsOperation:
    """Read public product titles and provider-reported prices."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("SMZDM client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query identity and retry policy."""
        return SEARCH_PRODUCTS_DEFINITION

    async def invoke(self, call: Call, input: SearchProductsInput) -> QueryAttempt[ProductPage]:
        """Sign one product search request and classify its outcome."""
        keyword = input.keyword if isinstance(input, SearchProductsInput) else ""
        if not _valid_text(keyword, 100):
            return _branch("defect", FailureKind.VALIDATION, "SMZDM keyword is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
            app_key, app_secret = credentials.app_key.reveal(), credentials.app_secret.reveal()
            if not _valid_credential(app_key) or not _valid_credential(app_secret):
                raise ValueError("invalid app credentials")
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "SMZDM App Key or App Secret is unavailable or invalid"
            )
        parameters = {
            "app_key": app_key,
            "c": "new_wiki",
            "offset": "0",
            "request_from": self.client.request_from,
            "s": keyword,
            "size": "20",
            "timestamp": str(int(self.client.clock().timestamp())),
        }
        message = (
            app_secret
            + "".join(key + parameters[key] for key in sorted(parameters) if parameters[key])
            + app_secret
        )
        query = dict(parameters, sign=hashlib.md5(message.encode()).hexdigest().upper())
        try:
            request = self.client.http_client.build_request(
                "GET",
                f"{self.client.base_url}/v1/searchapi/search/list",
                params=query,
                headers={"Accept": "application/json"},
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "SMZDM product-search request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "SMZDM product-search request failed")
        try:
            status = response.status_code
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "SMZDM rate limited product search", delay)
            if status == 408 or status >= 500:
                return _retry(
                    FailureKind.AVAILABILITY, "SMZDM product search is temporarily unavailable", delay
                )
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "SMZDM response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "SMZDM response was interrupted")
        finally:
            await response.aclose()
        if status != 200:
            return _branch("providerRejected", _classify_status(status), "SMZDM rejected product search")
        try:
            wire: Any = json.loads(body)
        except (ValueError, UnicodeDecodeError):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "SMZDM returned invalid product-search data"
            )
        if not isinstance(wire, dict) or type(wire.get("status")) is not bool:
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "SMZDM returned invalid product-search data"
            )
        if not wire["status"]:
            return _branch(
                "providerRejected", FailureKind.PROVIDER_REJECTION, "SMZDM rejected product search"
            )
        data = wire.get("data")
        total = data.get("total_num") if isinstance(data, dict) else None
        rows = data.get("rows") if isinstance(data, dict) else None
        if type(total) is not int or total < 0 or not isinstance(rows, list) or len(rows) > 20:
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "SMZDM returned invalid product-search data"
            )
        products = []
        for row in rows:
            if (
                not isinstance(row, dict)
                or not isinstance(row.get("title"), str)
                or not row["title"]
                or type(row.get("pro_price")) not in (int, float)
                or not math.isfinite(row["pro_price"])
                or row["pro_price"] < 0
            ):
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "SMZDM returned invalid product-search data"
                )
            products.append(Product(title=row["title"], price_yuan=float(row["pro_price"])))
        return QueryAttempt.branch_result(
            "listed", ProductPage(total=total, products=tuple(products)), Receipt()
        )
