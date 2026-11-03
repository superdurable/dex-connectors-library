# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Credential-safe Gorgias transport and provider response decoding."""

import base64
import json
import re
from collections.abc import Callable
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import TYPE_CHECKING, Any

import httpx
from dex_connectors.sdk import Call, CredentialProvider, Failure, FailureKind, Receipt
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
from .models import Customer, Ticket, TicketMessage

if TYPE_CHECKING:
    from .operations import (
        AddNoteOperation,
        CreateTicketOperation,
        FindCustomerByEmailOperation,
        GetTicketOperation,
        SearchTicketsOperation,
        UpdateTicketOperation,
    )

_DOMAIN = re.compile(r"^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$")
_CURSOR = re.compile(r"^[A-Za-z0-9+/=_.:-]{1,2048}$")
_TIMESTAMP = re.compile(r"^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d+)?(?:Z|[+-]\d\d:\d\d)?$")
_REQUEST_ID = re.compile(r"^[A-Za-z0-9._:-]{1,128}$")
_API_LIMIT = re.compile(r"^[0-9]{1,6}/[0-9]{1,6}$")
_ERROR_CODE = re.compile(r"^[a-z][a-z0-9]*(?:_[a-z0-9]+){1,7}$")
_ERROR_FIELD = re.compile(r"^[a-z][a-z0-9_]{0,63}$")


@dataclass(frozen=True, slots=True)
class Exchange:
    """One bounded provider exchange and its safe classification."""

    outcome: str
    failure: Failure | None = None
    body: bytes = b""
    status: int = 0
    headers: httpx.Headers | None = None
    retry_after: float = 0


