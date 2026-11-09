# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Azure Blob downloads signed with the account Shared Key."""

from __future__ import annotations

import base64
import binascii
import hashlib
import hmac
import re
from collections.abc import Callable
from dataclasses import dataclass
from datetime import UTC, datetime
from email.utils import format_datetime
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
from ._generated.definitions import GET_BLOB_DEFINITION
from .models import BlobDownload, GetBlobInput

_ACCOUNT_NAME = re.compile(r"^[a-z0-9]{3,24}$")
_CONTAINER_NAME = re.compile(r"^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$")
_STORAGE_VERSION = "2023-11-03"


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One bounded provider response or local failure."""

    status: int = 0
    body: bytes = b""
    content_type: str = ""
    etag: str = ""
    retry_after: float = 0
    error: str = ""


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "microsoft-azure-blob", "getBlob", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[BlobDownload]:
    return QueryAttempt.branch_result(branch, BlobDownload(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[BlobDownload]:
    return QueryAttempt.retry(_failure(kind, message), delay)


class Client:
    """Resolve an Azure Storage account key and sign each blob download."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        now: Callable[[], datetime] | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Azure Blob configuration and credential source are required")
        if (
            not _ACCOUNT_NAME.fullmatch(config.account_name)
            or config.timeout_seconds <= 0
            or config.max_response_bytes < 1
        ):
            raise ValueError("Azure Blob account or request limits are invalid")
        endpoint = config.endpoint or f"https://{config.account_name}.blob.core.windows.net"
        self.endpoint = validate_base_url(endpoint).rstrip("/")
        self.account_name = config.account_name
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None
        self.now = now or (lambda: datetime.now(UTC))

    def get_blob(self) -> GetBlobOperation:
        """Bind the getBlob Query to this client."""
        return GetBlobOperation(self)

    async def aclose(self) -> None:
        """Close an owned transport after Worker shutdown."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _download_blob(self, call: Call, container: str, blob_name: str) -> _Exchange:
        try:
            credentials = await self.credentials.resolve(call)
            account_key = credentials.account_key.reveal()
            signing_key = base64.b64decode(account_key, validate=True)
        except (ValueError, LookupError, OSError, AttributeError, binascii.Error):
            return _Exchange(error="credential")
        if not account_key or not signing_key or not is_header_safe_credential(account_key):
            return _Exchange(error="credential")
        # Match Go url.PathEscape so every port sends and signs identical paths.
        path = f"/{container}/{quote(blob_name, safe='/$&+:=@')}"
        url = self.endpoint + path
        timestamp = format_datetime(self.now().astimezone(UTC), usegmt=True)
        try:
            request = self.http_client.build_request(
                "GET", url, headers={"x-ms-date": timestamp, "x-ms-version": _STORAGE_VERSION}
            )
            # Shared Key signs the encoded path exactly as sent, including any endpoint path.
            wire_path = request.url.raw_path.split(b"?", 1)[0].decode("ascii")
            signed_value = (
                "GET"
                + "\n" * 12
                + f"x-ms-date:{timestamp}\n"
                + f"x-ms-version:{_STORAGE_VERSION}\n"
                + f"/{self.account_name}{wire_path}"
            )
            signature = base64.b64encode(
                hmac.new(signing_key, signed_value.encode(), hashlib.sha256).digest()
            ).decode()
            request.headers["Authorization"] = f"SharedKey {self.account_name}:{signature}"
            response, _ = await send_traced(self.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _Exchange(error="request")
        try:
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), self.now())
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _Exchange(status=response.status_code, retry_after=retry_after, error="too_large")
            except ProviderTransportError:
                return _Exchange(status=response.status_code, retry_after=retry_after, error="interrupted")
            if account_key.encode() in body:
                return _Exchange(status=response.status_code, retry_after=retry_after, error="credential")
            return _Exchange(
                status=response.status_code,
                body=body,
                content_type=response.headers.get("Content-Type", ""),
                etag=response.headers.get("ETag", ""),
                retry_after=retry_after,
            )
        finally:
            await response.aclose()


class GetBlobOperation:
    """Download one bounded Azure Storage blob."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query contract."""
        return GET_BLOB_DEFINITION

    async def invoke(self, call: Call, input: GetBlobInput) -> QueryAttempt[BlobDownload]:
        """Classify provider outcomes and encode complete blob bytes as base64."""
        if (
            not isinstance(input, GetBlobInput)
            or not isinstance(input.container, str)
            or not _CONTAINER_NAME.fullmatch(input.container)
            or "--" in input.container
            or not _valid_blob_name(input.blob_name)
        ):
            return _branch("defect", FailureKind.VALIDATION, "Azure Blob container or blob name is invalid")
        exchange = await self.client._download_blob(call, input.container, input.blob_name)
        if exchange.error == "credential":
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Azure Blob account key is unavailable or invalid"
            )
        if exchange.error == "request":
            return _retry(FailureKind.TRANSPORT, "Azure Blob request failed")
        if exchange.status == 429:
            return _retry(FailureKind.RATE_LIMIT, "Azure Blob rate limited the request", exchange.retry_after)
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(
                FailureKind.AVAILABILITY, "Azure Blob is temporarily unavailable", exchange.retry_after
            )
        if exchange.status == 404:
            return QueryAttempt.branch_result("missing", BlobDownload(), Receipt())
        if exchange.status != 200:
            return _branch(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                f"Azure Blob rejected HTTP {exchange.status}",
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "Azure Blob body exceeded the configured limit",
            )
        if exchange.error:
            return _retry(FailureKind.TRANSPORT, "Azure Blob response was interrupted")
        if not _safe_header(exchange.content_type) or not _safe_header(exchange.etag):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Azure Blob returned invalid response headers"
            )
        return QueryAttempt.branch_result(
            "downloaded",
            BlobDownload(base64.b64encode(exchange.body).decode(), exchange.content_type, exchange.etag),
            Receipt(),
        )


def _valid_blob_name(name: object) -> bool:
    return (
        isinstance(name, str)
        and 0 < len(name.encode()) <= 1024
        and not any(character in name for character in "\x00\r\n")
        and all(part not in ("", ".", "..") for part in name.split("/"))
    )


def _safe_header(value: str) -> bool:
    return len(value) <= 255 and not any(character in value for character in "\x00\r\n")
