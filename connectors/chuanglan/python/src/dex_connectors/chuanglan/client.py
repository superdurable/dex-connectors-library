# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Chuanglan 253 balance lookup and Go-compatible outcome classification."""

from __future__ import annotations

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
    CONNECTOR_ID as CONNECTOR_ID,
)
from ._generated.definitions import (
    DEFECT as DEFECT,
)
from ._generated.definitions import (
    FETCHED as FETCHED,
)
from ._generated.definitions import (
    GET_BALANCE_DEFINITION as GET_BALANCE_DEFINITION,
)
from ._generated.definitions import (
    GET_BALANCE_OPERATION_ID as GET_BALANCE_OPERATION_ID,
)
from ._generated.definitions import (
    INVALID_RESPONSE as INVALID_RESPONSE,
)
from ._generated.definitions import (
    PROVIDER_REJECTED as PROVIDER_REJECTED,
)
from .models import GetBalanceInput, GetBalanceOutput

_DEFAULT_BASE_URL = "https://smssh1.253.com"
_DEFAULT_TIMEOUT_SECONDS = 10
_DEFAULT_MAX_RESPONSE_BYTES = 65_536
_INT64_MIN = -(2**63)
_INT64_MAX = 2**63 - 1


def _valid_credential(value: str) -> bool:
    return bool(value) and len(value.encode()) <= 512 and all(33 <= ord(c) <= 126 for c in value)


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, CONNECTOR_ID, GET_BALANCE_OPERATION_ID, message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[GetBalanceOutput]:
    return QueryAttempt.branch_result(branch, GetBalanceOutput(), Receipt(), _failure(kind, message))


def _wire_field(document: dict[str, Any], name: str) -> Any:
    if name in document:
        return document[name]
    return next((value for key, value in document.items() if key.casefold() == name.casefold()), None)


def _parse_int64(text: str) -> int:
    if not re.fullmatch(r"[+-]?[0-9]+", text):
        raise ValueError("invalid integer")
    result = int(text)
    if not _INT64_MIN <= result <= _INT64_MAX:
        raise ValueError("integer overflow")
    return result


class Client:
    """Own immutable endpoint settings and resolve the current API credentials per lookup."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        """Validate local settings without contacting the provider."""
        if config is None or credentials is None:
            raise ValueError("Chuanglan settings and credential source are required")
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("Chuanglan timeout and maxResponseBytes must be positive")
        self.base_url = validate_base_url(config.base_url or _DEFAULT_BASE_URL)
        self.max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(
            config.timeout_seconds or _DEFAULT_TIMEOUT_SECONDS
        )

    def get_balance(self) -> GetBalanceOperation:
        """Bind the read-only balance Query."""
        return GetBalanceOperation(self)

    async def aclose(self) -> None:
        """Close the owned HTTP client after Worker shutdown."""
        await self.http_client.aclose()


class GetBalanceOperation:
    """Read the number of domestic SMS messages remaining in the connected account."""

    def __init__(self, client: Client) -> None:
        """Bind a validated client."""
        if client is None:
            raise ValueError("Chuanglan client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query identity and branches."""
        return GET_BALANCE_DEFINITION

    async def invoke(self, call: Call, input: GetBalanceInput) -> QueryAttempt[GetBalanceOutput]:
        """POST one balance lookup and classify a definite result or safe Retry."""
        try:
            credentials = await self.client.credentials.resolve(call)
            account = credentials.account.reveal()
            password = credentials.password.reveal()
        except Exception:  # noqa: BLE001
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
            )
        if not _valid_credential(account) or not _valid_credential(password):
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
            )
        try:
            body = json.dumps({"account": account, "password": password}, separators=(",", ":")).encode()
            request = self.client.http_client.build_request(
                "POST",
                self.client.base_url + "/msg/balance/json",
                content=body,
                headers={"Content-Type": "application/json", "Accept": "application/json"},
            )
        except (httpx.HTTPError, ValueError, TypeError):
            return _branch(DEFECT, FailureKind.VALIDATION, "balance request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return QueryAttempt.retry(_failure(FailureKind.AVAILABILITY, "Chuanglan request failed"))
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return QueryAttempt.retry(
                    _failure(FailureKind.RATE_LIMIT, "Chuanglan rate limit reached"), retry_after
                )
            if status == 408 or status >= 500:
                return QueryAttempt.retry(
                    _failure(FailureKind.AVAILABILITY, "Chuanglan is temporarily unavailable"),
                    retry_after,
                )
            try:
                contents = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    INVALID_RESPONSE,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Chuanglan response exceeded the configured limit",
                )
            except ProviderTransportError:
                return QueryAttempt.retry(
                    _failure(FailureKind.AVAILABILITY, "Chuanglan response was interrupted")
                )
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status in (401, 403)
                    else FailureKind.VALIDATION
                    if status == 400
                    else FailureKind.PROVIDER_REJECTION
                )
                return _branch(PROVIDER_REJECTED, kind, "Chuanglan rejected the balance lookup")
            try:
                document = json.loads(contents)
                if not isinstance(document, dict):
                    raise ValueError
                code = _wire_field(document, "code")
                if code is None or isinstance(code, (bool, list, dict)):
                    raise ValueError
                code_text = str(code)
                if code_text != "0":
                    _parse_int64(code_text)
                    return _branch(
                        PROVIDER_REJECTED,
                        FailureKind.PROVIDER_REJECTION,
                        "Chuanglan rejected the balance lookup",
                    )
                balance = _wire_field(document, "balance")
                if not isinstance(balance, str):
                    raise ValueError
                remaining_messages = _parse_int64(balance)
                if remaining_messages < 0:
                    raise ValueError
            except (ValueError, TypeError, UnicodeDecodeError):
                return _branch(
                    INVALID_RESPONSE,
                    FailureKind.PROTOCOL,
                    "Chuanglan returned an invalid balance response",
                )
            return QueryAttempt.branch_result(FETCHED, GetBalanceOutput(remaining_messages), Receipt())
        finally:
            await response.aclose()


Config.__doc__ = "Chuanglan HTTPS node and response bounds; zero or blank values use manifest defaults."
Credentials.__doc__ = "Private API account and password, resolved for every balance lookup."
