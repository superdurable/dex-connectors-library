# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one bounded page of Likelic notification rooms."""

import json
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
from ._generated.definitions import LIST_ROOMS_DEFINITION
from .models import ListRoomsInput, ListRoomsOutput, Room

_ENDPOINT = "https://agent.likelic.com/api/v1/rooms"


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListRoomsOutput]:
    return QueryAttempt.branch_result(
        branch, ListRoomsOutput(), Receipt(), Failure(kind, "likelic", "listRooms", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ListRoomsOutput]:
    return QueryAttempt.retry(Failure(kind, "likelic", "listRooms", message), delay)


class Client:
    """Resolve the latest API key and query Likelic over HTTPS."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if (
            config is None
            or credentials is None
            or config.timeout_seconds <= 0
            or not 0 < config.max_response_bytes <= 16 << 20
        ):
            raise ValueError("Likelic configuration and credentials are required")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_http_client = http_client is None

    def list_rooms(self) -> "ListRoomsOperation":
        """Bind the room-page Query."""
        return ListRoomsOperation(self)

    async def aclose(self) -> None:
        """Close a transport owned by this client."""
        if self._owns_http_client:
            await self.http_client.aclose()


class ListRoomsOperation:
    """Read room IDs and push states without personal metadata."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated room Query contract."""
        return LIST_ROOMS_DEFINITION

    async def invoke(self, call: Call, input: ListRoomsInput) -> QueryAttempt[ListRoomsOutput]:
        """Classify the provider's one-page room response."""
        if not isinstance(input, ListRoomsInput) or not 1 <= input.page <= 1_000_000:
            return _branch("defect", FailureKind.VALIDATION, "Likelic page is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
            customer_code = credentials.customer_code
            api_key = credentials.api_key.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch("defect", FailureKind.AUTHENTICATION, "Likelic credentials are unavailable")
        if (
            not customer_code
            or len(customer_code) > 50
            or any(not (char.isascii() and (char.isalnum() or char in "_-")) for char in customer_code)
            or len(api_key) != 51
            or not api_key.startswith("sk-")
            or any(char not in "0123456789abcdefABCDEF" for char in api_key[3:])
        ):
            return _branch("defect", FailureKind.AUTHENTICATION, "Likelic credentials are invalid")
        try:
            request = self.client.http_client.build_request(
                "GET",
                _ENDPOINT,
                params={"page": input.page, "limit": 20},
                headers={
                    "X-Customer-Code": customer_code,
                    "X-API-Key": api_key,
                    "Accept": "application/json",
                },
            )
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _retry(FailureKind.AVAILABILITY, "Likelic request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Likelic rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Likelic service is unavailable", delay)
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return _branch("providerRejected", kind, f"Likelic rejected HTTP {status}")
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse", FailureKind.RESPONSE_TOO_LARGE, "Likelic response is oversized"
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Likelic response was interrupted")
            try:
                wire: Any = json.loads(body)
                meta = wire["meta"]
                items = wire["data"]
                total = 0 if isinstance(meta, dict) and meta.get("total") is None else meta["total"]
                if (
                    wire.get("success") is not True
                    or not isinstance(items, list)
                    or not isinstance(meta, dict)
                    or type(meta.get("page")) is not int
                    or meta["page"] != input.page
                    or type(meta.get("limit")) is not int
                    or meta["limit"] != 20
                    or type(total) is not int
                    or total < 0
                    or len(items) > 20
                ):
                    raise ValueError("invalid room page")
                rooms = tuple(
                    Room(
                        channel=item["channel"],
                        instance_id=item["instance_id"],
                        room_id=item["room_id"],
                        push_state=item["push_state"],
                    )
                    for item in items
                )
                if any(
                    room.channel not in ("wework", "qq")
                    or type(room.instance_id) is not int
                    or room.instance_id <= 0
                    or not isinstance(room.room_id, str)
                    or not room.room_id
                    or len(room.room_id.encode("utf-8", "replace")) > 256
                    or any(char in room.room_id for char in "\r\n\0")
                    or room.push_state not in ("ready", "blocked", "unknown")
                    for room in rooms
                ):
                    raise ValueError("invalid room data")
            except (KeyError, TypeError, ValueError, UnicodeDecodeError):
                return _branch("invalidResponse", FailureKind.PROTOCOL, "Likelic returned invalid room data")
            return QueryAttempt.branch_result(
                "found", ListRoomsOutput(input.page, total, rooms), Receipt()
            )
        finally:
            await response.aclose()
