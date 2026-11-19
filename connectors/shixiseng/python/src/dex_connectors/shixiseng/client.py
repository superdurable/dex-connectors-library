# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Shixiseng signed public industry Query and provider decisions."""

from __future__ import annotations

import base64
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
from ._generated.definitions import LIST_INDUSTRIES_DEFINITION
from .models import ListIndustriesInput, ListIndustriesOutput


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListIndustriesOutput]:
    return QueryAttempt.branch_result(
        branch, ListIndustriesOutput(), Receipt(), Failure(kind, "shixiseng", "listIndustries", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ListIndustriesOutput]:
    return QueryAttempt.retry(Failure(kind, "shixiseng", "listIndustries", message), delay)


def _valid_credential(value: str) -> bool:
    return (
        bool(value)
        and len(value) <= 1024
        and value.strip() == value
        and all(32 <= ord(c) < 127 for c in value)
    )


class Client:
    """Own Shixiseng request settings and resolve merchant credentials per attempt."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        *,
        clock: Callable[[], datetime] | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Shixiseng configuration and credential source are required")
        if (
            config.timeout_seconds <= 0
            or config.max_response_bytes <= 0
            or config.max_response_bytes > 16 << 20
        ):
            raise ValueError("Shixiseng configuration is invalid")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None
        self.clock = clock or (lambda: datetime.now(UTC))

    def list_industries(self) -> ListIndustriesOperation:
        """Bind the industry-list Query to this client."""
        return ListIndustriesOperation(self)

    async def aclose(self) -> None:
        """Close the transport if this client owns it."""
        if self._owns_client:
            await self.http_client.aclose()


class ListIndustriesOperation:
    """Read public industry labels without applicant information."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Shixiseng client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query identity and retry policy."""
        return LIST_INDUSTRIES_DEFINITION

    async def invoke(self, call: Call, input: ListIndustriesInput) -> QueryAttempt[ListIndustriesOutput]:
        """Sign one industry request and classify its provider outcome."""
        try:
            credentials = await self.client.credentials.resolve(call)
            app_id, app_secret = credentials.app_id.reveal(), credentials.app_secret.reveal()
            if not _valid_credential(app_id) or not _valid_credential(app_secret):
                raise ValueError("invalid merchant credentials")
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch(
                "defect",
                FailureKind.AUTHENTICATION,
                "Shixiseng merchant credentials are unavailable or invalid",
            )
        timestamp = str(int(self.client.clock().timestamp()))
        signature = hashlib.md5((app_id + app_secret + timestamp).encode()).hexdigest().upper()
        authorization = base64.b64encode(f"{app_id}:{timestamp}".encode()).decode()
        try:
            request = self.client.http_client.build_request(
                "GET",
                "https://open.shixiseng.com/industry/list",
                params={"sign": signature},
                headers={"Authorization": authorization, "Accept": "text/html"},
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "Shixiseng industry request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "Shixiseng industry request failed")
        try:
            status = response.status_code
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Shixiseng rate limited the industry request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Shixiseng API is temporarily unavailable", delay)
            if status != 200:
                return _branch(
                    "providerRejected", FailureKind.PROVIDER_REJECTION, "Shixiseng rejected industry request"
                )
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Shixiseng industry response exceeded configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Shixiseng industry response was interrupted")
        finally:
            await response.aclose()
        try:
            wire: Any = json.loads(body)
        except (ValueError, UnicodeDecodeError):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Shixiseng returned malformed industry data"
            )
        if not isinstance(wire, dict) or type(wire.get("code")) is not int:
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Shixiseng returned malformed industry data"
            )
        if wire["code"] == 300:
            return _retry(FailureKind.AVAILABILITY, "Shixiseng reported internal industry failure", delay)
        if wire["code"] != 100:
            return _branch(
                "providerRejected", FailureKind.PROVIDER_REJECTION, "Shixiseng rejected industry request"
            )
        message = wire.get("msg")
        industries = message.get("industry_list") if isinstance(message, dict) else None
        if (
            not isinstance(industries, list)
            or not 1 <= len(industries) <= 256
            or any(not isinstance(value, str) or not value or len(value) > 128 for value in industries)
        ):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Shixiseng omitted a valid industry list")
        return QueryAttempt.branch_result(
            "found", ListIndustriesOutput(industries=tuple(industries)), Receipt()
        )
