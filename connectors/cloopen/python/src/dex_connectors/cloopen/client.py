# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Signed Cloopen account lookup and Go-compatible balance classification."""

from __future__ import annotations

import base64
import hashlib
import json
import re
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
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import CONNECTOR_ID as CONNECTOR_ID
from ._generated.definitions import DEFECT as DEFECT
from ._generated.definitions import FETCHED as FETCHED
from ._generated.definitions import GET_BALANCE_DEFINITION as GET_BALANCE_DEFINITION
from ._generated.definitions import GET_BALANCE_OPERATION_ID as GET_BALANCE_OPERATION_ID
from ._generated.definitions import INVALID_RESPONSE as INVALID_RESPONSE
from ._generated.definitions import PROVIDER_REJECTED as PROVIDER_REJECTED
from .models import GetBalanceInput, GetBalanceOutput

_DEFAULT_BASE_URL = "https://app.cloopen.com:8883"
_DEFAULT_TIMEOUT_SECONDS = 10
_DEFAULT_MAX_RESPONSE_BYTES = 65_536
_BALANCE_PATTERN = re.compile(r"^(0|[1-9][0-9]*)(\.[0-9]+)?$")
_ACCOUNT_SID_PATTERN = re.compile(r"^[A-Za-z0-9]{32}$")


def _valid_auth_token(token: str) -> bool:
    return len(token) == 32 and all(33 <= ord(character) <= 126 for character in token)


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, CONNECTOR_ID, GET_BALANCE_OPERATION_ID, message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[GetBalanceOutput]:
    return QueryAttempt.branch_result(branch, GetBalanceOutput(), Receipt(), _failure(kind, message))


def _wire_field(document: dict[str, Any], name: str) -> Any:
    if name in document:
        return document[name]
    return next((value for key, value in document.items() if key.casefold() == name.casefold()), None)


class Client:
    """Own endpoint settings, resolve rotated credentials, and sign each lookup at call time."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        clock: Callable[[], datetime] | None = None,
    ) -> None:
        """Validate local settings and inject an optional deterministic signature clock."""
        if config is None or credentials is None:
            raise ValueError("Cloopen settings and credential source are required")
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("Cloopen timeout and maxResponseBytes must be positive")
        self.base_url = validate_base_url(config.base_url or _DEFAULT_BASE_URL)
        self.max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(
            config.timeout_seconds or _DEFAULT_TIMEOUT_SECONDS
        )
        self.clock = clock or (lambda: datetime.now(UTC))

    def get_balance(self) -> GetBalanceOperation:
        """Bind the read-only signed account Query."""
        return GetBalanceOperation(self)

    async def aclose(self) -> None:
        """Close the owned HTTP client after Worker shutdown."""
        await self.http_client.aclose()


class GetBalanceOperation:
    """Read the main-account balance without persisting the SID or auth token."""

    def __init__(self, client: Client) -> None:
        """Bind one validated client."""
        if client is None:
            raise ValueError("Cloopen client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query identity and branch contract."""
        return GET_BALANCE_DEFINITION

    async def invoke(self, call: Call, input: GetBalanceInput) -> QueryAttempt[GetBalanceOutput]:
        """Send one UTC-signed GET and classify a definite result or safe Retry."""
        try:
            credentials = await self.client.credentials.resolve(call)
            account_sid = credentials.account_sid.reveal()
            auth_token = credentials.auth_token.reveal()
        except Exception:  # noqa: BLE001
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
            )
        if not _ACCOUNT_SID_PATTERN.fullmatch(account_sid) or not _valid_auth_token(auth_token):
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
            )
        try:
            timestamp = self.client.clock().astimezone(UTC).strftime("%Y%m%d%H%M%S")
            # Cloopen's REST protocol requires this legacy MD5 signature.
            signature = (
                hashlib.md5(
                    (account_sid + auth_token + timestamp).encode()
                )
                .hexdigest()
                .upper()
            )
            authorization = base64.b64encode((account_sid + ":" + timestamp).encode()).decode()
            url = (
                self.client.base_url + "/2013-12-26/Accounts/" + account_sid + "/AccountInfo?sig=" + signature
            )
            request = self.client.http_client.build_request(
                "GET",
                url,
                headers={
                    "Authorization": authorization,
                    "Accept": "application/json",
                    "Content-Type": "application/json;charset=utf-8",
                },
            )
        except (httpx.HTTPError, ValueError, TypeError, OverflowError):
            return _branch(DEFECT, FailureKind.VALIDATION, "account request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return QueryAttempt.retry(_failure(FailureKind.AVAILABILITY, "Cloopen request failed"))
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return QueryAttempt.retry(
                    _failure(FailureKind.RATE_LIMIT, "Cloopen rate limit reached"), retry_after
                )
            if status == 408 or status >= 500:
                return QueryAttempt.retry(
                    _failure(FailureKind.AVAILABILITY, "Cloopen is temporarily unavailable"),
                    retry_after,
                )
            try:
                contents = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    INVALID_RESPONSE,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Cloopen response exceeded the configured limit",
                )
            except ProviderTransportError:
                return QueryAttempt.retry(
                    _failure(FailureKind.AVAILABILITY, "Cloopen response was interrupted")
                )
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status in (401, 403)
                    else FailureKind.VALIDATION
                    if status == 400
                    else FailureKind.PROVIDER_REJECTION
                )
                return _branch(PROVIDER_REJECTED, kind, "Cloopen rejected the account lookup")
            try:
                document = json.loads(contents)
                if not isinstance(document, dict):
                    raise ValueError
                status_code = _wire_field(document, "statusCode")
                if not isinstance(status_code, str) or not status_code:
                    raise ValueError
                if status_code != "000000":
                    if status_code == "111111":
                        return QueryAttempt.retry(
                            _failure(FailureKind.RATE_LIMIT, "Cloopen concurrency limit reached")
                        )
                    if status_code.startswith("1110"):
                        return QueryAttempt.retry(
                            _failure(FailureKind.AVAILABILITY, "Cloopen reported an internal failure")
                        )
                    return _branch(
                        PROVIDER_REJECTED,
                        FailureKind.PROVIDER_REJECTION,
                        "Cloopen rejected the account lookup",
                    )
                account = _wire_field(document, "Account")
                if not isinstance(account, dict):
                    raise ValueError
                balance = _wire_field(account, "balance")
                if (
                    not isinstance(balance, str)
                    or len(balance) > 64
                    or not _BALANCE_PATTERN.fullmatch(balance)
                ):
                    raise ValueError
            except (ValueError, TypeError, UnicodeDecodeError):
                return _branch(INVALID_RESPONSE, FailureKind.PROTOCOL, "Cloopen returned an invalid balance")
            return QueryAttempt.branch_result(FETCHED, GetBalanceOutput(balance), Receipt())
        finally:
            await response.aclose()


Config.__doc__ = "Cloopen HTTPS origin and response bounds; blank or zero values use manifest defaults."
Credentials.__doc__ = "Private 32-character main-account SID and auth token, resolved per lookup."
