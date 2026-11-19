# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""KF5 provider transport and Go-compatible Query decisions."""

import base64
import json
import re
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
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import GET_TICKET_DEFINITION
from .models import GetTicketInput, GetTicketOutput


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[GetTicketOutput]:
    return QueryAttempt.branch_result(
        branch, GetTicketOutput(), Receipt(), Failure(kind, "kf5", "getTicket", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[GetTicketOutput]:
    return QueryAttempt.retry(Failure(kind, "kf5", "getTicket", message), delay)


def _ascii_value(value: str, limit: int, allow_empty: bool = False) -> bool:
    return (
        isinstance(value, str)
        and (allow_empty or bool(value))
        and len(value.encode()) <= limit
        and all(0x20 < ord(c) < 0x7F for c in value)
    )


class Client:
    """Resolve credentials per getTicket request and own a redirect-safe HTTP client."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("KF5 configuration and credentials are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("KF5 timeout or response limit is invalid")

        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_ticket(self) -> "GetTicketOperation":
        """Bind the getTicket Query to this client."""
        return GetTicketOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client if this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class GetTicketOperation:
    """Perform one getTicket read and filter the provider response."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("KF5 client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Go-derived Query identity."""
        return GET_TICKET_DEFINITION

    async def invoke(self, call: Call, input: GetTicketInput) -> QueryAttempt[GetTicketOutput]:
        """Classify transport and provider outcomes without exposing secrets."""
        try:
            if input.ticket_id <= 0:
                return _branch("defect", FailureKind.VALIDATION, "KF5 requires a positive ticket ID")
            credentials = await self.client.credentials.resolve(call)
            email = credentials.email.reveal()
            token = credentials.api_token.reveal()
            if (
                not re.fullmatch(r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?", credentials.subdomain)
                or not _ascii_value(email, 254)
                or "@" not in email
                or not _ascii_value(token, 1024)
            ):
                return _branch(
                    "defect", FailureKind.AUTHENTICATION, "KF5 tenant credentials are unavailable or invalid"
                )
            basic = base64.b64encode(f"{email}/token:{token}".encode()).decode()
            request = self.client.http_client.build_request(
                "GET",
                f"https://{credentials.subdomain}.kf5.com/apiv2/tickets/{input.ticket_id}.json",
                headers={"Accept": "application/json", "Authorization": f"Basic {basic}"},
            )
        except (ValueError, LookupError, OSError, httpx.HTTPError):
            return _branch("defect", FailureKind.AUTHENTICATION, "KF5 credentials or request are invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "KF5 request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "KF5 rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "KF5 is temporarily unavailable", delay)
            if status == 404:
                return _branch("noData", FailureKind.NOT_FOUND, "KF5 did not find the ticket")
            if status != 200:
                kind = FailureKind.PROVIDER_REJECTION
                if status == 401:
                    kind = FailureKind.AUTHENTICATION
                elif status == 403:
                    kind = FailureKind.AUTHORIZATION
                return _branch("providerRejected", kind, f"KF5 rejected request with HTTP {status}")
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "KF5 response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "KF5 response was interrupted")
        finally:
            await response.aclose()

        try:
            document: Any = json.loads(body)
        except (ValueError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "KF5 returned invalid JSON")
        ticket = document.get("ticket") if isinstance(document, dict) else None
        if (
            not isinstance(ticket, dict)
            or type(ticket.get("id")) is not int
            or ticket["id"] != input.ticket_id
            or ticket.get("status") not in ("new", "open", "pending", "solved", "closed")
            or not all(
                isinstance(ticket.get(k, ""), str) for k in ("priority", "type", "created_at", "updated_at")
            )
            or len(ticket.get("priority", "")) > 30
            or len(ticket.get("type", "")) > 30
            or len(ticket.get("created_at", "")) > 40
            or len(ticket.get("updated_at", "")) > 40
        ):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "KF5 returned invalid ticket data")
        return QueryAttempt.branch_result(
            "found",
            GetTicketOutput(
                ticket_id=ticket["id"],
                status=ticket["status"],
                priority=ticket.get("priority", ""),
                type=ticket.get("type", ""),
                created_at=ticket.get("created_at", ""),
                updated_at=ticket.get("updated_at", ""),
            ),
            Receipt(),
        )
