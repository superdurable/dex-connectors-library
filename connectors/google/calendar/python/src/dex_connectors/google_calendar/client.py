# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Google Calendar HTTP transport and operation constructors."""

import json
import re
from dataclasses import dataclass, field
from datetime import UTC, datetime
from typing import Any
from urllib.parse import quote, quote_plus, urlsplit

import httpx
from dex_connectors.sdk import Call, CredentialProvider, Failure, FailureKind, Receipt
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

_REASONS = frozenset(
    "badRequest invalid required timeRangeEmpty notFound deleted duplicate conditionNotMet "
    "forbidden forbiddenForNonOrganizer requiredAccessLevel insufficientPermissions quotaExceeded "
    "dailyLimitExceeded rateLimitExceeded userRateLimitExceeded backendError invalidAttendeeEmail "
    "cannotChangeOrganizer eventTypeRestriction authError".split()
)


@dataclass(frozen=True, slots=True)
class ProviderResponse:
    """One provider exchange, with correlation headers even when the body is oversized."""

    status: int = 0
    headers: httpx.Headers = field(default_factory=httpx.Headers)
    body: bytes = b""


class ProviderRequestError(Exception):
    """A provider request without a usable body."""

    def __init__(self, kind: FailureKind, message: str, response: ProviderResponse | None = None) -> None:
        super().__init__(message)
        self.kind = kind
        self.response = response or ProviderResponse()


def failure(kind: FailureKind, operation: str, message: str) -> Failure:
    """Create a provider-safe failure for one Calendar operation."""
    return Failure(kind, "google-calendar", operation, message)


def path_escape(value: str) -> str:
    """Match Go url.PathEscape for Calendar identifiers."""
    return quote(value, safe="-._~$&+=:@")


def query_encode(values: dict[str, str]) -> str:
    """Encode sorted query pairs as Go url.Values.Encode does."""
    return "&".join(
        f"{quote_plus(key, safe='')}={quote_plus(value, safe='')}"
        for key, value in sorted(values.items())
    )


def calendar_path(calendar_id: str) -> str:
    """Return one escaped Calendar API resource path."""
    return "/calendars/" + path_escape(calendar_id)


def event_path(calendar_id: str, event_id: str) -> str:
    """Return one escaped event resource path."""
    return calendar_path(calendar_id) + "/events/" + path_escape(event_id)


def classify_status(response: ProviderResponse) -> tuple[bool, FailureKind, str, float]:
    """Classify a non-success response using Go's Calendar status policy."""
    reason = ""
    try:
        envelope = json.loads(response.body)
        if isinstance(envelope, dict) and isinstance(envelope.get("error"), dict):
            details = envelope["error"].get("errors", [])
            if isinstance(details, list):
                for detail in details:
                    if isinstance(detail, dict) and detail.get("reason") in _REASONS:
                        reason = detail["reason"]
                        break
    except (ValueError, UnicodeDecodeError, TypeError):
        pass
    suffix = f" ({reason})" if reason else ""
    status = response.status
    if status == 429 or status == 403 and reason in ("rateLimitExceeded", "userRateLimitExceeded"):
        category = FailureKind.RATE_LIMIT
        message = "provider rate limited the request" + suffix
    elif status == 408 or status >= 500:
        category = FailureKind.AVAILABILITY
        message = "provider is temporarily unavailable" + suffix
    else:
        if reason in ("quotaExceeded", "dailyLimitExceeded"):
            return False, FailureKind.QUOTA_EXHAUSTED, "provider quota is exhausted" + suffix, 0
        kinds = {
            400: FailureKind.VALIDATION,
            401: FailureKind.AUTHENTICATION,
            403: FailureKind.AUTHORIZATION,
            404: FailureKind.NOT_FOUND,
            410: FailureKind.NOT_FOUND,
            409: FailureKind.CONFLICT,
            412: FailureKind.CONFLICT,
        }
        return (
            False,
            kinds.get(status, FailureKind.PROVIDER_REJECTION),
            f"provider rejected the request with HTTP {status}" + suffix,
            0,
        )
    retry_header = response.headers.get("Retry-After", "")
    if retry_header:
        try:
            if not re.fullmatch(r"[+-]?[0-9]+", retry_header):
                raise ValueError
            delay = int(retry_header)
            if delay < 0 or delay > 2**63 - 1:
                raise ValueError
        except ValueError:
            return True, FailureKind.PROTOCOL, "provider returned an invalid Retry-After header", 0
        return True, category, message, parse_retry_after(retry_header, datetime.now(UTC))
    return True, category, message, 0


