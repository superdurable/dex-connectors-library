# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded TikTok campaign reads with Go-compatible outcomes."""

from __future__ import annotations

import json
from dataclasses import dataclass
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
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import LIST_CAMPAIGNS_DEFINITION
from .models import CampaignMetadata, CampaignsPage, ListCampaignsInput


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One bounded provider response or local failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "tiktok-business", "listCampaigns", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[CampaignsPage]:
    return QueryAttempt.branch_result(branch, CampaignsPage(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[CampaignsPage]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _valid_decimal_id(identifier: object) -> bool:
    return (
        isinstance(identifier, str)
        and 1 <= len(identifier) <= 20
        and identifier[0] != "0"
        and identifier.isascii()
        and identifier.isdecimal()
    )


# TikTok reports API outcomes in the body code of an HTTP 200 response.
_CODE_RATE_LIMITED = 40100


def _is_system_error_code(code: int) -> bool:
    return 50000 <= code <= 59999


def _decode_page(body: bytes, requested_page: int, page_size: int) -> tuple[CampaignsPage, int]:
    """Return the page for body code zero, otherwise the nonzero API code."""
    raw: Any = json.loads(body)
    if not isinstance(raw, dict) or type(raw.get("code")) is not int:
        raise ValueError("missing API code")
    if raw["code"] != 0:
        return CampaignsPage(), raw["code"]
    data = raw.get("data")
    if not isinstance(data, dict):
        raise ValueError("missing campaign data")
    entries = data.get("list")
    page_info = data.get("page_info")
    if not isinstance(page_info, dict):
        raise ValueError("missing pagination")
    total_pages = page_info.get("total_page")
    if (
        not isinstance(entries, list)
        or len(entries) > page_size
        or type(page_info.get("page")) is not int
        or page_info["page"] != requested_page
        or type(total_pages) is not int
        or not 0 <= total_pages <= 1_000_000
    ):
        raise ValueError("invalid campaign pagination")
    campaigns: list[CampaignMetadata] = []
    seen: set[str] = set()
    for entry in entries:
        if not isinstance(entry, dict):
            raise ValueError("invalid campaign")
        campaign_id = entry.get("campaign_id")
        name = entry.get("campaign_name")
        if not _valid_decimal_id(campaign_id) or not isinstance(name, str) or not name.strip():
            raise ValueError("invalid campaign")
        assert isinstance(campaign_id, str)
        if campaign_id in seen:
            raise ValueError("duplicate campaign")
        seen.add(campaign_id)
        campaigns.append(CampaignMetadata(campaign_id, name))
    return CampaignsPage(tuple(campaigns), requested_page, total_pages), 0


class Client:
    """Resolve a Marketing API token per call and read a campaign page."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("TikTok configuration and credential source are required")
        if config.timeout_seconds <= 0 or config.max_response_bytes < 1:
            raise ValueError("TikTok request limits must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def list_campaigns(self) -> ListCampaignsOperation:
        """Bind the listCampaigns Query to this client."""
        return ListCampaignsOperation(self)

    async def aclose(self) -> None:
        """Close the transport after Worker shutdown when this client owns it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _get(self, call: Call, advertiser_id: str, page: int, page_size: int) -> _Exchange:
        try:
            credentials = await self.credentials.resolve(call)
            token = credentials.access_token.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return _Exchange(error="credential")
        if not is_header_safe_credential(token):
            return _Exchange(error="credential")
        query = urlencode(
            sorted(
                {
                    "advertiser_id": advertiser_id,
                    "page": str(page),
                    "page_size": str(page_size),
                }.items()
            )
        )
        try:
            request = self.http_client.build_request(
                "GET",
                self.base_url + "/campaign/get/?" + query,
                headers={"Accept": "application/json", "Access-Token": token},
            )
            response, _ = await send_traced(self.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _Exchange(error="request")
        try:
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _Exchange(status=response.status_code, retry_after=retry_after, error="too_large")
            except ProviderTransportError:
                return _Exchange(status=response.status_code, retry_after=retry_after, error="interrupted")
            if token.encode() in body:
                return _Exchange(status=response.status_code, retry_after=retry_after, error="credential")
            return _Exchange(status=response.status_code, body=body, retry_after=retry_after)
        finally:
            await response.aclose()


class ListCampaignsOperation:
    """Read one campaign page and classify provider outcomes."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated campaign Query contract."""
        return LIST_CAMPAIGNS_DEFINITION

    async def invoke(self, call: Call, input: ListCampaignsInput) -> QueryAttempt[CampaignsPage]:
        """Read one page and classify retries, rejection, and protocol errors."""
        if (
            not isinstance(input, ListCampaignsInput)
            or not _valid_decimal_id(input.advertiser_id)
            or not 0 <= input.page <= 1_000_000
            or not 0 <= input.page_size <= 1000
        ):
            return _branch("defect", FailureKind.VALIDATION, "advertiser ID or pagination is invalid")
        page = input.page or 1
        page_size = input.page_size or 10
        exchange = await self.client._get(call, input.advertiser_id, page, page_size)
        if exchange.error == "credential":
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "TikTok credentials are unavailable or invalid"
            )
        if exchange.error == "request":
            return _retry(FailureKind.TRANSPORT, "TikTok request failed")
        if exchange.status == 429:
            return _retry(FailureKind.RATE_LIMIT, "TikTok rate limited the request", exchange.retry_after)
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(FailureKind.AVAILABILITY, "TikTok is temporarily unavailable", exchange.retry_after)
        if exchange.status != 200:
            return _branch(
                "providerRejected", FailureKind.PROVIDER_REJECTION, f"TikTok rejected HTTP {exchange.status}"
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "TikTok response exceeded the configured limit",
            )
        if exchange.error:
            return _retry(FailureKind.TRANSPORT, "TikTok response was interrupted")
        try:
            result, api_code = _decode_page(exchange.body, page, page_size)
        except (ValueError, TypeError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "TikTok returned invalid campaign JSON")
        if api_code == _CODE_RATE_LIMITED:
            return _retry(FailureKind.RATE_LIMIT, "TikTok rate limited the request", exchange.retry_after)
        if _is_system_error_code(api_code):
            return _retry(FailureKind.AVAILABILITY, "TikTok is temporarily unavailable", exchange.retry_after)
        if api_code != 0:
            return _branch(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                f"TikTok rejected the campaign query with code {api_code}",
            )
        return QueryAttempt.branch_result("listed", result, Receipt())
