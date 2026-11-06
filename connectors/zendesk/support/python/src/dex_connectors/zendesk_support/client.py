# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Zendesk Support transport, credentials, and strict response decoding."""

import base64
import ipaddress
import json
import re
from collections.abc import Callable
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from typing import TYPE_CHECKING, Any
from urllib.parse import quote_plus

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
from .models import Ticket, TicketComment, TicketUser

if TYPE_CHECKING:
    from .operations import (
        CreateTicketOperation,
        GetTicketOperation,
        SearchTicketsOperation,
        UpdateTicketOperation,
    )

_SUBDOMAIN = re.compile(r"^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$")
_ERROR_FIELD = re.compile(r"^[a-z][a-z0-9_]{0,63}$")
_ERROR_TOKEN = re.compile(r"^[A-Za-z0-9_.:-]{1,64}$")
_RFC3339 = re.compile(
    r"^(\d{4})-(\d\d)-(\d\d)T(\d\d):(\d\d):(\d\d)(?:[.,](\d+))?(Z|([+-])(\d\d):(\d\d))$", re.ASCII
)


@dataclass(frozen=True, slots=True)
class Exchange:
    """One classified provider exchange."""

    outcome: str
    failure: Failure | None = None
    body: bytes = b""
    status: int = 0
    headers: httpx.Headers | None = None
    retry_after: float = 0


def failure(kind: FailureKind, operation: str, message: str) -> Failure:
    """Build a provider-scoped failure without credential data."""
    return Failure(kind, "zendesk", operation, message)


def bare_email(value: str) -> bool:
    """Accept the same bare address shape as Go mail.ParseAddress."""
    if not isinstance(value, str) or any(char in value for char in " \"'()<>,;"):
        return False
    local, separator, domain = value.rpartition("@")
    if not separator or not local or not domain:
        return False
    atom_characters = frozenset("!#$%&'*+-/=?^_`{|}~")

    def is_dot_atom(text: str) -> bool:
        return all(
            part
            and all(
                char.isascii() and (char.isalnum() or char in atom_characters) or ord(char) >= 128
                for char in part
            )
            for part in text.split(".")
        )

    if not is_dot_atom(local):
        return False
    if domain.startswith("[") and domain.endswith("]"):
        try:
            ipaddress.ip_address(domain[1:-1])
        except ValueError:
            return False
        return True
    return is_dot_atom(domain)


def parse_time(value: str) -> datetime:
    """Parse an RFC 3339 instant and return its UTC value."""
    if not isinstance(value, str) or (match := _RFC3339.fullmatch(value)) is None:
        raise ValueError("timestamp is not RFC 3339")
    year, month, day, hour, minute, second, fraction, _, sign, offset_hour, offset_minute = match.groups()
    if offset_hour is not None and (int(offset_hour) > 24 or int(offset_minute) > 60):
        raise ValueError("timestamp offset minute is invalid")
    try:
        parsed = datetime(
            int(year),
            int(month),
            int(day),
            int(hour),
            int(minute),
            int(second),
            int((fraction or "")[:6].ljust(6, "0")),
            tzinfo=UTC,
        )
    except (ValueError, OverflowError) as error:
        raise ValueError("timestamp is not RFC 3339") from error
    if offset_hour is not None:
        offset = timedelta(hours=int(offset_hour), minutes=int(offset_minute))
        try:
            parsed += -offset if sign == "+" else offset
        except OverflowError as error:
            raise ValueError("timestamp is outside Python datetime range") from error
    return parsed


def _string_field(value: dict[str, Any], key: str) -> str:
    field = value.get(key)
    if field is None:
        return ""
    if not isinstance(field, str):
        raise ValueError(f"{key} is not a string")
    return field


def _int_field(value: dict[str, Any], key: str) -> int:
    field = value.get(key)
    if field is None:
        return 0
    if type(field) is not int:
        raise ValueError(f"{key} is not an integer")
    return field


