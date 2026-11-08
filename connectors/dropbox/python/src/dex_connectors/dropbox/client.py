# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Dropbox transport and credential resolution."""

from __future__ import annotations

import json
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import TYPE_CHECKING

import httpx
from dex_connectors.sdk import Call, CredentialProvider, ReauthorizationRequiredError
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

from .models import Config as Config
from .models import Credentials as Credentials

if TYPE_CHECKING:
    from .operations import (
        ContinueFolderOperation,
        DownloadFileOperation,
        GetMetadataOperation,
        ListFolderOperation,
        UploadFileOperation,
    )


@dataclass(frozen=True, slots=True)
class Exchange:
    """One bounded provider response or classified transport failure."""

    status: int = 0
    body: bytes = b""
    metadata: str = ""
    retry_after: float = 0
    error: str = ""


class Client:
    """Resolve OAuth credentials per call and send bounded Dropbox requests."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Dropbox configuration and credential source are required")
        if config.timeout_seconds < 0 or config.max_response_bytes < 0 or config.max_download_bytes < 0:
            raise ValueError("Dropbox request limits must be positive")
        if not 0 <= config.max_upload_bytes <= 150 * 1024 * 1024:
            raise ValueError("Dropbox upload limit must be between 1 byte and 150 MiB")
        self.base_url = validate_base_url(config.base_url or "https://api.dropboxapi.com")
        self.content_base_url = validate_base_url(config.content_base_url or "https://content.dropboxapi.com")
        self.max_response_bytes = config.max_response_bytes or 2_097_152
        self.max_download_bytes = config.max_download_bytes or 1_048_576
        self.max_upload_bytes = config.max_upload_bytes or 1_048_576
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds or 15)

    def list_folder(self) -> ListFolderOperation:
        """Bind the folder listing Query."""
        from .operations import ListFolderOperation

        return ListFolderOperation(self)

    def get_metadata(self) -> GetMetadataOperation:
        """Bind the metadata lookup Query."""
        from .operations import GetMetadataOperation

        return GetMetadataOperation(self)

    def continue_folder(self) -> ContinueFolderOperation:
        """Bind the folder continuation Query."""
        from .operations import ContinueFolderOperation

        return ContinueFolderOperation(self)

    def download_file(self) -> DownloadFileOperation:
        """Bind the bounded download Query."""
        from .operations import DownloadFileOperation

        return DownloadFileOperation(self)

    def upload_file(self) -> UploadFileOperation:
        """Bind the bounded replacement Mutation."""
        from .operations import UploadFileOperation

        return UploadFileOperation(self)

    async def aclose(self) -> None:
        """Close the provider HTTP client after Worker shutdown."""
        await self.http_client.aclose()

    async def post_json(self, call: Call, path: str, payload: object) -> Exchange:
        """Send a Dropbox RPC request with a complete JSON body."""
        body = json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode()
        return await self._send(
            call,
            self.base_url + path,
            body,
            {"Accept": "application/json", "Content-Type": "application/json"},
            self.max_response_bytes,
        )

    async def download(self, call: Call, path: str) -> Exchange:
        """Read bounded content and metadata from Dropbox's content endpoint."""
        argument = json.dumps({"path": path}, ensure_ascii=True, separators=(",", ":"))
        return await self._send(
            call,
            self.content_base_url + "/2/files/download",
            b"",
            {"Dropbox-API-Arg": argument},
            self.max_download_bytes,
            is_download=True,
        )

    async def upload(self, call: Call, path: str, content: bytes) -> Exchange:
        """Overwrite one path with the same bytes on every retry."""
        argument = json.dumps(
            {"path": path, "mode": "overwrite", "autorename": False},
            ensure_ascii=True,
            separators=(",", ":"),
        )
        return await self._send(
            call,
            self.content_base_url + "/2/files/upload",
            content,
            {"Dropbox-API-Arg": argument, "Content-Type": "application/octet-stream"},
            self.max_response_bytes,
        )

    async def _send(
        self,
        call: Call,
        url: str,
        content: bytes,
        headers: dict[str, str],
        success_limit: int,
        *,
        is_download: bool = False,
    ) -> Exchange:
        try:
            credentials = await self.credentials.resolve(call)
        except ReauthorizationRequiredError:
            return Exchange(error="credential")
        except (ValueError, LookupError, OSError):
            return Exchange(error="refresh")
        token = credentials.access_token.reveal()
        if not token or not is_header_safe_credential(token):
            return Exchange(error="credential")
        request_headers = {
            **headers,
            "Authorization": "Bearer " + token,
            "User-Agent": "Dex Connector (https://superdurable.io)",
        }
        try:
            request = self.http_client.build_request("POST", url, content=content, headers=request_headers)
            response, _ = await send_traced(self.http_client, request)
        except (ProviderTransportError, httpx.HTTPError):
            return Exchange(error="request")
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            limit = success_limit if status == 200 else self.max_response_bytes
            try:
                body = await read_bounded_body(response, limit)
                read_error = ""
            except BodyTooLargeError:
                body, read_error = b"", "too_large"
            except ProviderTransportError:
                body, read_error = b"", "read"
            metadata = response.headers.get("Dropbox-API-Result", "") if is_download else ""
            if token.encode() in body or token in metadata or len(metadata) > 65536:
                return Exchange(status=status, retry_after=retry_after, error="credential")
            return Exchange(status, body, metadata, retry_after, read_error)
        finally:
            await response.aclose()
