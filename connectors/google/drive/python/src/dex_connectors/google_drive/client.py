# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Authenticated, bounded Google Drive HTTP requests."""

import json
from collections.abc import Callable
from dataclasses import dataclass, field
from datetime import UTC, datetime
from typing import TYPE_CHECKING, cast
from urllib.parse import quote_plus

import httpx
from dex_connectors.sdk import Call, CredentialProvider, Failure, FailureKind, Receipt
from dex_connectors.sdk.providerhttp import (
    MAX_ERROR_BODY_BYTES,
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

if TYPE_CHECKING:
    from .operations import (
        GetFileOperation,
        ReadFileTextOperation,
        SearchFilesOperation,
        UploadFileOperation,
    )

PROVIDER = "google-drive"
FULL_FILE_FIELDS = (
    "id,name,mimeType,description,parents,driveId,createdTime,modifiedTime,size,md5Checksum,"
    "version,webViewLink,trashed,owners(displayName,emailAddress),lastModifyingUser(displayName,emailAddress),"
    "shortcutDetails(targetId,targetMimeType)"
)


@dataclass(frozen=True, slots=True)
class Exchange:
    """One provider request and its bounded response."""

    status: int = 0
    headers: httpx.Headers = field(default_factory=httpx.Headers)
    body: bytes = b""
    error: str = ""
    is_body_too_large: bool = False
    was_sent: bool = False

    @property
    def request_id(self) -> str:
        """Return Google's request identifier without echoing response content."""
        return cast(str, self.headers.get("X-Goog-Request-Id") or self.headers.get("X-Request-Id", ""))


def query_string(values: dict[str, str]) -> str:
    """Encode sorted parameters with Go url.Values.Encode semantics."""
    def escape(value: str) -> str:
        return quote_plus(value, safe="").replace("~", "%7E")

    return "&".join(f"{escape(key)}={escape(value)}" for key, value in sorted(values.items()))


def failure(operation: str, kind: FailureKind, message: str) -> Failure:
    """Construct a secret-safe Drive failure."""
    return Failure(kind, PROVIDER, operation, message)


class Client:
    """Resolve credentials at each call and send bounded Drive requests."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        now: Callable[[], datetime] | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Google Drive configuration and credential source are required")
        endpoint = validate_base_url(config.endpoint or Config().endpoint)
        self.api_base_url = endpoint + "/drive/v3"
        self.upload_base_url = endpoint + "/upload/drive/v3"
        self.max_response_bytes = config.max_response_bytes or Config().max_response_bytes
        self.max_text_bytes = config.max_text_bytes or Config().max_text_bytes
        self.max_upload_bytes = config.max_upload_bytes or Config().max_upload_bytes
        self.search_page_size = config.search_page_size or Config().search_page_size
        if self.max_response_bytes < 1 or self.max_text_bytes < 1:
            raise ValueError("Google Drive response and text limits must be positive")
        if not 1 <= self.max_upload_bytes <= 5 << 20:
            raise ValueError("Google Drive upload limit must be from 1 to 5242880 bytes")
        if not 1 <= self.search_page_size <= 100:
            raise ValueError("Google Drive search page size must be from 1 to 100")
        self._credentials = credentials
        self._http_client = http_client or new_provider_http_client(25)
        self._owns_http_client = http_client is None
        self._now = now or (lambda: datetime.now(UTC))

    async def aclose(self) -> None:
        """Close the connector-owned HTTP client."""
        if self._owns_http_client:
            await self._http_client.aclose()

    def search_files(self) -> "SearchFilesOperation":
        """Bind the Drive file search Query."""
        from .operations import SearchFilesOperation

        return SearchFilesOperation(self)

    def get_file(self) -> "GetFileOperation":
        """Bind the Drive metadata Query."""
        from .operations import GetFileOperation

        return GetFileOperation(self)

    def read_file_text(self) -> "ReadFileTextOperation":
        """Bind the bounded text Query."""
        from .operations import ReadFileTextOperation

        return ReadFileTextOperation(self)

    def upload_file(self) -> "UploadFileOperation":
        """Bind the fenced upload Mutation."""
        from .operations import UploadFileOperation

        return UploadFileOperation(self)

    async def resolve_credentials(
        self, call: Call, operation: str
    ) -> tuple[Credentials | None, Failure | None]:
        """Resolve a valid bearer token without persisting it in Flow state."""
        try:
            credentials = await self._credentials.resolve(call)
            if (
                not isinstance(credentials, Credentials)
                or credentials.access_token is None
                or not is_header_safe_credential(credentials.access_token.reveal())
                or credentials.auth_method_id not in ("", "google-oauth", "workspace-domain-delegation")
            ):
                raise ValueError("Drive access token is unavailable")
            return credentials, None
        except Exception:
            return None, failure(
                operation, FailureKind.AUTHENTICATION, "connection credentials are unavailable"
            )

    async def exchange(
        self,
        call: Call,
        credentials: Credentials,
        method: str,
        target: str,
        *,
        body: bytes | None = None,
        content_type: str = "",
        limit: int | None = None,
        retry_unauthorized: bool = True,
    ) -> Exchange:
        """Send a Drive request, refreshing once after a rejected bearer token."""
        response_limit = self.max_response_bytes if limit is None else limit
        assert credentials.access_token is not None
        for attempt in range(2):
            headers = {"Authorization": f"Bearer {credentials.access_token.reveal()}"}
            if content_type:
                headers["Content-Type"] = content_type
            try:
                request = self._http_client.build_request(method, target, content=body, headers=headers)
            except (httpx.HTTPError, ValueError):
                return Exchange(error="notBuilt")
            try:
                response, _ = await send_traced(self._http_client, request)
            except ProviderTransportError as error:
                return Exchange(error="beforeSend" if error.provably_not_sent else "afterSend")
            except (httpx.HTTPError, OSError):
                return Exchange(error="afterSend")
            is_success = 200 <= response.status_code < 300
            try:
                content = await read_bounded_body(response, max(response_limit, MAX_ERROR_BODY_BYTES))
            except BodyTooLargeError:
                if is_success:
                    return Exchange(
                        response.status_code, response.headers, is_body_too_large=True, was_sent=True
                    )
                content = b""
            except (httpx.HTTPError, OSError):
                return Exchange(response.status_code, response.headers, error="bodyRead", was_sent=True)
            if is_success and len(content) > response_limit:
                return Exchange(response.status_code, response.headers, is_body_too_large=True, was_sent=True)
            exchange = Exchange(response.status_code, response.headers, content, was_sent=True)
            if response.status_code != 401 or attempt != 0 or not retry_unauthorized:
                return exchange
            refresh = getattr(self._credentials, "resolve_after_rejection", None)
            if refresh is None:
                return exchange
            try:
                credentials = await refresh(call)
                if (
                    not isinstance(credentials, Credentials)
                    or credentials.access_token is None
                    or not is_header_safe_credential(credentials.access_token.reveal())
                ):
                    return exchange
            except Exception:
                return exchange
        return Exchange(error="notBuilt")

    def receipt(self, call: Call, exchange: Exchange | None = None, object_id: str = "") -> Receipt:
        """Return the provider ID and request ID associated with a definite outcome."""
        return Receipt(
            call.id,
            PROVIDER,
            self._now().astimezone(UTC),
            call.idempotency_key,
            object_id,
            exchange.request_id if exchange else "",
        )

    def retry_after(self, exchange: Exchange) -> float:
        """Parse Retry-After relative to the injected clock."""
        return parse_retry_after(exchange.headers.get("Retry-After", ""), self._now())

    def files_list_url(self, values: dict[str, str]) -> str:
        """Build a Drive files.list URL across all visible drives."""
        return (
            self.api_base_url
            + "/files?"
            + query_string(
                {
                    **values,
                    "corpora": "allDrives",
                    "supportsAllDrives": "true",
                    "includeItemsFromAllDrives": "true",
                }
            )
        )

    def file_metadata_url(self, file_id: str, fields: str) -> str:
        """Build a metadata URL for one valid Drive ID."""
        return (
            self.api_base_url
            + "/files/"
            + file_id
            + "?"
            + query_string(
                {
                    "fields": fields,
                    "supportsAllDrives": "true",
                }
            )
        )

    def file_content_url(self, file_id: str) -> str:
        """Build a media download URL for one valid Drive ID."""
        return (
            self.api_base_url
            + "/files/"
            + file_id
            + "?"
            + query_string(
                {
                    "alt": "media",
                    "supportsAllDrives": "true",
                }
            )
        )

    def file_export_url(self, file_id: str, mime_type: str) -> str:
        """Build a Google Workspace export URL."""
        return self.api_base_url + "/files/" + file_id + "/export?" + query_string({"mimeType": mime_type})


def error_reasons(exchange: Exchange) -> list[str]:
    """Read Google's machine-readable reason and status from an error body."""
    try:
        parsed = json.loads(exchange.body)
    except (ValueError, UnicodeDecodeError):
        return []
    if not isinstance(parsed, dict) or not isinstance(parsed.get("error"), dict):
        return []
    error = parsed["error"]
    details = error.get("errors")
    reason = (
        details[0].get("reason")
        if isinstance(details, list) and details and isinstance(details[0], dict)
        else None
    )
    return [value for value in (reason, error.get("status")) if isinstance(value, str)]


def status_failure_kind(status: int, reasons: list[str]) -> FailureKind:
    """Classify a Google HTTP status with its first error reason."""
    if status == 401:
        return FailureKind.AUTHENTICATION
    if (
        status == 429
        or status == 403
        and reasons
        and reasons[0] in ("rateLimitExceeded", "userRateLimitExceeded")
    ):
        return FailureKind.RATE_LIMIT
    if status == 403:
        return FailureKind.AUTHORIZATION
    if status == 404:
        return FailureKind.NOT_FOUND
    if status == 409:
        return FailureKind.CONFLICT
    if status >= 500:
        return FailureKind.AVAILABILITY
    return FailureKind.PROVIDER_REJECTION
