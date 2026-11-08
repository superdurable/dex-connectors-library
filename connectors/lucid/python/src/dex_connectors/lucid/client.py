# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Lucid document-page reads with Go-compatible outcomes."""

from __future__ import annotations

import json
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any
from urllib.parse import parse_qs, urlencode, urlparse

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
from ._generated.definitions import LIST_DOCUMENTS_DEFINITION
from .models import DocumentMetadata, DocumentsPage, ListDocumentsInput


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One bounded provider response or request failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    link: str = ""
    error: str = ""


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "lucid", "listDocuments", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[DocumentsPage]:
    return QueryAttempt.branch_result(branch, DocumentsPage(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[DocumentsPage]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _valid_page_token(cursor: object) -> bool:
    return (
        isinstance(cursor, str)
        and len(cursor.encode("utf-8")) <= 2048
        and not any(ord(character) < 32 or 127 <= ord(character) <= 159 for character in cursor)
    )


def _next_page_token(link: str) -> str:
    if not link:
        return ""
    for segment in link.split(","):
        parts = [part.strip() for part in segment.split(";")]
        if len(parts) < 2:
            raise ValueError("invalid Link header")
        if not any(part in ('rel="next"', "rel=next") for part in parts[1:]):
            continue
        raw_url = parts[0]
        if not raw_url.startswith("<") or not raw_url.endswith(">"):
            raise ValueError("invalid next URL")
        parsed = urlparse(raw_url[1:-1])
        if (
            parsed.scheme != "https"
            or parsed.netloc != "api.lucid.co"
            or parsed.path not in ("/v1/documents/search", "/documents/search")
        ):
            raise ValueError("invalid next URL")
        tokens = parse_qs(parsed.query).get("pageToken", [])
        if len(tokens) != 1 or not tokens[0] or not _valid_page_token(tokens[0]):
            raise ValueError("invalid next page token")
        return tokens[0]
    return ""


def _decode_page(body: bytes, link: str) -> DocumentsPage:
    raw: Any = json.loads(body)
    if not isinstance(raw, list):
        raise ValueError("missing documents array")
    documents: list[DocumentMetadata] = []
    for document in raw:
        if (
            not isinstance(document, dict)
            or not isinstance(document.get("documentId"), str)
            or not document["documentId"]
            or not isinstance(document.get("title"), str)
            or not document["title"].strip()
        ):
            raise ValueError("missing document identity or title")
        documents.append(DocumentMetadata(document["documentId"], document["title"]))
    return DocumentsPage(tuple(documents), _next_page_token(link))


class Client:
    """Resolve an API key per call and read selected document metadata."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Lucid configuration and credential source are required")
        if (
            config.timeout_seconds <= 0
            or type(config.max_response_bytes) is not int
            or config.max_response_bytes < 1
        ):
            raise ValueError("Lucid request limits must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def list_documents(self) -> ListDocumentsOperation:
        """Bind the listDocuments Query to this client."""
        return ListDocumentsOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _search(self, call: Call, page_token: str) -> _Exchange:
        try:
            credential = await self.credentials.resolve(call)
            token = credential.api_key.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return _Exchange(error="credential")
        if not is_header_safe_credential(token):
            return _Exchange(error="credential")
        query = {"pageSize": "200"}
        if page_token:
            query["pageToken"] = page_token
        try:
            request = self.http_client.build_request(
                "POST",
                self.base_url + "/documents/search?" + urlencode(query),
                headers={
                    "Accept": "application/json",
                    "Content-Type": "application/json",
                    "Authorization": "Bearer " + token,
                    "Lucid-Api-Version": "1",
                },
                content=b'{"excludeTrashed":true}',
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
            return _Exchange(
                status=response.status_code,
                body=body,
                retry_after=retry_after,
                link=response.headers.get("Link", ""),
            )
        finally:
            await response.aclose()


class ListDocumentsOperation:
    """Read one document page and classify provider outcomes."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query contract."""
        return LIST_DOCUMENTS_DEFINITION

    async def invoke(self, call: Call, input: ListDocumentsInput) -> QueryAttempt[DocumentsPage]:
        """Classify document listing outcomes consistently with Go."""
        if not isinstance(input, ListDocumentsInput) or not _valid_page_token(input.page_token):
            return _branch(
                "defect",
                FailureKind.VALIDATION,
                "page token contains invalid characters or exceeds 2048 bytes",
            )
        exchange = await self.client._search(call, input.page_token)
        if exchange.error == "credential":
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Lucid credentials are unavailable or invalid"
            )
        if exchange.error == "request":
            return _retry(FailureKind.TRANSPORT, "Lucid request failed")
        if exchange.status == 429:
            return _retry(FailureKind.RATE_LIMIT, "Lucid rate limited the request", exchange.retry_after)
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(FailureKind.AVAILABILITY, "Lucid is temporarily unavailable", exchange.retry_after)
        if exchange.status != 200:
            return _branch(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                f"Lucid rejected HTTP {exchange.status}",
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "Lucid response exceeded the configured limit",
            )
        if exchange.error:
            return _retry(FailureKind.TRANSPORT, "Lucid response was interrupted")
        try:
            page = _decode_page(exchange.body, exchange.link)
        except (ValueError, TypeError, UnicodeDecodeError):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Lucid returned invalid document JSON or pagination"
            )
        return QueryAttempt.branch_result("listed", page, Receipt())
