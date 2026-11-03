# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Hiver API transport, response decoding, and connection-wide request pacing."""

import asyncio
import json
import re
from dataclasses import dataclass, field
from datetime import UTC, datetime, timedelta
from typing import TYPE_CHECKING, Any
from urllib.parse import quote, urlencode, urlsplit

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
from .models import (
    Conversation,
    ConversationAssignee,
    ConversationDetails,
    ConversationMessage,
    Inbox,
    Note,
    SharedDraft,
    Tag,
)

if TYPE_CHECKING:
    from .operations import (
        AddNoteOperation,
        CreateSharedDraftOperation,
        GetConversationOperation,
        ListConversationsOperation,
        ListInboxesOperation,
        UpdateConversationOperation,
    )

_HIVER_ID = re.compile(r"^[A-Za-z0-9_-]{1,64}$")
_PAGE_TOKEN = re.compile(r"^[A-Za-z0-9+/=_.-]+$")
_STATUS = re.compile(r"^[A-Za-z0-9 _-]{1,64}$")
_ASSIGNEE_TYPE = re.compile(r"^[a-z_]{1,32}$")
_GMAIL_ID = re.compile(r"^[A-Za-z0-9]{1,64}$")
_REQUEST_ID = re.compile(r"^[A-Za-z0-9._:-]{1,128}$")


@dataclass(frozen=True, slots=True)
class Exchange:
    """Classified evidence from one Hiver provider request."""

    outcome: str
    status: int = 0
    headers: httpx.Headers = field(default_factory=httpx.Headers)
    body: bytes = b""
    failure: Failure | None = None
    retry_after: float = 0


