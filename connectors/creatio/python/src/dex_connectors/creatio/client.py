# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Creatio OData contact lookup with renewable client credentials."""

from __future__ import annotations

import json
import re
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any
from urllib.parse import urlsplit

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
    resolve_credential,
)
from dex_connectors.sdk.oauthtoken import ManifestCredentialRefreshDriver
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
from ._generated.refresh import AUTHORIZATION, CREDENTIAL_FIELDS
from .models import ContactSummary, GetContactInput

_CONTACT_UUID = re.compile(r"^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$")


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One bounded OData response or local failure class."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "creatio", "getContact", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ContactSummary]:
    return QueryAttempt.branch_result(branch, ContactSummary(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ContactSummary]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _decode_contact(body: bytes, requested_id: str) -> tuple[ContactSummary | None, bool]:
    try:
        document: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return None, False
    if not isinstance(document, dict) or not isinstance(document.get("value"), list):
        return None, False
    records = document["value"]
    if not records:
        return None, True
    if len(records) != 1 or not isinstance(records[0], dict):
        return None, False
    record = records[0]
    contact_id, name, email = record.get("Id"), record.get("Name"), record.get("Email")
    if (
        not isinstance(contact_id, str)
        or not _CONTACT_UUID.fullmatch(contact_id)
        or contact_id.lower() != requested_id.lower()
        or not isinstance(name, str)
        or not name.strip()
        or email is not None and not isinstance(email, str)
    ):
        return None, False
    return ContactSummary(contact_id, name, email or ""), True


class Client:
    """Resolve Creatio tokens and read selected OData contact fields."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Creatio configuration and credential source are required")
        if config.timeout_seconds <= 0 or config.max_response_bytes < 1:
            raise ValueError("Creatio request limits must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        token_endpoint = validate_base_url(config.token_endpoint)
        parsed = urlsplit(token_endpoint)
        if not parsed.path.endswith("/connect/token") or parsed.query or parsed.fragment:
            raise ValueError("Creatio Identity Service token URL must end in /connect/token")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None
        self.refresh_driver = ManifestCredentialRefreshDriver[Credentials](
            "Creatio", AUTHORIZATION, CREDENTIAL_FIELDS, self.http_client,
            settings={"tokenEndpoint": token_endpoint},
        )

    def get_contact(self) -> GetContactOperation:
        """Bind the getContact Query to this client."""
        return GetContactOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client only when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def get(self, call: Call, contact_id: str) -> _Exchange:
        """Resolve a renewable token and send one bounded Contact query."""
        try:
            credentials = await resolve_credential(self.credentials, call, self.refresh_driver)
        except ReauthorizationRequiredError:
            return _Exchange(error="credential")
        except Exception:
            return _Exchange(error="refresh")
        token = credentials.access_token.reveal()
        if not is_header_safe_credential(token):
            return _Exchange(error="credential")
        try:
            request = self.http_client.build_request(
                "GET", self.base_url + "/0/odata/Contact",
                params={"$filter": "Id eq " + contact_id, "$select": "Id,Name,Email", "$top": "1"},
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
                return _Exchange(response.status_code, b"", retry_after, "too_large")
            except ProviderTransportError:
                return _Exchange(response.status_code, b"", retry_after, "interrupted")
            if token.encode() in body:
                return _Exchange(response.status_code, b"", retry_after, "credential")
            return _Exchange(response.status_code, body, retry_after)
        finally:
            await response.aclose()


class GetContactOperation:
    """Read one Creatio contact through OData 4."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query contract."""
        return GET_CONTACT_DEFINITION

    async def invoke(self, call: Call, input: GetContactInput) -> QueryAttempt[ContactSummary]:
        """Classify token, transport, HTTP, and OData outcomes."""
        if not _CONTACT_UUID.fullmatch(input.contact_id):
            return _branch("defect", FailureKind.VALIDATION, "Creatio contact UUID is invalid")
        exchange = await self.client.get(call, input.contact_id)
        if exchange.error == "credential":
            return _branch("defect", FailureKind.AUTHENTICATION, "Creatio connection needs authorization")
        if exchange.error == "refresh":
            return _retry(FailureKind.TRANSPORT, "Creatio credential minting failed")
        if exchange.error == "request":
            return _retry(FailureKind.TRANSPORT, "Creatio request failed")
        if exchange.status == 429:
            return _retry(FailureKind.RATE_LIMIT, "Creatio rate limited the request", exchange.retry_after)
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(
                FailureKind.AVAILABILITY, "Creatio is temporarily unavailable", exchange.retry_after
            )
        if exchange.status == 404:
            return _branch("notFound", FailureKind.NOT_FOUND, "Creatio contact was not found")
        if exchange.status != 200:
            return _branch(
                "providerRejected", FailureKind.PROVIDER_REJECTION, f"Creatio rejected HTTP {exchange.status}"
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "Creatio response exceeded the configured limit",
            )
        if exchange.error:
            return _retry(FailureKind.TRANSPORT, "Creatio response was interrupted")
        contact, valid = _decode_contact(exchange.body, input.contact_id)
        if not valid:
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Creatio returned invalid contact JSON")
        if contact is None:
            return _branch("notFound", FailureKind.NOT_FOUND, "Creatio contact was not found")
        return QueryAttempt.branch_result("found", contact, Receipt())
