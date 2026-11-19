# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read China SendCloud balances without exposing credential or contact data."""

import json
from datetime import UTC, datetime
from decimal import Decimal, InvalidOperation
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
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import GET_ACCOUNT_BALANCE_DEFINITION
from .models import GetAccountBalanceInput, GetAccountBalanceOutput


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "sendcloud", "getAccountBalance", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[GetAccountBalanceOutput]:
    return QueryAttempt.branch_result(branch, GetAccountBalanceOutput(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[GetAccountBalanceOutput]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _valid_credential(value: str) -> bool:
    return (
        bool(value) and len(value) <= 512 and value.strip() == value and all(32 < ord(c) < 127 for c in value)
    )


class Client:
    """Resolve current SendCloud API USER credentials for each balance read."""

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
        ):
            raise ValueError("SendCloud configuration and credentials are required")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_http_client = http_client is None

    def get_account_balance(self) -> "GetAccountBalanceOperation":
        """Bind the account balance Query."""
        return GetAccountBalanceOperation(self)

    async def aclose(self) -> None:
        """Close a connector-owned transport."""
        if self._owns_http_client:
            await self.http_client.aclose()


class GetAccountBalanceOperation:
    """Read balance and daily quota for the current SendCloud account."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query definition."""
        return GET_ACCOUNT_BALANCE_DEFINITION

    async def invoke(
        self, call: Call, input: GetAccountBalanceInput
    ) -> QueryAttempt[GetAccountBalanceOutput]:
        """Classify SendCloud HTTP and business status codes."""
        try:
            credentials = await self.client.credentials.resolve(call)
            api_user = credentials.api_user.reveal()
            api_key = credentials.api_key.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch("defect", FailureKind.AUTHENTICATION, "SendCloud credentials are unavailable")
        if not _valid_credential(api_user) or not _valid_credential(api_key):
            return _branch("defect", FailureKind.AUTHENTICATION, "SendCloud credentials are invalid")
        try:
            request = self.client.http_client.build_request(
                "POST",
                self.client.base_url + "/apiv2/userinfo/get",
                data={"apiUser": api_user, "apiKey": api_key},
                headers={"Accept": "application/json"},
            )
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _retry(FailureKind.AVAILABILITY, "SendCloud request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "SendCloud rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "SendCloud is temporarily unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "SendCloud response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "SendCloud response was interrupted")
            try:
                wire: Any = json.loads(body, parse_float=Decimal)
                if (
                    not isinstance(wire, dict)
                    or type(wire.get("result")) is not bool
                    or type(wire.get("statusCode")) is not int
                ):
                    raise ValueError("invalid envelope")
            except (ValueError, TypeError, UnicodeDecodeError):
                if status != 200:
                    return _branch(
                        "providerRejected",
                        FailureKind.PROVIDER_REJECTION,
                        f"SendCloud rejected HTTP {status}",
                    )
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "SendCloud returned an invalid response"
                )
            code = wire["statusCode"]
            if code == 50000:
                return _retry(FailureKind.RATE_LIMIT, "SendCloud rate limited the request", delay)
            if status != 200 or not wire["result"] or code != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if code == 40005
                    else FailureKind.AUTHORIZATION
                    if code in (40018, 40025, 40026, 6001)
                    else FailureKind.PROVIDER_REJECTION
                )
                return _branch("providerRejected", kind, "SendCloud rejected the account balance request")
            info = wire.get("info")
            if not isinstance(info, dict):
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "SendCloud returned an invalid response"
                )
            try:
                available = Decimal(str(info["avaliableBalance"]))
                balance = Decimal(str(info["balance"]))
                quota = info["quota"]
                used = info["todayUsedQuota"]
                if (
                    not available.is_finite()
                    or not balance.is_finite()
                    or type(quota) is not int
                    or type(used) is not int
                    or quota < 0
                    or used < 0
                ):
                    raise ValueError("invalid balance")
            except (KeyError, ValueError, TypeError, InvalidOperation):
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "SendCloud returned an invalid response"
                )
            return QueryAttempt.branch_result(
                "retrieved",
                GetAccountBalanceOutput(str(available), str(balance), quota, used),
                Receipt(),
            )
        finally:
            await response.aclose()
