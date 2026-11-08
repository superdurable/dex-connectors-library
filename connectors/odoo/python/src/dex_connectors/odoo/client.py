# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Odoo JSON-2 partner search with Go-compatible outcomes."""

from __future__ import annotations

import json
from dataclasses import dataclass
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
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import SEARCH_PARTNERS_DEFINITION
from .models import Partner, PartnerSearchPage, SearchPartnersInput


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One provider response or request failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "odoo", "searchPartners", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[PartnerSearchPage]:
    return QueryAttempt.branch_result(branch, PartnerSearchPage(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[PartnerSearchPage]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _valid_name_pattern(value: object) -> bool:
    if (
        not isinstance(value, str)
        or not value
        or value.strip() != value
        or any(point in "\x00\r\n" for point in value)
    ):
        return False
    try:
        return len(value.encode("utf-8")) <= 256
    except UnicodeEncodeError:
        return False


def _decode_partners(body: bytes, limit: int) -> PartnerSearchPage:
    raw: Any = json.loads(body)
    if not isinstance(raw, list) or len(raw) > limit:
        raise ValueError("partner list is missing or exceeds the limit")
    partners: list[Partner] = []
    for item in raw:
        if (
            not isinstance(item, dict)
            or type(item.get("id")) is not int
            or not 0 < item["id"] <= 2**63 - 1
            or not isinstance(item.get("name"), str)
            or not item["name"]
        ):
            raise ValueError("partner ID or name is invalid")
        partners.append(Partner(item["id"], item["name"]))
    return PartnerSearchPage(tuple(partners))


class Client:
    """Resolve an Odoo API key per call and search company partners."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Odoo configuration and credential source are required")
        if (
            config.timeout_seconds <= 0
            or type(config.max_response_bytes) is not int
            or config.max_response_bytes < 1
        ):
            raise ValueError("Odoo request limits must be positive")
        base_url = validate_base_url(config.base_url).rstrip("/")
        if urlsplit(base_url).path:
            raise ValueError("Odoo base URL must be an origin without a path")
        if not is_header_safe_credential(config.database) or len(config.database) > 128:
            raise ValueError("Odoo database name is invalid")
        self.base_url = base_url
        self.database = config.database
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def search_partners(self) -> SearchPartnersOperation:
        """Bind the searchPartners Query to this client."""
        return SearchPartnersOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client only when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _search(self, call: Call, body: bytes) -> _Exchange:
        try:
            credential = await self.credentials.resolve(call)
            api_key = credential.api_key.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return _Exchange(error="credential")
        if not is_header_safe_credential(api_key):
            return _Exchange(error="credential")
        try:
            request = self.http_client.build_request(
                "POST",
                self.base_url + "/json/2/res.partner/search_read",
                content=body,
                headers={
                    "Authorization": "Bearer " + api_key,
                    "X-Odoo-Database": self.database,
                    "Content-Type": "application/json",
                    "Accept": "application/json",
                },
            )
            response, _ = await send_traced(self.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _Exchange(error="request")
        try:
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            try:
                response_body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _Exchange(status=response.status_code, retry_after=retry_after, error="too_large")
            except ProviderTransportError:
                return _Exchange(status=response.status_code, retry_after=retry_after, error="interrupted")
            return _Exchange(status=response.status_code, body=response_body, retry_after=retry_after)
        finally:
            await response.aclose()


class SearchPartnersOperation:
    """Search company partners by Odoo ilike name pattern."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Odoo client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query contract."""
        return SEARCH_PARTNERS_DEFINITION

    async def invoke(self, call: Call, input: SearchPartnersInput) -> QueryAttempt[PartnerSearchPage]:
        """Search once and classify provider outcomes consistently with Go."""
        if (
            not isinstance(input, SearchPartnersInput)
            or not _valid_name_pattern(input.name_pattern)
            or type(input.limit) is not int
            or input.limit < 0
            or input.limit > 100
        ):
            return _branch("defect", FailureKind.VALIDATION, "name pattern or limit is invalid")
        limit = input.limit or 20
        body = json.dumps(
            {
                "domain": [["is_company", "=", True], ["name", "ilike", input.name_pattern]],
                "fields": ["id", "name"],
                "limit": limit,
            },
            ensure_ascii=False,
            separators=(",", ":"),
        ).encode("utf-8")
        exchange = await self.client._search(call, body)
        if exchange.error == "credential":
            return _branch("defect", FailureKind.AUTHENTICATION, "Odoo API key is unavailable or invalid")
        if exchange.error == "request":
            return _retry(FailureKind.TRANSPORT, "Odoo request failed")
        if exchange.status == 429:
            return _retry(FailureKind.RATE_LIMIT, "Odoo rate limited the request", exchange.retry_after)
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(FailureKind.AVAILABILITY, "Odoo is temporarily unavailable", exchange.retry_after)
        if exchange.status != 200:
            return _branch(
                "providerRejected", FailureKind.PROVIDER_REJECTION, f"Odoo rejected HTTP {exchange.status}"
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "Odoo response exceeded the configured limit",
            )
        if exchange.error == "interrupted":
            return _retry(FailureKind.TRANSPORT, "Odoo response was interrupted")
        try:
            partners = _decode_partners(exchange.body, limit)
        except (ValueError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Odoo returned invalid partner data")
        return QueryAttempt.branch_result("found", partners, Receipt())
