# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""FOFA domain search with Go-compatible provider decisions."""

import base64
import ipaddress
import json
from datetime import UTC, datetime
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
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import SEARCH_DOMAIN_ASSETS_DEFINITION
from .models import DomainAsset, SearchDomainAssetsInput, SearchDomainAssetsOutput

_ENDPOINT = "https://fofa.info/api/v1/search/all"


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[SearchDomainAssetsOutput]:
    return QueryAttempt.branch_result(
        branch, SearchDomainAssetsOutput(), Receipt(), Failure(kind, "fofa", "searchDomainAssets", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[SearchDomainAssetsOutput]:
    return QueryAttempt.retry(Failure(kind, "fofa", "searchDomainAssets", message), delay)


def _valid_domain(domain: str) -> bool:
    if (
        not domain
        or len(domain) > 253
        or domain.lower() != domain
        or domain.endswith(".")
        or "." not in domain
    ):
        return False
    return all(
        bool(label)
        and len(label) <= 63
        and not label.startswith("-")
        and not label.endswith("-")
        and all(character in "abcdefghijklmnopqrstuvwxyz0123456789-" for character in label)
        for label in domain.split(".")
    )


class Client:
    """Resolve the FOFA API Key for each search attempt."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("FOFA configuration and credential source are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("FOFA configuration is invalid")
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def search_domain_assets(self) -> "SearchDomainAssetsOperation":
        """Bind the public domain search Query."""
        return SearchDomainAssetsOperation(self)

    async def aclose(self) -> None:
        """Close an HTTP client this instance created."""
        if self._owns_client:
            await self.http_client.aclose()


class SearchDomainAssetsOperation:
    """Search one exact domain and return at most five indexed assets."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("FOFA client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query identity and retry policy."""
        return SEARCH_DOMAIN_ASSETS_DEFINITION

    async def invoke(
        self, call: Call, input: SearchDomainAssetsInput
    ) -> QueryAttempt[SearchDomainAssetsOutput]:
        """Send one provider request and classify its observed outcome."""
        domain = input.domain if isinstance(input, SearchDomainAssetsInput) else ""
        if not _valid_domain(domain):
            return _branch("defect", FailureKind.VALIDATION, "FOFA domain must be a valid DNS name")
        try:
            credentials = await self.client.credentials.resolve(call)
            api_key = credentials.api_key.reveal()
            if not api_key or len(api_key) > 1024 or any(not 0x20 < ord(c) < 0x7F for c in api_key):
                raise ValueError("invalid API Key")
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch("defect", FailureKind.AUTHENTICATION, "FOFA API Key is unavailable or invalid")
        query = urlencode(
            {
                "key": api_key,
                "qbase64": base64.b64encode(f'domain="{domain}"'.encode()).decode(),
                "fields": "host,ip,port",
                "page": "1",
                "size": "5",
                "r_type": "json",
            }
        )
        try:
            request = self.client.http_client.build_request(
                "GET", f"{_ENDPOINT}?{query}", headers={"Accept": "application/json"}
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "FOFA search request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "FOFA search request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "FOFA rate limited search", delay)
            if status == 408 or status >= 500:
                return _retry(
                    FailureKind.AVAILABILITY, "FOFA search service is temporarily unavailable", delay
                )
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else (FailureKind.AUTHORIZATION if status == 403 else FailureKind.PROVIDER_REJECTION)
                )
                return _branch("providerRejected", kind, f"FOFA rejected search with HTTP {status}")
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "FOFA response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "FOFA response was interrupted")
        finally:
            await response.aclose()
        return _decode_response(body)


def _decode_response(body: bytes) -> QueryAttempt[SearchDomainAssetsOutput]:
    try:
        document = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "FOFA returned invalid search data")
    if not isinstance(document, dict) or type(document.get("error")) is not bool:
        return _branch("invalidResponse", FailureKind.PROTOCOL, "FOFA returned invalid search data")
    if document["error"]:
        return _branch("providerRejected", FailureKind.PROVIDER_REJECTION, "FOFA rejected the search")
    total, rows = document.get("size"), document.get("results")
    if type(total) is not int or total < 0 or not isinstance(rows, list) or len(rows) > 5:
        return _branch("invalidResponse", FailureKind.PROTOCOL, "FOFA returned invalid search results")
    assets = []
    for row in rows:
        if not isinstance(row, list) or len(row) != 3 or any(not isinstance(cell, str) for cell in row):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "FOFA returned invalid asset data")
        host, ip_address, port_text = row
        try:
            ipaddress.ip_address(ip_address)
        except ValueError:
            return _branch("invalidResponse", FailureKind.PROTOCOL, "FOFA returned invalid asset data")
        if not host or len(host.encode()) > 2048:
            return _branch("invalidResponse", FailureKind.PROTOCOL, "FOFA returned invalid asset data")
        if not port_text.isascii() or not port_text.isdecimal() or not 1 <= int(port_text) <= 65535:
            return _branch("invalidResponse", FailureKind.PROTOCOL, "FOFA returned invalid asset port")
        assets.append(DomainAsset(host, ip_address, int(port_text)))
    return QueryAttempt.branch_result("found", SearchDomainAssetsOutput(total, tuple(assets)), Receipt())
