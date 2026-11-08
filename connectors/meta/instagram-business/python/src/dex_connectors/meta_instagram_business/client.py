# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Instagram Graph API media listing with Go-compatible provider decisions."""

import json
import re
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any, TypeVar
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
from ._generated.definitions import LIST_MEDIA_DEFINITION
from .models import InstagramMedia, ListMediaInput, MediaListPage

Output = TypeVar("Output")
_NUMERIC_ID = re.compile(r"[0-9]{1,40}\Z")


@dataclass(frozen=True, slots=True)
class _Outcome:
    """A classified provider branch or retry."""

    branch: str
    failure: Failure
    is_retry: bool = False
    retry_after_seconds: float = 0

    def with_empty(self, empty: Output) -> QueryAttempt[Output]:
        """Apply this decision to the operation's empty output."""
        if self.is_retry:
            return QueryAttempt.retry(self.failure, self.retry_after_seconds)
        return QueryAttempt.branch_result(self.branch, empty, Receipt(), self.failure)


def _decision(operation: str, branch: str, kind: FailureKind, message: str) -> _Outcome:
    return _Outcome(branch, Failure(kind, "instagram-business", operation, message))


def _invalid(operation: str, empty: Output) -> QueryAttempt[Output]:
    return _decision(
        operation, "invalidResponse", FailureKind.PROTOCOL, "Meta returned invalid JSON"
    ).with_empty(empty)


def _numeric_id(value: object) -> bool:
    return isinstance(value, str) and _NUMERIC_ID.fullmatch(value) is not None


class Client:
    """Resolve a User token per call and read bounded Graph API responses."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Instagram Business configuration and credentials are required")
        if type(config.max_response_bytes) is not int or config.max_response_bytes < 1:
            raise ValueError("Meta response limit must be positive")
        if config.timeout_seconds <= 0:
            raise ValueError("Meta timeout must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def list_media(self) -> "ListMediaOperation":
        """Bind the media Query."""
        return ListMediaOperation(self)

    async def aclose(self) -> None:
        """Close the owned transport."""
        if self._owns_client:
            await self.http_client.aclose()

    async def get(self, call: Call, operation: str, path: str) -> Any:
        """Fetch JSON or classify a provider failure without exposing its body."""
        try:
            credential = await self.credentials.resolve(call)
            access_token = credential.user_access_token.reveal()
            if not is_header_safe_credential(access_token):
                raise ValueError("invalid User token")
        except (ValueError, LookupError, OSError, AttributeError):
            return _decision(
                operation,
                "defect",
                FailureKind.AUTHENTICATION,
                "connection credentials are unavailable or invalid",
            )
        try:
            request = self.http_client.build_request(
                "GET",
                self.base_url + path,
                headers={
                    "Accept": "application/json",
                    "Authorization": "Bearer " + access_token,
                    "User-Agent": "Dex Connector (https://superdurable.io)",
                },
            )
            response, _ = await send_traced(self.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _Outcome(
                "",
                Failure(FailureKind.TRANSPORT, "instagram-business", operation, "Meta request failed"),
                True,
            )
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _Outcome(
                    "",
                    Failure(
                        FailureKind.RATE_LIMIT,
                        "instagram-business",
                        operation,
                        "Meta rate limited the request",
                    ),
                    True,
                    retry_after,
                )
            if status == 408 or status >= 500:
                return _Outcome(
                    "",
                    Failure(
                        FailureKind.AVAILABILITY,
                        "instagram-business",
                        operation,
                        "Meta is temporarily unavailable",
                    ),
                    True,
                    retry_after,
                )
            if status == 404:
                return _decision(
                    operation, "notFound", FailureKind.NOT_FOUND, "Meta did not find the resource"
                )
            if status != 200:
                return _decision(
                    operation,
                    "providerRejected",
                    FailureKind.PROVIDER_REJECTION,
                    f"Meta rejected HTTP {status}",
                )
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _decision(
                    operation,
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Meta response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _Outcome(
                    "",
                    Failure(
                        FailureKind.TRANSPORT,
                        "instagram-business",
                        operation,
                        "Meta response was interrupted",
                    ),
                    True,
                )
            if access_token.encode() in body:
                return _decision(
                    operation,
                    "defect",
                    FailureKind.AUTHENTICATION,
                    "connection credentials are unavailable or invalid",
                )
            try:
                return json.loads(body)
            except (ValueError, UnicodeDecodeError):
                return _decision(
                    operation, "invalidResponse", FailureKind.PROTOCOL, "Meta returned invalid JSON"
                )
        finally:
            await response.aclose()


class ListMediaOperation:
    """Read one Instagram media page without comments or reactions."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated media listing contract."""
        return LIST_MEDIA_DEFINITION

    async def invoke(self, call: Call, input: ListMediaInput) -> QueryAttempt[MediaListPage]:
        """Read one bounded media page or select an optional branch."""
        empty = MediaListPage()
        if (
            not isinstance(input, ListMediaInput)
            or not _numeric_id(input.ig_user_id)
            or not isinstance(input.after, str)
            or len(input.after) > 4096
            or any(char in input.after for char in "\r\n\x00")
        ):
            return _decision(
                "listMedia", "defect", FailureKind.VALIDATION, "Instagram account ID or cursor is invalid"
            ).with_empty(empty)
        query = {"fields": "id,caption,media_type,timestamp,permalink", "limit": "100"}
        if input.after:
            query["after"] = input.after
        response = await self.client.get(call, "listMedia", f"/{input.ig_user_id}/media?{urlencode(query)}")
        if isinstance(response, _Outcome):
            return response.with_empty(empty)
        if (
            not isinstance(response, dict)
            or not isinstance(response.get("data"), list)
            or len(response["data"]) > 100
        ):
            return _invalid("listMedia", empty)
        paging = response.get("paging") or {}
        if not isinstance(paging, dict):
            return _invalid("listMedia", empty)
        cursors = paging.get("cursors") or {}
        if not isinstance(cursors, dict):
            return _invalid("listMedia", empty)
        after = cursors.get("after") or ""
        if not isinstance(after, str) or len(after) > 4096:
            return _invalid("listMedia", empty)
        media = []
        for item in response["data"]:
            media_item = _project_media(item)
            if media_item is None:
                return _invalid("listMedia", empty)
            media.append(media_item)
        return QueryAttempt.branch_result("listed", MediaListPage(tuple(media), after), Receipt())


def _project_media(value: object) -> InstagramMedia | None:
    if not isinstance(value, dict) or not _numeric_id(value.get("id")):
        return None
    caption = value.get("caption") or ""
    media_type = value.get("media_type")
    timestamp = value.get("timestamp")
    permalink = value.get("permalink") or ""
    if (
        not isinstance(caption, str)
        or not isinstance(media_type, str)
        or not media_type
        or not isinstance(timestamp, str)
        or not timestamp
        or not isinstance(permalink, str)
        or (permalink and not permalink.startswith("https://"))
    ):
        return None
    return InstagramMedia(value["id"], caption, timestamp, permalink, media_type)
