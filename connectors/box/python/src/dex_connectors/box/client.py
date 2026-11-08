# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Box transport and OAuth credential resolution."""

from __future__ import annotations

import json
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import TYPE_CHECKING
from urllib.parse import urlsplit

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
    from .operations import DownloadFileOperation, UploadFileOperation


@dataclass(frozen=True, slots=True)
class Exchange:
    """One bounded provider response or classified transport failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""
    request_sent: bool = False


class Client:
    """Resolve OAuth credentials per call and send bounded Box requests."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Box configuration and credential source are required")
        if config.timeout_seconds < 0 or config.max_response_bytes < 0 or config.max_download_bytes < 0:
            raise ValueError("Box request limits must be positive")
        if not 0 <= config.max_upload_bytes <= 50 * 1024 * 1024:
            raise ValueError("Box direct upload limit must not exceed 50 MiB")
        self.base_url = validate_base_url(config.base_url or "https://api.box.com")
        self.upload_base_url = validate_base_url(config.upload_base_url or "https://upload.box.com/api")
        self.max_response_bytes = config.max_response_bytes or 2_097_152
        self.max_download_bytes = config.max_download_bytes or 1_048_576
        self.max_upload_bytes = config.max_upload_bytes or 1_048_576
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds or 15)

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

    async def download(self, call: Call, file_id: str) -> Exchange:
        """Download one file, following only a signed Box download URL."""
        token, error = await self._resolve_token(call)
        if error:
            return Exchange(error=error)
        headers = {
            "Authorization": "Bearer " + token,
            "User-Agent": "Dex Connector (https://superdurable.io)",
        }
        url = self.base_url + f"/2.0/files/{file_id}/content"
        first = await self._send("GET", url, headers, self.max_download_bytes, token)
        if first.status != 302 or first.error:
            return first
        signed_url = first.body.decode("utf-8", errors="ignore")
        if not _is_box_download_url(signed_url):
            return Exchange(status=302, error="invalid_redirect", request_sent=True)
        return await self._send(
            "GET",
            signed_url,
            {"User-Agent": "Dex Connector (https://superdurable.io)"},
            self.max_download_bytes,
            token,
        )

    async def get_file_metadata(self, call: Call, file_id: str) -> Exchange:
        """Read one file's identity, current digest, and size."""
        token, error = await self._resolve_token(call)
        if error:
            return Exchange(error=error)
        url = self.base_url + f"/2.0/files/{file_id}?fields=id,type,name,sha1,size"
        return await self._send(
            "GET",
            url,
            {
                "Authorization": "Bearer " + token,
                "Accept": "application/json",
                "User-Agent": "Dex Connector (https://superdurable.io)",
            },
            self.max_response_bytes,
            token,
        )

    async def upload(self, call: Call, file_id: str, name: str, content: bytes) -> Exchange:
        """Post multipart replacement content with attributes before file bytes."""
        token, error = await self._resolve_token(call)
        if error:
            return Exchange(error=error)
        url = self.upload_base_url + f"/2.0/files/{file_id}/content"
        attributes = json.dumps({"name": name}, ensure_ascii=False, separators=(",", ":"))
        headers = {
            "Authorization": "Bearer " + token,
            "Accept": "application/json",
            "User-Agent": "Dex Connector (https://superdurable.io)",
        }
        return await self._send(
            "POST",
            url,
            headers,
            self.max_response_bytes,
            token,
            data={"attributes": attributes},
            files={"file": (name, content, "application/octet-stream")},
        )

    async def _resolve_token(self, call: Call) -> tuple[str, str]:
        try:
            credentials = await self.credentials.resolve(call)
        except ReauthorizationRequiredError:
            return "", "credential"
        except (ValueError, LookupError, OSError):
            return "", "refresh"
        token = credentials.access_token.reveal()
        if not token or not is_header_safe_credential(token):
            return "", "credential"
        return token, ""

    async def _send(
        self,
        method: str,
        url: str,
        headers: dict[str, str],
        success_limit: int,
        access_token: str,
        *,
        data: dict[str, str] | None = None,
        files: dict[str, tuple[str, bytes, str]] | None = None,
    ) -> Exchange:
        try:
            request = self.http_client.build_request(method, url, headers=headers, data=data, files=files)
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError as error:
            return Exchange(error="request", request_sent=not error.provably_not_sent)
        except httpx.HTTPError:
            return Exchange(error="request")
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 302:
                location = response.headers.get("Location", "")
                return Exchange(
                    status=302, body=location.encode(), retry_after=retry_after, request_sent=True
                )
            limit = success_limit if status == 200 else self.max_response_bytes
            try:
                body = await read_bounded_body(response, limit)
                read_error = ""
            except BodyTooLargeError:
                body, read_error = b"", "too_large"
            except ProviderTransportError:
                body, read_error = b"", "read"
            if access_token.encode() in body:
                return Exchange(status=status, retry_after=retry_after, error="credential", request_sent=True)
            return Exchange(status, body, retry_after, read_error, True)
        finally:
            await response.aclose()


def _is_box_download_url(value: str) -> bool:
    parsed = urlsplit(value)
    return (
        parsed.scheme == "https"
        and parsed.hostname is not None
        and (parsed.hostname == "boxcloud.com" or parsed.hostname.endswith(".boxcloud.com"))
        and parsed.username is None
        and parsed.password is None
        and parsed.port is None
        and not parsed.fragment
        and bool(parsed.path)
    )
