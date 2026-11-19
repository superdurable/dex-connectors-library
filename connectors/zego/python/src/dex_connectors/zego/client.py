# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""ZEGO signed room member-count Query."""

from __future__ import annotations

import hashlib
import json
import os
from collections.abc import Callable
from datetime import UTC, datetime

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
from ._generated.definitions import GET_ROOM_USER_COUNT_DEFINITION
from .models import GetRoomUserCountInput, GetRoomUserCountOutput

_ENDPOINT = "https://zim-api.zego.im/"
_ROOM_CHARACTERS = frozenset(
    "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!#$%&()+-:;<=>?@[]^_{}|~."
)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[GetRoomUserCountOutput]:
    return QueryAttempt.branch_result(branch, GetRoomUserCountOutput(), Receipt(),
                                      Failure(kind, "zego", "getRoomUserCount", message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[GetRoomUserCountOutput]:
    return QueryAttempt.retry(Failure(kind, "zego", "getRoomUserCount", message), delay)


def _valid_credentials(credentials: Credentials) -> bool:
    app_id = credentials.app_id
    secret = credentials.server_secret.reveal()
    return (app_id.isascii() and app_id.isdecimal() and 0 < int(app_id) <= 2**32 - 1
            and str(int(app_id)) == app_id and bool(secret) and len(secret.encode()) <= 1024
            and all(32 < ord(character) < 127 for character in secret))


class Client:
    """Own ZEGO request settings and resolve project credentials per attempt."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        *,
        clock: Callable[[], datetime] | None = None,
        nonce_source: Callable[[int], bytes] | None = None,
    ) -> None:
        if (config is None or credentials is None or config.timeout_seconds <= 0
                or not 0 < config.max_response_bytes <= 16 << 20):
            raise ValueError("ZEGO configuration and credential source are required")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None
        self.clock = clock or (lambda: datetime.now(UTC))
        self.nonce_source = nonce_source or os.urandom

    def get_room_user_count(self) -> GetRoomUserCountOperation:
        """Bind a room member-count Query to this client."""
        return GetRoomUserCountOperation(self)

    async def aclose(self) -> None:
        """Close the transport if this client owns it."""
        if self._owns_client:
            await self.http_client.aclose()


class GetRoomUserCountOperation:
    """Read the current count without exposing room member identities."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("ZEGO client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query identity and retry policy."""
        return GET_ROOM_USER_COUNT_DEFINITION

    async def invoke(self, call: Call, input: GetRoomUserCountInput) -> QueryAttempt[GetRoomUserCountOutput]:
        """Sign one request and classify the ZEGO response."""
        room_id = input.room_id if isinstance(input, GetRoomUserCountInput) else ""
        if not room_id or len(room_id.encode()) > 32 or any(char not in _ROOM_CHARACTERS for char in room_id):
            return _branch("defect", FailureKind.VALIDATION, "ZEGO RoomId is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
            if not _valid_credentials(credentials):
                raise ValueError("invalid credentials")
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch("defect", FailureKind.AUTHENTICATION,
                           "ZEGO project credentials are unavailable or invalid")
        try:
            nonce_bytes = self.client.nonce_source(8)
            if len(nonce_bytes) != 8:
                raise OSError("nonce source returned short value")
        except (OSError, ValueError):
            return _retry(FailureKind.AVAILABILITY, "ZEGO nonce generation failed")
        nonce = nonce_bytes.hex()
        timestamp = str(int(self.client.clock().timestamp()))
        signature = hashlib.md5((credentials.app_id + nonce + credentials.server_secret.reveal()
                                 + timestamp).encode()).hexdigest()
        try:
            request = self.client.http_client.build_request("GET", _ENDPOINT, params={
                "Action": "QueryRoomUserList", "AppId": credentials.app_id,
                "SignatureNonce": nonce, "Timestamp": timestamp,
                "SignatureVersion": "2.0", "Signature": signature,
                "RoomId": room_id, "Limit": "1",
            }, headers={"Accept": "application/json"})
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "ZEGO room-count request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "ZEGO room-count request failed")
        try:
            status = response.status_code
            delay = parse_retry_after(response.headers.get("Retry-After", ""), self.client.clock())
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "ZEGO rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "ZEGO room service is temporarily unavailable", delay)
            if status != 200:
                kind = {401: FailureKind.AUTHENTICATION, 403: FailureKind.AUTHORIZATION}.get(
                    status, FailureKind.PROVIDER_REJECTION)
                return _branch("providerRejected", kind, f"ZEGO rejected room query with HTTP {status}")
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch("invalidResponse", FailureKind.RESPONSE_TOO_LARGE,
                               "ZEGO response exceeded the configured limit")
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "ZEGO response was interrupted")
            try:
                wire = json.loads(body)
                code = wire["Code"]
                if type(code) is not int:
                    raise ValueError("invalid code")
            except (ValueError, TypeError, KeyError):
                return _branch("invalidResponse", FailureKind.PROTOCOL, "ZEGO returned invalid room data")
            if code == 660300006:
                return _retry(FailureKind.RATE_LIMIT, "ZEGO room query rate limit was reached")
            if code in (660300007, 100000004):
                return _retry(FailureKind.AVAILABILITY, "ZEGO room query is temporarily unavailable")
            if code != 0:
                return _branch("providerRejected", FailureKind.PROVIDER_REJECTION,
                               f"ZEGO rejected room query with code {code}")
            count = wire.get("Count")
            if type(count) is not int or count < 0:
                return _branch("invalidResponse", FailureKind.PROTOCOL,
                               "ZEGO returned incomplete room count")
            return QueryAttempt.branch_result("found", GetRoomUserCountOutput(count=count), Receipt())
        finally:
            await response.aclose()
