# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bocha public web search with Go-compatible provider decisions."""

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
from ._generated.definitions import SEARCH_WEB_DEFINITION
from .models import SearchWebInput, SearchWebOutput, WebResult

_ENDPOINT = "https://api.bochaai.com/v1/web-search"


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[SearchWebOutput]:
    return QueryAttempt.branch_result(
        branch, SearchWebOutput(), Receipt(), Failure(kind, "bocha", "searchWeb", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[SearchWebOutput]:
    return QueryAttempt.retry(Failure(kind, "bocha", "searchWeb", message), delay)


def _valid_api_key(value: str) -> bool:
    return bool(value) and len(value) <= 1024 and all(0x20 < ord(character) < 0x7F for character in value)


def _valid_page_url(value: str) -> bool:
    try:
        parsed = urlsplit(value)
        return (
            parsed.scheme == "https"
            and bool(parsed.netloc)
            and bool(parsed.hostname)
            and parsed.username is None
            and parsed.password is None
            and not any(character.isspace() for character in parsed.netloc)
        )
    except ValueError:
        return False


class Client:
    """Own Bocha settings and resolve the API Key for each search attempt."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Bocha configuration and credential source are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("Bocha configuration is invalid")
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def search_web(self) -> "SearchWebOperation":
        """Bind the public web search Query."""
        return SearchWebOperation(self)

    async def aclose(self) -> None:
        """Close only an HTTP client this instance created."""
        if self._owns_client:
            await self.http_client.aclose()


class SearchWebOperation:
    """Search public pages while excluding provider metadata from the output."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Bocha client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated search Query identity and retry policy."""
        return SEARCH_WEB_DEFINITION

    async def invoke(self, call: Call, input: SearchWebInput) -> QueryAttempt[SearchWebOutput]:
        """Make one provider request and classify its observed outcome."""
        query = input.query if isinstance(input, SearchWebInput) else ""
        if not query.strip() or len(query) > 256:
            return _branch(
                "defect", FailureKind.VALIDATION, "Bocha query must contain 1 to 256 Unicode characters"
            )
        try:
            credentials = await self.client.credentials.resolve(call)
            api_key = credentials.api_key.reveal()
            if not _valid_api_key(api_key):
                raise ValueError("invalid Bocha API Key")
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch("defect", FailureKind.AUTHENTICATION, "Bocha API Key is unavailable or invalid")
        payload = json.dumps(
            {"query": query, "count": 5, "summary": False}, ensure_ascii=False, separators=(",", ":")
        )
        try:
            request = self.client.http_client.build_request(
                "POST",
                _ENDPOINT,
                headers={
                    "Authorization": f"Bearer {api_key}",
                    "Content-Type": "application/json",
                    "Accept": "application/json",
                },
                content=payload.encode(),
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "Bocha search request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "Bocha search request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Bocha rate limited the search", delay)
            if status == 408 or status >= 500:
                return _retry(
                    FailureKind.AVAILABILITY, "Bocha search service is temporarily unavailable", delay
                )
            if status != 200:
                kind = FailureKind.PROVIDER_REJECTION
                if status == 401:
                    kind = FailureKind.AUTHENTICATION
                elif status == 403:
                    kind = FailureKind.AUTHORIZATION
                return _branch("providerRejected", kind, f"Bocha rejected search with HTTP {status}")
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Bocha response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Bocha response was interrupted")
        finally:
            await response.aclose()
        return _decode_response(body)


def _decode_response(body: bytes) -> QueryAttempt[SearchWebOutput]:
    try:
        document: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "Bocha returned invalid search data")
    if not isinstance(document, dict) or type(document.get("code")) is not int:
        return _branch("invalidResponse", FailureKind.PROTOCOL, "Bocha returned invalid search data")
    if document["code"] != 200:
        return _branch(
            "providerRejected",
            FailureKind.PROVIDER_REJECTION,
            f"Bocha rejected search with code {document['code']}",
        )
    data = document.get("data")
    web_pages = data.get("webPages") if isinstance(data, dict) else None
    pages = web_pages.get("value") if isinstance(web_pages, dict) else None
    if not isinstance(pages, list) or len(pages) > 50:
        return _branch("invalidResponse", FailureKind.PROTOCOL, "Bocha returned invalid search results")
    results = []
    for page in pages[:5]:
        if not isinstance(page, dict):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Bocha returned invalid search result")
        name, url, snippet = page.get("name", ""), page.get("url", ""), page.get("snippet", "")
        if (
            not isinstance(name, str)
            or not name
            or len(name.encode()) > 500
            or not isinstance(url, str)
            or not _valid_page_url(url)
            or not isinstance(snippet, str)
            or len(snippet.encode()) > 2000
        ):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Bocha returned invalid search result")
        results.append(WebResult(name, url, snippet))
    return QueryAttempt.branch_result("found", SearchWebOutput(tuple(results)), Receipt())
