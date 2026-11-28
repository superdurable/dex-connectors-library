# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""textin provider transport and Go-compatible Query decisions."""

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
from ._generated.definitions import GET_ACCOUNT_BALANCE_DEFINITION
from .models import GetAccountBalanceInput, GetAccountBalanceOutput


class _ExactJSONDecimal(str):
    """Preserve the provider number spelling for Go-compatible T-coin output."""


def _integer_code(value: object) -> int | None:
    if (
        not isinstance(value, _ExactJSONDecimal)
        or len(value) > 20
        or not re.fullmatch(r"-?(?:0|[1-9][0-9]*)", value)
    ):
        return None
    result = int(value)
    return result if -(2**63) <= result < 2**63 else None


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[GetAccountBalanceOutput]:
    return QueryAttempt.branch_result(
        branch, GetAccountBalanceOutput(), Receipt(), Failure(kind, "textin", "getAccountBalance", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[GetAccountBalanceOutput]:
    return QueryAttempt.retry(Failure(kind, "textin", "getAccountBalance", message), delay)


class Client:
    """Resolve credentials per getAccountBalance request and own a redirect-safe HTTP client."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("textin configuration and credentials are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("textin timeout or response limit is invalid")
        self.base_url = validate_base_url(config.base_url)
        if httpx.URL(self.base_url).path not in ("", "/"):
            raise ValueError("Balance API URL must be a host root")
        self.config = config
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_account_balance(self) -> "GetAccountBalanceOperation":
        """Bind the getAccountBalance Query to this client."""
        return GetAccountBalanceOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client if this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class GetAccountBalanceOperation:
    """Perform the getAccountBalance read and return bounded provider data."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("textin client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Go-derived Query identity."""
        return GET_ACCOUNT_BALANCE_DEFINITION

    async def invoke(
        self, call: Call, input: GetAccountBalanceInput
    ) -> QueryAttempt[GetAccountBalanceOutput]:
        """Classify transport and provider outcomes without exposing secrets."""
        try:
            credentials = await self.client.credentials.resolve(call)
            request = self._build_request(input, credentials)
        except (ValueError, LookupError, OSError, httpx.HTTPError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "connection credentials or request are invalid"
            )
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "textin request failed")
        return await self._process_response(response, input, credentials)

    async def _process_response(
        self, response: httpx.Response, input: GetAccountBalanceInput, credentials: Credentials
    ) -> QueryAttempt[GetAccountBalanceOutput]:
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "textin rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "textin is temporarily unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "textin response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "textin response was interrupted")
        finally:
            await response.aclose()
        if status != 200:
            kind = (
                FailureKind.AUTHENTICATION
                if status == 401
                else FailureKind.AUTHORIZATION
                if status == 403
                else FailureKind.PROVIDER_REJECTION
            )
            return _branch("providerRejected", kind, f"textin rejected request with HTTP {status}")
        try:
            document: Any = json.loads(
                body,
                parse_int=_ExactJSONDecimal,
                parse_float=_ExactJSONDecimal,
                object_hook=lambda fields: {name.lower(): value for name, value in fields.items()},
                parse_constant=lambda _: (_ for _ in ()).throw(ValueError("invalid JSON number")),
            )
        except (ValueError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "textin returned invalid JSON")
        return self._decode(input, document)

    def _build_request(self, input: GetAccountBalanceInput, credentials: Credentials) -> httpx.Request:
        if not all(
            re.fullmatch(r"[!-~]{1,512}", field)
            for field in (credentials.app_id, credentials.secret_code.reveal())
        ):
            raise ValueError("missing credentials")
        return self.client.http_client.build_request(
            "GET",
            self.client.base_url + "/user/finance/balance",
            headers={
                "Accept": "application/json",
                "x-ti-app-id": credentials.app_id,
                "x-ti-secret-code": credentials.secret_code.reveal(),
            },
        )

    def _decode(self, input: GetAccountBalanceInput, document: Any) -> QueryAttempt[GetAccountBalanceOutput]:
        if not isinstance(document, dict):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "invalid balance response")
        code = _integer_code(document.get("code"))
        data = document.get("data")
        if code is None or (data is not None and not isinstance(data, dict)):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "invalid balance response")
        payment = data.get("pay_type") if data else None
        pay_type = _integer_code(payment)
        balance = data.get("available_balance") if data else None
        if (payment is not None and pay_type is None) or (
            balance is not None
            and (
                not isinstance(balance, str)
                or not re.fullmatch(r"-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?", balance)
            )
        ):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "invalid balance data")
        if code == 500:
            return _retry(FailureKind.AVAILABILITY, "balance service unavailable")
        if code != 200:
            return _branch(
                "providerRejected",
                FailureKind.AUTHENTICATION if code in (40101, 40102) else FailureKind.PROVIDER_REJECTION,
                "balance request rejected",
            )
        if balance is None or len(balance) > 64 or pay_type not in (1, 2):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "invalid balance data")
        return QueryAttempt.branch_result(
            "fetched",
            GetAccountBalanceOutput(available_t_coins=balance, pay_type=pay_type),
            Receipt(),
        )
