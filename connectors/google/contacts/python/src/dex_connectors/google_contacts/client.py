# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Google Contacts HTTP transport and OAuth credential resolution."""

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
    from .operations import CreateContactOperation, GetContactOperation, ListContactsOperation


@dataclass(frozen=True, slots=True)
class Exchange:
    """One provider response or transport failure with send evidence."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""
    request_sent: bool = False


class Client:
    """Resolve Google Contacts OAuth credentials per call and send bounded requests."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Google Contacts configuration and credential source are required")
        if config.timeout_seconds <= 0 or config.max_response_bytes < 1:
            raise ValueError("Google Contacts request limits must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)

    def list_contacts(self) -> ListContactsOperation:
        """Bind the contact page Query."""
        from .operations import ListContactsOperation

        return ListContactsOperation(self)

    def get_contact(self) -> GetContactOperation:
        """Bind the contact Query."""
        from .operations import GetContactOperation

        return GetContactOperation(self)

    def create_contact(self) -> CreateContactOperation:
        """Bind the contact-creation Mutation."""
        from .operations import CreateContactOperation

        return CreateContactOperation(self)

    async def aclose(self) -> None:
        """Close the transport after Worker shutdown."""
        await self.http_client.aclose()

    async def resolve_token(self, call: Call) -> tuple[str, str]:
        """Return a header-safe bearer token or a safe failure class."""
        try:
            credentials = await self.credentials.resolve(call)
        except ReauthorizationRequiredError:
            return "", "credential"
        except (ValueError, LookupError, OSError):
            return "", "refresh"
        token = credentials.access_token.reveal()
        if not is_header_safe_credential(token):
            return "", "credential"
        return token, ""

    async def request(
        self,
        call: Call,
        path: str,
        *,
        token: str | None = None,
        method: str = "GET",
        body: bytes | None = None,
    ) -> Exchange:
        """Send one bounded HTTP request without returning provider error text."""
        if token is None:
            token, error = await self.resolve_token(call)
            if error:
                return Exchange(error=error)
        headers = {
            "Accept": "application/json",
            "Authorization": "Bearer " + token,
            "User-Agent": "Dex Connector (https://superdurable.io)",
        }
        if body is not None:
            headers["Content-Type"] = "application/json"
        try:
            request = self.http_client.build_request(
                method,
                self.base_url + path,
                headers=headers,
                content=body,
            )
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError as error:
            return Exchange(error="request", request_sent=not error.provably_not_sent)
        except ValueError:
            return Exchange(error="not_built")
        except httpx.HTTPError:
            return Exchange(error="request", request_sent=True)
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
                return Exchange(response.status_code, b"", retry_after, "credential", True)
            return Exchange(response.status_code, body, retry_after, read_error, True)
        finally:
            await response.aclose()
