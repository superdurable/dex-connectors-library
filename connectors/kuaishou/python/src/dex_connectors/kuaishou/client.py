# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read authorized Kuaishou video metadata without raw account data."""

import json
from datetime import UTC, datetime
from typing import Any
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
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import LIST_USER_VIDEOS_DEFINITION
from .models import ListUserVideosInput, UserVideo, UserVideosPage


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[UserVideosPage]:
    return QueryAttempt.branch_result(
        branch, UserVideosPage(), Receipt(), Failure(kind, "kuaishou", "listUserVideos", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[UserVideosPage]:
    return QueryAttempt.retry(Failure(kind, "kuaishou", "listUserVideos", message), delay)


def _opaque(value: str, maximum: int) -> bool:
    return bool(value) and len(value) <= maximum and all(33 <= ord(char) <= 126 for char in value)


class Client:
    """Resolve current user token and read one Kuaishou page."""

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
            or config.max_response_bytes <= 0
            or not 1 <= config.page_size <= 200
        ):
            raise ValueError("Kuaishou configuration and credentials are required")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        parsed = urlsplit(self.base_url)
        if parsed.path or parsed.query or parsed.fragment:
            raise ValueError("Kuaishou base URL must be an origin")
        self.max_response_bytes = config.max_response_bytes
        self.page_size = config.page_size
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_http_client = http_client is None

    def list_user_videos(self) -> "ListUserVideosOperation":
        """Bind the video metadata Query."""
        return ListUserVideosOperation(self)

    async def aclose(self) -> None:
        """Close a connector-owned transport."""
        if self._owns_http_client:
            await self.http_client.aclose()


class ListUserVideosOperation:
    """Read one bounded page of authorized videos."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated video Query contract."""
        return LIST_USER_VIDEOS_DEFINITION

    async def invoke(self, call: Call, input: ListUserVideosInput) -> QueryAttempt[UserVideosPage]:
        """Validate and classify one Kuaishou response."""
        if not isinstance(input, ListUserVideosInput) or (input.cursor and not _opaque(input.cursor, 128)):
            return _branch("defect", FailureKind.VALIDATION, "Kuaishou cursor is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
            app_id = credentials.app_id
            token = credentials.access_token.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch("defect", FailureKind.AUTHENTICATION, "Kuaishou credentials are unavailable")
        if not _opaque(app_id, 4096) or not _opaque(token, 4096):
            return _branch("defect", FailureKind.AUTHENTICATION, "Kuaishou credentials are invalid")
        parameters = {"app_id": app_id, "access_token": token, "count": str(self.client.page_size)}
        if input.cursor:
            parameters["cursor"] = input.cursor
        try:
            request = self.client.http_client.build_request(
                "GET",
                self.client.base_url + "/openapi/photo/list",
                params=parameters,
                headers={"Accept": "application/json"},
            )
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _retry(FailureKind.TRANSPORT, "Kuaishou request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
                body_error: FailureKind | None = None
            except BodyTooLargeError:
                body = b""
                body_error = FailureKind.RESPONSE_TOO_LARGE
            except ProviderTransportError:
                body = b""
                body_error = FailureKind.TRANSPORT
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Kuaishou rate limited the query", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Kuaishou is unavailable", delay)
            if body_error == FailureKind.RESPONSE_TOO_LARGE:
                return _branch(
                    "invalidResponse", FailureKind.RESPONSE_TOO_LARGE, "Kuaishou response is oversized"
                )
            if body_error:
                return _retry(FailureKind.TRANSPORT, "Kuaishou response was interrupted")
            if status != 200:
                return _branch(
                    "providerRejected", FailureKind.PROVIDER_REJECTION, f"Kuaishou rejected HTTP {status}"
                )
            try:
                wire: Any = json.loads(body)
                if not isinstance(wire, dict) or type(wire.get("result")) is not int:
                    raise ValueError("missing result code")
                if wire["result"] != 1:
                    return _branch(
                        "providerRejected", FailureKind.PROVIDER_REJECTION, "Kuaishou rejected the query"
                    )
                items = wire.get("video_list")
                if not isinstance(items, list) or len(items) > self.client.page_size:
                    raise ValueError("invalid video list")
                videos = tuple(
                    UserVideo(
                        photo_id=item["photo_id"],
                        caption=item.get("caption", ""),
                        created_at_milliseconds=item.get("create_time", 0),
                        like_count=item.get("like_count", 0),
                        comment_count=item.get("comment_count", 0),
                        view_count=item.get("view_count", 0),
                        pending=item.get("pending", False),
                    )
                    for item in items
                )
                if any(
                    not _opaque(video.photo_id, 128)
                    or min(
                        video.created_at_milliseconds, video.like_count, video.comment_count, video.view_count
                    )
                    < 0
                    for video in videos
                ):
                    raise ValueError("invalid video metadata")
            except (KeyError, TypeError, ValueError, UnicodeDecodeError):
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "Kuaishou returned invalid video data"
                )
            return QueryAttempt.branch_result(
                "listed", UserVideosPage(videos, videos[-1].photo_id if videos else ""), Receipt()
            )
        finally:
            await response.aclose()
