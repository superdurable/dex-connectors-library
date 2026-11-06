# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Microsoft Graph requests and Teams provider response decoding."""

import asyncio
import re
import time
from collections.abc import Callable
from contextvars import ContextVar
from dataclasses import dataclass
from datetime import UTC, datetime
from html.parser import HTMLParser
from typing import Any
from urllib.parse import quote, urlsplit

import httpx
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    ReauthorizationRequiredError,
    Receipt,
    RejectedCredentialRefreshingProvider,
    resolve_credential,
    resolve_credential_after_rejection,
)
from dex_connectors.sdk.oauthtoken import ManifestCredentialRefreshDriver
from dex_connectors.sdk.providerhttp import (
    MAX_ERROR_BODY_BYTES,
    BodyTooLargeError,
    ProviderTransportError,
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    read_error_tokens,
    send_traced,
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.refresh import AUTHORIZATION, CREDENTIAL_FIELDS
from ._generated.timestamps import canonical_go_timestamp
from .models import Message, MessageSender

TEAM_ID = re.compile(r"^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$")
CHANNEL_ID = re.compile(r"^19:[A-Za-z0-9._-]{1,256}@thread\.[A-Za-z0-9]{1,32}$")
CHAT_ID = re.compile(r"^19:[A-Za-z0-9._-]{1,256}@[A-Za-z0-9.]{1,64}$")
MESSAGE_ID = re.compile(r"^[0-9]{1,20}$")


def path_escape(value: str) -> str:
    """Escape a path component like Go url.PathEscape."""
    return quote(value, safe="-._~:@$&+=")


def query_encode(values: dict[str, str]) -> str:
    """Encode a query like Go url.Values.Encode, including byte sorted keys."""
    safe = "-._~"

    def encode(value: str) -> str:
        return quote(value, safe=safe).replace("%20", "+")

    return "&".join(
        f"{encode(key)}={encode(values[key])}" for key in sorted(values, key=lambda item: item.encode())
    )


def valid_team_channel(team_id: str, channel_id: str) -> None:
    """Check the Graph identity forms accepted by the Go connector."""
    if not TEAM_ID.fullmatch(team_id):
        raise ValueError("teamId must be a team GUID such as fbe2bf47-16c8-47cf-b4a5-4b9b187c508b")
    if not CHANNEL_ID.fullmatch(channel_id):
        raise ValueError(
            "channelId must be a channel ID such as 19:4a95f7d8db4c4e7fae857bcebe0623e6@thread.tacv2"
        )


def valid_message_id(field_name: str, message_id: str) -> None:
    """Check a decimal Teams message ID."""
    if not MESSAGE_ID.fullmatch(message_id):
        raise ValueError(f"{field_name} must be a decimal Teams message ID such as 1616989510408")


def channel_messages_path(team_id: str, channel_id: str) -> str:
    """Return the Graph channel messages path."""
    return f"/teams/{team_id}/channels/{channel_id}/messages"


def thread_replies_path(team_id: str, channel_id: str, message_id: str) -> str:
    """Return the Graph thread replies path."""
    return channel_messages_path(team_id, channel_id) + f"/{message_id}/replies"


def safe_token(value: object) -> str:
    """Keep a bounded printable provider identifier."""
    return (
        value
        if isinstance(value, str)
        and 0 < len(value.encode()) <= 128
        and all(32 < ord(c) <= 126 for c in value)
        else ""
    )


def safe_web_url(value: object) -> str:
    """Keep an HTTPS Teams URL without a userinfo or control character."""
    if (
        not isinstance(value, str)
        or len(value.encode()) > 2048
        or not value.startswith("https://")
        or not safe_token_url(value)
    ):
        return ""
    parsed = urlsplit(value)
    return value if parsed.hostname and "@" not in parsed.netloc else ""


def safe_token_url(value: str) -> bool:
    """Require provider links to be printable ASCII."""
    return all(32 < ord(c) <= 126 for c in value)


def _parse_go_time(value: object, field_name: str) -> datetime:
    from ._generated.timestamps import parse_go_datetime

    if not isinstance(value, str):
        raise ValueError(f"{field_name} is invalid")
    return parse_go_datetime(value, field_name).astimezone(UTC)


class _TeamsHTML(HTMLParser):
    def __init__(self) -> None:
        super().__init__(convert_charrefs=True)
        self.parts: list[str] = []
        self.skipped = 0
        self.at_line_start = True

    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        if tag in ("script", "style"):
            self.skipped += 1
        elif self.skipped == 0:
            if tag == "br":
                self.parts.append("\n")
                self.at_line_start = True
            elif tag in ("img", "emoji"):
                self.handle_data(dict(attrs).get("alt") or "")
            elif tag in {
                "p",
                "div",
                "li",
                "tr",
                "blockquote",
                "pre",
                "h1",
                "h2",
                "h3",
                "h4",
                "h5",
                "h6",
                "ul",
                "ol",
                "table",
            }:
                self._boundary()

    def handle_endtag(self, tag: str) -> None:
        if tag in ("script", "style"):
            self.skipped = max(0, self.skipped - 1)
        elif self.skipped == 0 and tag in {
            "p",
            "div",
            "li",
            "tr",
            "blockquote",
            "pre",
            "h1",
            "h2",
            "h3",
            "h4",
            "h5",
            "h6",
            "ul",
            "ol",
            "table",
        }:
            self._boundary()

    def handle_data(self, data: str) -> None:
        if self.skipped == 0:
            cleaned = data.replace("\r", " ").replace("\n", " ").replace("\t", " ")
            self.parts.append(cleaned)
            if cleaned.strip():
                self.at_line_start = False

    def _boundary(self) -> None:
        if self.parts and not self.at_line_start:
            self.parts.append("\n")
            self.at_line_start = True


def extract_message_text(content_type: str, content: str) -> str:
    """Keep visible Teams text, including emoji alt text and block boundaries."""
    if content_type != "html":
        return content.replace("\r\n", "\n").strip()
    parser = _TeamsHTML()
    parser.feed(content)
    lines = []
    previous_blank = True
    for line in "".join(parser.parts).split("\n"):
        folded = " ".join(line.split())
        if folded:
            lines.append(folded)
            previous_blank = False
        elif not previous_blank:
            lines.append("")
            previous_blank = True
    return "\n".join(lines).strip()


def decode_message(resource: object, max_text_characters: int = 0) -> Message:
    """Decode the Go connector's bounded chatMessage subset."""
    if (
        not isinstance(resource, dict)
        or not isinstance(resource.get("id"), str)
        or not MESSAGE_ID.fullmatch(resource["id"])
    ):
        raise ValueError("message ID is invalid")
    created_value = resource.get("createdDateTime")
    if not isinstance(created_value, str):
        raise ValueError("message createdDateTime is invalid")
    try:
        created_at = canonical_go_timestamp(created_value, "createdDateTime")
    except ValueError:
        raise ValueError("message createdDateTime is invalid") from None
    sender = MessageSender()
    from_value = resource.get("from")
    if isinstance(from_value, dict):
        for source, field in (("user", "user_id"), ("application", "application_id")):
            identity = from_value.get(source)
            if isinstance(identity, dict) and safe_token(identity.get("id")):
                sender = MessageSender(
                    **{
                        field: identity["id"],
                        "display_name": "".join(
                            c for c in str(identity.get("displayName") or "") if c.isprintable()
                        )[:256],
                    }
                )
                break
    deleted = resource.get("deletedDateTime") is not None
    body = resource.get("body")
    content_type = safe_token(body.get("contentType")) if isinstance(body, dict) else ""
    text = (
        extract_message_text(content_type, str(body.get("content") or ""))
        if isinstance(body, dict) and not deleted
        else ""
    )
    truncated = max_text_characters > 0 and len(text) > max_text_characters
    edited_at = None
    if isinstance(resource.get("lastEditedDateTime"), str):
        try:
            edited_at = canonical_go_timestamp(resource["lastEditedDateTime"], "lastEditedDateTime")
        except ValueError:
            pass
    reply_to_id = resource.get("replyToId")
    subject = resource.get("subject")
    return Message(
        id=resource["id"],
        reply_to_id=reply_to_id if isinstance(reply_to_id, str) and MESSAGE_ID.fullmatch(reply_to_id) else "",
        message_type=safe_token(resource.get("messageType"))
        if isinstance(resource.get("messageType"), str)
        else "",
        created_at=created_at,
        last_edited_at=edited_at,
        is_deleted=deleted,
        sender=sender,
        subject="".join(c for c in subject if c.isprintable())[:1024] if isinstance(subject, str) else "",
        importance=safe_token(resource.get("importance")) or None,
        content_type=content_type or None,
        text=text[:max_text_characters] if truncated else text,
        is_text_truncated=truncated,
        web_url=safe_web_url(resource.get("webUrl")),
    )


@dataclass(frozen=True)
class Exchange:
    """One Graph response with safe metadata and dispatch evidence."""

    status: int = 0
    headers: httpx.Headers | None = None
    body: bytes = b""
    body_too_large: bool = False
    body_read_failed: bool = False
    reflects_secret: bool = False
    error: Exception | None = None
    dispatched: bool = False


class Client:
    """Execute Microsoft Teams operations with call-time renewable credentials."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        now: Callable[[], datetime] | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Microsoft Teams configuration and credentials are required")
        self.endpoint = validate_base_url(config.endpoint or Config().endpoint)
        self.max_response_bytes = (
            Config().max_response_bytes if config.max_response_bytes == 0 else config.max_response_bytes
        )
        self.max_message_bytes = (
            Config().max_message_bytes if config.max_message_bytes == 0 else config.max_message_bytes
        )
        if (
            type(self.max_response_bytes) is not int
            or type(self.max_message_bytes) is not int
            or self.max_response_bytes < 1
            or self.max_message_bytes < 1
        ):
            raise ValueError("Microsoft Teams response and message limits must be positive")
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(20)
        self.owns_http_client = http_client is None
        self.now = now or (lambda: datetime.now(UTC))
        self.operation_deadline: ContextVar[float | None] = ContextVar(
            "microsoft_teams_operation_deadline", default=None
        )
        self.refresh_driver = ManifestCredentialRefreshDriver[Credentials](
            "Microsoft", AUTHORIZATION, CREDENTIAL_FIELDS, self.http_client, now=self.now
        )

    async def aclose(self) -> None:
        """Close the client only when this connector created it."""
        if self.owns_http_client:
            await self.http_client.aclose()

    def list_thread_replies(self) -> Any:
        """Return the listThreadReplies Query."""
        from .operations import ListThreadRepliesOperation

        return ListThreadRepliesOperation(self)

    def post_channel_message(self) -> Any:
        """Return the postChannelMessage Mutation."""
        from .operations import PostChannelMessageOperation

        return PostChannelMessageOperation(self)

    def post_thread_reply(self) -> Any:
        """Return the postThreadReply Mutation."""
        from .operations import PostThreadReplyOperation

        return PostThreadReplyOperation(self)

    def post_chat_message(self) -> Any:
        """Return the postChatMessage Mutation."""
        from .operations import PostChatMessageOperation

        return PostChatMessageOperation(self)

    async def resolve_credentials(self, call: Call) -> Credentials:
        """Resolve and validate the current access token."""
        self.operation_deadline.set(time.monotonic() + 25)
        async with asyncio.timeout(25):
            credentials = await resolve_credential(self.credentials, call, self.refresh_driver)
        if not is_header_safe_credential(credentials.access_token.reveal()):
            raise ReauthorizationRequiredError()
        return credentials

    async def exchange(
        self,
        call: Call,
        credentials: Credentials,
        method: str,
        path: str,
        payload: bytes | None = None,
        *,
        absolute_url: str = "",
        query: dict[str, str] | None = None,
        retry_rejected: bool = True,
        timeout: float = 20,
    ) -> tuple[Exchange, Credentials]:
        """Send one bounded request and refresh a rejected read credential once."""
        target = absolute_url or self.endpoint + path + (("?" + query_encode(query)) if query else "")
        for attempt in range(2):
            deadline = self.operation_deadline.get()
            remaining = min(timeout, max(0, deadline - time.monotonic())) if deadline is not None else timeout
            if remaining <= 0:
                return (
                    Exchange(error=TimeoutError("Microsoft Teams operation deadline exceeded")),
                    credentials,
                )
            headers = {
                "Authorization": "Bearer " + credentials.access_token.reveal(),
                "Accept": "application/json",
                "client-request-id": str(call.id),
            }
            if payload is not None:
                headers["Content-Type"] = "application/json"
            try:
                request = self.http_client.build_request(method, target, headers=headers, content=payload)
                async with asyncio.timeout(remaining):
                    response, _ = await send_traced(self.http_client, request)
                    if (
                        response.status_code == 401
                        and retry_rejected
                        and attempt == 0
                        and isinstance(self.credentials, RejectedCredentialRefreshingProvider)
                    ):
                        try:
                            replacement = await resolve_credential_after_rejection(
                                self.credentials, call, self.refresh_driver
                            )
                            if is_header_safe_credential(replacement.access_token.reveal()):
                                await response.aclose()
                                credentials = replacement
                                continue
                        except Exception:
                            pass
                    status, response_headers = response.status_code, response.headers
                    try:
                        body = await read_bounded_body(
                            response, self.max_response_bytes if 200 <= status < 300 else MAX_ERROR_BODY_BYTES
                        )
                    except BodyTooLargeError:
                        return Exchange(
                            status, response_headers, body_too_large=True, dispatched=True
                        ), credentials
                    except ProviderTransportError:
                        return Exchange(
                            status, response_headers, body_read_failed=True, dispatched=True
                        ), credentials
                    return Exchange(
                        status,
                        response_headers,
                        body=b"" if credentials.access_token.reveal().encode() in body else body,
                        reflects_secret=credentials.access_token.reveal().encode() in body,
                        dispatched=True,
                    ), credentials
            except ProviderTransportError as error:
                return Exchange(error=error, dispatched=not error.provably_not_sent), credentials
            except (TimeoutError, httpx.HTTPError) as error:
                return Exchange(error=error, dispatched=True), credentials
            except (ValueError, TypeError) as error:
                return Exchange(error=error, dispatched=False), credentials
        raise RuntimeError("Microsoft Teams credential retry exhausted")

    def receipt(self, call: Call, exchange: Exchange, object_id: str = "") -> Receipt:
        """Record safe Graph request metadata without credential material."""
        provider_request_id = safe_token(exchange.headers.get("request-id", "")) if exchange.headers else ""
        return Receipt(
            call_id=call.id,
            idempotency_key=call.idempotency_key,
            provider="microsoft-teams",
            provider_object_id=object_id,
            provider_request_id=provider_request_id,
            observed_at=self.now(),
        )

    def failure(self, operation: str, kind: FailureKind, message: str) -> Failure:
        """Construct a secret-safe Graph failure."""
        return Failure(kind=kind, provider="microsoft-teams", operation=operation, message=message)

    def retry_after(self, exchange: Exchange) -> float:
        """Parse Retry-After with the Connector SDK policy."""
        return (
            parse_retry_after(exchange.headers.get("Retry-After", ""), self.now()) if exchange.headers else 0
        )

    def error_code_suffix(self, exchange: Exchange) -> str:
        """Read the machine-readable Graph error code only."""
        tokens = read_error_tokens(exchange.body, ("/error/code",))
        return " " + tokens[0] if tokens else ""
