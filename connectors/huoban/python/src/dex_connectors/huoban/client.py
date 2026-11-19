# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Huoban workspace list transport and Go-compatible decisions."""

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
from ._generated.definitions import LIST_SPACES_DEFINITION
from .models import ListSpacesInput, ListSpacesOutput, SpaceMetadata

_ENDPOINT = "https://api.huoban.com/openapi/v1/space/list"


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListSpacesOutput]:
    return QueryAttempt.branch_result(
        branch, ListSpacesOutput(), Receipt(), Failure(kind, "huoban", "listSpaces", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ListSpacesOutput]:
    return QueryAttempt.retry(Failure(kind, "huoban", "listSpaces", message), delay)


def _valid_api_key(value: str) -> bool:
    return bool(value) and len(value) <= 1024 and all(0x20 < ord(character) < 0x7F for character in value)


class Client:
    """Own the HTTP transport and resolve the Huoban Key on every attempt."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Huoban configuration and credential source are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("Huoban configuration is invalid")
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def list_spaces(self) -> "ListSpacesOperation":
        """Bind the workspace-list Query."""
        return ListSpacesOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP transport if this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class ListSpacesOperation:
    """List at most fifty workspace IDs visible to the API Key."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Huoban client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated space-list Query identity."""
        return LIST_SPACES_DEFINITION

    async def invoke(self, call: Call, input: ListSpacesInput) -> QueryAttempt[ListSpacesOutput]:
        """Make one space-list request and classify its provider outcome."""
        try:
            credentials = await self.client.credentials.resolve(call)
            api_key = credentials.api_key.reveal()
            if not _valid_api_key(api_key):
                raise ValueError("invalid API Key")
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch("defect", FailureKind.AUTHENTICATION, "Huoban API Key is unavailable or invalid")
        try:
            request = self.client.http_client.build_request(
                "GET",
                _ENDPOINT,
                headers={"Open-Authorization": "Bearer " + api_key, "Accept": "application/json"},
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "Huoban workspace request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "Huoban workspace request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Huoban rate limited the workspace request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Huoban API is temporarily unavailable", delay)
            if status != 200:
                kind = FailureKind.PROVIDER_REJECTION
                if status == 401:
                    kind = FailureKind.AUTHENTICATION
                elif status == 403:
                    kind = FailureKind.AUTHORIZATION
                return _branch(
                    "providerRejected", kind, f"Huoban rejected workspace request with HTTP {status}"
                )
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Huoban response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Huoban response was interrupted")
        finally:
            await response.aclose()
        return _decode_response(body)


def _decode_response(body: bytes) -> QueryAttempt[ListSpacesOutput]:
    try:
        document: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "Huoban returned invalid workspace metadata")
    if not isinstance(document, dict) or type(document.get("code")) is not int:
        return _branch("invalidResponse", FailureKind.PROTOCOL, "Huoban returned invalid workspace metadata")
    code = document["code"]
    if code != 0:
        return _branch(
            "providerRejected",
            FailureKind.PROVIDER_REJECTION,
            f"Huoban rejected workspace list with code {code}",
        )
    data = document.get("data")
    if not isinstance(data, dict) or not isinstance(data.get("spaces"), list):
        return _branch(
            "invalidResponse", FailureKind.PROTOCOL, "Huoban returned incomplete workspace metadata"
        )
    spaces: list[SpaceMetadata] = []
    for item in data["spaces"]:
        if (
            not isinstance(item, dict)
            or not isinstance(item.get("space_id"), str)
            or not isinstance(item.get("name"), str)
            or not item["space_id"]
            or not item["name"]
        ):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Huoban returned an incomplete workspace")
        spaces.append(SpaceMetadata(space_id=item["space_id"], name=item["name"]))
    return QueryAttempt.branch_result("listed", ListSpacesOutput(spaces=tuple(spaces)), Receipt())
