# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Dataverse contact lookup using Entra client credentials."""

from __future__ import annotations

import json
import re
from dataclasses import dataclass
from datetime import UTC, datetime
from urllib.parse import urlencode, urlparse

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
from ._generated.definitions import FIND_CONTACT_DEFINITION
from .models import Contact, FindContactInput

_UUID = re.compile(r"^[0-9a-fA-F]{8}(?:-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}$")
_EMAIL = re.compile(r"^[^\s@]+@[^\s@]+\.[^\s@]+$")


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One bounded provider response or local failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "microsoft-dynamics-365-sales", "findContact", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[Contact]:
    return QueryAttempt.branch_result(branch, Contact(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[Contact]:
    return QueryAttempt.retry(_failure(kind, message), delay)


class Client:
    """Resolve the client secret and acquire a fresh token for every Query attempt."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        token_endpoint: str | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Dynamics 365 Sales configuration and credential source are required")
        if not _UUID.fullmatch(config.tenant_id) or not _UUID.fullmatch(config.client_id):
            raise ValueError("Dynamics 365 Sales tenant and client IDs must be UUIDs")
        if config.timeout_seconds <= 0 or config.max_response_bytes < 1:
            raise ValueError("Dynamics 365 Sales request limits must be positive")
        environment_url = validate_base_url(config.environment_url).rstrip("/")
        parsed = urlparse(environment_url)
        if (
            parsed.path
            or not (parsed.hostname or "").endswith(".dynamics.com")
            and parsed.hostname not in ("localhost", "127.0.0.1", "::1")
        ):
            raise ValueError("Dynamics 365 Sales environment URL must be a Dataverse origin")
        self.environment_url = environment_url
        self.client_id = config.client_id
        self.token_endpoint = (
            token_endpoint or f"https://login.microsoftonline.com/{config.tenant_id}/oauth2/v2.0/token"
        )
        token_url = urlparse(self.token_endpoint)
        if (
            not token_url.hostname
            or token_url.username
            or token_url.query
            or token_url.fragment
            or token_url.scheme not in ("https", "http")
            or (token_url.scheme == "http" and token_url.hostname not in ("localhost", "127.0.0.1", "::1"))
        ):
            raise ValueError("Dynamics 365 Sales token endpoint is invalid")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def find_contact(self) -> FindContactOperation:
        """Bind the findContact Query to this client."""
        return FindContactOperation(self)

    async def aclose(self) -> None:
        """Close an owned transport after Worker shutdown."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _send(
        self, method: str, url: str, *, headers: dict[str, str] | None = None, body: str | None = None
    ) -> _Exchange:
        try:
            request = self.http_client.build_request(method, url, headers=headers, content=body)
            response, _ = await send_traced(self.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _Exchange(error="request")
        try:
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            try:
                content = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _Exchange(status=response.status_code, retry_after=retry_after, error="too_large")
            except ProviderTransportError:
                return _Exchange(status=response.status_code, retry_after=retry_after, error="interrupted")
            return _Exchange(status=response.status_code, body=content, retry_after=retry_after)
        finally:
            await response.aclose()

    async def _query_contacts(self, call: Call, email_address: str) -> _Exchange:
        try:
            credentials = await self.credentials.resolve(call)
            client_secret = credentials.client_secret.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return _Exchange(error="credential")
        if not is_header_safe_credential(client_secret):
            return _Exchange(error="credential")
        form = urlencode(
            {
                "client_id": self.client_id,
                "client_secret": client_secret,
                "scope": self.environment_url + "/.default",
                "grant_type": "client_credentials",
            }
        )
        token = await self._send(
            "POST",
            self.token_endpoint,
            headers={"Content-Type": "application/x-www-form-urlencoded"},
            body=form,
        )
        if token.status != 200 or token.error:
            return token
        try:
            token_data = json.loads(token.body)
            access_token = token_data["access_token"]
            if token_data["token_type"] != "Bearer" or not is_header_safe_credential(access_token):
                raise ValueError("invalid token")
        except (ValueError, KeyError, TypeError, AttributeError):
            return _Exchange(status=200, error="invalid_token")
        quoted_email = email_address.replace("'", "''")
        query = urlencode(
            {
                "$select": "contactid,fullname,emailaddress1",
                "$filter": f"emailaddress1 eq '{quoted_email}'",
                "$top": "2",
            }
        )
        return await self._send(
            "GET",
            self.environment_url + "/api/data/v9.2/contacts?" + query,
            headers={
                "Authorization": "Bearer " + access_token,
                "Accept": "application/json",
                "OData-Version": "4.0",
                "OData-MaxVersion": "4.0",
            },
        )


class FindContactOperation:
    """Find exactly one Dataverse contact by primary email address."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query contract."""
        return FIND_CONTACT_DEFINITION

    async def invoke(self, call: Call, input: FindContactInput) -> QueryAttempt[Contact]:
        """Classify token, transport, and contact lookup outcomes."""
        if (
            not isinstance(input, FindContactInput)
            or not isinstance(input.email_address, str)
            or len(input.email_address) > 320
            or not _EMAIL.fullmatch(input.email_address)
        ):
            return _branch("defect", FailureKind.VALIDATION, "contact email address is invalid")
        exchange = await self.client._query_contacts(call, input.email_address)
        if exchange.error == "credential":
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "client credentials are unavailable or invalid"
            )
        if exchange.error == "request":
            return _retry(FailureKind.TRANSPORT, "Microsoft request failed")
        if exchange.status == 429:
            return _retry(FailureKind.RATE_LIMIT, "Microsoft rate limited the request", exchange.retry_after)
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(
                FailureKind.AVAILABILITY, "Microsoft is temporarily unavailable", exchange.retry_after
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "Microsoft response exceeded the configured limit",
            )
        if exchange.error == "invalid_token":
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Entra returned an invalid access token")
        if exchange.error:
            return _retry(FailureKind.TRANSPORT, "Microsoft response was interrupted")
        if exchange.status != 200:
            return _branch(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                f"Microsoft rejected HTTP {exchange.status}",
            )
        try:
            payload = json.loads(exchange.body)
            records = payload["value"]
            if not isinstance(records, list) or len(records) > 2:
                raise ValueError("invalid contacts")
        except (ValueError, TypeError, KeyError):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Dataverse returned invalid contacts JSON"
            )
        if not records:
            return QueryAttempt.branch_result("missing", Contact(), Receipt())
        if len(records) > 1:
            return QueryAttempt.branch_result("multipleMatches", Contact(), Receipt())
        record = records[0]
        if (
            not isinstance(record, dict)
            or not isinstance(record.get("contactid"), str)
            or not _UUID.fullmatch(record["contactid"])
            or not isinstance(record.get("emailaddress1"), str)
            or record["emailaddress1"].lower() != input.email_address.lower()
        ):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Dataverse returned an invalid contact")
        return QueryAttempt.branch_result(
            "found",
            Contact(record["contactid"], record.get("fullname") or "", record["emailaddress1"]),
            Receipt(),
        )
