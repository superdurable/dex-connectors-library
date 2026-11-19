# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider Query implementation with Go-compatible classifications."""

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
from ._generated.definitions import (
    DEFECT,
    FOUND,
    GET_CONVERSATION_DEFINITION,
    INVALID_RESPONSE,
    NO_DATA,
    PROVIDER_REJECTED,
)
from .models import GetConversationInput, GetConversationOutput


class Client:
    """Own a bounded transport and resolve private credentials per call."""

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
            raise ValueError("Meiqia configuration and credentials are invalid")
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)

    def get_conversation(self) -> "GetConversationOperation":
        """Bind the read-only provider Query."""
        return GetConversationOperation(self)

    async def aclose(self) -> None:
        """Close the provider transport after Worker shutdown."""
        await self.http_client.aclose()


class GetConversationOperation:
    """Read provider data with typed Go-compatible outcomes."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Meiqia client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the stable Query and branch contract."""
        return GET_CONVERSATION_DEFINITION

    async def invoke(self, call: Call, input: GetConversationInput) -> QueryAttempt[GetConversationOutput]:
        """Classify one bounded provider request and discard secret response fields."""
        if (
            not isinstance(input, GetConversationInput)
            or type(input.conversation_id) is not int
            or input.conversation_id <= 0
        ):
            return _branch(DEFECT, FailureKind.VALIDATION, "Meiqia requires a positive conversation ID")
        try:
            credentials = await self.client.credentials.resolve(call)
        except (ValueError, LookupError, OSError):
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "Meiqia credentials are unavailable or invalid"
            )
        if not isinstance(credentials, Credentials) or not _valid_secret(credentials.access_token.reveal()):
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "Meiqia credentials are unavailable or invalid"
            )
        endpoint = "https://api.meiqia.com/v1/conversations/" + str(input.conversation_id)
        headers = {
            "Accept": "application/json",
            "Authorization": "Bearer " + credentials.access_token.reveal(),
        }
        try:
            request = self.client.http_client.build_request("GET", endpoint, headers=headers)
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _retry(FailureKind.AVAILABILITY, "Meiqia request failed")
        try:
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Meiqia rate limited the request", retry_after)
            if status == 408 or status >= 500:
                return _retry(
                    FailureKind.AVAILABILITY, "Meiqia service is temporarily unavailable", retry_after
                )
            if status == 404:
                return _branch(NO_DATA, FailureKind.NOT_FOUND, "Meiqia did not find the conversation")
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return _branch(PROVIDER_REJECTED, kind, f"Meiqia rejected the request with HTTP {status}")
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    INVALID_RESPONSE,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Meiqia response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Meiqia response was interrupted")
            try:
                data: Any = json.loads(
                    body, parse_constant=lambda _: (_ for _ in ()).throw(ValueError("invalid JSON number"))
                )
                result = _decode(data, input)
            except (ValueError, TypeError, KeyError, OverflowError):
                return _branch(INVALID_RESPONSE, FailureKind.PROTOCOL, "Meiqia returned invalid data")
            if isinstance(result, QueryAttempt):
                return result
            return QueryAttempt.branch_result(FOUND, result, Receipt())
        finally:
            await response.aclose()


def _valid_secret(value: object) -> bool:
    return isinstance(value, str) and 0 < len(value) <= 1024 and all(32 < ord(c) < 127 for c in value)


def _decode(data: Any, input: GetConversationInput) -> GetConversationOutput:
    if not isinstance(data, dict):
        raise ValueError("invalid conversation")
    identifier, start = data.get("conv_id"), data.get("conv_start_tm")
    end, channel = data.get("conv_end_tm", ""), data.get("main_channel", "")
    if (
        type(identifier) is not int
        or identifier != input.conversation_id
        or not isinstance(start, str)
        or not start
        or len(start) > 64
        or not isinstance(end, str)
        or len(end) > 64
        or not isinstance(channel, str)
        or len(channel) > 64
    ):
        raise ValueError("invalid conversation metadata")
    return GetConversationOutput(identifier, start, end, channel)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[GetConversationOutput]:
    return QueryAttempt.branch_result(
        branch, GetConversationOutput(), Receipt(), Failure(kind, "meiqia", "getConversation", message)
    )


def _retry(kind: FailureKind, message: str, retry_after: float = 0) -> QueryAttempt[GetConversationOutput]:
    return QueryAttempt.retry(Failure(kind, "meiqia", "getConversation", message), retry_after)
