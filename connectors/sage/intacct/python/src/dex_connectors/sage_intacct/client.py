# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Sage Intacct transport with renewable OAuth credential resolution."""

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

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials

if TYPE_CHECKING:
    from .operations import GetInvoiceOperation, ListInvoicesOperation


@dataclass(frozen=True, slots=True)
class Exchange:
    """One bounded HTTP response or local failure classification."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


class Client:
    """Resolve Sage Intacct OAuth credentials per call and send bounded requests."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Sage Intacct configuration and credential source are required")
        if config.timeout_seconds <= 0 or config.max_response_bytes < 1:
            raise ValueError("Sage Intacct request limits must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)

    def get_invoice(self) -> GetInvoiceOperation:
        """Bind the accounts receivable invoice Query."""
        from .operations import GetInvoiceOperation

        return GetInvoiceOperation(self)

    def list_invoices(self) -> ListInvoicesOperation:
        """Bind the first-page invoice listing Query."""
        from .operations import ListInvoicesOperation

        return ListInvoicesOperation(self)

    async def aclose(self) -> None:
        """Close the transport after Worker shutdown."""
        await self.http_client.aclose()

    async def get(self, call: Call, path: str) -> Exchange:
        """Send one GET without returning provider error text or tokens."""
        try:
            credentials = await self.credentials.resolve(call)
        except ReauthorizationRequiredError:
            return Exchange(error="credential")
        except (ValueError, LookupError, OSError):
            return Exchange(error="refresh")
        token = credentials.access_token.reveal()
        if not is_header_safe_credential(token):
            return Exchange(error="credential")
        try:
            request = self.http_client.build_request(
                "GET",
                self.base_url + path,
                headers={"Accept": "application/json", "Authorization": "Bearer " + token},
            )
            response, _ = await send_traced(self.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return Exchange(error="request")
        try:
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
                read_error = ""
            except BodyTooLargeError:
                body, read_error = b"", "too_large"
            except ProviderTransportError:
                body, read_error = b"", "read"
            if _contains_access_token(body, token):
                return Exchange(response.status_code, b"", retry_after, "credential")
            return Exchange(response.status_code, body, retry_after, read_error)
        finally:
            await response.aclose()


def _contains_access_token(body: bytes, token: str) -> bool:
    if token.encode() in body:
        return True
    if b"\\" not in body:
        return False
    text = body.decode("utf-8", errors="replace")
    decoder = json.JSONDecoder()
    position = text.find('"')
    while position >= 0:
        try:
            field, end = decoder.raw_decode(text, position)
        except ValueError:
            return False
        if token in field:
            return True
        position = text.find('"', end)
    return False
