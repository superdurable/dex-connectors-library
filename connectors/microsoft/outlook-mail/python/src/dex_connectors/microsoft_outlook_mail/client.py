# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Authenticated Microsoft Graph session and safe response classification."""

import json
import re
from collections.abc import Callable, Sequence
from dataclasses import dataclass
from datetime import UTC, datetime
from email.utils import parseaddr
from typing import TYPE_CHECKING, Any
from urllib.parse import quote, quote_plus, urlsplit

import httpx
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    ReauthorizationRequiredError,
    Receipt,
    RetryError,
)
from dex_connectors.sdk.providerhttp import (
    BodyTooLargeError,
    ProviderTransportError,
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    read_error_tokens,
    send_traced,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials

if TYPE_CHECKING:
    from .operations import (
        GetMessageOperation,
        MoveMessageOperation,
        ReplyToMessageOperation,
        SearchMessagesOperation,
        SendMessageOperation,
        SetMessageFlagsOperation,
    )

GRAPH_BASE_URL = "https://graph.microsoft.com/v1.0"
GRAPH_HOST = "graph.microsoft.com"
REQUEST_ID_PATTERN = re.compile(r"^[A-Za-z0-9-]{1,64}$")
GRAPH_ID_PATTERN = re.compile(r"^[A-Za-z0-9=+_-]{1,1024}$")
GUID_PATTERN = re.compile(r"^[0-9A-Fa-f]{8}(?:-[0-9A-Fa-f]{4}){3}-[0-9A-Fa-f]{12}$")


def path_escape(value: str) -> str:
    """Escape a Graph path segment as Go url.PathEscape does."""
    return quote(value, safe="$&+-._:=@~")


def query_escape(value: str) -> str:
    """Encode an ordered Graph query component as Go QueryEscape with spaces as %20."""
    return quote_plus(value, safe="~").replace("+", "%20")


def validate_graph_id(field_name: str, value: str) -> None:
    """Require a Graph opaque ID before it enters a URL."""
    if not GRAPH_ID_PATTERN.fullmatch(value):
        raise ValueError(
            f"{field_name} must be a Microsoft Graph ID from an earlier Result, such as the id of searchMessages"  # noqa: E501
        )


def validate_next_page_link(link: str) -> None:
    """Keep continuation links on the Graph message collection."""
    if not link or len(link.encode()) > 4096:
        raise ValueError("next-page link is empty or too long")
    try:
        parsed = urlsplit(link)
        if (
            parsed.scheme != "https"
            or parsed.netloc != GRAPH_HOST
            or parsed.fragment
            or not parsed.path.startswith("/v1.0/")
            or not parsed.path.lower().endswith("/messages")
            or ".." in parsed.path
        ):
            raise ValueError("next-page link is not a Microsoft Graph v1.0 messages collection")
    except ValueError as error:
        raise ValueError("next-page link cannot be parsed") from error


def failure(operation: str, kind: FailureKind, message: str) -> Failure:
    """Create a secret-safe Microsoft Graph failure."""
    return Failure(kind, "microsoft-graph", operation, message)


@dataclass(frozen=True, slots=True)
class Exchange:
    """One Graph exchange with its Go-equivalent outcome."""

    outcome: str
    status_code: int = 0
    body: bytes = b""
    request_id: str = ""
    error_code: str = ""
    problem: Failure | None = None
    retry_after: float = 0


class Client:
    """Execute Outlook Mail operations with call-time credential resolution."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        now: Callable[[], datetime] | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Outlook Mail configuration and credential provider are required")
        if type(config.max_response_bytes) is not int or config.max_response_bytes < 1:
            raise ValueError("Microsoft Graph response limit must be positive")
        self.mailbox = config.mailbox.strip()
        mailbox_name, mailbox_address = parseaddr(self.mailbox)
        if self.mailbox and not (
            GUID_PATTERN.fullmatch(self.mailbox)
            or (
                not mailbox_name
                and mailbox_address == self.mailbox
                and not any(character in self.mailbox for character in "/?#%'\\")
            )
        ):
            raise ValueError(
                "configuration mailbox must be an address such as support@contoso.com or an Entra object ID"
            )
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(20)
        self._owns_http_client = http_client is None
        self.now = now or (lambda: datetime.now(UTC))

    async def aclose(self) -> None:
        """Close the HTTP client when this connector created it."""
        if self._owns_http_client:
            await self.http_client.aclose()

    def search_messages(self) -> "SearchMessagesOperation":
        """Return the searchMessages Query."""
        from .operations import SearchMessagesOperation

        return SearchMessagesOperation(self)

    def get_message(self) -> "GetMessageOperation":
        """Return the getMessage Query."""
        from .operations import GetMessageOperation

        return GetMessageOperation(self)

    def send_message(self) -> "SendMessageOperation":
        """Return the sendMessage Mutation."""
        from .operations import SendMessageOperation

        return SendMessageOperation(self)

    def reply_to_message(self) -> "ReplyToMessageOperation":
        """Return the replyToMessage Mutation."""
        from .operations import ReplyToMessageOperation

        return ReplyToMessageOperation(self)

    def move_message(self) -> "MoveMessageOperation":
        """Return the moveMessage Mutation."""
        from .operations import MoveMessageOperation

        return MoveMessageOperation(self)

    def set_message_flags(self) -> "SetMessageFlagsOperation":
        """Return the setMessageFlags Mutation."""
        from .operations import SetMessageFlagsOperation

        return SetMessageFlagsOperation(self)

    async def open_session(self, call: Call, operation: str) -> tuple["Session | None", Exchange | None]:
        """Resolve the current OAuth or app-only token and mailbox path."""
        try:
            if hasattr(self.credentials, "driver"):
                resolved = await self.credentials.resolve_with_refresh(call)  # type: ignore[attr-defined]
            else:
                resolved = await self.credentials.resolve(call)
        except ReauthorizationRequiredError:
            return None, Exchange(
                "rejected",
                problem=failure(
                    operation,
                    FailureKind.AUTHENTICATION,
                    "Microsoft authorization must be renewed: authorize the connection again or replace the client secret",  # noqa: E501
                ),
            )
        except (OSError, RetryError):
            return None, Exchange(
                "retry",
                problem=failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    "the Microsoft token endpoint is temporarily unavailable",
                ),
            )
        except Exception:
            return None, Exchange(
                "defect",
                problem=failure(
                    operation,
                    FailureKind.AUTHENTICATION,
                    "Outlook Mail connection credentials are unavailable",
                ),
            )
        if resolved.access_token is None or not is_header_safe_credential(resolved.access_token.reveal()):
            return None, Exchange(
                "defect",
                problem=failure(
                    operation,
                    FailureKind.AUTHENTICATION,
                    "Outlook Mail connection credentials are unavailable",
                ),
            )
        if resolved.auth_method_id == "microsoft-oauth":
            root = "/me"
        elif resolved.auth_method_id == "app-only" and self.mailbox:
            root = "/users/" + path_escape(self.mailbox)
        elif resolved.auth_method_id != "app-only":
            return None, Exchange(
                "defect",
                problem=failure(
                    operation, FailureKind.VALIDATION, "Outlook Mail authorization method is not supported"
                ),
            )
        else:
            return None, Exchange(
                "defect",
                problem=failure(
                    operation,
                    FailureKind.VALIDATION,
                    "an app-only connection needs the mailbox configuration field",
                ),
            )
        return Session(self, call, operation, resolved, root), None

    def receipt(self, call: Call, result: Exchange, object_id: str = "") -> Receipt:
        """Record provider identity and an accepted request ID without secrets."""
        return Receipt(
            call.id, "microsoft-graph", self.now(), call.idempotency_key, object_id, result.request_id
        )


class Session:
    """One operation's resolved token and mailbox path."""

    def __init__(
        self, client: Client, call: Call, operation: str, credentials: Credentials, root: str
    ) -> None:
        self.client = client
        self.call = call
        self.operation = operation
        self.credentials = credentials
        self.root = root
        self.has_refreshed_after_rejection = False

    async def exchange(
        self,
        method: str,
        path: str,
        query: Sequence[tuple[str, str]] = (),
        payload: Any = None,
        *,
        absolute_url: str = "",
        prefers_text_body: bool = False,
    ) -> Exchange:
        """Send once, with at most one token refresh and resend after a 401."""
        result = await self._send(method, path, query, payload, absolute_url, prefers_text_body)
        if (
            result.status_code != 401
            or self.has_refreshed_after_rejection
            or not hasattr(self.client.credentials, "resolve_after_rejection")
        ):
            return result
        self.has_refreshed_after_rejection = True
        try:
            replacement = await self.client.credentials.resolve_after_rejection(self.call)
            if (
                replacement.auth_method_id != self.credentials.auth_method_id
                or replacement.access_token is None
                or not is_header_safe_credential(replacement.access_token.reveal())
            ):
                return result
        except Exception:
            return result
        self.credentials = replacement
        return await self._send(method, path, query, payload, absolute_url, prefers_text_body)

    async def _send(
        self,
        method: str,
        path: str,
        query: Sequence[tuple[str, str]],
        payload: Any,
        absolute_url: str,
        prefers_text_body: bool,
    ) -> Exchange:
        url = absolute_url or GRAPH_BASE_URL + self.root + path
        if query and not absolute_url:
            url += "?" + "&".join(query_escape(key) + "=" + query_escape(value) for key, value in query)
        try:
            headers = [
                ("Authorization", "Bearer " + self.credentials.access_token.reveal()),  # type: ignore[union-attr]  # noqa: E501
                ("Accept", "application/json"),
                ("Prefer", 'IdType="ImmutableId"'),
            ]
            if prefers_text_body:
                headers.append(("Prefer", 'outlook.body-content-type="text"'))
            content = (
                json.dumps(
                    payload, ensure_ascii=False, allow_nan=False, separators=(",", ":"), sort_keys=True
                ).encode()
                if payload is not None
                else (b"" if method == "POST" else None)
            )
            if payload is not None:
                headers.append(("Content-Type", "application/json"))
            request = self.client.http_client.build_request(method, url, headers=headers, content=content)
            request.extensions["timeout"] = {"connect": 20.0, "read": 20.0, "write": 20.0, "pool": 20.0}
        except (ValueError, TypeError, httpx.HTTPError):
            return Exchange(
                "defect",
                problem=failure(
                    self.operation, FailureKind.LOCAL_DEFECT, "Microsoft Graph request could not be built"
                ),
            )
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError as error:
            if error.provably_not_sent:
                return Exchange(
                    "retry",
                    problem=failure(
                        self.operation,
                        FailureKind.TRANSPORT,
                        "Microsoft Graph could not be reached; no request was sent",
                    ),
                )
            return Exchange(
                "ambiguous",
                problem=failure(
                    self.operation,
                    FailureKind.TRANSPORT,
                    "the Microsoft Graph request failed before an answer arrived",
                ),
            )
        request_id = response.headers.get("request-id", "")
        if not REQUEST_ID_PATTERN.fullmatch(request_id):
            request_id = ""
        if not 200 <= response.status_code < 300:
            try:
                body = await read_bounded_body(response, 64 << 10)
            except (BodyTooLargeError, ProviderTransportError):
                body = b""
            token = self.credentials.access_token.reveal() if self.credentials.access_token else ""
            error_code = next(
                (
                    item
                    for item in read_error_tokens(body, ("/error/code", "/error/innerError/code"))
                    if not token or token not in item
                ),
                "",
            )
            return classify_failed_status(
                self.operation,
                response.status_code,
                request_id,
                error_code,
                parse_retry_after(response.headers.get("Retry-After", ""), self.client.now()),
            )
        try:
            body = await read_bounded_body(response, self.client.max_response_bytes)
        except BodyTooLargeError:
            return Exchange(
                "invalid",
                response.status_code,
                request_id=request_id,
                problem=failure(
                    self.operation,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "the Microsoft Graph response exceeds the configured maxResponseBytes limit",
                ),
            )
        except ProviderTransportError:
            return Exchange(
                "ambiguous",
                response.status_code,
                request_id=request_id,
                problem=failure(
                    self.operation, FailureKind.TRANSPORT, "the Microsoft Graph response was interrupted"
                ),
            )
        token = self.credentials.access_token.reveal() if self.credentials.access_token else ""
        if token and token.encode() in body:
            return Exchange(
                "invalid",
                response.status_code,
                request_id=request_id,
                problem=failure(
                    self.operation,
                    FailureKind.PROTOCOL,
                    "the Microsoft Graph response reflected the connection credential",
                ),
            )
        return Exchange("succeeded", response.status_code, body, request_id)


def classify_failed_status(
    operation: str, status: int, request_id: str, error_code: str, retry_after: float
) -> Exchange:
    """Classify a Graph failure using status and safe machine error code."""
    suffix = f" (HTTP {status}" + (f" {error_code}" if error_code else "") + ")"

    def make(outcome: str, kind: FailureKind, message: str) -> Exchange:
        return Exchange(
            outcome,
            status,
            request_id=request_id,
            error_code=error_code,
            problem=failure(operation, kind, message + suffix),
            retry_after=retry_after,
        )

    if status in (429, 509):
        return make("retry", FailureKind.RATE_LIMIT, "Microsoft Graph throttled the request")
    if status == 408:
        return make("retry", FailureKind.AVAILABILITY, "Microsoft Graph timed out waiting for the request")
    if status in (409, 423):
        return make(
            "retry", FailureKind.CONFLICT, "Microsoft Graph reported a conflicting change to the item"
        )
    if status == 501:
        return make(
            "rejected", FailureKind.PROVIDER_REJECTION, "Microsoft Graph does not implement the request"
        )
    if status == 507:
        return make("rejected", FailureKind.QUOTA_EXHAUSTED, "the mailbox is over its storage quota")
    if status >= 500:
        return make(
            "ambiguous", FailureKind.AVAILABILITY, "Microsoft Graph failed or is temporarily unavailable"
        )
    if status == 401:
        return make("rejected", FailureKind.AUTHENTICATION, "Microsoft Graph rejected the access token")
    if status == 403 and "Quota" in error_code:
        return make(
            "rejected", FailureKind.QUOTA_EXHAUSTED, "Microsoft Graph refused the request for a mailbox quota"
        )
    if status == 403:
        return make(
            "rejected", FailureKind.AUTHORIZATION, "Microsoft Graph denied access to the mailbox or item"
        )
    if (status == 404 and error_code in ("", "ErrorItemNotFound")) or (
        status == 400 and error_code == "ErrorInvalidIdMalformed"
    ):
        return make("not_found", FailureKind.NOT_FOUND, "Microsoft Graph found no such item in the mailbox")
    if status == 404:
        return make("rejected", FailureKind.NOT_FOUND, "Microsoft Graph found no such mailbox or resource")
    if 300 <= status < 400:
        return make("rejected", FailureKind.PROTOCOL, "Microsoft Graph redirected the request")
    if status in (400, 413, 422):
        return make("rejected", FailureKind.VALIDATION, "Microsoft Graph rejected the request")
    return make("rejected", FailureKind.PROVIDER_REJECTION, "Microsoft Graph rejected the request")