class Client:
    """Bind six Gorgias operations to one credential source and owned HTTP transport."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        api_base_url: str = "",
        now: Callable[[], datetime] | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Gorgias configuration and credential provider are required")
        if not _DOMAIN.fullmatch(config.domain):
            raise ValueError("Gorgias domain must be lowercase letters, digits, and hyphens")
        if config.max_response_bytes < 1:
            raise ValueError("Gorgias response limit must be positive")
        self.base_url = validate_base_url(api_base_url or f"https://{config.domain}.gorgias.com/api")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(8)
        self._owns_http_client = http_client is None
        self.now = now or (lambda: datetime.now(UTC))

    async def aclose(self) -> None:
        """Close the transport when this connector constructed it."""
        if self._owns_http_client:
            await self.http_client.aclose()

    def search_tickets(self) -> "SearchTicketsOperation":
        """Return the ticket search Query."""
        from .operations import SearchTicketsOperation

        return SearchTicketsOperation(self)

    def get_ticket(self) -> "GetTicketOperation":
        """Return the ticket read Query."""
        from .operations import GetTicketOperation

        return GetTicketOperation(self)

    def find_customer_by_email(self) -> "FindCustomerByEmailOperation":
        """Return the customer lookup Query."""
        from .operations import FindCustomerByEmailOperation

        return FindCustomerByEmailOperation(self)

    def create_ticket(self) -> "CreateTicketOperation":
        """Return the single-dispatch ticket creation Mutation."""
        from .operations import CreateTicketOperation

        return CreateTicketOperation(self)

    def update_ticket(self) -> "UpdateTicketOperation":
        """Return the ticket update Mutation."""
        from .operations import UpdateTicketOperation

        return UpdateTicketOperation(self)

    def add_note(self) -> "AddNoteOperation":
        """Return the single-dispatch note or reply Mutation."""
        from .operations import AddNoteOperation

        return AddNoteOperation(self)

    async def resolve_credentials(
        self, call: Call, operation: str
    ) -> tuple[Credentials | None, Failure | None]:
        """Resolve the current Basic credential without exposing it to durable state."""
        try:
            credentials = await self.credentials.resolve(call)
            if (
                not bare_email(credentials.email)
                or ":" in credentials.email
                or not is_header_safe_credential(credentials.api_key.reveal())
            ):
                raise ValueError("invalid Gorgias credential")
            return credentials, None
        except Exception:
            return None, failure(
                FailureKind.AUTHENTICATION, operation, "connection credentials are unavailable"
            )

    async def exchange(
        self,
        call: Call,
        credentials: Credentials,
        operation: str,
        method: str,
        path: str,
        query: dict[str, str] | None = None,
        payload: dict[str, Any] | None = None,
    ) -> Exchange:
        """Send one request and classify the outcome without selecting an operation branch."""
        basic = base64.b64encode(f"{credentials.email}:{credentials.api_key.reveal()}".encode()).decode()
        headers = {"Accept": "application/json", "Authorization": "Basic " + basic}
        if payload is not None:
            headers["Content-Type"] = "application/json"
        try:
            request = self.http_client.build_request(
                method,
                self.base_url + path,
                params=query,
                content=json.dumps(payload, separators=(",", ":")) if payload is not None else None,
                headers=headers,
            )
        except (ValueError, TypeError, httpx.HTTPError):
            return Exchange(
                "defect", failure(FailureKind.LOCAL_DEFECT, operation, "Gorgias request could not be built")
            )
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError as error:
            outcome = "notSent" if error.provably_not_sent else "unavailable"
            message = (
                "Gorgias could not be reached; no request was sent"
                if error.provably_not_sent
                else "Gorgias request failed before a response arrived"
            )
            return Exchange(outcome, failure(FailureKind.TRANSPORT, operation, message))
        try:
            body = await read_bounded_body(response, self.max_response_bytes)
        except BodyTooLargeError:
            if 200 <= response.status_code < 300:
                return Exchange(
                    "invalid",
                    failure(
                        FailureKind.RESPONSE_TOO_LARGE,
                        operation,
                        "Gorgias response exceeds the configured maxResponseBytes limit",
                    ),
                    status=response.status_code,
                    headers=response.headers,
                )
            body = b""
        except ProviderTransportError:
            return Exchange(
                "unavailable",
                failure(FailureKind.TRANSPORT, operation, "Gorgias response could not be read"),
                status=response.status_code,
                headers=response.headers,
            )
        if 200 <= response.status_code < 300:
            if credentials.api_key.reveal().encode() in body:
                return Exchange(
                    "invalid",
                    failure(
                        FailureKind.PROTOCOL,
                        operation,
                        "Gorgias response reflected the connection credential",
                    ),
                    status=response.status_code,
                    headers=response.headers,
                )
            return Exchange("success", body=body, status=response.status_code, headers=response.headers)
        status = response.status_code
        summary = describe_error(body, credentials.api_key.reveal())
        suffix = f" [{summary}]" if summary else ""
        delay = parse_retry_after(response.headers.get("Retry-After", ""), self.now())
        if status == 429:
            return Exchange(
                "rateLimited",
                failure(
                    FailureKind.RATE_LIMIT, operation, "Gorgias rate limited the request (HTTP 429)" + suffix
                ),
                status=status,
                headers=response.headers,
                retry_after=delay,
            )
        if status == 408 or status >= 500:
            return Exchange(
                "unavailable",
                failure(
                    FailureKind.AVAILABILITY,
                    operation,
                    f"Gorgias could not complete the request (HTTP {status})" + suffix,
                ),
                status=status,
                headers=response.headers,
                retry_after=delay,
            )
        if status == 404:
            return Exchange(
                "notFound",
                failure(
                    FailureKind.NOT_FOUND, operation, "Gorgias found no such resource (HTTP 404)" + suffix
                ),
                status=status,
                headers=response.headers,
            )
        if 300 <= status < 400:
            return Exchange(
                "rejected",
                failure(
                    FailureKind.PROTOCOL,
                    operation,
                    f"Gorgias redirected the request (HTTP {status}); "
                    "the ticket or customer may have been merged into another",
                ),
                status=status,
                headers=response.headers,
            )
        kind = {
            401: FailureKind.AUTHENTICATION,
            403: FailureKind.AUTHORIZATION,
            409: FailureKind.CONFLICT,
            400: FailureKind.VALIDATION,
        }.get(status, FailureKind.PROVIDER_REJECTION)
        return Exchange(
            "rejected",
            failure(kind, operation, f"Gorgias rejected the request (HTTP {status})" + suffix),
            status=status,
            headers=response.headers,
        )

    def receipt(self, call: Call, exchange: Exchange, object_id: int = 0) -> Receipt:
        """Build provider-safe correlation data, retaining only validated headers."""
        headers = exchange.headers or httpx.Headers()
        request_id = headers.get("X-Request-Id", "")
        limit = headers.get("X-Gorgias-Account-Api-Call-Limit", "")
        return Receipt(
            call.id,
            "gorgias",
            self.now(),
            call.idempotency_key,
            str(object_id) if object_id > 0 else "",
            request_id if _REQUEST_ID.fullmatch(request_id) else "",
            {"apiCallLimit": limit} if _API_LIMIT.fullmatch(limit) else {},
        )


def failure(kind: FailureKind, operation: str, message: str) -> Failure:
    """Construct a provider-safe Gorgias failure."""
    return Failure(kind, "gorgias", operation, message)


def describe_error(body: bytes, secret: str) -> str:
    """Keep only safe provider error codes and field names."""
    try:
        document = json.loads(body)
        error = document["error"]
        code = error.get("msg", "")
        parts = [code] if isinstance(code, str) and _ERROR_CODE.fullmatch(code) and secret not in code else []
        fields = error.get("data", {})
        if isinstance(fields, dict):
            names = sorted(name for name in fields if _ERROR_FIELD.fullmatch(name) and secret not in name)
            if names:
                parts.append("fields: " + ", ".join(names[:5]))
        return "; ".join(parts)
    except (ValueError, TypeError, KeyError, AttributeError):
        return ""


def bare_email(value: str) -> bool:
    """Require one bare mailbox accepted by Go's mail.ParseAddress shape."""
    if not isinstance(value, str) or not value or any(character in value for character in " \"'()<>,;"):
        return False
    from email.policy import default

    try:
        address_header = default.header_factory("to", value)
        return (
            not address_header.defects
            and len(address_header.addresses) == 1
            and address_header.addresses[0].display_name == ""
            and str(address_header.addresses[0]) == value
            and "@" in value
        )
    except (TypeError, ValueError):
        return False


