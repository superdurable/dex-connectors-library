# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Yunpian SMS account balance Query with Go-compatible outcomes."""

import ipaddress
import json
import math
from datetime import UTC, datetime
from typing import Any
from urllib.parse import urlsplit

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
from .models import AccountBalance, GetAccountBalanceInput


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[AccountBalance]:
    return QueryAttempt.branch_result(
        branch, AccountBalance(), Receipt(), Failure(kind, "yunpian", "getAccountBalance", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[AccountBalance]:
    return QueryAttempt.retry(Failure(kind, "yunpian", "getAccountBalance", message), delay)


def _validated_base_url(value: str) -> str:
    if not value or any(ord(character) <= 0x20 or ord(character) > 0x7E for character in value):
        raise ValueError("Yunpian base URL must be printable ASCII")
    try:
        parsed = urlsplit(value)
        host = parsed.hostname
    except ValueError as error:
        raise ValueError("Yunpian base URL is invalid") from error
    if not host or parsed.username or parsed.password or parsed.query or parsed.fragment:
        raise ValueError("Yunpian base URL is invalid")
    if parsed.scheme != "https":
        try:
            is_loopback = host == "localhost" or ipaddress.ip_address(host).is_loopback
        except ValueError:
            is_loopback = host == "localhost"
        if parsed.scheme != "http" or not is_loopback:
            raise ValueError("Yunpian base URL must use HTTPS")
    return value.rstrip("/")


class Client:
    """Own Yunpian settings and resolve the API key for each balance Query."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("configuration and credentials are required")
        if config.timeout_seconds <= 0 or config.max_response_bytes <= 0:
            raise ValueError("Yunpian timeout and response limit must be positive")
        self.base_url = _validated_base_url(config.base_url)
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_account_balance(self) -> "GetAccountBalanceOperation":
        """Bind the getAccountBalance Query."""
        return GetAccountBalanceOperation(self)

    async def aclose(self) -> None:
        """Close only an HTTP client this instance created."""
        if self._owns_client:
            await self.http_client.aclose()


class GetAccountBalanceOperation:
    """Read one Yunpian account's remaining SMS balance."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("provider client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query identity and retry policy."""
        return GET_ACCOUNT_BALANCE_DEFINITION

    async def invoke(self, call: Call, input: GetAccountBalanceInput) -> QueryAttempt[AccountBalance]:
        """Make one read-only provider request."""
        try:
            credentials = await self.client.credentials.resolve(call)
            api_key = credentials.api_key.reveal()
            if not api_key or api_key.strip() != api_key:
                raise ValueError("invalid key")
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Yunpian credentials are unavailable or invalid"
            )
        try:
            request = self.client.http_client.build_request(
                "POST",
                self.client.base_url + "/v2/user/get.json",
                headers={
                    "Accept": "application/json",
                    "Content-Type": "application/x-www-form-urlencoded; charset=utf-8",
                },
                data={"apikey": api_key},
            )
        except (httpx.HTTPError, ValueError):
            return _retry(FailureKind.TRANSPORT, "Yunpian request failed")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.TRANSPORT, "Yunpian request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Yunpian rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Yunpian is temporarily unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Yunpian response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.TRANSPORT, "Yunpian response was interrupted")
        finally:
            await response.aclose()
        return _decode_balance(status, body, delay)


def _decode_balance(status: int, body: bytes, delay: float) -> QueryAttempt[AccountBalance]:
    try:
        document: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "Yunpian returned invalid account JSON")
    if not isinstance(document, dict):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "Yunpian returned invalid account JSON")
    code = document.get("code")
    if code is not None:
        if type(code) is not int:
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Yunpian returned invalid account JSON")
        if code == -5:
            return _retry(FailureKind.RATE_LIMIT, "Yunpian query frequency exceeded", delay)
        if code in (-50, -51):
            return _retry(FailureKind.AVAILABILITY, "Yunpian reported a temporary failure", delay)
        if code != 0:
            return _branch(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                f"Yunpian rejected the account query with code {code}",
            )
    if status != 200:
        return _branch("providerRejected", FailureKind.PROVIDER_REJECTION, f"Yunpian rejected HTTP {status}")
    balance = document.get("balance")
    if balance is None:
        return _branch("invalidResponse", FailureKind.PROTOCOL, "Yunpian account balance is missing")
    if type(balance) not in (int, float) or balance < 0 or not math.isfinite(balance):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "Yunpian account balance is invalid")
    return QueryAttempt.branch_result("read", AccountBalance(float(balance)), Receipt())
