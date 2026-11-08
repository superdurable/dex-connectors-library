# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Keap CRM v2 contact lookup with Go-compatible outcomes."""

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
from .models import ContactProfile, GetContactInput

_CONTACT_ID = re.compile(r"[A-Za-z0-9_-]{1,128}\Z")


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One bounded provider response or request failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "keap", "getContact", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ContactProfile]:
    return QueryAttempt.branch_result(branch, ContactProfile(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ContactProfile]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _valid_contact_id(value: object) -> bool:
    return isinstance(value, str) and _CONTACT_ID.fullmatch(value) is not None


def _decode_contact(body: bytes, contact_id: str) -> ContactProfile:
    raw: Any = json.loads(body)
    if not isinstance(raw, dict) or raw.get("id") != contact_id:
        raise ValueError("contact ID mismatch")
    given_name = raw.get("given_name")
    family_name = raw.get("family_name")
    given_name = "" if given_name is None else given_name
    family_name = "" if family_name is None else family_name
    if not isinstance(given_name, str) or not isinstance(family_name, str):
        raise ValueError("contact names are invalid")
    return ContactProfile(contact_id, given_name, family_name)


class Client:
    """Resolve a Keap Personal Access Token per call and read contacts."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Keap configuration and credential source are required")
        if (
            config.timeout_seconds <= 0
            or type(config.max_response_bytes) is not int
            or config.max_response_bytes < 1
        ):
            raise ValueError("Keap request limits must be positive")
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

    async def _get(self, call: Call, input: GetContactInput) -> _Exchange:
        try:
            credential = await self.credentials.resolve(call)
            access_token = credential.personal_access_token.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return _Exchange(error="credential")
        if not is_header_safe_credential(access_token):
            return _Exchange(error="credential")
        try:
            request = self.http_client.build_request(
                "GET",
                self.base_url + "/rest/v2/contacts/" + input.contact_id,
                params={"fields": "id,given_name,family_name"},
                headers={"Accept": "application/json", "Authorization": "Bearer " + access_token},
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
    """Read one contact by ID from Keap CRM v2."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query worker."""
        return GET_CONTACT_DEFINITION

    async def invoke(self, call: Call, input: GetContactInput) -> QueryAttempt[ContactProfile]:
        """Classify provider outcomes consistently with Go."""
        if not isinstance(input, GetContactInput) or not _valid_contact_id(input.contact_id):
            return _branch(
                "defect", FailureKind.VALIDATION,
                "contactId must contain 1–128 ASCII letters, digits, hyphens, or underscores",
            )
        exchange = await self.client._get(call, input)
        if exchange.error == "credential":
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Keap credentials are unavailable or invalid"
            )
        if exchange.error == "request":
            return _retry(FailureKind.TRANSPORT, "Keap request failed")
        if exchange.status == 429:
            return _retry(FailureKind.RATE_LIMIT, "Keap rate limited the request", exchange.retry_after)
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(FailureKind.AVAILABILITY, "Keap is temporarily unavailable", exchange.retry_after)
        if exchange.status == 404:
            return _branch("notFound", FailureKind.NOT_FOUND, "Keap did not find the contact")
        if exchange.status != 200:
            return _branch(
                "providerRejected", FailureKind.PROVIDER_REJECTION, f"Keap rejected HTTP {exchange.status}"
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "Keap response exceeded the configured limit",
            )
        if exchange.error:
            return _retry(FailureKind.TRANSPORT, "Keap response was interrupted")
        try:
            profile = _decode_contact(exchange.body, input.contact_id)
        except (ValueError, TypeError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Keap returned invalid contact JSON")
        return QueryAttempt.branch_result("found", profile, Receipt())
