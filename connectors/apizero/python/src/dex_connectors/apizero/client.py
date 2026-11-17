# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""ApiZero account-information requests and Go-compatible decisions."""

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
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import GET_ACCOUNT_INFO_DEFINITION
from .models import GetAccountInfoInput, GetAccountInfoOutput

_ENDPOINT = "https://v1.apizero.cn/api/user-info"


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[GetAccountInfoOutput]:
    return QueryAttempt.branch_result(
        branch, GetAccountInfoOutput(), Receipt(), Failure(kind, "apizero", "getAccountInfo", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[GetAccountInfoOutput]:
    return QueryAttempt.retry(Failure(kind, "apizero", "getAccountInfo", message), delay)


def _valid_yuan(value: object) -> bool:
    if not isinstance(value, str) or not 1 <= len(value) <= 32 or not "0" <= value[0] <= "9":
        return False
    integer, separator, fraction = value.partition(".")
    return (
        bool(integer)
        and integer.isascii()
        and integer.isdigit()
        and (not separator or bool(fraction) and fraction.isascii() and fraction.isdigit())
    )


class Client:
    """Own an HTTP transport and resolve the ApiZero Key for each Query attempt."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("ApiZero configuration and credential source are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("ApiZero configuration is invalid")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_account_info(self) -> "GetAccountInfoOperation":
        """Bind the read-only account-information Query."""
        return GetAccountInfoOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP transport when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class GetAccountInfoOperation:
    """Read the standard Key balance or prepaid Key remaining quota."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("ApiZero client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query identity and retry policy."""
        return GET_ACCOUNT_INFO_DEFINITION

    async def invoke(self, call: Call, input: GetAccountInfoInput) -> QueryAttempt[GetAccountInfoOutput]:
        """Make one provider request and classify its observed result."""
        if not isinstance(input, GetAccountInfoInput):
            return _branch("defect", FailureKind.VALIDATION, "ApiZero account input is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
            key = credentials.api_key.reveal()
            if not key.startswith("sk_live_") or len(key) > 1024 or not is_header_safe_credential(key):
                raise ValueError("invalid key")
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch("defect", FailureKind.AUTHENTICATION, "ApiZero API Key is unavailable or invalid")
        try:
            request = self.client.http_client.build_request(
                "GET", _ENDPOINT, headers={"Authorization": f"Bearer {key}", "Accept": "application/json"}
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "ApiZero account request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "ApiZero account request failed")
        try:
            status = response.status_code
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "ApiZero rate limited the request", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "ApiZero response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "ApiZero response was interrupted")
        finally:
            await response.aclose()
        try:
            wire: Any = json.loads(body, parse_constant=lambda _: (_ for _ in ()).throw(ValueError()))
        except (ValueError, UnicodeDecodeError):
            wire = None
        if (
            isinstance(wire, dict)
            and type(wire.get("code")) is int
            and -(1 << 63) <= wire["code"] < (1 << 63)
        ):
            code = wire["code"]
            if code != 0:
                return _classify_code(code, delay)
            if status != 200:
                return _branch(
                    "invalidResponse",
                    FailureKind.PROTOCOL,
                    "ApiZero returned success with unexpected HTTP status",
                )
            data = wire.get("data")
            if isinstance(data, dict):
                scope = data.get("scope")
                if scope == "account" and isinstance(data.get("balance"), dict):
                    balance = data["balance"].get("yuan")
                    if _valid_yuan(balance):
                        return QueryAttempt.branch_result(
                            "found", GetAccountInfoOutput(scope="account", balance_yuan=balance), Receipt()
                        )
                elif scope == "key" and isinstance(data.get("key"), dict):
                    remaining = data["key"].get("quota_remaining")
                    if type(remaining) is int and remaining >= 0 and remaining <= (1 << 63) - 1:
                        return QueryAttempt.branch_result(
                            "found",
                            GetAccountInfoOutput(scope="key", key_quota_remaining=remaining),
                            Receipt(),
                        )
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "ApiZero returned incomplete account data"
            )
        if status == 408 or status >= 500:
            return _retry(FailureKind.AVAILABILITY, "ApiZero service is temporarily unavailable", delay)
        if status != 200:
            kind = FailureKind.PROVIDER_REJECTION
            if status == 401:
                kind = FailureKind.AUTHENTICATION
            elif status == 403:
                kind = FailureKind.AUTHORIZATION
            return _branch("providerRejected", kind, f"ApiZero rejected account request with HTTP {status}")
        return _branch("invalidResponse", FailureKind.PROTOCOL, "ApiZero returned invalid account data")


def _classify_code(code: int, delay: float) -> QueryAttempt[GetAccountInfoOutput]:
    if code == 4029:
        return _retry(FailureKind.RATE_LIMIT, "ApiZero per-second rate limit exceeded", delay)
    if code in (5000, 5020, 5021, 5030):
        return _retry(FailureKind.AVAILABILITY, "ApiZero service is temporarily unavailable", delay)
    if code in (4011, 4015):
        return _branch("providerRejected", FailureKind.AUTHENTICATION, "ApiZero rejected the API Key")
    if code in (4013, 4014):
        return _branch(
            "providerRejected", FailureKind.AUTHORIZATION, "ApiZero API Key cannot access this endpoint"
        )
    return _branch(
        "providerRejected",
        FailureKind.PROVIDER_REJECTION,
        f"ApiZero rejected account request with code {code}",
    )
