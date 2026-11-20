# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Tushare Pro trading-calendar transport and Go-compatible Query decisions."""

import json
from datetime import UTC, datetime

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
from ._generated.definitions import GET_TRADING_DAY_DEFINITION
from .models import GetTradingDayInput, GetTradingDayOutput


def _valid_date(value: str) -> bool:
    try:
        return datetime.strptime(value, "%Y%m%d").strftime("%Y%m%d") == value
    except (TypeError, ValueError):
        return False


def _valid_token(value: str) -> bool:
    return (
        isinstance(value, str)
        and 0 < len(value) <= 1024
        and all(0x20 < ord(character) < 0x7F for character in value)
    )


class Client:
    """Resolve a Tushare token for each trading calendar Query."""

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
            raise ValueError("Tushare configuration and credentials are required")
        self.config = config
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_http_client = http_client is None

    def get_trading_day(self) -> "GetTradingDayOperation":
        """Bind the getTradingDay Query."""
        return GetTradingDayOperation(self)

    async def aclose(self) -> None:
        """Close the owned HTTP transport."""
        if self._owns_http_client:
            await self.http_client.aclose()


class GetTradingDayOperation:
    """Read one exchange calendar date from Tushare Pro."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query definition."""
        return GET_TRADING_DAY_DEFINITION

    def _branch(self, branch: str, kind: FailureKind) -> QueryAttempt[GetTradingDayOutput]:
        return QueryAttempt.branch_result(
            branch,
            GetTradingDayOutput(),
            Receipt(),
            Failure(kind, "tushare", "getTradingDay", "Tushare calendar query failed"),
        )

    def _retry(self, kind: FailureKind, delay: float = 0) -> QueryAttempt[GetTradingDayOutput]:
        return QueryAttempt.retry(
            Failure(kind, "tushare", "getTradingDay", "Tushare calendar request failed"),
            delay,
        )

    async def invoke(self, call: Call, input: GetTradingDayInput) -> QueryAttempt[GetTradingDayOutput]:
        """Read a bounded provider response without persisting the API token."""
        if not isinstance(input, GetTradingDayInput) or input.exchange not in ("SSE", "SZSE", "BSE"):
            return self._branch("defect", FailureKind.VALIDATION)
        if not _valid_date(input.date):
            return self._branch("defect", FailureKind.VALIDATION)
        try:
            credentials = await self.client.credentials.resolve(call)
        except (ValueError, LookupError, OSError, AttributeError):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        token = credentials.token.reveal()
        if not _valid_token(token):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        body = {
            "api_name": "trade_cal",
            "token": token,
            "params": {"exchange": input.exchange, "start_date": input.date, "end_date": input.date},
            "fields": "exchange,cal_date,is_open,pretrade_date",
        }
        request = self.client.http_client.build_request(
            "POST",
            "https://api.tushare.pro",
            content=json.dumps(body, ensure_ascii=False, separators=(",", ":")).encode(),
            headers={"Accept": "application/json", "Content-Type": "application/json"},
        )
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return self._retry(FailureKind.AVAILABILITY)
        try:
            status = response.status_code
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return self._retry(FailureKind.RATE_LIMIT, delay)
            if status == 408 or status >= 500:
                return self._retry(FailureKind.AVAILABILITY, delay)
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return self._branch("providerRejected", kind)
            try:
                response_body = await read_bounded_body(response, self.client.config.max_response_bytes)
            except BodyTooLargeError:
                return self._branch("invalidResponse", FailureKind.RESPONSE_TOO_LARGE)
            except ProviderTransportError:
                return self._retry(FailureKind.AVAILABILITY)
        finally:
            await response.aclose()
        try:
            wire = json.loads(response_body)
        except (UnicodeDecodeError, ValueError):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        if (
            not isinstance(wire, dict)
            or not isinstance(wire.get("code"), int)
            or isinstance(wire["code"], bool)
        ):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        code = wire["code"]
        if code == 2002:
            return self._branch("providerRejected", FailureKind.AUTHORIZATION)
        if code == 429:
            return self._retry(FailureKind.RATE_LIMIT, delay)
        if 500 <= code <= 599:
            return self._retry(FailureKind.AVAILABILITY, delay)
        if code != 0:
            return self._branch("providerRejected", FailureKind.PROVIDER_REJECTION)
        data = wire.get("data")
        if (
            not isinstance(data, dict)
            or data.get("fields") != ["exchange", "cal_date", "is_open", "pretrade_date"]
            or not isinstance(data.get("items"), list)
        ):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        items = data["items"]
        if not items:
            return self._branch("noData", FailureKind.NOT_FOUND)
        if len(items) != 1 or not isinstance(items[0], list) or len(items[0]) != 4:
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        exchange, date, is_open, previous = items[0]
        if (
            exchange != input.exchange
            or date != input.date
            or not isinstance(is_open, int)
            or isinstance(is_open, bool)
            or is_open not in (0, 1)
            or not isinstance(previous, str)
            or (previous != "" and len(previous) != 8)
        ):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        return QueryAttempt.branch_result(
            "found", GetTradingDayOutput(exchange, date, is_open == 1, previous), Receipt()
        )
