# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Braze profile exports with Go-compatible outcomes."""

from __future__ import annotations

import json
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any

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
from ._generated.definitions import GET_USER_PROFILE_DEFINITION
from .models import GetUserProfileInput, UserProfile

FIELDS_TO_EXPORT = ("external_id", "braze_id", "first_name", "last_name", "email")


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One bounded provider response or request failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "braze", "getUserProfile", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[UserProfile]:
    return QueryAttempt.branch_result(branch, UserProfile(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[UserProfile]:
    return QueryAttempt.retry(_failure(kind, message), delay)


class Client:
    """Resolve one REST API key per call and read selected user profile fields."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Braze configuration and credential source are required")
        if (
            config.timeout_seconds <= 0
            or type(config.max_response_bytes) is not int
            or config.max_response_bytes < 1
        ):
            raise ValueError("Braze request limits must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_user_profile(self) -> GetUserProfileOperation:
        """Bind the getUserProfile Query to this client."""
        return GetUserProfileOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _post(self, call: Call, external_id: str) -> _Exchange:
        try:
            credential = await self.credentials.resolve(call)
            token = credential.rest_api_key.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return _Exchange(error="credential")
        if not is_header_safe_credential(token):
            return _Exchange(error="credential")
        try:
            request = self.http_client.build_request(
                "POST",
                self.base_url + "/users/export/ids",
                headers={
                    "Accept": "application/json",
                    "Content-Type": "application/json",
                    "Authorization": "Bearer " + token,
                },
                content=json.dumps(
                    {"external_ids": [external_id], "fields_to_export": FIELDS_TO_EXPORT},
                    separators=(",", ":"),
                ),
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


class GetUserProfileOperation:
    """Read one Braze user's selected identity and contact fields."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query contract."""
        return GET_USER_PROFILE_DEFINITION

    async def invoke(self, call: Call, input: GetUserProfileInput) -> QueryAttempt[UserProfile]:
        """Classify profile exports the same way as the Go implementation."""
        if (
            not isinstance(input, GetUserProfileInput)
            or not isinstance(input.external_id, str)
            or not input.external_id.strip()
            or any(0xD800 <= ord(character) <= 0xDFFF for character in input.external_id)
            or len(input.external_id.encode("utf-8")) > 987
        ):
            return _branch(
                "defect",
                FailureKind.VALIDATION,
                "external ID must be a nonempty UTF-8 value of at most 987 bytes",
            )
        exchange = await self.client._post(call, input.external_id)
        if exchange.error == "credential":
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Braze credentials are unavailable or invalid"
            )
        if exchange.error == "request":
            return _retry(FailureKind.TRANSPORT, "Braze profile request failed")
        if exchange.status == 429:
            return _retry(FailureKind.RATE_LIMIT, "Braze rate limited the request", exchange.retry_after)
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(FailureKind.AVAILABILITY, "Braze is temporarily unavailable", exchange.retry_after)
        if exchange.status not in (200, 201):
            return _branch(
                "providerRejected", FailureKind.PROVIDER_REJECTION, f"Braze rejected HTTP {exchange.status}"
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "Braze response exceeded the configured limit",
            )
        if exchange.error:
            return _retry(FailureKind.TRANSPORT, "Braze response was interrupted")
        try:
            raw: Any = json.loads(exchange.body)
            if (
                not isinstance(raw, dict)
                or raw.get("message") != "success"
                or not isinstance(raw.get("users"), list)
            ):
                raise ValueError("invalid profile envelope")
            users = raw["users"]
            if len(users) > 1:
                raise ValueError("unexpected profile count")
            if not users:
                return _branch("notFound", FailureKind.NOT_FOUND, "Braze did not find the user")
            user = users[0]
            if not isinstance(user, dict) or user.get("external_id") != input.external_id:
                raise ValueError("returned external ID differs")
            values = [user.get(field) for field in ("braze_id", "first_name", "last_name", "email")]
            if not all(value is None or isinstance(value, str) for value in values):
                raise ValueError("invalid profile field")
            profile = UserProfile(input.external_id, *(value or "" for value in values))
        except (ValueError, TypeError, UnicodeDecodeError):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Braze returned an invalid profile response"
            )
        return QueryAttempt.branch_result("found", profile, Receipt())
