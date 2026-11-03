# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Front HTTP client and provider response decoding."""

import json
import math
import re
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import TYPE_CHECKING, Any
from urllib.parse import parse_qs, urlsplit

import httpx
from dex_connectors.sdk import Call, CredentialProvider, Failure, FailureKind, Receipt
from dex_connectors.sdk.providerhttp import (
    MAX_ERROR_BODY_BYTES,
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
from .models import Conversation, Recipient, Tag, Teammate

if TYPE_CHECKING:
    from .operations import (
        FindContactByEmailOperation,
        GetConversationOperation,
        ReplyToConversationOperation,
        SearchConversationsOperation,
        UpdateConversationOperation,
    )

_ID = {
    "conversation": re.compile(r"cnv_[A-Za-z0-9]{1,40}\Z"),
    "teammate": re.compile(r"tea_[A-Za-z0-9]{1,40}\Z"),
    "tag": re.compile(r"tag_[A-Za-z0-9]{1,40}\Z"),
    "inbox": re.compile(r"inb_[A-Za-z0-9]{1,40}\Z"),
    "status": re.compile(r"sts_[A-Za-z0-9]{1,40}\Z"),
    "contact": re.compile(r"crd_[A-Za-z0-9]{1,40}\Z"),
    "message": re.compile(r"msg_[A-Za-z0-9]{1,40}\Z"),
    "comment": re.compile(r"com_[A-Za-z0-9]{1,40}\Z"),
}
_PAGE_TOKEN = re.compile(r"[A-Za-z0-9._~-]{1,1000}\Z")
_COMPANY_HOST = re.compile(r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.api\.frontapp\.com\Z")


def failure(kind: FailureKind, operation: str, message: str) -> Failure:
    """Construct a secret-safe Front failure."""
    return Failure(kind, "front", operation, message)


def receipt(call: Call, object_id: str = "", now: datetime | None = None) -> Receipt:
    """Record the provider identity and observation time for one call."""
    return Receipt(call.id, "front", now or datetime.now(UTC), call.idempotency_key, object_id)


@dataclass(frozen=True, slots=True)
class Exchange:
    """One classified provider exchange."""

    outcome: str
    status: int = 0
    headers: httpx.Headers | None = None
    body: bytes = b""
    error: Failure | None = None
    retry_after: float = 0

    @property
    def retryable(self) -> bool:
        """Report whether a Query or repeatable Mutation can retry."""
        return self.outcome in ("notSent", "rateLimited", "unconfirmed")


class Client:
    """Resolve current Front credentials for each bounded API request."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        *,
        api_base_url: str = "https://api2.frontapp.com",
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Front config and credential source are required")
        if config.max_response_bytes < 1:
            raise ValueError("Front maxResponseBytes must be positive")
        self.base_url = validate_base_url(api_base_url)
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(25)

    def search_conversations(self) -> "SearchConversationsOperation":
        """Return the conversation search Query."""
        from .operations import SearchConversationsOperation

        return SearchConversationsOperation(self)

    def get_conversation(self) -> "GetConversationOperation":
        """Return the conversation read Query."""
        from .operations import GetConversationOperation

        return GetConversationOperation(self)

    def reply_to_conversation(self) -> "ReplyToConversationOperation":
        """Return the fenced conversation reply Mutation."""
        from .operations import ReplyToConversationOperation

        return ReplyToConversationOperation(self)

    def update_conversation(self) -> "UpdateConversationOperation":
        """Return the convergent conversation update Mutation."""
        from .operations import UpdateConversationOperation

        return UpdateConversationOperation(self)

    def find_contact_by_email(self) -> "FindContactByEmailOperation":
        """Return the contact lookup Query."""
        from .operations import FindContactByEmailOperation

        return FindContactByEmailOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP transport after Steps stop."""
        await self.http_client.aclose()

    async def resolve_credentials(
        self, call: Call, operation: str
    ) -> tuple[Credentials | None, Failure | None, Exception | None]:
        """Resolve and validate the current token without storing it in a result."""
        try:
            credentials = await self.credentials.resolve(call)
        except Exception as error:
            return (
                None,
                failure(
                    FailureKind.AUTHENTICATION,
                    operation,
                    "the Front API token is unavailable, blank, or contains characters a header cannot carry",
                ),
                error,
            )
        if credentials is None or not is_header_safe_credential(credentials.api_token.reveal()):
            return (
                None,
                failure(
                    FailureKind.AUTHENTICATION,
                    operation,
                    "the Front API token is unavailable, blank, or contains characters a header cannot carry",
                ),
                None,
            )
        return credentials, None, None

    async def exchange(
        self,
        credentials: Credentials,
        operation: str,
        method: str,
        path: str,
        *,
        parameters: dict[str, str] | None = None,
        payload: Any = None,
    ) -> Exchange:
        """Send one request with redirects disabled and classify status before reading errors."""
        headers = {"Authorization": "Bearer " + credentials.api_token.reveal(), "Accept": "application/json"}
        if payload is not None:
            headers["Content-Type"] = "application/json"
        try:
            request = self.http_client.build_request(
                method,
                self.base_url + path,
                params=parameters,
                content=json.dumps(payload, separators=(",", ":")).encode() if payload is not None else None,
                headers=headers,
            )
        except (httpx.HTTPError, ValueError, TypeError):
            return Exchange(
                "defect",
                error=failure(FailureKind.LOCAL_DEFECT, operation, "Front request could not be built"),
            )
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError as error:
            if error.provably_not_sent:
                return Exchange(
                    "notSent",
                    error=failure(
                        FailureKind.TRANSPORT, operation, "Front could not be reached; no request was sent"
                    ),
                )
            return Exchange(
                "unconfirmed",
                error=failure(
                    FailureKind.TRANSPORT,
                    operation,
                    "the Front request may have been sent, but no answer arrived",
                ),
            )
        status = response.status_code
        response_headers = response.headers.copy()
        is_success = 200 <= status < 300
        try:
            body = await read_bounded_body(
                response, self.max_response_bytes if is_success else MAX_ERROR_BODY_BYTES
            )
        except BodyTooLargeError:
            if is_success:
                return Exchange(
                    "invalid",
                    status,
                    response_headers,
                    error=failure(
                        FailureKind.RESPONSE_TOO_LARGE,
                        operation,
                        "the Front response exceeds the configured maxResponseBytes",
                    ),
                )
            body = b""
        except ProviderTransportError:
            if is_success:
                return Exchange(
                    "unconfirmed",
                    status,
                    response_headers,
                    error=failure(FailureKind.TRANSPORT, operation, "the Front response could not be read"),
                )
            body = b""
        if is_success:
            if credentials.api_token.reveal().encode() in body:
                return Exchange(
                    "invalid",
                    status,
                    response_headers,
                    error=failure(
                        FailureKind.PROTOCOL, operation, "the Front response reflected the API token"
                    ),
                )
            return Exchange("succeeded", status, response_headers, body)
        if status == 429:
            delay = parse_retry_after(response_headers.get("Retry-After", ""), datetime.now(UTC))
            if not delay:
                try:
                    reset = int(response_headers.get("X-Ratelimit-Reset", ""))
                    if reset > 0:
                        delay = min(max(reset - datetime.now(UTC).timestamp(), 1), 60)
                except ValueError:
                    pass
            return Exchange(
                "rateLimited",
                status,
                response_headers,
                error=failure(FailureKind.RATE_LIMIT, operation, "Front rate limited the request (HTTP 429)"),
                retry_after=delay,
            )
        if status == 408 or status >= 500:
            return Exchange(
                "unconfirmed",
                status,
                response_headers,
                error=failure(
                    FailureKind.AVAILABILITY,
                    operation,
                    f"Front could not complete the request (HTTP {status})",
                ),
            )
        if status == 301:
            return Exchange(
                "merged",
                status,
                response_headers,
                error=failure(
                    FailureKind.NOT_FOUND,
                    operation,
                    "Front reports that the conversation was merged into another (HTTP 301)",
                ),
            )
        if status in (404, 410):
            return Exchange(
                "notFound",
                status,
                response_headers,
                error=failure(
                    FailureKind.NOT_FOUND, operation, f"Front found no such resource (HTTP {status})"
                ),
            )
        if 300 <= status < 400:
            return Exchange(
                "rejected",
                status,
                response_headers,
                error=failure(
                    FailureKind.PROTOCOL,
                    operation,
                    f"Front redirected the request (HTTP {status}), which the connector never follows",
                ),
            )
        if status == 401:
            return Exchange(
                "rejected",
                status,
                response_headers,
                error=failure(
                    FailureKind.AUTHENTICATION,
                    operation,
                    "Front rejected the API token (HTTP 401); check that the token still exists",
                ),
            )
        if status == 403:
            return Exchange(
                "rejected",
                status,
                response_headers,
                error=failure(
                    FailureKind.AUTHORIZATION,
                    operation,
                    (
                        "Front denied access (HTTP 403); check the token's namespaces "
                        "and its read, write, or send permission for this resource"
                    ),
                ),
            )
        kind = (
            FailureKind.VALIDATION
            if status in (400, 413, 422)
            else (FailureKind.CONFLICT if status == 409 else FailureKind.PROVIDER_REJECTION)
        )
        return Exchange(
            "rejected",
            status,
            response_headers,
            error=failure(kind, operation, f"Front rejected the request (HTTP {status})"),
        )

    def next_page_token(self, next_link: str | None, expected_path_prefix: str) -> str:
        """Extract only a Front-hosted page token for the expected resource."""
        if not next_link:
            return ""
        parsed = urlsplit(next_link)
        base = urlsplit(self.base_url)
        if parsed.username or parsed.password or parsed.fragment:
            raise ValueError("the next-page link is not a plain URL")
        is_front_host = parsed.scheme == "https" and (
            parsed.netloc == "api2.frontapp.com" or _COMPANY_HOST.fullmatch(parsed.netloc)
        )
        if not is_front_host and (parsed.scheme != base.scheme or parsed.netloc != base.netloc):
            raise ValueError("the next-page link points outside Front's API hosts")
        if not parsed.path.startswith(expected_path_prefix):
            raise ValueError("the next-page link names another resource")
        token = parse_qs(parsed.query).get("page_token", [""])[0]
        if not _PAGE_TOKEN.fullmatch(token):
            raise ValueError("the next-page link has no valid page_token")
        return token


def resource_id(value: str, kind: str, field: str, example: str) -> None:
    """Require Front's stable ID shape."""
    if not _ID[kind].fullmatch(value):
        raise ValueError(f"{field} must be a Front ID such as {example}")


def timestamp(value: Any) -> datetime | None:
    """Decode Front Unix seconds with rounded millisecond fractions."""
    if value is None:
        return None
    if (
        isinstance(value, bool)
        or not isinstance(value, (int, float))
        or value < 0
        or value > 1e11
        or math.isnan(value)
    ):
        raise ValueError("a Front timestamp is not Unix seconds")
    if value == 0:
        return None
    return datetime.fromtimestamp(math.floor(value * 1000 + 0.5) / 1000, UTC)


def _teammate(wire: dict[str, Any] | None) -> Teammate | None:
    if wire is None or (not wire.get("id") and not wire.get("email")):
        return None
    resource_id(wire.get("id", ""), "teammate", "teammate", "tea_2thf")
    return Teammate(wire["id"], wire.get("email", ""), wire.get("first_name", ""), wire.get("last_name", ""))


def _recipient(wire: dict[str, Any]) -> Recipient:
    related = wire.get("_links", {}).get("related", {}).get("contact", "")
    contact_id = urlsplit(related.strip()).path.rsplit("/", 1)[-1] if isinstance(related, str) else ""
    if not _ID["contact"].fullmatch(contact_id):
        contact_id = ""
    return Recipient(wire.get("handle", ""), wire.get("role", ""), wire.get("name", ""), contact_id)


def _tag(wire: dict[str, Any]) -> Tag:
    if not _ID["tag"].fullmatch(wire.get("id", "")):
        raise ValueError("a conversation tag lacks a valid ID")
    return Tag(wire["id"], wire.get("name", ""))


def decode_conversation(wire: dict[str, Any]) -> Conversation:
    """Convert one Front conversation into the public typed model."""
    resource_id(wire.get("id", ""), "conversation", "conversation", "cnv_55c8c149")
    status = wire.get("status", "")
    if not status:
        raise ValueError("a conversation lacks a valid ID or status")
    status_id = wire.get("status_id", "")
    if status_id:
        resource_id(status_id, "status", "status_id", "sts_5x")
    created = timestamp(wire.get("created_at")) or datetime.min.replace(tzinfo=UTC)
    return Conversation(
        id=wire["id"],
        type=wire.get("type", ""),
        subject=wire.get("subject", ""),
        status=status,
        status_id=status_id,
        status_category=wire.get("status_category") or None,
        ticket_ids=tuple(wire.get("ticket_ids") or ()),
        assignee=_teammate(wire.get("assignee")),
        recipient=_recipient(wire["recipient"])
        if wire.get("recipient") and wire["recipient"].get("handle")
        else None,
        tags=tuple(_tag(tag) for tag in wire.get("tags") or ()),
        is_private=wire.get("is_private", False),
        is_snoozed=status == "archived" and bool(wire.get("scheduled_reminders")),
        created_at=created,
        updated_at=timestamp(wire.get("updated_at")),
        waiting_since=timestamp(wire.get("waiting_since")),
        due_at=timestamp(wire.get("due_at")),
    )


def decode_json(body: bytes) -> dict[str, Any]:
    """Require a JSON object response."""
    value = json.loads(body)
    if not isinstance(value, dict):
        raise ValueError("Front returned a non-object JSON response")
    return value


def decode_page(body: bytes, name: str) -> dict[str, Any]:
    """Require a Front _results list."""
    value = decode_json(body)
    if not isinstance(value.get("_results"), list):
        raise ValueError(name)
    return value


def truncate_utf8(value: str, limit: int = 65536) -> tuple[str, bool]:
    """Cut a body on a UTF-8 boundary, matching Go's byte limit."""
    encoded = value.encode()
    if len(encoded) <= limit:
        return value, False
    return encoded[:limit].decode("utf-8", errors="ignore"), True


def merged_conversation_id(location: str) -> str:
    """Read a valid conversation ID from a redirect Location."""
    segment = urlsplit(location.strip()).path.rsplit("/", 1)[-1]
    return segment if _ID["conversation"].fullmatch(segment) else ""
