# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Google Sheets HTTP client; OAuth refresh is manifest-generated."""

import json
from collections.abc import Callable, Mapping
from dataclasses import dataclass, field
from datetime import UTC, datetime
from urllib.parse import quote, urlsplit

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
    read_bounded_body,
    send_traced,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials

_PATH_SAFE = "-._~$&+=:@"
_QUERY_SAFE = ""


def escape_path(value: str) -> str:
    """Escape a single path segment with Go url.PathEscape's safe set."""
    return quote(value, safe=_PATH_SAFE)


def encode_query(values: dict[str, str]) -> str:
    """Encode sorted Go url.Values with QueryEscape's handling of tilde."""
    return "&".join(
        f"{quote(key, safe=_QUERY_SAFE).replace('%20', '+').replace('~', '%7E')}="
        f"{quote(values[key], safe=_QUERY_SAFE).replace('%20', '+').replace('~', '%7E')}"
        for key in sorted(values)
    )


@dataclass(frozen=True, slots=True)
class ProviderResponse:
    """Status and bounded body from one Google Sheets exchange."""

    status: int = 0
    headers: httpx.Headers = field(default_factory=httpx.Headers)
    body: bytes = b""
    too_large: bool = False


class RequestError(Exception):
    """Classified transport or local request failure without secret details."""

    def __init__(self, kind: FailureKind, message: str, *, not_sent: bool = False) -> None:
        super().__init__(message)
        self.kind = kind
        self.not_sent = not_sent


class Client:
    """Send Google Sheets requests using a call-time credential provider."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        now: Callable[[], datetime] | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Google Sheets configuration and credential provider are required")
        config = Config(
            config.endpoint or Config().endpoint,
            config.max_response_bytes or Config().max_response_bytes,
            config.max_rows or Config().max_rows,
        )
        endpoint = urlsplit(config.endpoint)
        if not endpoint.scheme or not endpoint.hostname:
            raise ValueError("Google Sheets endpoint must be absolute")
        if endpoint.scheme != "https" and endpoint.hostname not in ("localhost", "127.0.0.1"):
            raise ValueError("Google Sheets endpoint must use HTTPS")
        if config.max_response_bytes < 1 or config.max_rows < 2:
            raise ValueError("Google Sheets response and row limits must be positive")
        self.config = config
        self.endpoint = config.endpoint.rstrip("/")
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(25)
        self.now = now or (lambda: datetime.now(UTC))

    def get_values(self):
        """Return the bounded range Query."""
        from .operations import GetValuesOperation

        return GetValuesOperation(self)

    def find_row(self):
        """Return the stable-key row Query."""
        from .operations import FindRowOperation

        return FindRowOperation(self)

    def upsert_row(self):
        """Return the fenced row Mutation."""
        from .operations import UpsertRowOperation

        return UpsertRowOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider transport."""
        await self.http_client.aclose()

    async def resolve(self, call: Call, operation: str) -> tuple[Credentials | None, Failure | None, bool]:
        """Resolve generated OAuth credentials and classify terminal revocation."""
        try:
            credential = await self.credentials.resolve(call)
            if not isinstance(credential, Credentials) or not credential.access_token.reveal():
                raise ValueError("missing access token")
            return credential, None, False
        except Exception as error:
            terminal = isinstance(error, ReauthorizationRequiredError)
            kind = FailureKind.AUTHENTICATION if terminal else FailureKind.AVAILABILITY
            message = (
                "connection credentials are unavailable; reconnect the connection"
                if terminal
                else "credential resolution is temporarily unavailable"
            )
            return None, failure(operation, kind, message), terminal

    async def refresh_after_rejection(
        self, call: Call, rejected: Credentials
    ) -> tuple[Credentials | None, bool]:
        """Use the generated source's rejection refresh; return whether reauthorization is needed."""
        if not hasattr(self.credentials, "resolve_after_rejection"):
            return None, True
        try:
            credential = await self.credentials.resolve_after_rejection(call)
            if (
                not credential.access_token.reveal()
                or credential.access_token.reveal() == rejected.access_token.reveal()
            ):
                return None, True
            return credential, False
        except Exception as error:
            return None, isinstance(error, ReauthorizationRequiredError)

    async def request(
        self,
        call: Call,
        credential: Credentials,
        method: str,
        spreadsheet_id: str,
        suffix: str,
        query: dict[str, str],
        payload: Mapping[str, object] | None = None,
        *,
        refresh: bool = True,
        write: bool = False,
    ) -> tuple[ProviderResponse, Credentials]:
        """Send one request, with one rejection refresh only for repeatable reads."""
        target = f"{self.endpoint}/spreadsheets/{escape_path(spreadsheet_id)}{suffix}"
        if query:
            target += "?" + encode_query(query)
        for attempt in range(2 if refresh else 1):
            headers = {"Authorization": f"Bearer {credential.access_token.reveal()}"}
            if payload is not None:
                headers["Content-Type"] = "application/json"
            if write:
                headers["X-Idempotency-Key"] = str(call.idempotency_key)
            try:
                encoded = (
                    json.dumps(payload, separators=(",", ":"), ensure_ascii=False).encode()
                    if payload is not None
                    else None
                )
                request = self.http_client.build_request(method, target, headers=headers, content=encoded)
            except Exception as error:
                raise RequestError(
                    FailureKind.LOCAL_DEFECT, "provider request could not be built", not_sent=True
                ) from error
            try:
                response, trace = await send_traced(self.http_client, request)
                status, response_headers = response.status_code, response.headers
                try:
                    body = await read_bounded_body(response, self.config.max_response_bytes)
                    result = ProviderResponse(status, response_headers, body)
                except BodyTooLargeError:
                    result = ProviderResponse(status, response_headers, too_large=True)
            except ProviderTransportError as error:
                raise RequestError(
                    FailureKind.TRANSPORT, "provider request failed", not_sent=error.provably_not_sent
                ) from error
            if (
                result.status == 401
                and not result.too_large
                and attempt == 0
                and refresh
                and hasattr(self.credentials, "resolve_after_rejection")
            ):
                replacement, _ = await self.refresh_after_rejection(call, credential)
                if replacement is not None:
                    credential = replacement
                    continue
            return result, credential
        raise RequestError(FailureKind.LOCAL_DEFECT, "authenticated request retry was exhausted")

    def receipt(self, call: Call, response: ProviderResponse | None = None, object_id: str = "") -> Receipt:
        """Capture provider IDs without storing response bodies or credentials."""
        headers = response.headers if response is not None else httpx.Headers()
        request_id = headers.get("X-Goog-Request-Id") or headers.get("X-Request-Id", "")
        return Receipt(
            call.id, "google-sheets", self.now().astimezone(UTC), call.idempotency_key, object_id, request_id
        )


def failure(operation: str, kind: FailureKind, message: str) -> Failure:
    """Build a secret-safe Google Sheets failure."""
    return Failure(kind, "google-sheets", operation, message)
