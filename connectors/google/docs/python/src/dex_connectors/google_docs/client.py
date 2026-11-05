# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Google Docs and Drive HTTP client."""

import json
import re
from dataclasses import dataclass, field
from datetime import UTC, datetime
from typing import TYPE_CHECKING, Any
from urllib.parse import quote, urlencode

import httpx
from dex_connectors.sdk import Call, CredentialProvider, Failure, FailureKind, Receipt
from dex_connectors.sdk.providerhttp import (
    MAX_ERROR_BODY_BYTES,
    BodyTooLargeError,
    ProviderTransportError,
    is_header_safe_credential,
    new_provider_http_client,
    read_bounded_body,
    send_traced,
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials

if TYPE_CHECKING:
    from .operations import (
        AppendTextOperation,
        CreateDocumentOperation,
        GetDocumentTextOperation,
        ReplaceDocumentTextOperation,
    )

DRIVE_ID = re.compile(r"[A-Za-z0-9_-]{1,256}\Z")
TAB_ID = re.compile(r"[A-Za-z0-9_.-]{1,256}\Z")
GO_PATH_SAFE = "-._~$&+=:@"
READ_FIELDS = (
    "documentId,title,revisionId,tabs(tabProperties/tabId,"
    "childTabs/tabProperties/tabId,documentTab(body,lists))"
)
WRITE_FIELDS = "documentId,revisionId,tabs(tabProperties/tabId,documentTab/body)"
CREATED_FIELDS = "id,name,mimeType,parents,webViewLink,createdTime,trashed"
GOOGLE_DOCUMENT_MIME = "application/vnd.google-apps.document"


@dataclass(frozen=True, slots=True)
class Exchange:
    """One Google HTTP response and dispatch observation."""

    status: int = 0
    headers: httpx.Headers = field(default_factory=httpx.Headers)
    body: bytes = b""
    error: str = ""
    request_id: str = ""


class Client:
    """Resolve credentials per call and send bounded requests without redirects."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Google Docs configuration and credentials are required")
        self.docs_endpoint = (
            validate_base_url(config.docs_endpoint or Config().docs_endpoint) + "/v1/documents"
        )
        drive_root = validate_base_url(config.drive_endpoint or Config().drive_endpoint)
        self.drive_endpoint = drive_root + "/drive/v3"
        self.drive_upload_endpoint = drive_root + "/upload/drive/v3"
        self.max_response_bytes = config.max_response_bytes or Config().max_response_bytes
        self.max_text_bytes = config.max_text_bytes or Config().max_text_bytes
        if (
            not isinstance(self.max_response_bytes, int)
            or isinstance(self.max_response_bytes, bool)
            or self.max_response_bytes < 1
        ):
            raise ValueError("Google Docs response limit must be positive")
        if (
            not isinstance(self.max_text_bytes, int)
            or isinstance(self.max_text_bytes, bool)
            or not 1 <= self.max_text_bytes <= 4 << 20
        ):
            raise ValueError("Google Docs text limit must be from 1 to 4194304 bytes")
        self._credentials = credentials
        self._http_client = http_client or new_provider_http_client(25)
        self._owns_http_client = http_client is None

    def get_document_text(self) -> "GetDocumentTextOperation":
        """Bind the document read Query."""
        from .operations import GetDocumentTextOperation

        return GetDocumentTextOperation(self)

    def create_document(self) -> "CreateDocumentOperation":
        """Bind the duplicate-aware create Mutation."""
        from .operations import CreateDocumentOperation

        return CreateDocumentOperation(self)

    def replace_document_text(self) -> "ReplaceDocumentTextOperation":
        """Bind the revision-guarded replace Mutation."""
        from .operations import ReplaceDocumentTextOperation

        return ReplaceDocumentTextOperation(self)

    def append_text(self) -> "AppendTextOperation":
        """Bind the revision-guarded append Mutation."""
        from .operations import AppendTextOperation

        return AppendTextOperation(self)

    async def aclose(self) -> None:
        """Close an owned provider transport."""
        if self._owns_http_client:
            await self._http_client.aclose()

    async def credential(self, call: Call) -> Credentials | None:
        """Resolve a valid access token without persisting it in Flow state."""
        try:
            credential = await self._credentials.resolve(call)
            token = credential.access_token.reveal() if credential.access_token else ""
            if credential.auth_method_id not in ("", "google-oauth", "workspace-domain-delegation"):
                return None
            return credential if is_header_safe_credential(token) else None
        except Exception:
            return None

    async def exchange(
        self,
        call: Call,
        credential: Credentials,
        method: str,
        target: str,
        body: bytes | None = None,
        content_type: str = "",
        *,
        single_dispatch: bool = False,
    ) -> Exchange:
        """Send one request, refreshing a rejected credential only for ordinary reads and guarded writes."""
        for attempt in range(2):
            token = credential.access_token.reveal() if credential.access_token else ""
            headers = {"Authorization": "Bearer " + token}
            if content_type:
                headers["Content-Type"] = content_type
            try:
                request = self._http_client.build_request(method, target, content=body, headers=headers)
            except (httpx.HTTPError, ValueError):
                return Exchange(error="notBuilt")
            try:
                response, observation = await send_traced(self._http_client, request)
            except ProviderTransportError as error:
                return Exchange(error="beforeSend" if error.provably_not_sent else "afterSend")
            limit = self.max_response_bytes if 200 <= response.status_code < 300 else MAX_ERROR_BODY_BYTES
            try:
                contents = await read_bounded_body(response, limit)
            except BodyTooLargeError:
                if 200 <= response.status_code < 300:
                    return Exchange(
                        status=response.status_code,
                        headers=response.headers,
                        error="tooLarge",
                        request_id=_request_id(response.headers),
                    )
                contents = b""
            except ProviderTransportError:
                return Exchange(
                    status=response.status_code,
                    headers=response.headers,
                    error="afterSend",
                    request_id=_request_id(response.headers),
                )
            result = Exchange(
                response.status_code, response.headers, contents, request_id=_request_id(response.headers)
            )
            if (
                response.status_code != 401
                or attempt
                or single_dispatch
                or not hasattr(self._credentials, "resolve_after_rejection")
            ):
                return result
            try:
                credential = await self._credentials.resolve_after_rejection(call)
            except Exception:
                return result
        return Exchange(error="notBuilt")

    def receipt(self, call: Call, exchange: Exchange, object_id: str = "") -> Receipt:
        """Record a Google request without response or credential material."""
        return Receipt(
            call.id, "google-docs", datetime.now(UTC), call.idempotency_key, object_id, exchange.request_id
        )

    def document_url(self, document_id: str, for_write: bool = False) -> str:
        """Build a document read URL with Go query encoding."""
        query = {
            "includeTabsContent": "true",
            "suggestionsViewMode": "SUGGESTIONS_INLINE" if for_write else "PREVIEW_WITHOUT_SUGGESTIONS",
            "fields": WRITE_FIELDS if for_write else READ_FIELDS,
        }
        return self.docs_endpoint + "/" + quote(document_id, safe=GO_PATH_SAFE) + "?" + go_query(query)

    def batch_url(self, document_id: str) -> str:
        """Build the atomic batch update URL."""
        return self.docs_endpoint + "/" + quote(document_id, safe=GO_PATH_SAFE) + ":batchUpdate"


def _request_id(headers: httpx.Headers) -> str:
    return str(headers.get("X-Goog-Request-Id") or headers.get("X-Request-Id", ""))


def go_query(values: dict[str, str]) -> str:
    """Encode sorted query keys like Go url.Values.Encode."""
    return urlencode(sorted(values.items()), safe="")


def google_error_tokens(body: bytes) -> tuple[str, ...]:
    """Extract Google machine error tokens from a bounded body."""
    try:
        value: Any = json.loads(body)
        error = value.get("error", {})
        tokens = [error.get("status", "")]
        tokens.extend(item.get("reason", "") for item in error.get("errors", [])[:1])
        tokens.extend(item.get("reason", "") for item in error.get("details", [])[:1])
        return tuple(token for token in tokens if isinstance(token, str))
    except (ValueError, AttributeError, TypeError):
        return ()


def retryable(status: int, tokens: tuple[str, ...]) -> bool:
    """Classify Google transient failures, including quota 403s."""
    return (
        status >= 500
        or status in (408, 429)
        or (
            status == 403
            and any(
                token in ("rateLimitExceeded", "userRateLimitExceeded", "RATE_LIMIT_EXCEEDED")
                for token in tokens
            )
        )
    )


def status_kind(status: int, tokens: tuple[str, ...]) -> FailureKind:
    """Map Google status and error reason to a Dex failure kind."""
    if status == 401:
        return FailureKind.AUTHENTICATION
    if status == 429 or status == 403 and retryable(status, tokens):
        return FailureKind.RATE_LIMIT
    if status == 403:
        return FailureKind.AUTHORIZATION
    if status == 404:
        return FailureKind.NOT_FOUND
    if status == 409:
        return FailureKind.CONFLICT
    if status >= 500 or status == 408:
        return FailureKind.AVAILABILITY
    return FailureKind.PROVIDER_REJECTION


def failure(operation: str, kind: FailureKind, message: str) -> Failure:
    """Return a secret-safe Google Docs failure."""
    return Failure(kind, "google-docs", operation, message)
