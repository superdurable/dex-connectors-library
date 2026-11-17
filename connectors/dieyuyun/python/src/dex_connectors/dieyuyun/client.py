# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Wuliang AI account balance with exact JSON decimal text and filtered metadata."""

import json
import re
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
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import (
    CONNECTOR_ID,
    DEFECT,
    FETCHED,
    GET_ACCOUNT_BALANCE_DEFINITION,
    GET_ACCOUNT_BALANCE_OPERATION_ID,
    INVALID_RESPONSE,
    PROVIDER_REJECTED,
)
from .models import GetAccountBalanceInput, GetAccountBalanceOutput

_DECIMAL = re.compile(r"-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?\Z")


class Client:
    """Resolve the API Key per Query attempt and return only an exact CNY balance."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        """Validate the connection and own the transport unless one was injected."""
        if config is None or credentials is None:
            raise ValueError("Dieyuyun configuration and credential source are required")
        self.base_url = validate_base_url(config.base_url)
        if httpx.URL(self.base_url).path not in ("", "/"):
            raise ValueError("Dieyuyun API URL must be a host root")
        if config.timeout_seconds <= 0 or config.max_response_bytes <= 0:
            raise ValueError("Dieyuyun timeout and response limit must be positive")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self.owns_http_client = http_client is None

    def get_account_balance(self) -> "GetAccountBalanceOperation":
        """Bind the tenant balance Query."""
        return GetAccountBalanceOperation(self)

    async def aclose(self) -> None:
        """Close the connector-owned transport after the Worker stops."""
        if self.owns_http_client:
            await self.http_client.aclose()


class GetAccountBalanceOperation:
    """Read available CNY yuan without exposing API Key metadata."""

    def __init__(self, client: Client) -> None:
        """Bind a validated Dieyuyun client."""
        if client is None:
            raise ValueError("Dieyuyun client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest operation identity and branches."""
        return GET_ACCOUNT_BALANCE_DEFINITION

    async def invoke(
        self, call: Call, input: GetAccountBalanceInput
    ) -> QueryAttempt[GetAccountBalanceOutput]:
        """Send one GET and classify provider, protocol, and transport outcomes."""
        if not isinstance(input, GetAccountBalanceInput):
            return _branch(DEFECT, FailureKind.VALIDATION, "balance input is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
            api_key = credentials.api_key.reveal()
        except Exception:
            return _branch(DEFECT, FailureKind.AUTHENTICATION, "connection API Key is unavailable or invalid")
        if not _valid_api_key(api_key):
            return _branch(DEFECT, FailureKind.AUTHENTICATION, "connection API Key is unavailable or invalid")
        try:
            request = self.client.http_client.build_request(
                "GET",
                self.client.base_url + "/v1/balance",
                headers={"Accept": "application/json", "Authorization": "Bearer " + api_key},
            )
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError):
            return _retry(FailureKind.AVAILABILITY, "Wuliang AI request failed")
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Wuliang AI rate limited the request", retry_after)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Wuliang AI is temporarily unavailable", retry_after)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    INVALID_RESPONSE,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Wuliang AI response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Wuliang AI response was interrupted")
            if status != 200:
                kind = FailureKind.PROVIDER_REJECTION
                if status == 401:
                    kind = FailureKind.AUTHENTICATION
                elif status == 403:
                    kind = FailureKind.AUTHORIZATION
                return _branch(PROVIDER_REJECTED, kind, f"Wuliang AI rejected the request with HTTP {status}")
            balance = _decode_balance(body)
            if balance is None:
                return _branch(
                    INVALID_RESPONSE, FailureKind.PROTOCOL, "Wuliang AI returned invalid balance data"
                )
            return QueryAttempt.branch_result(FETCHED, GetAccountBalanceOutput(balance), Receipt())
        finally:
            await response.aclose()


def _valid_api_key(value: object) -> bool:
    return isinstance(value, str) and 0 < len(value) <= 512 and all(0x21 <= ord(c) <= 0x7E for c in value)


def _decode_balance(body: bytes) -> str | None:
    try:
        wire: Any = json.loads(body.decode("utf-8"), parse_int=str, parse_float=str)
    except (UnicodeDecodeError, ValueError):
        return None
    if not isinstance(wire, dict) or wire.get("object") != "balance" or wire.get("currency") != "CNY":
        return None
    balance = wire.get("balance")
    if not isinstance(balance, str) or len(balance) > 64 or not _DECIMAL.fullmatch(balance):
        return None
    return balance


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[GetAccountBalanceOutput]:
    return QueryAttempt.branch_result(
        branch,
        GetAccountBalanceOutput(),
        Receipt(),
        Failure(kind, CONNECTOR_ID, GET_ACCOUNT_BALANCE_OPERATION_ID, message),
    )


def _retry(kind: FailureKind, message: str, after: float = 0) -> QueryAttempt[GetAccountBalanceOutput]:
    return QueryAttempt.retry(Failure(kind, CONNECTOR_ID, GET_ACCOUNT_BALANCE_OPERATION_ID, message), after)
