# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Constant Contact reads with Go-compatible outcomes."""

from __future__ import annotations

import json
import re
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any

import httpx
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    QueryAttempt,
    QueryDefinition,
    ReauthorizationRequiredError,
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
from ._generated.definitions import GET_CONTACT_DEFINITION
from .models import ContactMetadata, GetContactInput

_CONTACT_ID = re.compile(r"[a-fA-F0-9]{8}-(?:[a-fA-F0-9]{4}-){3}[a-fA-F0-9]{12}\Z")


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One bounded response or classified request failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "constantcontact", "getContact", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ContactMetadata]:
    return QueryAttempt.branch_result(branch, ContactMetadata(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ContactMetadata]:
    return QueryAttempt.retry(_failure(kind, message), delay)


class Client:
    """Resolve OAuth credentials per call and read bounded contacts."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Constant Contact configuration and credential source are required")
        if (
            config.timeout_seconds <= 0
            or type(config.max_response_bytes) is not int
            or config.max_response_bytes < 1
        ):
            raise ValueError("Constant Contact request limits must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_contact(self) -> GetContactOperation:
        """Bind the getContact Query to this client."""
        return GetContactOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _get(self, call: Call, contact_id: str) -> _Exchange:
        try:
            credential = await self.credentials.resolve(call)
        except ReauthorizationRequiredError:
            return _Exchange(error="credential")
        except (ValueError, LookupError, OSError, AttributeError):
            return _Exchange(error="refresh")
        token = credential.access_token.reveal()
        if not is_header_safe_credential(token):
            return _Exchange(error="credential")
        try:
            request = self.http_client.build_request(
                "GET",
                self.base_url + "/contacts/" + contact_id,
                headers={"Accept": "application/json", "Authorization": "Bearer " + token},
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


class GetContactOperation:
    """Read a contact's selected identity and email fields."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query contract."""
        return GET_CONTACT_DEFINITION

    async def invoke(self, call: Call, input: GetContactInput) -> QueryAttempt[ContactMetadata]:
        """Read one contact and classify Go-compatible provider outcomes."""
        if not isinstance(input, GetContactInput) or _CONTACT_ID.fullmatch(input.contact_id) is None:
            return _branch("defect", FailureKind.VALIDATION, "contact ID must be a UUID")
        exchange = await self.client._get(call, input.contact_id)
        if exchange.error == "credential":
            return _branch(
                "defect",
                FailureKind.AUTHENTICATION,
                "Constant Contact credentials are unavailable or invalid",
            )
        if exchange.error == "refresh":
            return _retry(FailureKind.AVAILABILITY, "Constant Contact credentials could not be refreshed")
        if exchange.error == "request":
            return _retry(FailureKind.TRANSPORT, "Constant Contact request failed")
        if exchange.status == 429:
            return _retry(
                FailureKind.RATE_LIMIT, "Constant Contact rate limited the request", exchange.retry_after
            )
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(
                FailureKind.AVAILABILITY, "Constant Contact is temporarily unavailable", exchange.retry_after
            )
        if exchange.status == 404:
            return _branch("notFound", FailureKind.NOT_FOUND, "Constant Contact did not find the contact")
        if exchange.status != 200:
            return _branch(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                f"Constant Contact rejected HTTP {exchange.status}",
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "Constant Contact response exceeded the configured limit",
            )
        if exchange.error:
            return _retry(FailureKind.TRANSPORT, "Constant Contact response was interrupted")
        try:
            raw: Any = json.loads(exchange.body)
            if not isinstance(raw, dict):
                raise ValueError("contact is not an object")
            contact_id = raw.get("contact_id")
            if not isinstance(contact_id, str) or _CONTACT_ID.fullmatch(contact_id) is None:
                raise ValueError("contact identity missing")
            email = raw.get("email_address") or {}
            if not isinstance(email, dict):
                raise ValueError("invalid email channel")
            values = (
                email.get("address", ""),
                raw.get("first_name", ""),
                raw.get("last_name", ""),
                email.get("permission_to_send", ""),
            )
            if not all(isinstance(value, str) for value in values):
                raise ValueError("invalid contact field")
            result = ContactMetadata(contact_id, *values)
        except (ValueError, TypeError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Constant Contact returned invalid JSON")
        return QueryAttempt.branch_result("found", result, Receipt())
