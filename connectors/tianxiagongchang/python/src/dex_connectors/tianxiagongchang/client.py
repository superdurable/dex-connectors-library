# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Tianxia Gongchang account Query and provider error classification."""

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
from ._generated.definitions import GET_ACCOUNT_STATUS_DEFINITION
from .models import GetAccountStatusInput, GetAccountStatusOutput

_ENDPOINT = "https://open.tianxiagongchang.com/open/v1/account"


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[GetAccountStatusOutput]:
    return QueryAttempt.branch_result(
        branch,
        GetAccountStatusOutput(),
        Receipt(),
        Failure(kind, "tianxiagongchang", "getAccountStatus", message),
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[GetAccountStatusOutput]:
    return QueryAttempt.retry(Failure(kind, "tianxiagongchang", "getAccountStatus", message), delay)


class Client:
    """Own a bounded HTTP transport and resolve rotated credentials per call."""

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
            raise ValueError("Tianxia Gongchang configuration and credentials are invalid")
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_account_status(self) -> "GetAccountStatusOperation":
        """Bind the read-only account-status Query."""
        return GetAccountStatusOperation(self)

    async def aclose(self) -> None:
        """Close the transport if this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class GetAccountStatusOperation:
    """Read safe account identity and credit fields."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Tianxia Gongchang client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the stable generated Query definition."""
        return GET_ACCOUNT_STATUS_DEFINITION

    async def invoke(self, call: Call, input: GetAccountStatusInput) -> QueryAttempt[GetAccountStatusOutput]:
        """Perform one bounded provider read and classify its outcome."""
        if not isinstance(input, GetAccountStatusInput):
            return _branch("defect", FailureKind.VALIDATION, "account input is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
            key = credentials.api_key.reveal()
            if not key or len(key) > 512 or any(not 0x20 < ord(c) < 0x7F for c in key):
                raise ValueError("invalid key")
        except (ValueError, LookupError, OSError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Open Platform key is unavailable or invalid"
            )
        try:
            request = self.client.http_client.build_request(
                "GET", _ENDPOINT, headers={"Accept": "application/json", "Authorization": "Bearer " + key}
            )
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _retry(FailureKind.AVAILABILITY, "account request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if response.status_code == 429:
                return _retry(FailureKind.RATE_LIMIT, "provider rate limited the request", delay)
            if response.status_code == 408 or response.status_code >= 500:
                return _retry(FailureKind.AVAILABILITY, "provider temporarily unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "account response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "account response was interrupted")
            try:
                wire: Any = _decode_envelope(body)
            except (ValueError, UnicodeDecodeError):
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "provider returned an invalid account response"
                )
            if not isinstance(wire, dict) or type(wire.get("code")) is not int:
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "provider returned an invalid account response"
                )
            code = wire["code"]
            if code == 42900:
                return _retry(FailureKind.RATE_LIMIT, "provider rate limited the request", delay)
            if code in (50000, 50400):
                return _retry(FailureKind.AVAILABILITY, "provider temporarily unavailable", delay)
            if code == 40100:
                return _branch("providerRejected", FailureKind.AUTHENTICATION, "provider rejected the key")
            if code in (40300, 42901):
                return _branch(
                    "providerRejected", FailureKind.AUTHORIZATION, "provider denied account access"
                )
            if code != 0:
                return _branch(
                    "providerRejected",
                    FailureKind.PROVIDER_REJECTION,
                    f"provider rejected the account request with code {code}",
                )
            data = wire.get("data")
            if (
                response.status_code != 200
                or type(wire.get("credits_charged")) is not int
                or wire["credits_charged"] != 0
                or type(wire.get("credits_balance")) is not int
                or wire["credits_balance"] < 0
                or not isinstance(data, dict)
                or type(data.get("app_id")) is not int
                or data["app_id"] <= 0
                or type(data.get("frozen_credits")) is not int
                or data["frozen_credits"] < 0
                or type(data.get("is_sandbox")) is not bool
            ):
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "provider returned invalid account fields"
                )
            return QueryAttempt.branch_result(
                "retrieved",
                GetAccountStatusOutput(
                    app_id=data["app_id"],
                    credits_balance=wire["credits_balance"],
                    frozen_credits=data["frozen_credits"],
                    is_sandbox=data["is_sandbox"],
                ),
                Receipt(),
            )
        finally:
            await response.aclose()


def _invalid_json_number(token: str) -> None:
    raise ValueError("Invalid JSON number")


def _optional_integer(value: object) -> bool:
    return value is None or (type(value) is int and -(1 << 63) <= value < (1 << 63))


def _decode_envelope(body: bytes) -> dict[str, Any]:
    wire = json.loads(
        body.decode("utf-8", errors="replace"),
        object_hook=lambda fields: {key.lower(): value for key, value in fields.items()},
        parse_constant=_invalid_json_number,
    )
    if not isinstance(wire, dict) or not _optional_integer(wire.get("code")):
        raise ValueError("Invalid account status")
    data = wire.get("data")
    if data is not None and not isinstance(data, dict):
        raise ValueError("Invalid account fields")
    if not all(_optional_integer(wire.get(field)) for field in ("credits_balance", "credits_charged")):
        raise ValueError("Invalid credit information")
    if data is not None:
        if not all(_optional_integer(data.get(field)) for field in ("app_id", "frozen_credits")):
            raise ValueError("Invalid application information")
        if data.get("is_sandbox") is not None and type(data["is_sandbox"]) is not bool:
            raise ValueError("Invalid sandbox flag")
    return wire
