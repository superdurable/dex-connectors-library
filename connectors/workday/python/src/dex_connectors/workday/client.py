# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Workday transport with renewable OAuth credential resolution."""

from __future__ import annotations

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
    from .operations import GetWorkerOperation


@dataclass(frozen=True, slots=True)
class Exchange:
    """One bounded HTTP response or local failure classification."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


class Client:
    """Resolve Workday OAuth credentials per call and send bounded requests."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Workday configuration and credential source are required")
        if config.timeout_seconds <= 0 or config.max_response_bytes < 1:
            raise ValueError("Workday request limits must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)

    def get_worker(self) -> GetWorkerOperation:
        """Bind the worker profile Query."""
        from .operations import GetWorkerOperation

        return GetWorkerOperation(self)

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
            if token.encode() in body:
                return Exchange(response.status_code, b"", retry_after, "credential")
            return Exchange(response.status_code, body, retry_after, read_error)
        finally:
            await response.aclose()