class Client:
    """Use one bearer credential source and pace all requests of this connection."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        api_base_url: str = "",
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Hiver configuration and credential source are required")
        if isinstance(config.max_response_bytes, bool) or not isinstance(config.max_response_bytes, int):
            raise ValueError("Hiver response limit must be an integer")
        if isinstance(config.request_interval_milliseconds, bool) or not isinstance(
            config.request_interval_milliseconds, int
        ):
            raise ValueError("Hiver request interval must be an integer")
        max_response_bytes = config.max_response_bytes or Config().max_response_bytes
        request_interval_milliseconds = (
            config.request_interval_milliseconds or Config().request_interval_milliseconds
        )
        if max_response_bytes < 1:
            raise ValueError("Hiver response limit must be positive")
        if not 1 <= request_interval_milliseconds <= 60000:
            raise ValueError("Hiver request interval must be between 1 and 60000 milliseconds")
        self.api_base_url = validate_base_url(api_base_url or "https://api2.hiverhq.com/v1")
        self.max_response_bytes = max_response_bytes
        self.request_interval = request_interval_milliseconds / 1000
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(12)
        self._owns_http_client = http_client is None
        self._pacer_lock = asyncio.Lock()
        self._next_request_at = 0.0
        self._rate_limit_penalty = 0.0

    def list_inboxes(self) -> "ListInboxesOperation":
        """Bind the inbox page Query."""
        from .operations import ListInboxesOperation

        return ListInboxesOperation(self)

    def list_conversations(self) -> "ListConversationsOperation":
        """Bind the conversation page Query."""
        from .operations import ListConversationsOperation

        return ListConversationsOperation(self)

    def get_conversation(self) -> "GetConversationOperation":
        """Bind the conversation detail Query."""
        from .operations import GetConversationOperation

        return GetConversationOperation(self)

    def update_conversation(self) -> "UpdateConversationOperation":
        """Bind the repeatable conversation Mutation."""
        from .operations import UpdateConversationOperation

        return UpdateConversationOperation(self)

    def add_note(self) -> "AddNoteOperation":
        """Bind the fenced internal note Mutation."""
        from .operations import AddNoteOperation

        return AddNoteOperation(self)

    def create_shared_draft(self) -> "CreateSharedDraftOperation":
        """Bind the fenced shared draft Mutation."""
        from .operations import CreateSharedDraftOperation

        return CreateSharedDraftOperation(self)

    async def aclose(self) -> None:
        """Close the provider HTTP client only when this client created it."""
        if self._owns_http_client:
            await self.http_client.aclose()

    async def token(self, call: Call) -> str | None:
        """Resolve a current, header-safe key for one attempt."""
        try:
            credentials = await self.credentials.resolve(call)
            if not isinstance(credentials, Credentials):
                return None
            token = credentials.api_key.reveal()
            return token if is_header_safe_credential(token) else None
        except Exception:
            return None

    async def _wait_for_slot(self) -> None:
        async with self._pacer_lock:
            loop = asyncio.get_running_loop()
            now = loop.time()
            if self._next_request_at > now:
                await asyncio.sleep(self._next_request_at - now)
            self._next_request_at = loop.time() + self.request_interval

    async def exchange(
        self,
        call: Call,
        token: str,
        operation: str,
        method: str,
        path: str,
        query: dict[str, str] | None = None,
        payload: object | None = None,
        fields: list[tuple[str, str]] | None = None,
        fenced: bool = False,
    ) -> Exchange:
        """Pace and send one request, classifying status before its optional body."""
        try:
            await self._wait_for_slot()
        except asyncio.CancelledError:
            return Exchange(
                "notSent",
                failure=hiver_failure(
                    FailureKind.AVAILABILITY,
                    operation,
                    "the wait for the connection's next Hiver request slot ended; no request was sent",
                ),
            )
        return await self._send(call, token, operation, method, path, query, payload, fields, fenced)

    async def _send(
        self,
        call: Call,
        token: str,
        operation: str,
        method: str,
        path: str,
        query: dict[str, str] | None,
        payload: object | None,
        fields: list[tuple[str, str]] | None,
        fenced: bool,
    ) -> Exchange:
        target = self.api_base_url + path
        if query:
            target += "?" + urlencode(sorted(query.items()))
        headers = {"Authorization": "Bearer " + token, "Accept": "application/json"}
        if fenced:
            headers["X-Dex-Call-ID"] = str(call.id)
        try:
            if fields is not None:
                request = self.http_client.build_request(
                    method, target, headers=headers, files=[(key, (None, value)) for key, value in fields]
                )
            elif payload is not None:
                headers["Content-Type"] = "application/json"
                request = self.http_client.build_request(
                    method,
                    target,
                    headers=headers,
                    content=json.dumps(payload, separators=(",", ":"), ensure_ascii=False).encode(),
                )
            else:
                request = self.http_client.build_request(method, target, headers=headers)
        except (httpx.HTTPError, ValueError, TypeError):
            return Exchange(
                "defect",
                failure=hiver_failure(
                    FailureKind.LOCAL_DEFECT, operation, "Hiver request could not be built"
                ),
            )
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError as error:
            if error.provably_not_sent:
                return Exchange(
                    "notSent",
                    failure=hiver_failure(
                        FailureKind.TRANSPORT, operation, "Hiver could not be reached; no request was sent"
                    ),
                )
            return Exchange(
                "unavailable",
                failure=hiver_failure(
                    FailureKind.TRANSPORT, operation, "Hiver request failed before a response arrived"
                ),
            )
        status = response.status_code
        retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
        if status == 429:
            self._rate_limit_penalty = min(60, max(self.request_interval * 4, self._rate_limit_penalty * 2))
            self._next_request_at = max(
                self._next_request_at,
                asyncio.get_running_loop().time() + max(retry_after, self._rate_limit_penalty),
            )
        else:
            self._rate_limit_penalty = 0
        if status < 200 or status >= 300:
            await response.aclose()
            return classify_status(operation, status, response.headers, retry_after)
        try:
            body = await read_bounded_body(response, self.max_response_bytes)
        except BodyTooLargeError:
            return Exchange(
                "invalid",
                status,
                response.headers,
                failure=hiver_failure(
                    FailureKind.RESPONSE_TOO_LARGE,
                    operation,
                    "Hiver response exceeds the configured maxResponseBytes limit",
                ),
            )
        except ProviderTransportError:
            return Exchange(
                "unavailable",
                status,
                response.headers,
                failure=hiver_failure(FailureKind.TRANSPORT, operation, "Hiver response could not be read"),
            )
        if token.encode() in body:
            return Exchange(
                "invalid",
                status,
                response.headers,
                failure=hiver_failure(
                    FailureKind.PROTOCOL, operation, "Hiver response reflected the connection credential"
                ),
            )
        return Exchange("success", status, response.headers, body)

    def receipt(self, call: Call, exchange: Exchange | None = None, object_id: str = "") -> Receipt:
        """Carry only the safe provider request ID and the operation's object ID."""
        request_id = exchange.headers.get("X-Request-Id", "") if exchange else ""
        if not _REQUEST_ID.fullmatch(request_id):
            request_id = ""
        return Receipt(call.id, "hiver", datetime.now(UTC), call.idempotency_key, object_id, request_id)


