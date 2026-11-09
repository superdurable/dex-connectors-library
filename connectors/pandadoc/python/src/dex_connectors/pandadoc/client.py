# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded PandaDoc document reads with Go-compatible outcomes."""

from __future__ import annotations

import json
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any
from urllib.parse import quote

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
from ._generated.definitions import GET_DOCUMENT_DEFINITION
from .models import DocumentStatus, GetDocumentInput


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One bounded provider response or request failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "pandadoc", "getDocument", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[DocumentStatus]:
    return QueryAttempt.branch_result(branch, DocumentStatus(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[DocumentStatus]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _valid_document_id(document_id: object) -> bool:
    return (
        isinstance(document_id, str)
        and bool(document_id)
        and len(document_id.encode("utf-8")) <= 256
        and not any(ord(char) < 32 or 127 <= ord(char) <= 159 or char in "/?#" for char in document_id)
    )


def _decode_document(body: bytes, requested_document_id: str) -> DocumentStatus:
    raw: Any = json.loads(body)
    if (
        not isinstance(raw, dict)
        or raw.get("id") != requested_document_id
        or not isinstance(raw.get("name"), str)
        or not raw["name"].strip()
        or not isinstance(raw.get("status"), str)
        or not raw["status"].startswith("document.")
        or len(raw["status"]) <= len("document.")
    ):
        raise ValueError("document identity or name missing")
    return DocumentStatus(raw["id"], raw["name"], raw["status"])


class Client:
    """Resolve an API key per call and read selected document metadata."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("PandaDoc configuration and credential source are required")
        if (
            config.timeout_seconds <= 0
            or type(config.max_response_bytes) is not int
            or config.max_response_bytes < 1
        ):
            raise ValueError("PandaDoc request limits must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_document(self) -> GetDocumentOperation:
        """Bind the getDocument Query to this client."""
        return GetDocumentOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _get(self, call: Call, document_id: str) -> _Exchange:
        try:
            credential = await self.credentials.resolve(call)
            api_key = credential.api_key.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return _Exchange(error="credential")
        if not is_header_safe_credential(api_key):
            return _Exchange(error="credential")
        try:
            request = self.http_client.build_request(
                "GET",
                self.base_url + "/documents/" + quote(document_id, safe=""),
                headers={"Accept": "application/json", "Authorization": "API-Key " + api_key},
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
            return _Exchange(status=response.status_code, body=body, retry_after=retry_after)
        finally:
            await response.aclose()


class GetDocumentOperation:
    """Read one document's ID, name, and status."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query contract."""
        return GET_DOCUMENT_DEFINITION

    async def invoke(self, call: Call, input: GetDocumentInput) -> QueryAttempt[DocumentStatus]:
        """Classify document lookup outcomes consistently with Go."""
        if not isinstance(input, GetDocumentInput) or not _valid_document_id(input.document_id):
            return _branch("defect", FailureKind.VALIDATION, "document ID must be 1–256 printable characters")
        exchange = await self.client._get(call, input.document_id)
        if exchange.error == "credential":
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "PandaDoc credentials are unavailable or invalid"
            )
        if exchange.error == "request":
            return _retry(FailureKind.TRANSPORT, "PandaDoc request failed")
        if exchange.status == 429:
            return _retry(FailureKind.RATE_LIMIT, "PandaDoc rate limited the request", exchange.retry_after)
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(
                FailureKind.AVAILABILITY, "PandaDoc is temporarily unavailable", exchange.retry_after
            )
        if exchange.status == 404:
            return _branch("notFound", FailureKind.NOT_FOUND, "PandaDoc did not expose the document")
        if exchange.status != 200:
            return _branch(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                f"PandaDoc rejected HTTP {exchange.status}",
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "PandaDoc response exceeded the configured limit",
            )
        if exchange.error:
            return _retry(FailureKind.TRANSPORT, "PandaDoc response was interrupted")
        try:
            document = _decode_document(exchange.body, input.document_id)
        except (ValueError, TypeError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "PandaDoc returned invalid document JSON")
        return QueryAttempt.branch_result("found", document, Receipt())