def decode_ticket(value: Any, agent_ticket_url: str) -> tuple[Ticket, str]:
    """Decode a Zendesk ticket and retain its exact safe-update timestamp."""
    if not isinstance(value, dict):
        raise ValueError("ticket is not an object")
    ticket_id = _int_field(value, "id")
    status = _string_field(value, "status")
    if ticket_id < 1:
        raise ValueError("ticket ID is missing")
    if not status:
        raise ValueError("ticket status is missing")
    created_stamp = _string_field(value, "created_at")
    updated_stamp = _string_field(value, "updated_at")
    try:
        created_at = parse_time(created_stamp)
        updated_at = parse_time(updated_stamp)
    except ValueError as error:
        raise ValueError("ticket timestamp is not RFC 3339") from error
    description = _string_field(value, "description")
    description, truncated = truncate_text(description)
    tags = value.get("tags")
    if tags is None:
        tags = []
    if not isinstance(tags, list) or not all(tag is None or isinstance(tag, str) for tag in tags):
        raise ValueError("ticket tags are invalid")
    via = value.get("via") or {}
    if not isinstance(via, dict):
        raise ValueError("ticket via is invalid")
    channel = via.get("channel")
    if isinstance(channel, str):
        pass
    elif channel is None:
        channel = ""
    else:
        channel = json.dumps(channel, separators=(",", ":"), ensure_ascii=False).strip()
    due_stamp = _string_field(value, "due_at")
    due_at = parse_time(due_stamp) if due_stamp else None
    ticket = Ticket(
        id=ticket_id,
        agent_url=agent_ticket_url + str(ticket_id),
        subject=_string_field(value, "subject"),
        description=description,
        is_description_truncated=truncated,
        status=status,
        custom_status_id=_int_field(value, "custom_status_id"),
        priority=_string_field(value, "priority") or None,
        type=_string_field(value, "type") or None,
        requester_id=_int_field(value, "requester_id"),
        submitter_id=_int_field(value, "submitter_id"),
        assignee_id=_int_field(value, "assignee_id"),
        group_id=_int_field(value, "group_id"),
        organization_id=_int_field(value, "organization_id"),
        brand_id=_int_field(value, "brand_id"),
        tags=tuple(tag or "" for tag in tags),
        external_id=_string_field(value, "external_id"),
        channel=channel,
        created_at=created_at,
        updated_at=updated_at,
        due_at=due_at,
    )
    return ticket, updated_stamp


def decode_ticket_envelope(body: bytes, agent_ticket_url: str) -> tuple[Ticket, str, dict[str, Any]]:
    """Decode the required ticket member of one response."""
    value = json.loads(body)
    if not isinstance(value, dict) or not isinstance(value.get("ticket"), dict):
        raise ValueError("ticket response has no ticket")
    ticket, updated_stamp = decode_ticket(value["ticket"], agent_ticket_url)
    return ticket, updated_stamp, value


def decode_comment(value: Any) -> TicketComment:
    """Decode one latest comment with bounded text."""
    if not isinstance(value, dict) or _int_field(value, "id") < 1:
        raise ValueError("comment ID is missing")
    created_at = parse_time(_string_field(value, "created_at"))
    plain_body = _string_field(value, "plain_body")
    body, truncated = truncate_text(plain_body or _string_field(value, "body"))
    public = value.get("public")
    if public is not None and not isinstance(public, bool):
        raise ValueError("comment public is invalid")
    return TicketComment(
        id=value["id"],
        author_id=_int_field(value, "author_id"),
        is_public=bool(public),
        body=body,
        is_body_truncated=truncated,
        created_at=created_at,
    )


def decode_user(value: Any) -> TicketUser:
    """Decode one sideloaded user and reject wrong-typed provider fields."""
    if not isinstance(value, dict):
        raise ValueError("user is invalid")
    return TicketUser(
        id=_int_field(value, "id"),
        name=_string_field(value, "name"),
        email=_string_field(value, "email"),
        role=_string_field(value, "role"),
    )


def truncate_text(value: str) -> tuple[str, bool]:
    """Cut text at the Go connector's UTF-8 byte boundary."""
    encoded = value.encode("utf-8")
    if len(encoded) <= 16384:
        return value, False
    return encoded[:16384].decode("utf-8", errors="ignore"), True