def hiver_failure(kind: FailureKind, operation: str, message: str) -> Failure:
    """Create a secret-free provider failure."""
    return Failure(kind, "hiver", operation, message)


def classify_status(operation: str, status: int, headers: httpx.Headers, retry_after: float) -> Exchange:
    """Classify a Hiver status without reading provider error text."""
    if status == 429:
        return Exchange(
            "rateLimited",
            status,
            headers,
            failure=hiver_failure(
                FailureKind.RATE_LIMIT,
                operation,
                (
                    "Hiver rate limited the request (HTTP 429): one request per second "
                    "or the daily request limit was exceeded"
                ),
            ),
            retry_after=retry_after,
        )
    if status == 408 or status >= 500:
        return Exchange(
            "unavailable",
            status,
            headers,
            failure=hiver_failure(
                FailureKind.AVAILABILITY, operation, f"Hiver could not complete the request (HTTP {status})"
            ),
            retry_after=retry_after,
        )
    if status == 404:
        return Exchange(
            "notFound",
            status,
            headers,
            failure=hiver_failure(
                FailureKind.NOT_FOUND, operation, "Hiver found no such resource (HTTP 404)"
            ),
        )
    if 300 <= status < 400:
        return Exchange(
            "rejected",
            status,
            headers,
            failure=hiver_failure(
                FailureKind.PROTOCOL,
                operation,
                f"Hiver redirected the request (HTTP {status}), which the connector never follows",
            ),
        )
    kind = {
        400: FailureKind.VALIDATION,
        401: FailureKind.AUTHENTICATION,
        403: FailureKind.AUTHORIZATION,
        409: FailureKind.CONFLICT,
        422: FailureKind.VALIDATION,
    }.get(status, FailureKind.PROVIDER_REJECTION)
    return Exchange(
        "rejected",
        status,
        headers,
        failure=hiver_failure(kind, operation, f"Hiver rejected the request (HTTP {status})"),
    )


def path_escape(value: str) -> str:
    """Use Go url.PathEscape's path segment safe set."""
    return quote(value, safe="-._~$&+=:@")


def validate_id(field: str, value: str) -> None:
    """Require one Hiver ID."""
    if not isinstance(value, str) or not _HIVER_ID.fullmatch(value):
        raise ValueError(f"{field} must be a Hiver ID of 1 to 64 letters, digits, hyphens, or underscores")


def validate_page(token: str, size: int) -> None:
    """Validate one list page selector."""
    if size != 0 and not 10 <= size <= 100:
        raise ValueError("pageSize must be zero or from 10 to 100")
    if token and (len(token.encode()) > 4096 or not _PAGE_TOKEN.fullmatch(token)):
        raise ValueError("pageToken must be a next-page token exactly as Hiver returned it")


def page_query(token: str, size: int) -> dict[str, str]:
    """Build Hiver's limit and optional next_page query."""
    query = {"limit": str(size or 50)}
    if token:
        query["next_page"] = token
    return query


def wire_id(value: Any) -> str:
    """Decode Hiver's string or unsigned JSON integer ID."""
    if value is None:
        return ""
    if isinstance(value, str):
        return value
    if type(value) is int and value >= 0:
        return str(value)
    raise ValueError("ID is neither a string nor an integer")


