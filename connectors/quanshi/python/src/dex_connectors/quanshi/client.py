# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Quanshi meeting-room Query and short-lived token transport."""

import hashlib
import json
import os
from collections.abc import Callable
from datetime import UTC, datetime
from urllib.parse import urlsplit

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
from ._generated.definitions import LIST_ROOMS_DEFINITION
from .models import ListRoomsInput, ListRoomsOutput, RoomSummary


def _valid_credential(value: str) -> bool:
    return (
        isinstance(value, str) and 0 < len(value) <= 512 and all(0x21 <= ord(char) <= 0x7E for char in value)
    )


class Client:
    """Own configuration and resolve current Quanshi app credentials per Query."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        clock: Callable[[], datetime] | None = None,
        random_bytes: Callable[[int], bytes] | None = None,
    ) -> None:
        if (
            config is None
            or credentials is None
            or config.timeout_seconds <= 0
            or config.max_response_bytes <= 0
        ):
            raise ValueError("Quanshi configuration and credentials are required")
        origin = urlsplit(config.base_url)
        if (
            origin.scheme != "https"
            or origin.netloc not in ("openapi.quanshicloud.com", "openapi-beta.quanshicloud.com")
            or origin.path not in ("", "/")
            or origin.query
            or origin.fragment
            or origin.username
            or origin.password
        ):
            raise ValueError("Quanshi base URL must be a documented HTTPS OpenAPI origin")
        self.config = config
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_http_client = http_client is None
        self.clock = clock or (lambda: datetime.now(UTC))
        self.random_bytes = random_bytes or os.urandom

    def list_rooms(self) -> "ListRoomsOperation":
        """Bind the listRooms Query to this client."""
        return ListRoomsOperation(self)

    async def aclose(self) -> None:
        """Close only a transport owned by this client."""
        if self._owns_http_client:
            await self.http_client.aclose()


class ListRoomsOperation:
    """Read Quanshi meeting-room metadata for the current administrator."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query definition."""
        return LIST_ROOMS_DEFINITION

    def _branch(self, branch: str, kind: FailureKind) -> QueryAttempt[ListRoomsOutput]:
        return QueryAttempt.branch_result(
            branch,
            ListRoomsOutput(),
            Receipt(),
            Failure(kind, "quanshi", "listRooms", "Quanshi room query failed"),
        )

    def _retry(self, kind: FailureKind, delay: float = 0) -> QueryAttempt[ListRoomsOutput]:
        return QueryAttempt.retry(Failure(kind, "quanshi", "listRooms", "Quanshi request failed"), delay)

    def _provider_code(self, code: int) -> QueryAttempt[ListRoomsOutput]:
        kind = (
            FailureKind.AUTHENTICATION
            if code == 9004
            else FailureKind.AUTHORIZATION
            if code == 9020
            else FailureKind.PROVIDER_REJECTION
        )
        return self._branch("providerRejected", kind)

    async def _response(
        self, method: str, path: str, body: bytes, headers: dict[str, str]
    ) -> bytes | QueryAttempt[ListRoomsOutput]:
        request = self.client.http_client.build_request(
            method, self.client.config.base_url.rstrip("/") + path, content=body, headers=headers
        )
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return self._retry(FailureKind.AVAILABILITY)
        try:
            status = response.status_code
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return self._retry(FailureKind.RATE_LIMIT, delay)
            if status == 408 or status >= 500:
                return self._retry(FailureKind.AVAILABILITY, delay)
            try:
                data = await read_bounded_body(response, self.client.config.max_response_bytes)
            except BodyTooLargeError:
                return self._branch("invalidResponse", FailureKind.RESPONSE_TOO_LARGE)
            except ProviderTransportError:
                return self._retry(FailureKind.AVAILABILITY)
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return self._branch("providerRejected", kind)
            return data
        finally:
            await response.aclose()

    async def invoke(self, call: Call, input: ListRoomsInput) -> QueryAttempt[ListRoomsOutput]:
        """Acquire one short-lived token and list its administrator's rooms."""
        if not isinstance(input, ListRoomsInput):
            return self._branch("defect", FailureKind.VALIDATION)
        try:
            credentials = await self.client.credentials.resolve(call)
            app_id, app_secret = credentials.app_id.reveal(), credentials.app_secret.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        if not _valid_credential(app_id) or not _valid_credential(app_secret):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        try:
            nonce = self.client.random_bytes(16).hex()
        except (ValueError, OSError):
            return self._retry(FailureKind.AVAILABILITY)
        cur_time = str(int(self.client.clock().timestamp() * 1000))
        checksum = hashlib.sha1((app_secret + nonce + cur_time).encode()).hexdigest()
        token_result = await self._response(
            "POST",
            "/eventopenapi/token/v2/create",
            b"{}",
            {
                "Accept": "application/json",
                "Content-Type": "application/json",
                "AppId": app_id,
                "Nonce": nonce,
                "CurTime": cur_time,
                "CheckSum": checksum,
            },
        )
        if not isinstance(token_result, bytes):
            return token_result
        try:
            token_wire = json.loads(token_result)
        except (UnicodeDecodeError, ValueError):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        if (
            not isinstance(token_wire, dict)
            or not isinstance(token_wire.get("code"), int)
            or isinstance(token_wire["code"], bool)
        ):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        if token_wire["code"] != 200:
            return self._provider_code(token_wire["code"])
        token_data = token_wire.get("data")
        if not isinstance(token_data, dict):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        token, user_id = token_data.get("token"), token_data.get("userId")
        if (
            not isinstance(token, str)
            or not _valid_credential(token)
            or not isinstance(user_id, int)
            or isinstance(user_id, bool)
            or user_id <= 0
        ):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        room_result = await self._response(
            "POST",
            "/confopenapi/room/list",
            json.dumps({"userId": user_id}, separators=(",", ":")).encode(),
            {"Accept": "application/json", "Content-Type": "application/json", "Token": token},
        )
        if not isinstance(room_result, bytes):
            return room_result
        try:
            wire = json.loads(room_result)
        except (UnicodeDecodeError, ValueError):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        if (
            not isinstance(wire, dict)
            or not isinstance(wire.get("status"), int)
            or isinstance(wire["status"], bool)
        ):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        if wire["status"] != 0:
            return self._provider_code(wire["status"])
        result = wire.get("result")
        items = result.get("room") if isinstance(result, dict) else None
        if not isinstance(items, list) or len(items) > 1000:
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        rooms: list[RoomSummary] = []
        for item in items:
            if not isinstance(item, dict):
                return self._branch("invalidResponse", FailureKind.PROTOCOL)
            identifier, name, capacity, status = (
                item.get("roomId"),
                item.get("roomName"),
                item.get("roomCapacity"),
                item.get("roomStatus"),
            )
            if (
                not isinstance(identifier, int)
                or isinstance(identifier, bool)
                or identifier <= 0
                or not isinstance(name, str)
                or not name
                or len(name) > 512
                or not isinstance(capacity, int)
                or isinstance(capacity, bool)
                or capacity < 0
                or not isinstance(status, int)
                or isinstance(status, bool)
            ):
                return self._branch("invalidResponse", FailureKind.PROTOCOL)
            rooms.append(RoomSummary(identifier, name, capacity, status))
        return QueryAttempt.branch_result("listed", ListRoomsOutput(tuple(rooms)), Receipt())
