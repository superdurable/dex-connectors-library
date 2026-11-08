# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Amplitude user search with Go-compatible outcomes."""

from __future__ import annotations

import base64
import json
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any
from urllib.parse import urlencode

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
from ._generated.definitions import SEARCH_USERS_DEFINITION
from .models import SearchUsersInput, UserSearchPage


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One bounded provider response or request failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "amplitude", "searchUsers", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[UserSearchPage]:
    return QueryAttempt.branch_result(branch, UserSearchPage(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[UserSearchPage]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _valid_user_search(user: object) -> bool:
    if not isinstance(user, str) or not user or user.strip() != user:
        return False
    try:
        byte_count = len(user.encode("utf-8"))
    except UnicodeEncodeError:
        return False
    return byte_count <= 256 and all(ord(character) >= 0x20 and ord(character) != 0x7F for character in user)


def _decode_users(body: bytes, user: str) -> UserSearchPage:
    raw: Any = json.loads(body)
    if (
        not isinstance(raw, dict)
        or not isinstance(raw.get("matches"), list)
        or not isinstance(raw.get("type"), str)
        or not raw["type"]
    ):
        raise ValueError("user search page is missing")
    user_ids: list[str] = []
    for match in raw["matches"]:
        if not isinstance(match, dict) or not isinstance(match.get("user_id"), str) or not match["user_id"]:
            raise ValueError("matched user ID is missing")
        user_ids.append(match["user_id"])
    return UserSearchPage(user, tuple(user_ids), raw["type"])


class Client:
    """Resolve project keys per call and search users."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Amplitude configuration and credential source are required")
        if (
            config.timeout_seconds <= 0
            or type(config.max_response_bytes) is not int
            or config.max_response_bytes < 1
        ):
            raise ValueError("Amplitude request limits must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def search_users(self) -> SearchUsersOperation:
        """Bind the searchUsers Query to this client."""
        return SearchUsersOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _get(self, call: Call, user: str) -> _Exchange:
        try:
            credential = await self.credentials.resolve(call)
            api_key = credential.api_key.reveal()
            secret_key = credential.secret_key.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return _Exchange(error="credential")
        if (
            not is_header_safe_credential(api_key)
            or not is_header_safe_credential(secret_key)
            or ":" in api_key
        ):
            return _Exchange(error="credential")
        authorization = base64.b64encode(f"{api_key}:{secret_key}".encode()).decode("ascii")
        try:
            request = self.http_client.build_request(
                "GET",
                self.base_url + "/api/2/usersearch?" + urlencode({"user": user}),
                headers={"Accept": "application/json", "Authorization": "Basic " + authorization},
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


class SearchUsersOperation:
    """Search project users by Amplitude or external identity."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query worker."""
        return SEARCH_USERS_DEFINITION

    async def invoke(self, call: Call, input: SearchUsersInput) -> QueryAttempt[UserSearchPage]:
        """Classify user search outcomes consistently with Go."""
        if not isinstance(input, SearchUsersInput) or not _valid_user_search(input.user):
            return _branch("defect", FailureKind.VALIDATION, "user must contain 1–256 printable characters")
        exchange = await self.client._get(call, input.user)
        if exchange.error == "credential":
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Amplitude credentials are unavailable or invalid"
            )
        if exchange.error == "request":
            return _retry(FailureKind.TRANSPORT, "Amplitude request failed")
        if exchange.status == 429:
            return _retry(FailureKind.RATE_LIMIT, "Amplitude rate limited the request", exchange.retry_after)
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(
                FailureKind.AVAILABILITY, "Amplitude is temporarily unavailable", exchange.retry_after
            )
        if exchange.status != 200:
            return _branch(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                f"Amplitude rejected HTTP {exchange.status}",
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "Amplitude response exceeded the configured limit",
            )
        if exchange.error:
            return _retry(FailureKind.TRANSPORT, "Amplitude response was interrupted")
        try:
            page = _decode_users(exchange.body, input.user)
        except (ValueError, TypeError, UnicodeDecodeError):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Amplitude returned invalid user-search JSON"
            )
        return QueryAttempt.branch_result("searched", page, Receipt())
