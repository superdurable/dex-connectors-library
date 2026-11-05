# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Authenticated, bounded Freshdesk HTTP client."""

import asyncio
import base64
import json
import re
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import TYPE_CHECKING, Any
from urllib.parse import quote_plus

import httpx
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    ReauthorizationRequiredError,
    Receipt,
)
from dex_connectors.sdk.projectconfig.broker import ConnectionNotSelectedError, CredentialBrokerRejectedError
from dex_connectors.sdk.providerhttp import (
    BodyTooLargeError,
    ProviderTransportError,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials

if TYPE_CHECKING:
    from .operations import (
        AddNoteOperation,
        CreateTicketOperation,
        GetTicketOperation,
        SearchTicketsOperation,
        UpdateTicketOperation,
    )

_DOMAIN = re.compile(r"^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$")
_REQUEST_ID = re.compile(r"^[A-Za-z0-9._:-]{1,128}$")
_FIELD = re.compile(r"^[a-z][a-z0-9_]{0,63}$")
_TOKEN = re.compile(r"^[A-Za-z0-9_.:-]{1,64}$")


@dataclass(frozen=True, slots=True)
class Exchange:
    """One classified provider response, retaining only safe response data."""

    outcome: str
    failure: Failure | None = None
    status: int = 0
    headers: httpx.Headers | None = None
    body: bytes = b""
    retry_after: float = 0


class Client:
    """Execute Freshdesk API calls with credentials resolved at call time."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        *,
        api_base_url: str = "",
        now: Any = None,
    ) -> None:
        if config is None or credentials is None or not _DOMAIN.fullmatch(config.domain):
            raise ValueError("Freshdesk domain and credential provider are required")
        if type(config.max_response_bytes) is not int or config.max_response_bytes < 1:
            raise ValueError("Freshdesk response limit must be positive")
        self.config = config
        self.credentials = credentials
        self._endpoint = validate_base_url(
            api_base_url or f"https://{config.domain}.freshdesk.com/api/v2"
        ).rstrip("/")
        self._client = http_client or new_provider_http_client(12)
        self._owns_client = http_client is None
        self._now = now or (lambda: datetime.now(UTC))

    async def aclose(self) -> None:
        """Close the owned transport after operation invocations finish."""
        if self._owns_client:
            await self._client.aclose()

    def search_tickets(self) -> "SearchTicketsOperation":
        """Return the ticket search query."""
        from .operations import SearchTicketsOperation

        return SearchTicketsOperation(self)

    def get_ticket(self) -> "GetTicketOperation":
        """Return the ticket read query."""
        from .operations import GetTicketOperation

        return GetTicketOperation(self)

    def create_ticket(self) -> "CreateTicketOperation":
        """Return the ticket creation mutation."""
        from .operations import CreateTicketOperation

        return CreateTicketOperation(self)

    def update_ticket(self) -> "UpdateTicketOperation":
        """Return the ticket update mutation."""
        from .operations import UpdateTicketOperation

        return UpdateTicketOperation(self)

    def add_note(self) -> "AddNoteOperation":
        """Return the note and reply mutation."""
        from .operations import AddNoteOperation

        return AddNoteOperation(self)

    async def resolve_credentials(self, call: Call, operation: str) -> tuple[str, Failure | None, bool]:
        """Resolve the current key, distinguishing terminal and transient credential errors."""
        try:
            credentials = await self.credentials.resolve(call)
        except (
            LookupError,
            ReauthorizationRequiredError,
            ConnectionNotSelectedError,
            CredentialBrokerRejectedError,
        ):
            return (
                "",
                self.failure(operation, FailureKind.AUTHENTICATION, "connection credentials are unavailable"),
                False,
            )
        except Exception:
            return (
                "",
                self.failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    "connection credentials could not be read; no request was sent",
                ),
                True,
            )
        if not isinstance(credentials, Credentials):
            return (
                "",
                self.failure(operation, FailureKind.AUTHENTICATION, "connection credentials are unavailable"),
                False,
            )
        key = credentials.api_key.reveal()
        if not key or ":" in key or any(ord(char) < 33 or ord(char) > 126 for char in key):
            return (
                "",
                self.failure(operation, FailureKind.AUTHENTICATION, "connection credentials are unavailable"),
                False,
            )
        return key, None, False

    async def exchange(
        self,
        call: Call,
        operation: str,
        key: str,
        method: str,
        path: str,
        query: dict[str, str] | None = None,
        payload: dict[str, Any] | None = None,
        write_budget: float | None = None,
    ) -> Exchange:
        """Send one request and classify its observed provider result."""
        try:
            url = self._endpoint + path
            if query:
                url += "?" + "&".join(
                    quote_plus(query_key, safe="") + "=" + quote_plus(value, safe="")
                    for query_key, value in sorted(query.items())
                )
            headers = {
                "Accept": "application/json",
                "Authorization": "Basic " + base64.b64encode(f"{key}:X".encode()).decode(),
            }
        except (TypeError, ValueError, UnicodeError):
            return Exchange(
                "defect",
                self.failure(operation, FailureKind.LOCAL_DEFECT, "Freshdesk request could not be built"),
            )
        deadline = getattr(call.context, "deadline", None)
        remaining = (deadline - datetime.now(UTC)).total_seconds() if deadline is not None else None
        minimum_request_time = write_budget if write_budget is not None else 2.0
        if remaining is not None and remaining <= 1 + minimum_request_time:
            return Exchange(
                "notSent",
                self.failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    "too little attempt time remained, so the request was not sent",
                ),
            )
        try:
            body = None
            if payload is not None:
                body = json.dumps(payload, separators=(",", ":"), ensure_ascii=False).encode()
                headers["Content-Type"] = "application/json"
            request = self._client.build_request(method, url, headers=headers, content=body)
        except (TypeError, ValueError, UnicodeError, httpx.HTTPError):
            return Exchange(
                "defect",
                self.failure(operation, FailureKind.LOCAL_DEFECT, "Freshdesk request could not be built"),
            )
        try:
            async with asyncio.timeout(remaining - 1 if remaining is not None else None):
                response, trace = await send_traced(self._client, request)
                try:
                    contents = await read_bounded_body(response, self.config.max_response_bytes)
                    exchange = Exchange(
                        "succeeded", status=response.status_code, headers=response.headers, body=contents
                    )
                except BodyTooLargeError:
                    exchange = Exchange("invalid", status=response.status_code, headers=response.headers)
            if 200 <= exchange.status < 300:
                if exchange.outcome == "invalid":
                    return Exchange(
                        "invalid",
                        self.failure(
                            operation,
                            FailureKind.RESPONSE_TOO_LARGE,
                            "Freshdesk response exceeds the configured maxResponseBytes limit",
                        ),
                        exchange.status,
                        exchange.headers,
                    )
                if key.encode() in exchange.body:
                    return Exchange(
                        "invalid",
                        self.failure(
                            operation,
                            FailureKind.PROTOCOL,
                            "Freshdesk response reflected the connection credential",
                        ),
                        exchange.status,
                        exchange.headers,
                    )
                return exchange
            return self._classify_status(operation, key, exchange)
        except ProviderTransportError as error:
            not_sent = getattr(error, "provably_not_sent", False)
            message = (
                "Freshdesk could not be reached; no request was sent"
                if not_sent
                else "Freshdesk request failed before a response arrived"
            )
            return Exchange(
                "notSent" if not_sent else "unavailable",
                self.failure(operation, FailureKind.TRANSPORT, message),
            )
        except (httpx.ConnectError, httpx.ConnectTimeout):
            return Exchange(
                "notSent",
                self.failure(
                    operation, FailureKind.TRANSPORT, "Freshdesk could not be reached; no request was sent"
                ),
            )
        except Exception:
            return Exchange(
                "unavailable",
                self.failure(
                    operation, FailureKind.TRANSPORT, "Freshdesk request failed before a response arrived"
                ),
            )

    def _classify_status(self, operation: str, key: str, exchange: Exchange) -> Exchange:
        status = exchange.status
        if status == 429:
            outcome, kind, message = (
                "rateLimited",
                FailureKind.RATE_LIMIT,
                "Freshdesk rate limited the request (HTTP 429)",
            )
        elif status == 503:
            outcome, kind, message = (
                "serviceUnavailable",
                FailureKind.AVAILABILITY,
                "Freshdesk is unavailable (HTTP 503)",
            )
        elif status == 408 or status >= 500:
            outcome, kind, message = (
                "unavailable",
                FailureKind.AVAILABILITY,
                f"Freshdesk could not complete the request (HTTP {status})",
            )
        elif status == 404:
            outcome, kind, message = (
                "notFound",
                FailureKind.NOT_FOUND,
                "Freshdesk found no such resource (HTTP 404)",
            )
        elif 300 <= status < 400:
            outcome, kind, message = (
                "rejected",
                FailureKind.PROTOCOL,
                f"Freshdesk redirected the request (HTTP {status}); check that the domain is the "
                "freshdesk.com helpdesk domain",
            )
        else:
            outcome = "rejected"
            kind = {
                401: FailureKind.AUTHENTICATION,
                403: FailureKind.AUTHORIZATION,
                409: FailureKind.CONFLICT,
                400: FailureKind.VALIDATION,
            }.get(status, FailureKind.PROVIDER_REJECTION)
            message = f"Freshdesk rejected the request (HTTP {status})"
        if not 300 <= status < 400:
            message += _error_summary(exchange.body, key)
        delay = (
            parse_retry_after(exchange.headers.get("Retry-After", ""), self._now()) if exchange.headers else 0
        )
        return Exchange(
            outcome, self.failure(operation, kind, message), status, exchange.headers, retry_after=delay
        )

    def receipt(self, call: Call, exchange: Exchange | None = None, object_id: int = 0) -> Receipt:
        """Record safe provider identifiers and the stable mutation correlation key."""
        request_id = exchange.headers.get("X-Request-Id", "") if exchange and exchange.headers else ""
        return Receipt(
            call.id,
            "freshdesk",
            self._now().astimezone(UTC),
            call.idempotency_key,
            str(object_id) if object_id > 0 else "",
            request_id if _REQUEST_ID.fullmatch(request_id) else "",
        )

    @staticmethod
    def failure(operation: str, kind: FailureKind | str, message: str) -> Failure:
        """Construct a secret-free operation failure."""
        return Failure(FailureKind(kind), "freshdesk", operation, message)


def _error_summary(body: bytes, secret: str) -> str:
    try:
        value = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return ""
    if not isinstance(value, dict):
        return ""
    parts = []
    code = value.get("code")
    if isinstance(code, str) and _TOKEN.fullmatch(code) and secret not in code:
        parts.append(code)
    errors = value.get("errors")
    if isinstance(errors, list):
        fields = []
        for item in errors:
            if not isinstance(item, dict):
                continue
            field, field_code = item.get("field"), item.get("code")
            if isinstance(field, str) and _FIELD.fullmatch(field) and secret not in field:
                fields.append(
                    field
                    + (
                        "=" + field_code
                        if isinstance(field_code, str)
                        and _TOKEN.fullmatch(field_code)
                        and secret not in field_code
                        else ""
                    )
                )
        if fields:
            parts.append("errors: " + ", ".join(sorted(fields)[:5]))
    return " [" + "; ".join(parts) + "]" if parts else ""