def single_data(body: bytes) -> dict[str, Any]:
    """Decode one data object or one-element data array."""
    try:
        envelope = json.loads(body)
        data = envelope["data"]
        if isinstance(data, list):
            if len(data) != 1:
                raise ValueError("data array does not hold exactly one object")
            data = data[0]
        if not isinstance(data, dict):
            raise ValueError("data is neither an object nor an array")
        return data
    except (ValueError, TypeError, KeyError) as error:
        raise ValueError("response has no data object") from error


def list_page(body: bytes) -> tuple[list[dict[str, Any]], str]:
    """Decode a Hiver results page and validated next-page token."""
    try:
        envelope = json.loads(body)
        page = envelope["data"]
        results = page["results"]
        pagination = page.get("pagination") or {}
        token = pagination.get("next_page")
        if token is None:
            token = ""
        if not isinstance(results, list):
            raise ValueError("response is not a Hiver list page")
        if not isinstance(token, str) or (
            token and (len(token.encode()) > 4096 or not _PAGE_TOKEN.fullmatch(token))
        ):
            raise ValueError("next_page is not a page token")
        return results, token
    except (ValueError, TypeError, KeyError, AttributeError) as error:
        raise ValueError("response is not a Hiver list page") from error


def decode_inbox(raw: dict[str, Any]) -> Inbox:
    """Decode one inbox while preserving Hiver's optional fields."""
    identifier = wire_id(raw.get("id"))
    validate_id("inbox id", identifier)
    return Inbox(
        identifier,
        string_field(raw, "display_name"),
        string_field(raw, "email").strip(),
        string_field(raw, "channel_type"),
        bool_field(raw, "is_authorised"),
    )


def decode_conversation(raw: dict[str, Any], inbox_id: str) -> ConversationDetails:
    """Decode one conversation and its message references."""
    identifier = wire_id(raw.get("id"))
    validate_id("conversation id", identifier)
    status = raw.get("status")
    if not isinstance(status, str) or not _STATUS.fullmatch(status):
        raise ValueError(f"conversation {identifier} status is missing or invalid")
    assignee = None
    raw_assignee = raw.get("assignee")
    if raw_assignee is not None:
        if not isinstance(raw_assignee, dict):
            raise ValueError(f"conversation {identifier} assignee is invalid")
        assignee_id = wire_id(raw_assignee.get("assignee_id"))
        if assignee_id:
            assignee_type = string_field(raw_assignee, "assignee_type")
            if not _HIVER_ID.fullmatch(assignee_id) or not _ASSIGNEE_TYPE.fullmatch(assignee_type):
                raise ValueError(f"conversation {identifier} assignee is invalid")
            assignee = ConversationAssignee(assignee_type, assignee_id)
    tag_ids = []
    for tag in list_field(raw, "tag_ids"):
        tag_id = wire_id(tag)
        validate_id("tag ID", tag_id)
        tag_ids.append(tag_id)
    gmail_thread_id = string_field(raw, "gmail_thread_id")
    if gmail_thread_id and not _GMAIL_ID.fullmatch(gmail_thread_id):
        raise ValueError(f"conversation {identifier} gmail_thread_id is invalid")
    permalink = string_field(raw, "private_permalink")
    if permalink:
        url = urlsplit(permalink)
        if url.scheme != "https" or not url.netloc or url.username is not None or len(permalink) > 2048:
            raise ValueError(f"conversation {identifier} private_permalink is not an HTTPS URL")
    messages = []
    for raw_message in list_field(raw, "message_ids"):
        if not isinstance(raw_message, dict):
            raise ValueError(f"conversation {identifier} has an invalid message ID")
        hiver_id = wire_id(raw_message.get("hiver_message_id"))
        gmail_id = string_field(raw_message, "gmail_message_id")
        if (
            (hiver_id and not _HIVER_ID.fullmatch(hiver_id))
            or (gmail_id and not _GMAIL_ID.fullmatch(gmail_id))
            or not (hiver_id or gmail_id)
        ):
            raise ValueError(f"conversation {identifier} has an invalid message ID")
        messages.append(ConversationMessage(hiver_id, gmail_id))
    conversation = Conversation(
        identifier,
        inbox_id,
        assignee=assignee,
        tag_ids=tuple(tag_ids),
        gmail_thread_id=gmail_thread_id,
        private_permalink=permalink,
        status="closed" if status in ("close", "closed") else status,
    )
    return ConversationDetails(conversation, tuple(messages))


