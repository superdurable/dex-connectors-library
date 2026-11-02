# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Re:amaze API client and Go-compatible provider classification."""

from __future__ import annotations

import base64
import re
from dataclasses import dataclass
from datetime import UTC, datetime
from email.utils import parseaddr
from typing import TYPE_CHECKING
from urllib.parse import quote

import httpx
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    ReauthorizationRequiredError,
    Receipt,
)
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
        CreateConversationOperation,
        FindContactByEmailOperation,
        GetConversationOperation,
        ReplyToConversationOperation,
        SearchConversationsOperation,
        UpdateConversationOperation,
    )

_BRAND = re.compile(r"^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$")
_REQUEST_ID = re.compile(r"^[A-Za-z0-9._:-]{1,128}$")


@dataclass(frozen=True, slots=True)
class Exchange:
    """One classified provider exchange with safe response metadata."""

    outcome: str
    failure: Failure | None = None
    retry_after_seconds: float = 0
    status: int = 0
    body: bytes = b""
    request_id: str = ""

    @property
    def is_retryable_read(self) -> bool:
        """Return whether repeating a read is safe and may succeed."""
        return self.outcome in {"rate_limited", "not_sent", "unavailable"}


class CredentialsUnavailableError(OSError):
    """Credential lookup may succeed on a later attempt."""


class CredentialsUnusableError(ValueError):
    """Credential material requires connection repair."""


