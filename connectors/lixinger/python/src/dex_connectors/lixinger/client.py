# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read bounded A-share company identifiers from Lixinger."""

import json
import re
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
from ._generated.definitions import LIST_STOCK_COMPANIES_DEFINITION
from .models import ListStockCompaniesInput, ListStockCompaniesOutput, StockCompany


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListStockCompaniesOutput]:
    return QueryAttempt.branch_result(
        branch,
        ListStockCompaniesOutput(),
        Receipt(),
        Failure(kind, "lixinger", "listStockCompanies", message),
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ListStockCompaniesOutput]:
    return QueryAttempt.retry(Failure(kind, "lixinger", "listStockCompanies", message), delay)


class Client:
    """Resolve the current token for each company metadata request."""

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
            or config.max_response_bytes <= 0
        ):
            raise ValueError("Lixinger configuration and credentials are required")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        parsed = urlsplit(self.base_url)
        if parsed.netloc != "open.lixinger.com" or parsed.path or parsed.query or parsed.fragment:
            raise ValueError("Lixinger base URL must be its documented origin")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_http_client = http_client is None

    def list_stock_companies(self) -> "ListStockCompaniesOperation":
        """Bind the A-share company Query."""
        return ListStockCompaniesOperation(self)

    async def aclose(self) -> None:
        """Close an HTTP transport owned by this client."""
        if self._owns_http_client:
            await self.http_client.aclose()


class ListStockCompaniesOperation:
    """Read safe company identifiers without financial data."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated company Query contract."""
        return LIST_STOCK_COMPANIES_DEFINITION

    async def invoke(
        self, call: Call, input: ListStockCompaniesInput
    ) -> QueryAttempt[ListStockCompaniesOutput]:
        """Validate and classify one provider company response."""
        if (
            not isinstance(input, ListStockCompaniesInput)
            or not 1 <= len(input.stock_codes) <= 100
            or any(re.fullmatch(r"[0-9]{6}", code) is None for code in input.stock_codes)
            or len(set(input.stock_codes)) != len(input.stock_codes)
        ):
            return _branch("defect", FailureKind.VALIDATION, "stockCodes are invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
            token = credentials.token.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch("defect", FailureKind.AUTHENTICATION, "Lixinger token is unavailable")
        if not token or len(token) > 512 or any(not 33 <= ord(char) <= 126 for char in token):
            return _branch("defect", FailureKind.AUTHENTICATION, "Lixinger token is invalid")
        payload = json.dumps(
            {"stockCodes": input.stock_codes, "token": token}, ensure_ascii=False, separators=(",", ":")
        )
        try:
            request = self.client.http_client.build_request(
                "POST",
                self.client.base_url + "/api/cn/company",
                content=payload,
                headers={"Content-Type": "application/json", "Accept": "application/json"},
            )
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _retry(FailureKind.AVAILABILITY, "Lixinger request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Lixinger rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Lixinger is unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse", FailureKind.RESPONSE_TOO_LARGE, "Lixinger response is oversized"
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Lixinger response was interrupted")
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return _branch("providerRejected", kind, f"Lixinger rejected HTTP {status}")
            try:
                wire: Any = json.loads(body)
                if not isinstance(wire, dict) or type(wire.get("code")) is not int:
                    raise ValueError("invalid company code")
                if wire["code"] != 1:
                    return _branch(
                        "providerRejected", FailureKind.PROVIDER_REJECTION, "Lixinger rejected the request"
                    )
                items = wire.get("data")
                if not isinstance(items, list) or len(items) > len(input.stock_codes):
                    raise ValueError("invalid company list")
                companies = tuple(
                    StockCompany(
                        item["stockCode"], item["name"], item["exchange"], item.get("listingStatus", "")
                    )
                    for item in items
                )
                seen: set[str] = set()
                for company in companies:
                    if (
                        company.stock_code not in input.stock_codes
                        or company.stock_code in seen
                        or not company.name
                        or len(company.name) > 512
                        or company.exchange not in ("sh", "sz", "bj")
                        or len(company.listing_status) > 128
                    ):
                        raise ValueError("invalid company metadata")
                    seen.add(company.stock_code)
            except (KeyError, TypeError, ValueError, UnicodeDecodeError):
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "Lixinger returned invalid company data"
                )
            return QueryAttempt.branch_result("listed", ListStockCompaniesOutput(companies), Receipt())
        finally:
            await response.aclose()
