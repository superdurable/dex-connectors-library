# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Buffer GraphQL channel lookup with Go-compatible outcomes."""

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
from ._generated.definitions import GET_CHANNEL_DEFINITION
from .models import ChannelProfile, GetChannelInput

_GET_CHANNEL_GRAPHQL = "query GetChannel($id: ChannelId!) { channel(input: {id: $id}) { id name service } }"


class _GraphQLRejection(Exception):
    """Buffer rejected the GraphQL query."""


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One bounded provider response or request failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "buffer", "getChannel", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ChannelProfile]:
    return QueryAttempt.branch_result(branch, ChannelProfile(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ChannelProfile]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _valid_graphql_id(value: object) -> bool:
    if not isinstance(value, str) or not value or value.strip() != value:
        return False
    try:
        return len(value.encode("utf-8")) <= 128 and all(
            ord(char) >= 0x20 and ord(char) != 0x7F for char in value
        )
    except UnicodeEncodeError:
        return False


def _decode_channel(body: bytes, channel_id: str) -> ChannelProfile | None:
    raw: Any = json.loads(body)
    if not isinstance(raw, dict):
        raise ValueError("GraphQL response is not an object")
    errors = raw.get("errors", [])
    if errors is not None and not isinstance(errors, list):
        raise ValueError("GraphQL errors are invalid")
    if isinstance(errors, list) and errors:
        raise _GraphQLRejection()
    data = raw.get("data")
    if not isinstance(data, dict):
        raise ValueError("GraphQL data is missing")
    channel = data.get("channel")
    if channel is None:
        return None
    if not isinstance(channel, dict):
        raise ValueError("GraphQL channel is invalid")
    if (
        channel.get("id") != channel_id
        or not isinstance(channel.get("name"), str)
        or not channel["name"]
        or not isinstance(channel.get("service"), str)
        or not channel["service"]
    ):
        raise ValueError("channel identity, name, or service is invalid")
    return ChannelProfile(channel["id"], channel["name"], channel["service"])


class Client:
    """Resolve Buffer API keys per call and read channels."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Buffer configuration and credential source are required")
        if (
            config.timeout_seconds <= 0
            or type(config.max_response_bytes) is not int
            or config.max_response_bytes < 1
        ):
            raise ValueError("Buffer request limits must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_channel(self) -> GetChannelOperation:
        """Bind the getChannel Query to this client."""
        return GetChannelOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _get(self, call: Call, input: GetChannelInput) -> _Exchange:
        try:
            credential = await self.credentials.resolve(call)
            api_key = credential.api_key.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return _Exchange(error="credential")
        if not is_header_safe_credential(api_key):
            return _Exchange(error="credential")
        try:
            request = self.http_client.build_request(
                "POST",
                self.base_url + "/",
                headers={
                    "Accept": "application/json",
                    "Content-Type": "application/json",
                    "Authorization": "Bearer " + api_key,
                },
                json={"query": _GET_CHANNEL_GRAPHQL, "variables": {"id": input.channel_id}},
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


class GetChannelOperation:
    """Read one channel by its Buffer GraphQL ID."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query worker."""
        return GET_CHANNEL_DEFINITION

    async def invoke(self, call: Call, input: GetChannelInput) -> QueryAttempt[ChannelProfile]:
        """Classify provider outcomes consistently with Go."""
        if not isinstance(input, GetChannelInput) or not _valid_graphql_id(input.channel_id):
            return _branch(
                "defect", FailureKind.VALIDATION, "channelId must contain 1–128 printable characters"
            )
        exchange = await self.client._get(call, input)
        if exchange.error == "credential":
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Buffer credentials are unavailable or invalid"
            )
        if exchange.error == "request":
            return _retry(FailureKind.TRANSPORT, "Buffer request failed")
        if exchange.status == 429:
            return _retry(FailureKind.RATE_LIMIT, "Buffer rate limited the request", exchange.retry_after)
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(
                FailureKind.AVAILABILITY, "Buffer is temporarily unavailable", exchange.retry_after
            )
        if exchange.status != 200:
            return _branch(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                f"Buffer rejected HTTP {exchange.status}",
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "Buffer response exceeded the configured limit",
            )
        if exchange.error:
            return _retry(FailureKind.TRANSPORT, "Buffer response was interrupted")
        try:
            profile = _decode_channel(exchange.body, input.channel_id)
        except _GraphQLRejection:
            return _branch(
                "providerRejected", FailureKind.PROVIDER_REJECTION, "Buffer rejected the GraphQL query"
            )
        except (ValueError, TypeError, UnicodeDecodeError):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Buffer returned invalid channel JSON"
            )
        if profile is None:
            return _branch("notFound", FailureKind.NOT_FOUND, "Buffer did not expose the channel")
        return QueryAttempt.branch_result("found", profile, Receipt())
