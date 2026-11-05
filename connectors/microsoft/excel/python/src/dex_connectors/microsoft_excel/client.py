# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Microsoft Graph client for Excel workbook operations."""

from __future__ import annotations

import ipaddress
import json
import re
from collections.abc import Callable
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import TYPE_CHECKING
from urllib.parse import quote, urlsplit

import httpx
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
    RejectedCredentialRefreshingProvider,
    resolve_credential,
    resolve_credential_after_rejection,
)
from dex_connectors.sdk.oauthtoken import ManifestCredentialRefreshDriver
from dex_connectors.sdk.providerhttp import is_header_safe_credential

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.refresh import AUTHORIZATION, CREDENTIAL_FIELDS

if TYPE_CHECKING:
    from .operations import (
        AppendTableRowsOperation,
        GetTableRowsOperation,
        GetValuesOperation,
        UpdateValuesOperation,
    )

_GRAPH_BASE = "https://graph.microsoft.com/v1.0"
_GUID = re.compile(r"^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$")
_PATH_SAFE = "-._~$&+=:@"


def path_escape(value: str) -> str:
    """Escape one Graph path segment with Go url.PathEscape's safe set."""
    return quote(value, safe=_PATH_SAFE)


def workbook_url(drive_id: str, workbook_id: str) -> str:
    """Address a workbook by its Graph drive and item IDs."""
    return f"{_GRAPH_BASE}/drives/{path_escape(drive_id)}/items/{path_escape(workbook_id)}/workbook"


def table_url(drive_id: str, workbook_id: str, table: str) -> str:
    """Address one Excel table in a workbook."""
    return workbook_url(drive_id, workbook_id) + "/tables/" + path_escape(table)


def range_url(drive_id: str, workbook_id: str, worksheet: str, address: str) -> str:
    """Address a validated A1 range in one worksheet."""
    return (
        workbook_url(drive_id, workbook_id)
        + "/worksheets/"
        + path_escape(worksheet)
        + "/range(address='"
        + address
        + "')"
    )


@dataclass(frozen=True, slots=True)
class GraphResponse:
    """Safe Graph response metadata with bounded body bytes."""

    status: int
    headers: httpx.Headers
    body: bytes
    request_id: str
    is_body_too_large: bool = False


class GraphTransportError(OSError):
    """A Graph request failed before a complete response was received."""

    def __init__(self, is_request_built: bool = True) -> None:
        self.is_request_built = is_request_built
        super().__init__("Microsoft Graph request failed or its response could not be read")


class Client:
    """Execute Graph Excel requests with call-time renewable credentials."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        local_provider_url: str = "",
        now: Callable[[], datetime] | None = None,
    ) -> None:
        if credentials is None:
            raise ValueError("credential provider is required")
        if config.max_response_bytes < 1 or not 1 <= config.max_cells <= 100000:
            raise ValueError("Microsoft Excel limits are invalid")
        if local_provider_url:
            parsed = urlsplit(local_provider_url)
            hostname = parsed.hostname or ""
            try:
                is_loopback = ipaddress.ip_address(hostname).is_loopback
            except ValueError:
                is_loopback = hostname.lower() == "localhost"
            if (
                parsed.scheme not in ("http", "https")
                or not is_loopback
                or parsed.path not in ("", "/")
                or parsed.query
                or parsed.fragment
                or parsed.username
            ):
                raise ValueError("local provider URL must use loopback without path or credentials")
        self.config = config
        self.credentials = credentials
        self.http_client = http_client or httpx.AsyncClient(timeout=20, follow_redirects=False)
        self._owns_http_client = http_client is None
        self._local_provider_url = local_provider_url.rstrip("/")
        self.now = now or (lambda: datetime.now(UTC))
        self.refresh_driver = ManifestCredentialRefreshDriver[Credentials](
            "Microsoft Excel", AUTHORIZATION, CREDENTIAL_FIELDS, self.http_client
        )

    async def aclose(self) -> None:
        """Close the connector-owned HTTP client."""
        if self._owns_http_client:
            await self.http_client.aclose()

    def get_table_rows(self) -> GetTableRowsOperation:
        """Return the table read operation."""
        from .operations import GetTableRowsOperation

        return GetTableRowsOperation(self)

    def get_values(self) -> GetValuesOperation:
        """Return the range read operation."""
        from .operations import GetValuesOperation

        return GetValuesOperation(self)

    def update_values(self) -> UpdateValuesOperation:
        """Return the range write operation."""
        from .operations import UpdateValuesOperation

        return UpdateValuesOperation(self)

    def append_table_rows(self) -> AppendTableRowsOperation:
        """Return the duplicate-safe table append operation."""
        from .operations import AppendTableRowsOperation

        return AppendTableRowsOperation(self)

    async def resolve_credentials(self, call: Call) -> Credentials:
        """Resolve OAuth credentials through the generated refresh declaration."""
        credential = await resolve_credential(self.credentials, call, self.refresh_driver)
        if (
            credential.auth_method_id != "microsoft-oauth"
            or credential.access_token is None
            or not is_header_safe_credential(credential.access_token.reveal())
        ):
            raise ValueError("connection credentials are unavailable")
        return credential

    async def request(
        self, call: Call, credential: Credentials, method: str, target: str, body: bytes | None = None
    ) -> GraphResponse:
        """Send one request and refresh once after a rejected access token."""
        if self._local_provider_url:
            target = self._local_provider_url + target.removeprefix("https://graph.microsoft.com")
        for attempt in range(2):
            try:
                assert credential.access_token is not None
                headers = {
                    "Authorization": "Bearer " + credential.access_token.reveal(),
                    "Accept": "application/json",
                }
                if body is not None:
                    headers["Content-Type"] = "application/json"
                request = self.http_client.build_request(
                    method, target, headers=headers, content=body, timeout=20
                )
            except (ValueError, httpx.InvalidURL):
                raise GraphTransportError(False) from None
            try:
                response = await self.http_client.send(request, stream=True, follow_redirects=False)
                limit = max(self.config.max_response_bytes, 64 * 1024)
                collected = bytearray()
                try:
                    async for chunk in response.aiter_bytes():
                        collected.extend(chunk[: max(0, limit + 1 - len(collected))])
                        if len(collected) > limit:
                            break
                finally:
                    await response.aclose()
                content = bytes(collected)
            except (httpx.HTTPError, OSError):
                raise GraphTransportError() from None
            if (
                response.status_code == 401
                and attempt == 0
                and isinstance(self.credentials, RejectedCredentialRefreshingProvider)
            ):
                try:
                    refreshed = await resolve_credential_after_rejection(
                        self.credentials, call, self.refresh_driver
                    )
                    if (
                        refreshed.auth_method_id != "microsoft-oauth"
                        or refreshed.access_token is None
                        or not is_header_safe_credential(refreshed.access_token.reveal())
                    ):
                        raise ValueError("invalid refreshed credential")
                except Exception:
                    pass  # The original 401 remains the definitive provider response.
                else:
                    credential = refreshed
                    continue
            request_id = response.headers.get("request-id", "")
            if not _GUID.fullmatch(request_id):
                request_id = ""
            limit = self.config.max_response_bytes
            is_large = len(content) > limit and 200 <= response.status_code < 300
            if len(content) > max(limit, 64 * 1024):
                content = b""
            if is_large:
                content = b""
            return GraphResponse(response.status_code, response.headers, content, request_id, is_large)
        raise GraphTransportError()


def compact_json(value: object) -> bytes:
    """Encode the exact provider request object without padding whitespace."""
    return json.dumps(value, separators=(",", ":"), ensure_ascii=False, allow_nan=False).encode()