def decode_tag(raw: dict[str, Any]) -> Tag:
    """Decode one Hiver tag."""
    identifier = wire_id(raw.get("id"))
    validate_id("tag id", identifier)
    return Tag(identifier, string_field(raw, "name"))


def decode_note(body: bytes) -> Note:
    """Decode Hiver's accepted note object."""
    raw = single_data(body)
    identifier = wire_id(raw.get("id"))
    validate_id("note id", identifier)
    conversation_id = wire_id(raw.get("conversation_id"))
    if conversation_id:
        validate_id("note conversation_id", conversation_id)
    author = raw.get("author") or {}
    if not isinstance(author, dict):
        raise ValueError("note author is invalid")
    author_id = wire_id(author.get("id"))
    if author_id:
        validate_id("note author id", author_id)
    created = string_field(raw, "created_at")
    if created:
        try:
            instant = parse_go_rfc3339(created)
        except ValueError as error:
            if "precision beyond Python microseconds" in str(error):
                raise ValueError("note created_at has precision beyond Python microseconds") from error
            raise ValueError("note created_at is not an RFC 3339 time") from error
    else:
        instant = datetime.min.replace(tzinfo=UTC)
    return Note(identifier, conversation_id, author_id, string_field(author, "email"), instant)


def decode_draft(body: bytes) -> SharedDraft:
    """Decode Hiver's accepted shared draft object."""
    raw = single_data(body)
    identifier = wire_id(raw.get("shared_draft_id"))
    validate_id("shared_draft_id", identifier)
    conversation_id = wire_id(raw.get("id"))
    reply_id = wire_id(raw.get("reply_to_message_id"))
    if conversation_id:
        validate_id("conversation id", conversation_id)
    if reply_id:
        validate_id("reply-to ID", reply_id)
    return SharedDraft(identifier, conversation_id, reply_id)


def string_field(raw: dict[str, Any], field_name: str) -> str:
    """Read a string with Go's zero value for an absent field."""
    value = raw.get(field_name, "")
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError(f"{field_name} is not a string")
    return value


def bool_field(raw: dict[str, Any], field_name: str) -> bool:
    """Read a boolean with Go's zero value for an absent field."""
    value = raw.get(field_name, False)
    if value is None:
        return False
    if not isinstance(value, bool):
        raise ValueError(f"{field_name} is not a boolean")
    return value


def list_field(raw: dict[str, Any], field_name: str) -> list[Any]:
    """Read an array with Go's empty value for an absent field."""
    value = raw.get(field_name, [])
    if value is None:
        return []
    if not isinstance(value, list):
        raise ValueError(f"{field_name} is not a list")
    return value


_RFC3339 = re.compile(r"^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:[.,](\d+))?(Z|[+-]\d{2}:\d{2})$")


def parse_go_rfc3339(value: str) -> datetime:
    """Parse Go RFC3339Nano without losing a provider timestamp's precision."""
    match = _RFC3339.fullmatch(value)
    if match is None:
        raise ValueError("created_at is not an RFC 3339 time")
    year, month, day, hour, minute, second, fraction, zone = match.groups()
    fraction = fraction or ""
    if any(digit != "0" for digit in fraction[6:]):
        raise ValueError("created_at has precision beyond Python microseconds")
    if zone != "Z" and (int(zone[1:3]) > 24 or int(zone[4:]) > 59):
        raise ValueError("created_at has an invalid offset")
    instant = datetime(
        int(year),
        int(month),
        int(day),
        int(hour),
        int(minute),
        int(second),
        int(fraction[:6].ljust(6, "0")),
        tzinfo=UTC,
    )
    if zone != "Z":
        offset = timedelta(hours=int(zone[1:3]), minutes=int(zone[4:]))
        instant = instant - offset if zone[0] == "+" else instant + offset
    return instant
