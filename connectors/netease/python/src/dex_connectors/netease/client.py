# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""NetEase Yunxin signed profile Query and provider decisions."""

from __future__ import annotations

import hashlib
import json
import os
from collections.abc import Callable
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
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import GET_USER_PROFILE_DEFINITION
from .models import GetUserProfileInput, GetUserProfileOutput


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[GetUserProfileOutput]:
    return QueryAttempt.branch_result(
        branch, GetUserProfileOutput(), Receipt(), Failure(kind, "netease", "getUserProfile", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[GetUserProfileOutput]:
    return QueryAttempt.retry(Failure(kind, "netease", "getUserProfile", message), delay)


def _valid_credential(value: str) -> bool:
    return (
        bool(value) and len(value) <= 512 and value.strip() == value and all(32 < ord(c) < 127 for c in value)
    )


class Client:
    """Own Yunxin settings and resolve signed credentials on every attempt."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        *,
        clock: Callable[[], datetime] | None = None,
        nonce_source: Callable[[int], bytes] | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Yunxin configuration and credential source are required")
        if config.timeout_seconds <= 0 or config.max_response_bytes <= 0:
            raise ValueError("Yunxin timeout and response limit must be positive")
        self.base_url = validate_base_url(config.base_url)
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None
        self.clock = clock or (lambda: datetime.now(UTC))
        self.nonce_source = nonce_source or os.urandom

    def get_user_profile(self) -> GetUserProfileOperation:
        """Bind the user-profile Query to this client."""
        return GetUserProfileOperation(self)

    async def aclose(self) -> None:
        """Close the transport if this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class GetUserProfileOperation:
    """Read a Yunxin account nickname without private contact fields."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Yunxin client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query identity and retry policy."""
        return GET_USER_PROFILE_DEFINITION

    async def invoke(self, call: Call, input: GetUserProfileInput) -> QueryAttempt[GetUserProfileOutput]:
        """Sign one provider request and classify the observed outcome."""
        account_id = input.account_id if isinstance(input, GetUserProfileInput) else ""
        if (
            not account_id
            or len(account_id) > 128
            or account_id.strip() != account_id
            or any(character in account_id for character in "/?#")
        ):
            return _branch("defect", FailureKind.VALIDATION, "account ID is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
            app_key, app_secret = credentials.app_key.reveal(), credentials.app_secret.reveal()
            if not _valid_credential(app_key) or not _valid_credential(app_secret):
                raise ValueError("invalid credentials")
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
            )
        try:
            nonce_bytes = self.client.nonce_source(16)
            if len(nonce_bytes) != 16:
                raise ValueError("invalid nonce")
        except (ValueError, OSError):
            return _retry(FailureKind.AVAILABILITY, "request nonce unavailable")
        nonce = nonce_bytes.hex()
        current_time = str(int(self.client.clock().timestamp()))
        checksum = hashlib.sha1((app_secret + nonce + current_time).encode()).hexdigest()
        try:
            request = self.client.http_client.build_request(
                "GET",
                f"{self.client.base_url}/im/v2/users/{quote(account_id, safe='')}",
                headers={
                    "AppKey": app_key,
                    "Nonce": nonce,
                    "CurTime": current_time,
                    "CheckSum": checksum,
                    "Accept": "application/json",
                    "Content-Type": "application/json",
                },
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "profile request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "Yunxin request failed")
        try:
            status = response.status_code
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Yunxin rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Yunxin is temporarily unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Yunxin response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Yunxin response was interrupted")
        finally:
            await response.aclose()
        try:
            wire: Any = json.loads(body)
        except (ValueError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Yunxin returned an invalid response")
        if not isinstance(wire, dict) or type(wire.get("code")) is not int or wire["code"] == 0:
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Yunxin returned an invalid response")
        code = wire["code"]
        if code == 200:
            data = wire.get("data")
            if (
                status != 200
                or not isinstance(data, dict)
                or (data.get("account_id", "") not in ("", account_id))
                or not isinstance(data.get("name", ""), str)
            ):
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "Yunxin returned an inconsistent profile"
                )
            return QueryAttempt.branch_result(
                "fetched", GetUserProfileOutput(account_id=account_id, name=data.get("name", "")), Receipt()
            )
        if code in (102404, 103404):
            return _branch(
                "notFound", FailureKind.PROVIDER_REJECTION, "Yunxin account or profile does not exist"
            )
        if code == 500:
            return _retry(FailureKind.AVAILABILITY, "Yunxin is temporarily unavailable", delay)
        if code == 429:
            return _retry(FailureKind.RATE_LIMIT, "Yunxin rate limited the request", delay)
        if code == 414:
            return _branch(
                "providerRejected",
                FailureKind.AUTHENTICATION,
                "Yunxin rejected authentication or request parameters",
            )
        return _branch(
            "providerRejected", FailureKind.PROVIDER_REJECTION, "Yunxin rejected the profile request"
        )
