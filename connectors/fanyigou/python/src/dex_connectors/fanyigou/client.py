# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Fanyigou account balance transport and Go-compatible decisions."""

import json
import secrets
from collections.abc import Callable
from datetime import UTC, datetime
from typing import Any
from urllib.parse import urlencode

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

_ENDPOINT = "https://www.fanyigou.com/TranslateApi/api/getAccount"


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[GetAccountBalanceOutput]:
    return QueryAttempt.branch_result(
        branch, GetAccountBalanceOutput(), Receipt(), Failure(kind, "fanyigou", "getAccountBalance", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[GetAccountBalanceOutput]:
    return QueryAttempt.retry(Failure(kind, "fanyigou", "getAccountBalance", message), delay)


def _valid_credential(value: str) -> bool:
    return (
        bool(value)
        and len(value.encode()) <= 1024
        and value.strip() == value
        and all(ord(character) >= 0x20 and ord(character) != 0x7F for character in value)
    )


class Client:
    """Resolve credentials for each account Query and own a redirect-safe transport."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        nonce_bytes: Callable[[int], bytes] | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Fanyigou configuration and credential source are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("Fanyigou timeout or response limit is invalid")
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None
        self.nonce_bytes = nonce_bytes or secrets.token_bytes

    def get_account_balance(self) -> "GetAccountBalanceOperation":
        """Bind the account balance Query to this client."""
        return GetAccountBalanceOperation(self)

    async def aclose(self) -> None:
        """Close the transport when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class GetAccountBalanceOperation:
    """Read the document-translation resource balance."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Fanyigou client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated account balance Query identity."""
        return GET_ACCOUNT_BALANCE_DEFINITION

    async def invoke(
        self, call: Call, input: GetAccountBalanceInput
    ) -> QueryAttempt[GetAccountBalanceOutput]:
        """Send one authorized account read and classify its provider outcome."""
        try:
            credentials = await self.client.credentials.resolve(call)
            app_id = credentials.app_id.reveal()
            private_key = credentials.private_key.reveal()
            if not _valid_credential(app_id) or not _valid_credential(private_key):
                raise ValueError("invalid credentials")
        except (ValueError, LookupError, OSError):
            return _branch(
                "defect",
                FailureKind.AUTHENTICATION,
                "Fanyigou application credentials are unavailable or invalid",
            )
        try:
            nonce = self.client.nonce_bytes(16)
            if len(nonce) != 16:
                raise ValueError("nonce length")
        except (ValueError, OSError):
            return _retry(FailureKind.AVAILABILITY, "Fanyigou request nonce is unavailable")
        try:
            request = self.client.http_client.build_request(
                "POST",
                _ENDPOINT,
                content=urlencode({"appid": app_id, "nonce_str": nonce.hex(), "type": "1"}),
                headers={
                    "Content-Type": "application/x-www-form-urlencoded; charset=UTF-8",
                    "Accept": "application/json",
                    "Authorization": "Bearer " + private_key,
                },
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "Fanyigou account request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "Fanyigou account request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Fanyigou rate limited account request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Fanyigou API is temporarily unavailable", delay)
            if status != 200:
                return _branch(
                    "providerRejected",
                    FailureKind.PROVIDER_REJECTION,
                    f"Fanyigou rejected account request with HTTP {status}",
                )
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Fanyigou account response exceeded configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Fanyigou account response was interrupted")
        finally:
            await response.aclose()
        return _decode_account(body, delay)


def _decode_account(body: bytes, delay: float) -> QueryAttempt[GetAccountBalanceOutput]:
    try:
        wire: Any = json.loads(
            body,
            object_hook=lambda fields: {key.lower(): value for key, value in fields.items()},
            parse_constant=lambda _: (_ for _ in ()).throw(ValueError("invalid number")),
        )
    except (ValueError, UnicodeDecodeError):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "Fanyigou returned malformed account data")
    if not isinstance(wire, dict) or not _int64(wire.get("code")):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "Fanyigou returned malformed account data")
    code = wire["code"]
    data = wire.get("data")
    if data is not None and (
        not isinstance(data, dict)
        or any(data.get(field) is not None and not _int64(data[field]) for field in ("deducttype", "balance"))
    ):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "Fanyigou returned malformed resource data")
    if code in (102, 105):
        return _retry(FailureKind.AVAILABILITY, "Fanyigou account service is unavailable", delay)
    if code == 108:
        return _retry(FailureKind.RATE_LIMIT, "Fanyigou rate limited account request", delay)
    if code != 100:
        return _branch(
            "providerRejected",
            FailureKind.PROVIDER_REJECTION,
            f"Fanyigou rejected account request with code {code}",
        )
    if (
        not isinstance(data, dict)
        or type(data.get("deducttype")) is not int
        or data["deducttype"] not in (0, 1)
        or type(data.get("balance")) is not int
        or data["balance"] < 0
    ):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "Fanyigou omitted a valid document balance")
    return QueryAttempt.branch_result(
        "found", GetAccountBalanceOutput(deduct_type=data["deducttype"], balance=data["balance"]), Receipt()
    )


def _int64(value: object) -> bool:
    return type(value) is int and -(2**63) <= value < 2**63