class Client:
    """Execute authenticated API calls with call-time credentials and a bounded body."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        api_base_url: str | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Re:amaze configuration and credential provider are required")
        if not _BRAND.fullmatch(config.brand):
            raise ValueError(
                "Re:amaze brand must be lowercase letters, digits, and hyphens, such as acme for https://acme.reamaze.io"
            )
        if config.max_response_bytes < 1:
            raise ValueError("Re:amaze response limit must be positive")
        base_url = api_base_url or f"https://{config.brand}.reamaze.io/api/v1"
        self.api_base_url = validate_base_url(base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(12)
        self.owns_http_client = http_client is None

    async def aclose(self) -> None:
        """Close the transport when this client created it."""
        if self.owns_http_client:
            await self.http_client.aclose()

    def search_conversations(self) -> SearchConversationsOperation:
        """Return the conversation search Query."""
        from .operations import SearchConversationsOperation

        return SearchConversationsOperation(self)

    def get_conversation(self) -> GetConversationOperation:
        """Return the conversation detail Query."""
        from .operations import GetConversationOperation

        return GetConversationOperation(self)

    def create_conversation(self) -> CreateConversationOperation:
        """Return the fenced conversation creation Mutation."""
        from .operations import CreateConversationOperation

        return CreateConversationOperation(self)

    def update_conversation(self) -> UpdateConversationOperation:
        """Return the absolute conversation update Mutation."""
        from .operations import UpdateConversationOperation

        return UpdateConversationOperation(self)

    def reply_to_conversation(self) -> ReplyToConversationOperation:
        """Return the fenced reply Mutation."""
        from .operations import ReplyToConversationOperation

        return ReplyToConversationOperation(self)

    def find_contact_by_email(self) -> FindContactByEmailOperation:
        """Return the exact-email contact Query."""
        from .operations import FindContactByEmailOperation

        return FindContactByEmailOperation(self)

    async def resolve_credentials(self, call: Call) -> Credentials:
        """Resolve current credentials and distinguish transient reads from unusable values."""
        try:
            credentials = await self.credentials.resolve(call)
        except ReauthorizationRequiredError as error:
            raise CredentialsUnusableError("connection credentials are unavailable") from error
        except Exception as error:
            raise CredentialsUnavailableError(
                "connection credentials could not be loaded yet; nothing was sent"
            ) from error
        if not is_bare_email_address(credentials.email) or not _is_safe_token(credentials.api_token.reveal()):
            raise CredentialsUnusableError("connection credentials are unavailable")
        return credentials

    async def exchange(
        self,
        call: Call,
        credentials: Credentials,
        operation: str,
        method: str,
        path: str,
        *,
        query: dict[str, str] | None = None,
        payload: object | None = None,
    ) -> Exchange:
        """Send one request and classify transport, status, and body independently of an operation."""
        headers = {
            "Authorization": "Basic "
            + base64.b64encode(f"{credentials.email}:{credentials.api_token.reveal()}".encode()).decode(),
            "Accept": "application/json",
        }
        if payload is not None:
            headers["Content-Type"] = "application/json"
        try:
            request = self.http_client.build_request(
                method,
                self.api_base_url + path,
                params=query,
                json=payload,
                headers=headers,
            )
        except (httpx.HTTPError, ValueError, TypeError):
            return Exchange(
                "defect",
                failure=reamaze_failure(
                    FailureKind.LOCAL_DEFECT, operation, "Re:amaze request could not be built"
                ),
            )
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError as error:
            outcome = "not_sent" if error.provably_not_sent else "unavailable"
            message = (
                "Re:amaze could not be reached; no request was sent"
                if error.provably_not_sent
                else "Re:amaze request failed before a response arrived"
            )
            return Exchange(outcome, failure=reamaze_failure(FailureKind.TRANSPORT, operation, message))
        status = response.status_code
        request_id = response.headers.get("X-Request-Id", "")
        if not _REQUEST_ID.fullmatch(request_id):
            request_id = ""
        retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
        try:
            body = await read_bounded_body(response, self.max_response_bytes)
        except BodyTooLargeError:
            if 200 <= status < 300:
                return Exchange(
                    "invalid",
                    reamaze_failure(
                        FailureKind.RESPONSE_TOO_LARGE,
                        operation,
                        "Re:amaze response exceeds the configured maxResponseBytes limit",
                    ),
                    status=status,
                    request_id=request_id,
                )
            body = b""
        except ProviderTransportError:
            return Exchange(
                "unavailable",
                reamaze_failure(FailureKind.TRANSPORT, operation, "Re:amaze response could not be read"),
                status=status,
                request_id=request_id,
            )
        if 200 <= status < 300:
            if credentials.api_token.reveal().encode() in body:
                return Exchange(
                    "invalid",
                    reamaze_failure(
                        FailureKind.PROTOCOL,
                        operation,
                        "Re:amaze response reflected the connection credential",
                    ),
                    status=status,
                    request_id=request_id,
                )
            return Exchange("succeeded", status=status, body=body, request_id=request_id)
        if status == 429:
            return Exchange(
                "rate_limited",
                reamaze_failure(
                    FailureKind.RATE_LIMIT, operation, "Re:amaze rate limited the request (HTTP 429)"
                ),
                retry_after or 60,
                status,
                request_id=request_id,
            )
        if status == 408 or status >= 500:
            return Exchange(
                "unavailable",
                reamaze_failure(
                    FailureKind.AVAILABILITY,
                    operation,
                    f"Re:amaze could not complete the request (HTTP {status})",
                ),
                retry_after,
                status,
                request_id=request_id,
            )
        if status == 404:
            return Exchange(
                "not_found",
                reamaze_failure(
                    FailureKind.NOT_FOUND,
                    operation,
                    "Re:amaze found no such resource in the brand (HTTP 404)",
                ),
                status=status,
                request_id=request_id,
            )
        if 300 <= status < 400:
            return Exchange(
                "rejected",
                reamaze_failure(
                    FailureKind.PROTOCOL,
                    operation,
                    (
                        f"Re:amaze redirected the request (HTTP {status}); "
                        "check that brand is the brand's reamaze.io subdomain"
                    ),
                ),
                status=status,
                request_id=request_id,
            )
        kind = {
            401: FailureKind.AUTHENTICATION,
            403: FailureKind.AUTHORIZATION,
            409: FailureKind.CONFLICT,
            400: FailureKind.VALIDATION,
            422: FailureKind.VALIDATION,
        }.get(status, FailureKind.PROVIDER_REJECTION)
        return Exchange(
            "rejected",
            reamaze_failure(kind, operation, f"Re:amaze rejected the request (HTTP {status})"),
            status=status,
            request_id=request_id,
        )

    def receipt(self, call: Call, exchange: Exchange, object_id: str = "") -> Receipt:
        """Record safe provider identity and request metadata."""
        return Receipt(
            call.id, "reamaze", datetime.now(UTC), call.idempotency_key, object_id, exchange.request_id
        )


def reamaze_failure(kind: FailureKind, operation: str, message: str) -> Failure:
    """Construct a secret-safe failure for one Re:amaze operation."""
    return Failure(kind, "reamaze", operation, message)


def is_bare_email_address(value: str) -> bool:
    """Require one bare RFC 5322 address without a display name or Basic-auth delimiter."""
    if not value or any(character in value for character in ' "()<>,;:[]\\'):
        return False
    display_name, address = parseaddr(value)
    return not display_name and address == value and "@" in value


def _is_safe_token(value: str) -> bool:
    return bool(value) and all(33 <= ord(character) <= 126 for character in value)


def conversation_path(conversation_id: str) -> str:
    """Escape a conversation ID with Go url.PathEscape semantics."""
    return "/conversations/" + quote(conversation_id, safe="-._~!$&'()*+,;=:@")
