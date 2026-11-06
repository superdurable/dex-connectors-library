# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Zoom HTTP transport and safe response classification."""

import json
from collections.abc import Callable
from dataclasses import dataclass, field
from datetime import UTC, datetime
from typing import TYPE_CHECKING
from urllib.parse import urlencode

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

if TYPE_CHECKING:
    from .operations import (
        CreateMeetingOperation,
        GetMeetingOperation,
        ListMeetingsOperation,
        ListPastMeetingParticipantsOperation,
        UpdateMeetingOperation,
    )


@dataclass(frozen=True, slots=True)
class Exchange:
    """One provider exchange with dispatch evidence and bounded response."""

    status: int = 0
    headers: httpx.Headers = field(default_factory=httpx.Headers)
    body: bytes = b""
    error: str = ""
    is_dispatched: bool = False
    is_body_too_large: bool = False
    has_body_read_failed: bool = False
    reflects_secret: bool = False


def _encode_query(values: dict[str, str]) -> str:
    """Match Go url.Values.Encode for sorted query pairs."""
    return urlencode(sorted(values.items()))


class Client:
    """Resolve Zoom credentials per attempt and own optional provider transport."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        now: Callable[[], datetime] | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Zoom configuration and credential source are required")
        self.endpoint = validate_base_url(config.endpoint or Config().endpoint)
        self.max_response_bytes = config.max_response_bytes or Config().max_response_bytes
        if type(self.max_response_bytes) is not int or self.max_response_bytes < 1:
            raise ValueError("Zoom maxResponseBytes must be positive")
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(20)
        self.owns_http_client = http_client is None
        self.now = now or (lambda: datetime.now(UTC))

    def list_meetings(self) -> "ListMeetingsOperation":
        """Return the meeting page Query."""
        from .operations import ListMeetingsOperation

        return ListMeetingsOperation(self)

    def get_meeting(self) -> "GetMeetingOperation":
        """Return the meeting detail Query."""
        from .operations import GetMeetingOperation

        return GetMeetingOperation(self)

    def create_meeting(self) -> "CreateMeetingOperation":
        """Return the fenced meeting create Mutation."""
        from .operations import CreateMeetingOperation

        return CreateMeetingOperation(self)

    def update_meeting(self) -> "UpdateMeetingOperation":
        """Return the repeatable meeting patch Mutation."""
        from .operations import UpdateMeetingOperation

        return UpdateMeetingOperation(self)

    def list_past_meeting_participants(self) -> "ListPastMeetingParticipantsOperation":
        """Return the past participant page Query."""
        from .operations import ListPastMeetingParticipantsOperation

        return ListPastMeetingParticipantsOperation(self)

    async def aclose(self) -> None:
        """Close the connector-owned transport."""
        if self.owns_http_client:
            await self.http_client.aclose()

    async def resolve(self, call: Call, operation: str) -> tuple[Credentials | None, Failure | None, bool]:
        """Resolve current material, distinguishing transient refresh outages."""
        try:
            credentials = await self.credentials.resolve(call)
        except ReauthorizationRequiredError:
            return (
                None,
                failure(
                    operation,
                    FailureKind.AUTHENTICATION,
                    "Zoom connection requires reauthorization; reconnect the connection",
                ),
                False,
            )
        except Exception:
            return (
                None,
                failure(
                    operation, FailureKind.AVAILABILITY, "Zoom connection credentials could not be refreshed"
                ),
                True,
            )
        if not is_header_safe_credential(credentials.access_token.reveal()):
            return (
                None,
                failure(
                    operation,
                    FailureKind.AUTHENTICATION,
                    "Zoom connection credentials are unavailable; reconnect the connection",
                ),
                False,
            )
        return credentials, None, False

    async def exchange(
        self,
        call: Call,
        credentials: Credentials,
        method: str,
        path: str,
        query: dict[str, str] | None = None,
        payload: dict[str, object] | None = None,
        fenced: bool = False,
    ) -> Exchange:
        """Send once, refreshing after a rejected bearer only for unfenced calls."""
        token = credentials.access_token.reveal()
        for attempt in range(2):
            exchange = await self.send_once(token, method, path, query, payload)
            if exchange.error or exchange.status != 401 or fenced or attempt != 0:
                return exchange
            resolver = getattr(self.credentials, "resolve_after_rejection", None)
            if resolver is None:
                return exchange
            try:
                replacement = await resolver(call)
                token = replacement.access_token.reveal()
                if not is_header_safe_credential(token):
                    return exchange
            except Exception:
                return exchange
        return exchange

    async def send_once(
        self,
        token: str,
        method: str,
        path: str,
        query: dict[str, str] | None = None,
        payload: dict[str, object] | None = None,
    ) -> Exchange:
        """Perform one HTTP exchange without following redirects."""
        target = self.endpoint + path
        if query:
            target += "?" + _encode_query(query)
        headers = {"Authorization": "Bearer " + token, "Accept": "application/json"}
        body = None
        if payload is not None:
            headers["Content-Type"] = "application/json"
            try:
                body = json.dumps(payload, separators=(",", ":"), ensure_ascii=False).encode()
            except (TypeError, ValueError):
                return Exchange(error="notBuilt")
        try:
            request = self.http_client.build_request(method, target, headers=headers, content=body)
        except (httpx.HTTPError, ValueError):
            return Exchange(error="notBuilt")
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError as error:
            return Exchange(error="transport", is_dispatched=not error.provably_not_sent)
        limit = self.max_response_bytes if 200 <= response.status_code < 300 else MAX_ERROR_BODY_BYTES
        try:
            contents = await read_bounded_body(response, limit)
        except BodyTooLargeError:
            return Exchange(
                status=response.status_code,
                headers=response.headers,
                is_dispatched=True,
                is_body_too_large=200 <= response.status_code < 300,
            )
        except ProviderTransportError:
            return Exchange(
                status=response.status_code,
                headers=response.headers,
                is_dispatched=True,
                has_body_read_failed=True,
            )
        reflected = 200 <= response.status_code < 300 and bool(token) and token.encode() in contents
        return Exchange(
            status=response.status_code,
            headers=response.headers,
            body=b"" if reflected else contents,
            is_dispatched=True,
            reflects_secret=reflected,
        )

    def receipt(self, call: Call, object_id: str = "", error_code: str = "") -> Receipt:
        """Persist stable identity and a safe numeric Zoom error code."""
        metadata = {"zoomErrorCode": error_code} if error_code else None
        return Receipt(
            call.id,
            "zoom",
            self.now().astimezone(UTC),
            call.idempotency_key,
            object_id,
            metadata=metadata or {},
        )


def failure(operation: str, kind: FailureKind, message: str) -> Failure:
    """Create a secret-free Zoom failure."""
    return Failure(kind, "zoom", operation, message)


def classify(client: Client, operation: str, exchange: Exchange) -> tuple[str, Failure | None, float, str]:
    """Return Go's status outcome, failure, retry hint, and numeric error code."""
    status = exchange.status
    if 200 <= status < 300:
        if exchange.is_body_too_large:
            return (
                "invalid",
                failure(
                    operation,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Zoom response exceeds the configured size limit",
                ),
                0,
                "",
            )
        if exchange.reflects_secret:
            return (
                "invalid",
                failure(operation, FailureKind.PROTOCOL, "Zoom response contained the connection credential"),
                0,
                "",
            )
        if exchange.has_body_read_failed:
            return "retry", failure(operation, FailureKind.TRANSPORT, "Zoom response was interrupted"), 0, ""
        return "usable", None, 0, ""
    error_code = ""
    for token in read_error_tokens(exchange.body, ("/code",)):
        if token.isascii() and token.isdecimal() and int(token) <= 2**32 - 1:
            error_code = token
            break
    suffix = f" with HTTP {status}" + (f" (code {error_code})" if error_code else "")
    if 300 <= status < 400:
        return (
            "invalid",
            failure(operation, FailureKind.PROTOCOL, "Zoom redirected the request" + suffix),
            0,
            error_code,
        )
    if status == 404:
        return (
            "notFound",
            failure(operation, FailureKind.NOT_FOUND, "Zoom reported the resource missing" + suffix),
            0,
            error_code,
        )
    if status == 429:
        try:
            message = json.loads(exchange.body).get("message", "")
        except (ValueError, UnicodeDecodeError, AttributeError):
            message = ""
        if (
            not exchange.has_body_read_failed
            and isinstance(message, str)
            and message.strip()
            .lower()
            .startswith("you have reached the maximum daily rate limit for this api.")
        ):
            return (
                "rejected",
                failure(operation, FailureKind.RATE_LIMIT, "Zoom daily API rate limit reached" + suffix),
                0,
                error_code,
            )
        return (
            "retry",
            failure(operation, FailureKind.RATE_LIMIT, "Zoom rate limited the request" + suffix),
            parse_retry_after(exchange.headers.get("Retry-After", ""), client.now()),
            error_code,
        )
    if status == 409:
        return (
            "retry",
            failure(operation, FailureKind.CONFLICT, "Zoom reported a conflict" + suffix),
            0,
            error_code,
        )
    if status == 408 or status >= 500:
        return (
            "retry",
            failure(operation, FailureKind.AVAILABILITY, "Zoom is temporarily unavailable" + suffix),
            parse_retry_after(exchange.headers.get("Retry-After", ""), client.now()),
            error_code,
        )
    kind = {401: FailureKind.AUTHENTICATION, 403: FailureKind.AUTHORIZATION, 412: FailureKind.CONFLICT}.get(
        status, FailureKind.PROVIDER_REJECTION
    )
    return "rejected", failure(operation, kind, "Zoom rejected the request" + suffix), 0, error_code