def parse_time(value: str) -> datetime:
    """Decode Gorgias UTC timestamps with or without an explicit offset."""
    if not isinstance(value, str) or not _TIMESTAMP.fullmatch(value):
        raise ValueError("timestamp is not ISO 8601")
    result = datetime.fromisoformat(value.replace("Z", "+00:00"))
    return result.replace(tzinfo=UTC) if result.tzinfo is None else result.astimezone(UTC)


def optional_time(value: Any) -> datetime | None:
    """Decode a nullable Gorgias timestamp."""
    return parse_time(value) if value else None


def decode_list(body: bytes, resource: str) -> tuple[list[dict[str, Any]], str]:
    """Require a cursor-paginated Gorgias list envelope."""
    try:
        document = json.loads(body)
        data = document["data"]
        cursor = document.get("meta", {}).get("next_cursor") or ""
        if (
            not isinstance(data, list)
            or any(not isinstance(item, dict) for item in data)
            or (cursor and (not isinstance(cursor, str) or not _CURSOR.fullmatch(cursor)))
        ):
            raise ValueError
        return data, cursor
    except (ValueError, TypeError, KeyError, AttributeError):
        raise ValueError(f"{resource} page is not a Gorgias list") from None


def decode_customer(wire: dict[str, Any]) -> Customer:
    """Decode a Gorgias customer with a positive ID."""
    identifier = wire.get("id", 0)
    if type(identifier) is not int or identifier < 1:
        raise ValueError("customer ID is missing")
    return Customer(
        identifier,
        wire.get("email") or "",
        wire.get("name") or "",
        wire.get("external_id") or "",
        wire.get("language") or "",
    )


def decode_ticket(wire: dict[str, Any], expected_id: int = 0) -> Ticket:
    """Decode the common Gorgias ticket wire shape."""
    identifier = wire.get("id", 0)
    if type(identifier) is not int or identifier < 1:
        raise ValueError("ticket ID is missing")
    if expected_id and identifier != expected_id:
        raise ValueError("ticket response is for another ticket")
    status = wire.get("status")
    if not isinstance(status, str) or not status:
        raise ValueError("ticket status is missing")
    try:
        created = parse_time(wire.get("created_datetime", ""))
    except ValueError:
        raise ValueError("ticket created_datetime is not an ISO 8601 timestamp") from None
    updated = optional_time(wire.get("updated_datetime")) or created
    tags = tuple(tag["name"] for tag in wire.get("tags") or ())
    if any(not tag for tag in tags):
        raise ValueError("ticket tag has no name")
    customer = wire.get("customer") or {}
    assignee_user = wire.get("assignee_user") or {}
    assignee_team = wire.get("assignee_team") or {}
    return Ticket(
        id=identifier,
        subject=wire.get("subject") or "",
        status=status,
        priority=wire.get("priority") or "",
        channel=wire.get("channel") or "",
        via=wire.get("via") or "",
        requester_id=customer.get("id", 0),
        requester_email=customer.get("email") or "",
        assignee_user_id=assignee_user.get("id", 0),
        assignee_team_id=assignee_team.get("id", 0),
        tags=tags,
        external_id=wire.get("external_id") or "",
        language=wire.get("language") or "",
        is_spam=wire.get("spam") or False,
        message_count=wire.get("messages_count") or 0,
        created_at=created,
        updated_at=updated,
        last_received_message_at=optional_time(wire.get("last_received_message_datetime")),
        last_message_at=optional_time(wire.get("last_message_datetime")),
        closed_at=optional_time(wire.get("closed_datetime")),
        snoozed_until=optional_time(wire.get("snooze_datetime")),
        trashed_at=optional_time(wire.get("trashed_datetime")),
    )


def decode_message(wire: dict[str, Any], ticket_id: int = 0) -> TicketMessage:
    """Decode one Gorgias message and bound its durable text."""
    identifier = wire.get("id", 0)
    if type(identifier) is not int or identifier < 1:
        raise ValueError("message ID is missing")
    try:
        created = parse_time(wire.get("created_datetime", ""))
    except ValueError:
        raise ValueError("message created_datetime is not an ISO 8601 timestamp") from None
    text = wire.get("body_text") or wire.get("stripped_text") or ""
    encoded = text.encode()
    is_truncated = len(encoded) > 16384
    if is_truncated:
        text = encoded[:16384].decode("utf-8", "ignore")
    channel = wire.get("channel") or ""
    return TicketMessage(
        id=identifier,
        ticket_id=ticket_id or wire.get("ticket_id", 0),
        channel=channel,
        via=wire.get("via") or "",
        is_public=channel != "internal-note" and wire.get("public") is not False,
        is_from_agent=wire.get("from_agent") or False,
        sender_id=(wire.get("sender") or {}).get("id", 0),
        subject=wire.get("subject") or "",
        body=text,
        is_body_truncated=is_truncated,
        external_id=wire.get("external_id") or "",
        created_at=created,
        sent_at=optional_time(wire.get("sent_datetime")),
        failed_at=optional_time(wire.get("failed_datetime")),
    )
