# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Udesk ticket-status Query and signed HTTP transport."""

import hashlib
import json
import os
from collections.abc import Callable
from datetime import UTC, datetime
from urllib.parse import urlsplit

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
from ._generated.definitions import LIST_TICKET_STATUSES_DEFINITION
from .models import ListTicketStatusesInput, ListTicketStatusesOutput, TicketStatus


def _valid_email(value: str) -> bool:
    if not isinstance(value, str) or len(value) > 320 or value.strip() != value or value.count("@") != 1:
        return False
    local, domain = value.split("@")
    return bool(local and "." in domain and not any(char in " \t\r\n&=?#" for char in value))


def _valid_token(value: str) -> bool:
    return (
        isinstance(value, str) and 0 < len(value) <= 512 and all(0x21 <= ord(char) <= 0x7E for char in value)
    )


class Client:
    """Own account configuration and resolve current credentials for each Query."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        clock: Callable[[], datetime] | None = None,
        random_bytes: Callable[[int], bytes] | None = None,
    ) -> None:
        if (
            config is None
            or credentials is None
            or config.timeout_seconds <= 0
            or config.max_response_bytes <= 0
        ):
            raise ValueError("Udesk configuration and credentials are required")
        origin = urlsplit(config.base_url)
        host = (origin.hostname or "").lower()
        local = host in ("127.0.0.1", "localhost", "::1")
        account = host.endswith(".udesk.cn") and len(host.removesuffix(".udesk.cn")) <= 63
        if (
            origin.scheme not in (("http", "https") if local else ("https",))
            or not (local or account)
            or origin.path not in ("", "/")
            or origin.query
            or origin.fragment
            or origin.username
            or origin.password
            or (not local and origin.port is not None)
        ):
            raise ValueError("Udesk base URL must be an HTTPS account origin")
        self.config = config
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_http_client = http_client is None
        self.clock = clock or (lambda: datetime.now(UTC))
        self.random_bytes = random_bytes or os.urandom

    def list_ticket_statuses(self) -> "ListTicketStatusesOperation":
        """Bind the listTicketStatuses Query to this client."""
        return ListTicketStatusesOperation(self)

    async def aclose(self) -> None:
        """Close only a transport owned by this client."""
        if self._owns_http_client:
            await self.http_client.aclose()


class ListTicketStatusesOperation:
    """Read ticket-status metadata from the Udesk Open API."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query definition."""
        return LIST_TICKET_STATUSES_DEFINITION

    def _branch(self, branch: str, kind: FailureKind) -> QueryAttempt[ListTicketStatusesOutput]:
        return QueryAttempt.branch_result(
            branch,
            ListTicketStatusesOutput(),
            Receipt(),
            Failure(kind, "udesk", "listTicketStatuses", "Udesk ticket status query failed"),
        )

    def _retry(self, kind: FailureKind, delay: float = 0) -> QueryAttempt[ListTicketStatusesOutput]:
        return QueryAttempt.retry(Failure(kind, "udesk", "listTicketStatuses", "Udesk request failed"), delay)

    async def invoke(
        self, call: Call, input: ListTicketStatusesInput
    ) -> QueryAttempt[ListTicketStatusesOutput]:
        """Sign and read one Go-compatible ticket-status list."""
        if not isinstance(input, ListTicketStatusesInput):
            return self._branch("defect", FailureKind.VALIDATION)
        try:
            credentials = await self.client.credentials.resolve(call)
            email, token = credentials.admin_email.reveal(), credentials.api_token.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        if not _valid_email(email) or not _valid_token(token):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        try:
            nonce = self.client.random_bytes(16).hex()
        except (ValueError, OSError):
            return self._retry(FailureKind.AVAILABILITY)
        timestamp = str(int(self.client.clock().timestamp()))
        signature = hashlib.sha256(f"{email}&{token}&{timestamp}&{nonce}&v2".encode()).hexdigest()
        params = {
            "email": email,
            "timestamp": timestamp,
            "nonce": nonce,
            "sign_version": "v2",
            "sign": signature,
        }
        if input.enabled_only is not None:
            params["enable"] = "true" if input.enabled_only else "false"
        request = self.client.http_client.build_request(
            "GET",
            self.client.config.base_url.rstrip("/") + "/open_api_v1/tickets/status",
            params=params,
            headers={"Accept": "application/json"},
        )
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return self._retry(FailureKind.AVAILABILITY)
        try:
            status = response.status_code
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return self._retry(FailureKind.RATE_LIMIT, delay)
            if status == 408 or status >= 500:
                return self._retry(FailureKind.AVAILABILITY, delay)
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return self._branch("providerRejected", kind)
            try:
                body = await read_bounded_body(response, self.client.config.max_response_bytes)
            except BodyTooLargeError:
                return self._branch("invalidResponse", FailureKind.RESPONSE_TOO_LARGE)
            except ProviderTransportError:
                return self._retry(FailureKind.AVAILABILITY)
        finally:
            await response.aclose()
        try:
            wire = json.loads(body)
        except (UnicodeDecodeError, ValueError):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        if (
            not isinstance(wire, dict)
            or not isinstance(wire.get("code"), int)
            or isinstance(wire["code"], bool)
        ):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        if wire["code"] != 1000:
            kind = FailureKind.AUTHENTICATION if wire["code"] == 2059 else FailureKind.PROVIDER_REJECTION
            return self._branch("providerRejected", kind)
        values = wire.get("status")
        if not isinstance(values, list):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        statuses: list[TicketStatus] = []
        for item in values:
            if not isinstance(item, dict):
                return self._branch("invalidResponse", FailureKind.PROTOCOL)
            identifier, name, english, enabled, color = (
                item.get("status_id"),
                item.get("name", ""),
                item.get("en_name", ""),
                item.get("enable"),
                item.get("color", ""),
            )
            if (
                not isinstance(identifier, int)
                or isinstance(identifier, bool)
                or identifier <= 0
                or not isinstance(name, str)
                or not name
                or not isinstance(english, str)
                or not english
                or not isinstance(enabled, bool)
                or not isinstance(color, str)
            ):
                return self._branch("invalidResponse", FailureKind.PROTOCOL)
            statuses.append(TicketStatus(identifier, name, english, enabled, color))
        return QueryAttempt.branch_result("listed", ListTicketStatusesOutput(tuple(statuses)), Receipt())
