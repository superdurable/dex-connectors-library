# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider transport and Go-compatible Query decisions."""

import json
import re
from datetime import UTC, datetime
from decimal import Decimal
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
from ._generated.definitions import GET_ACCOUNT_BALANCE_DEFINITION
from .models import GetAccountBalanceInput, GetAccountBalanceOutput

_PROVIDER = "tripo"
_OPERATION = "getAccountBalance"


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[GetAccountBalanceOutput]:
    return QueryAttempt.branch_result(
        branch, GetAccountBalanceOutput(), Receipt(), Failure(kind, _PROVIDER, _OPERATION, message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[GetAccountBalanceOutput]:
    return QueryAttempt.retry(Failure(kind, _PROVIDER, _OPERATION, message), delay)


class Client:
    """Own the provider HTTP transport and resolve rotating credentials per Query."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Tripo configuration and credentials are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("Tripo configuration is invalid")
        from dex_connectors.sdk.providerhttp import validate_base_url

        base_url = validate_base_url(config.base_url)
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None
        self.base_url = base_url

    def get_account_balance(self) -> "GetAccountBalanceOperation":
        """Bind the getAccountBalance Query."""
        return GetAccountBalanceOperation(self)

    async def aclose(self) -> None:
        """Close this client's own transport."""
        if self._owns_client:
            await self.http_client.aclose()


class GetAccountBalanceOperation:
    """Run the getAccountBalance provider Query."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Tripo client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query identity."""
        return GET_ACCOUNT_BALANCE_DEFINITION

    async def invoke(
        self, call: Call, input: GetAccountBalanceInput
    ) -> QueryAttempt[GetAccountBalanceOutput]:
        """Send one request and classify its provider outcome."""
        prepared = await _prepare(self.client, call, input)
        if isinstance(prepared, QueryAttempt):
            return prepared
        try:
            response, _ = await send_traced(self.client.http_client, prepared)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "Tripo request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Tripo rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Tripo is temporarily unavailable", delay)
            if status != 200 and not True:
                return _http_rejection(status)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Tripo response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Tripo response was interrupted")
            if status != 200:
                return _http_rejection(status)
        finally:
            await response.aclose()
        return _decode(body, input, delay)


def _valid_key(value: str) -> bool:
    return bool(value) and len(value) <= 512 and all(33 <= ord(c) <= 126 for c in value)


async def _prepare(
    client: Client, call: Call, input: GetAccountBalanceInput
) -> httpx.Request | QueryAttempt[GetAccountBalanceOutput]:
    try:
        credentials = await client.credentials.resolve(call)
        key = credentials.api_key.reveal()
        if not _valid_key(key):
            raise ValueError("invalid key")
    except (ValueError, LookupError, OSError):
        return _branch(
            "defect", FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
        )
    try:
        return client.http_client.build_request(
            "GET",
            client.base_url + "/v3/account/balance",
            headers={"Accept": "application/json", "Authorization": "Bearer " + key},
        )
    except (httpx.HTTPError, ValueError):
        return _branch("defect", FailureKind.VALIDATION, "account balance request is invalid")


def _http_rejection(status: int) -> QueryAttempt[GetAccountBalanceOutput]:
    kind = FailureKind.PROVIDER_REJECTION
    if status == 401:
        kind = FailureKind.AUTHENTICATION
    elif status == 403:
        kind = FailureKind.AUTHORIZATION
    return _branch("providerRejected", kind, f"Tripo rejected the request with HTTP {status}")


def _credit(value: Any) -> str | None:
    if isinstance(value, bool) or not isinstance(value, (Decimal, int)):
        return None
    text = str(value)
    if len(text) > 30 or not re.fullmatch(r"[0-9]+(?:\.[0-9]{1,2})?", text):
        return None
    return text


def _decode(
    body: bytes, input: GetAccountBalanceInput, delay: float
) -> QueryAttempt[GetAccountBalanceOutput]:
    try:
        document: Any = json.loads(body, parse_float=Decimal)
    except (ValueError, UnicodeDecodeError):
        document = None
    if not isinstance(document, dict) or type(document.get("code")) is not int:
        return _branch("invalidResponse", FailureKind.PROTOCOL, "Tripo returned an invalid response")
    code = document["code"]
    if code in (1000, 1001):
        return _retry(FailureKind.AVAILABILITY, "Tripo service is temporarily unavailable")
    if code in (1007, 2000):
        return _retry(FailureKind.RATE_LIMIT, "Tripo rate limited the request")
    if code in (2, 1002):
        return _branch("providerRejected", FailureKind.AUTHENTICATION, "Tripo rejected the API key")
    if code == 2010:
        return _branch("providerRejected", FailureKind.QUOTA_EXHAUSTED, "Tripo credits are exhausted")
    if code != 0:
        return _branch(
            "providerRejected", FailureKind.PROVIDER_REJECTION, "Tripo rejected the account request"
        )
    data = document.get("data")
    if (
        not isinstance(data, dict)
        or (balance := _credit(data.get("balance"))) is None
        or (frozen := _credit(data.get("frozen"))) is None
    ):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "Tripo response lacks valid credit balances")
    return QueryAttempt.branch_result("fetched", GetAccountBalanceOutput(balance, frozen), Receipt())
