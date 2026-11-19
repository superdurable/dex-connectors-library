# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Polyv signed channel list and provider decisions."""

from __future__ import annotations

import hashlib
import json
from collections.abc import Callable
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
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import LIST_CHANNELS_DEFINITION
from .models import ListChannelsInput, ListChannelsOutput


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListChannelsOutput]:
    return QueryAttempt.branch_result(
        branch, ListChannelsOutput(), Receipt(), Failure(kind, "polyv", "listChannels", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ListChannelsOutput]:
    return QueryAttempt.retry(Failure(kind, "polyv", "listChannels", message), delay)


def _valid(value: str) -> bool:
    return (
        bool(value) and len(value) <= 512 and value.strip() == value and all(32 < ord(c) < 127 for c in value)
    )


class Client:
    """Own Polyv settings and resolve credentials per request."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        *,
        clock: Callable[[], datetime] | None = None,
    ) -> None:
        if (
            config is None
            or credentials is None
            or config.timeout_seconds <= 0
            or config.max_response_bytes <= 0
        ):
            raise ValueError("valid Polyv configuration and credentials are required")
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None
        self.clock = clock or (lambda: datetime.now(UTC))

    def list_channels(self) -> ListChannelsOperation:
        """Bind the channel list Query to this client."""
        return ListChannelsOperation(self)

    async def aclose(self) -> None:
        """Close the owned transport."""
        if self._owns_client:
            await self.http_client.aclose()


class ListChannelsOperation:
    """Read public channel IDs from the Polyv account."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Polyv client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query identity and retry policy."""
        return LIST_CHANNELS_DEFINITION

    async def invoke(self, call: Call, input: ListChannelsInput) -> QueryAttempt[ListChannelsOutput]:
        """Sign one request and classify its observed outcome."""
        try:
            credentials = await self.client.credentials.resolve(call)
            app_id, secret = credentials.app_id, credentials.app_secret.reveal()
            if not _valid(app_id) or not _valid(secret):
                raise ValueError("invalid credentials")
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch("defect", FailureKind.AUTHENTICATION, "Polyv credentials are invalid")
        timestamp = str(int(self.client.clock().timestamp() * 1000))
        signature = (
            hashlib.md5(f"{secret}appId{app_id}timestamp{timestamp}{secret}".encode()).hexdigest().upper()
        )
        try:
            request = self.client.http_client.build_request(
                "GET",
                "https://api.polyv.net/live/v3/user/channels",
                params={"appId": app_id, "timestamp": timestamp, "sign": signature},
                headers={"Accept": "application/json"},
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "Polyv request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "Polyv request failed")
        try:
            status = response.status_code
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Polyv rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Polyv is unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse", FailureKind.RESPONSE_TOO_LARGE, "Polyv response exceeded the limit"
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Polyv response was interrupted")
        finally:
            await response.aclose()
        if status != 200:
            kind = (
                FailureKind.AUTHENTICATION
                if status == 401
                else FailureKind.AUTHORIZATION
                if status == 403
                else FailureKind.PROVIDER_REJECTION
            )
            return _branch("providerRejected", kind, f"Polyv rejected HTTP {status}")
        try:
            wire: Any = json.loads(body)
        except (ValueError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Polyv returned invalid JSON")
        if not isinstance(wire, dict) or type(wire.get("code")) is not int:
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Polyv omitted a valid code")
        if wire["code"] != 200:
            message = wire.get("message")
            if message == "qps exceeds number of calls":
                return _retry(FailureKind.RATE_LIMIT, "Polyv rate limited the request", delay)
            kind = (
                FailureKind.AUTHENTICATION
                if message in ("invalid signature.", "need to login")
                else FailureKind.AUTHORIZATION
                if message == "access forbidden"
                else FailureKind.PROVIDER_REJECTION
            )
            return _branch("providerRejected", kind, "Polyv rejected the signed request")
        data = wire.get("data")
        channels = data.get("channels") if isinstance(data, dict) else None
        if not isinstance(channels, list):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Polyv omitted channels")
        identifiers: list[str] = []
        for channel in channels:
            identifier = str(channel) if type(channel) in (int, str) else ""
            if (
                not identifier
                or len(identifier) > 64
                or not identifier.isascii()
                or not identifier.isdecimal()
            ):
                return _branch("invalidResponse", FailureKind.PROTOCOL, "Polyv returned invalid channel ID")
            identifiers.append(identifier)
        return QueryAttempt.branch_result(
            "listed", ListChannelsOutput(channel_ids=tuple(identifiers)), Receipt()
        )