class Client:
    """Bind Zendesk operations to one credential source and HTTP transport."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        api_base_url: str = "",
        now: Callable[[], datetime] | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Zendesk configuration and credential source are required")
        if not _SUBDOMAIN.fullmatch(config.subdomain):
            raise ValueError("Zendesk subdomain must be lowercase letters, digits, and hyphens")
        if config.max_response_bytes < 1:
            raise ValueError("Zendesk response limit must be positive")
        self.base_url = validate_base_url(api_base_url or f"https://{config.subdomain}.zendesk.com/api/v2")
        self.agent_ticket_url = f"https://{config.subdomain}.zendesk.com/agent/tickets/"
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(9)
        self._owns_http_client = http_client is None
        self.now = now or (lambda: datetime.now(UTC))

    async def aclose(self) -> None:
        """Close the transport when this client constructed it."""
        if self._owns_http_client:
            await self.http_client.aclose()

    def search_tickets(self) -> "SearchTicketsOperation":
        """Return the search Query."""
        from .operations import SearchTicketsOperation

        return SearchTicketsOperation(self)

    def get_ticket(self) -> "GetTicketOperation":
        """Return the ticket read Query."""
        from .operations import GetTicketOperation

        return GetTicketOperation(self)

    def create_ticket(self) -> "CreateTicketOperation":
        """Return the keyed create Mutation."""
        from .operations import CreateTicketOperation

        return CreateTicketOperation(self)

    def update_ticket(self) -> "UpdateTicketOperation":
        """Return the safe update Mutation."""
        from .operations import UpdateTicketOperation

        return UpdateTicketOperation(self)

    async def resolve_credentials(
        self, call: Call, operation: str
    ) -> tuple[Credentials | None, Failure | None]:
        """Resolve the latest API token for this call."""
        try:
            credentials = await self.credentials.resolve(call)
            token = credentials.api_token.reveal()
            if not bare_email(credentials.email) or not token or not is_header_safe_credential(token):
                raise ValueError("invalid credential")
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
        idempotency_key: str = "",
    ) -> Exchange:
        """Send and classify one Zendesk request without choosing its branch."""
        basic = base64.b64encode(
            f"{credentials.email}/token:{credentials.api_token.reveal()}".encode()
        ).decode()
        headers = {"Accept": "application/json", "Authorization": "Basic " + basic}
        if payload is not None:
            headers["Content-Type"] = "application/json"
        if idempotency_key:
            headers["Idempotency-Key"] = idempotency_key
        try:
            target = self.base_url + path
            if query:
                target += "?" + "&".join(
                    f"{_query_escape(key)}={_query_escape(value)}" for key, value in sorted(query.items())
                )
            request = self.http_client.build_request(
                method,
                target,
                content=json.dumps(payload, separators=(",", ":"), ensure_ascii=False)
                if payload is not None
                else None,
                headers=headers,
            )
        except (ValueError, TypeError, httpx.HTTPError):
            return Exchange(
                "defect", failure(FailureKind.LOCAL_DEFECT, operation, "Zendesk request could not be built")
            )
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError:
            return Exchange(
                "retry",
                failure(FailureKind.TRANSPORT, operation, "Zendesk request failed before a response arrived"),
            )
        try:
            body = await read_bounded_body(response, self.max_response_bytes)
        except BodyTooLargeError:
            if 200 <= response.status_code < 300:
                return Exchange(
                    "invalid",
                    failure(
                        FailureKind.RESPONSE_TOO_LARGE,
                        operation,
                        "Zendesk response exceeds the configured maxResponseBytes limit",
                    ),
                    status=response.status_code,
                    headers=response.headers,
                )
            body = b""
        except ProviderTransportError:
            return Exchange(
                "retry",
                failure(FailureKind.TRANSPORT, operation, "Zendesk response could not be read"),
                status=response.status_code,
                headers=response.headers,
            )
        status = response.status_code
        if 200 <= status < 300:
            if credentials.api_token.reveal().encode() in body:
                return Exchange(
                    "invalid",
                    failure(
                        FailureKind.PROTOCOL,
                        operation,
                        "Zendesk response reflected the connection credential",
                    ),
                    status=status,
                    headers=response.headers,
                )
            return Exchange("success", body=body, status=status, headers=response.headers)
        summary = _describe_error(body, credentials.api_token.reveal())
        suffix = f" [{summary}]" if summary else ""
        retry_after = parse_retry_after(response.headers.get("Retry-After", ""), self.now())
        if status == 429:
            return Exchange(
                "retry",
                failure(
                    FailureKind.RATE_LIMIT, operation, "Zendesk rate limited the request (HTTP 429)" + suffix
                ),
                status=status,
                headers=response.headers,
                retry_after=retry_after,
            )
        if status in (408, 409) or status >= 500:
            kind = FailureKind.CONFLICT if status == 409 else FailureKind.AVAILABILITY
            return Exchange(
                "retry",
                failure(
                    kind, operation, f"Zendesk could not complete the request yet (HTTP {status})" + suffix
                ),
                status=status,
                headers=response.headers,
                retry_after=retry_after,
            )
        if status in (404, 410):
            return Exchange(
                "notFound",
                failure(
                    FailureKind.NOT_FOUND,
                    operation,
                    f"Zendesk found no such resource (HTTP {status})" + suffix,
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
                    f"Zendesk redirected the request (HTTP {status}); "
                    "check that the subdomain is the zendesk.com account subdomain",
                ),
                status=status,
                headers=response.headers,
            )
        code = summary.partition(";")[0]
        kind = (
            FailureKind.CONFLICT
            if code == "IdempotentRequestError"
            else FailureKind.AUTHENTICATION
            if status == 401
            else FailureKind.AUTHORIZATION
            if status == 403
            else FailureKind.VALIDATION
            if status in (400, 422)
            else FailureKind.PROVIDER_REJECTION
        )
        return Exchange(
            "rejected",
            failure(kind, operation, f"Zendesk rejected the request (HTTP {status})" + suffix),
            status=status,
            headers=response.headers,
        )

    def receipt(self, call: Call, exchange: Exchange, ticket_id: int = 0) -> Receipt:
        """Record the provider request and ticket identity without response content."""
        return Receipt(
            call_id=call.id,
            idempotency_key=call.idempotency_key,
            provider="zendesk",
            provider_request_id=exchange.headers.get("X-Zendesk-Request-Id", "")
            if exchange.headers is not None
            else "",
            provider_object_id=str(ticket_id) if ticket_id > 0 else "",
            observed_at=self.now(),
        )


def _describe_error(body: bytes, secret: str) -> str:
    try:
        document = json.loads(body, parse_int=str, parse_float=str)
    except (ValueError, UnicodeDecodeError):
        return ""
    if not isinstance(document, dict):
        return ""
    parts = []
    code = document.get("error")
    if (
        isinstance(code, (str, int, float))
        and not isinstance(code, bool)
        and _ERROR_TOKEN.fullmatch(str(code))
        and secret not in str(code)
    ):
        parts.append(str(code))
    details = document.get("details")
    if isinstance(details, dict):
        fields = sorted(field for field in details if _ERROR_FIELD.fullmatch(field) and secret not in field)[
            :5
        ]
        tokens = []
        for field in fields:
            entry = details[field]
            token = ""
            if isinstance(entry, list) and entry and isinstance(entry[0], dict):
                for key in ("type", "error"):
                    candidate = entry[0].get(key)
                    if (
                        isinstance(candidate, (str, int, float))
                        and not isinstance(candidate, bool)
                        and _ERROR_TOKEN.fullmatch(str(candidate))
                    ):
                        token = str(candidate)
                        break
            tokens.append(field + ("=" + token if token and secret not in token else ""))
        if tokens:
            parts.append("details: " + ", ".join(tokens))
    return "; ".join(parts)


def _query_escape(value: str) -> str:
    """Encode a Zendesk query parameter like Go url.Values.Encode."""
    return quote_plus(value, safe="")