class Client:
    """Own a bounded Calendar transport and resolve credentials for each call."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Google Calendar configuration and credentials are required")
        self.config = Config(
            config.endpoint or Config().endpoint, config.max_response_bytes or Config().max_response_bytes
        )
        parsed = urlsplit(self.config.endpoint)
        if not parsed.scheme or not parsed.hostname:
            raise ValueError("Google Calendar endpoint must be absolute")
        if parsed.scheme != "https" and parsed.hostname not in ("localhost", "127.0.0.1"):
            raise ValueError("Google Calendar endpoint must use HTTPS")
        if self.config.max_response_bytes < 1:
            raise ValueError("Google Calendar response limit must be positive")
        self.endpoint = self.config.endpoint.rstrip("/")
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(25)
        self._owns_http_client = http_client is None

    async def aclose(self) -> None:
        """Close the owned provider transport."""
        if self._owns_http_client:
            await self.http_client.aclose()

    def list_events(self) -> "ListEventsOperation":
        """Return the event-listing Query."""
        from .operations import ListEventsOperation

        return ListEventsOperation(self)

    def get_event(self) -> "GetEventOperation":
        """Return the event-read Query."""
        from .operations import GetEventOperation

        return GetEventOperation(self)

    def create_event(self) -> "CreateEventOperation":
        """Return the idempotent event-create Mutation."""
        from .operations import CreateEventOperation

        return CreateEventOperation(self)

    def update_event(self) -> "UpdateEventOperation":
        """Return the idempotent event-update Mutation."""
        from .operations import UpdateEventOperation

        return UpdateEventOperation(self)

    def query_free_busy(self) -> "QueryFreeBusyOperation":
        """Return the availability Query."""
        from .operations import QueryFreeBusyOperation

        return QueryFreeBusyOperation(self)

    async def resolve(self, call: Call, operation: str) -> Credentials | Failure:
        """Resolve the generated OAuth source and validate the bearer token."""
        try:
            result = await self.credentials.resolve(call)
            if (
                not isinstance(result, Credentials)
                or result.access_token is None
                or not result.access_token.reveal()
                or result.auth_method_id not in ("", "google-oauth", "workspace-domain-delegation")
            ):
                raise ValueError("missing bearer token")
            return result
        except Exception:
            return failure(FailureKind.AUTHENTICATION, operation, "connection credentials are unavailable")

    async def send(
        self,
        call: Call,
        credentials: Credentials,
        method: str,
        path: str,
        query: dict[str, str] | None = None,
        payload: dict[str, Any] | None = None,
        if_match: str = "",
    ) -> ProviderResponse:
        """Send one authenticated Calendar request and refresh once after 401."""
        encoded_query = query_encode(query or {})
        target = self.endpoint + path + ("?" + encoded_query if encoded_query else "")
        data = None
        if payload is not None:
            try:
                data = json.dumps(payload, separators=(",", ":"), ensure_ascii=False).encode()
            except (TypeError, ValueError) as error:
                raise ProviderRequestError(
                    FailureKind.LOCAL_DEFECT, "provider request could not be encoded"
                ) from error
        for index in range(2):
            assert credentials.access_token is not None
            headers = {
                "Authorization": "Bearer " + credentials.access_token.reveal(),
                "Accept": "application/json",
            }
            if data is not None:
                headers["Content-Type"] = "application/json"
            if if_match:
                headers["If-Match"] = if_match
            try:
                request = self.http_client.build_request(
                    method, target, headers=headers, content=data, timeout=25
                )
                response, _ = await send_traced(self.http_client, request)
                result = ProviderResponse(response.status_code, response.headers)
                try:
                    body = await read_bounded_body(response, self.config.max_response_bytes)
                except BodyTooLargeError as error:
                    raise ProviderRequestError(
                        FailureKind.RESPONSE_TOO_LARGE, "provider response exceeds configured limit", result
                    ) from error
                result = ProviderResponse(response.status_code, response.headers, body)
            except ProviderRequestError:
                raise
            except (ProviderTransportError, httpx.HTTPError, ValueError) as error:
                raise ProviderRequestError(FailureKind.TRANSPORT, "provider request failed") from error
            if result.status != 401 or index == 1 or not hasattr(self.credentials, "resolve_after_rejection"):
                return result
            try:
                replacement = await self.credentials.resolve_after_rejection(call)
                if (
                    not isinstance(replacement, Credentials)
                    or replacement.access_token is None
                    or not replacement.access_token.reveal()
                ):
                    return result
                credentials = replacement
            except Exception:
                return result
        raise ProviderRequestError(FailureKind.LOCAL_DEFECT, "authenticated request retry was exhausted")

    def receipt(self, call: Call, response: ProviderResponse, object_id: str = "") -> Receipt:
        """Retain exactly the provider IDs and request correlation Go records."""
        request_id = response.headers.get("X-Goog-Request-Id") or response.headers.get("X-Request-Id", "")
        return Receipt(
            call.id, "google-calendar", datetime.now(UTC), call.idempotency_key, object_id, request_id
        )


from .operations import (  # noqa: E402
    CreateEventOperation,
    GetEventOperation,
    ListEventsOperation,
    QueryFreeBusyOperation,
    UpdateEventOperation,
)
