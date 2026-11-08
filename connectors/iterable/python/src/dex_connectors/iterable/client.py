# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Iterable user reads with Go-compatible outcomes."""

from __future__ import annotations

import json
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any
from urllib.parse import quote

import httpx
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    QueryAttempt,
    QueryDefinition,
    Receipt,
)
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
from ._generated.definitions import GET_USER_DEFINITION
from .models import GetUserInput, UserIdentity


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One bounded provider response or request failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "iterable", "getUser", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[UserIdentity]:
    return QueryAttempt.branch_result(branch, UserIdentity(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[UserIdentity]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _valid_email(email: object) -> bool:
    return (
        isinstance(email, str)
        and bool(email)
        and "@" in email
        and len(email.encode("utf-8")) <= 256
        and not any(
            ord(char) < 32 or 127 <= ord(char) <= 159 or char.isspace() or char in "/?#" for char in email
        )
    )


def _decode_user(body: bytes, requested_email: str) -> UserIdentity:
    raw: Any = json.loads(body)
    if not isinstance(raw, dict) or not isinstance(raw.get("user"), dict):
        raise ValueError("user object missing")
    user = raw["user"]
    email = user.get("email")
    user_id = user.get("userId", "")
    if not _valid_email(email) or email.lower() != requested_email.lower() or not isinstance(user_id, str):
        raise ValueError("user identity invalid")
    return UserIdentity(email, user_id)


class Client:
    """Resolve an API key per call and read selected user metadata."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Iterable configuration and credential source are required")
        if (
            config.timeout_seconds <= 0
            or type(config.max_response_bytes) is not int
            or config.max_response_bytes < 1
        ):
            raise ValueError("Iterable request limits must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_user(self) -> GetUserOperation:
        """Bind the getUser Query to this client."""
        return GetUserOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _get(self, call: Call, email: str) -> _Exchange:
        try:
            credential = await self.credentials.resolve(call)
            api_key = credential.api_key.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return _Exchange(error="credential")
        if not is_header_safe_credential(api_key):
            return _Exchange(error="credential")
        try:
            request = self.http_client.build_request(
                "GET",
                self.base_url + "/users/" + quote(email, safe=""),
                headers={"Accept": "application/json", "Api-Key": api_key},
            )
            response, _ = await send_traced(self.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _Exchange(error="request")
        try:
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _Exchange(status=response.status_code, retry_after=retry_after, error="too_large")
            except ProviderTransportError:
                return _Exchange(status=response.status_code, retry_after=retry_after, error="interrupted")
            return _Exchange(status=response.status_code, body=body, retry_after=retry_after)
        finally:
            await response.aclose()


class GetUserOperation:
    """Read one user's ID, name, and status."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query contract."""
        return GET_USER_DEFINITION

    async def invoke(self, call: Call, input: GetUserInput) -> QueryAttempt[UserIdentity]:
        """Classify user lookup outcomes consistently with Go."""
        if not isinstance(input, GetUserInput) or not _valid_email(input.email):
            return _branch(
                "defect", FailureKind.VALIDATION, "email must be 1–256 printable characters with @"
            )
        exchange = await self.client._get(call, input.email)
        if exchange.error == "credential":
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Iterable credentials are unavailable or invalid"
            )
        if exchange.error == "request":
            return _retry(FailureKind.TRANSPORT, "Iterable request failed")
        if exchange.status == 429:
            return _retry(FailureKind.RATE_LIMIT, "Iterable rate limited the request", exchange.retry_after)
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(
                FailureKind.AVAILABILITY, "Iterable is temporarily unavailable", exchange.retry_after
            )
        if exchange.status == 404:
            return _branch("notFound", FailureKind.NOT_FOUND, "Iterable did not expose the user")
        if exchange.status != 200:
            return _branch(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                f"Iterable rejected HTTP {exchange.status}",
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "Iterable response exceeded the configured limit",
            )
        if exchange.error:
            return _retry(FailureKind.TRANSPORT, "Iterable response was interrupted")
        try:
            user_identity = _decode_user(exchange.body, input.email)
        except (ValueError, TypeError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Iterable returned invalid user JSON")
        return QueryAttempt.branch_result("found", user_identity, Receipt())
