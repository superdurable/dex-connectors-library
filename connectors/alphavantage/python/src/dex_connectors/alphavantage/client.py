# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Alpha Vantage provider reads and Go-compatible branch classification."""

import json
import re
from collections.abc import Callable
from datetime import UTC, datetime, timedelta
from typing import Any, TypeVar
from urllib.parse import urlencode

import httpx
from dex_connectors.sdk import (
    BranchDefinition,
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    QueryAttempt,
    QueryDefinition,
    ReauthorizationRequiredError,
    Receipt,
    StepDefaults,
)
from dex_connectors.sdk.providerhttp import (
    BodyTooLargeError,
    ProviderTransportError,
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
    validate_base_url,
)
from dex_connectors.sdk.retry_policy import default_connector_step_execute_retry_policy

from ._generated.config import (
    Config as Config,
)
from ._generated.config import (
    Credentials as Credentials,
)
from ._generated.definitions import (
    CONNECTOR_ID as CONNECTOR_ID,
)
from ._generated.definitions import (
    DEFECT as DEFECT,
)
from ._generated.definitions import (
    FOUND as FOUND,
)
from ._generated.definitions import (
    GET_GLOBAL_QUOTE_DEFINITION as GET_GLOBAL_QUOTE_DEFINITION,
)
from ._generated.definitions import (
    GET_GLOBAL_QUOTE_OPERATION_ID as GET_GLOBAL_QUOTE_OPERATION_ID,
)
from ._generated.definitions import (
    GET_TIME_SERIES_DAILY_DEFINITION as GET_TIME_SERIES_DAILY_DEFINITION,
)
from ._generated.definitions import (
    GET_TIME_SERIES_DAILY_OPERATION_ID as GET_TIME_SERIES_DAILY_OPERATION_ID,
)
from ._generated.definitions import (
    INVALID_RESPONSE as INVALID_RESPONSE,
)
from ._generated.definitions import (
    NOT_FOUND as NOT_FOUND,
)
from ._generated.definitions import (
    PROVIDER_REJECTED as PROVIDER_REJECTED,
)
from .models import DailyPrice, DailySeriesOutput, GlobalQuoteOutput, StockInput

_DEFAULT_BASE_URL = "https://www.alphavantage.co"
_DEFAULT_TIMEOUT_SECONDS = 15
_DEFAULT_MAX_RESPONSE_BYTES = 1_048_576
_SYMBOL = re.compile(r"[A-Za-z0-9][A-Za-z0-9._-]{0,24}")
_RATE_LIMIT_PHRASES = ("rate limit", "call frequency", "requests per day")
_STEP_DEFAULTS = StepDefaults(
    timedelta(seconds=30), 2, execute_retry=default_connector_step_execute_retry_policy()
)
_BRANCHES = (
    BranchDefinition(FOUND, "Alpha Vantage returned the requested result."),
    BranchDefinition(NOT_FOUND, "No market data exists for the requested symbol.", True),
    BranchDefinition(PROVIDER_REJECTED, "The provider conclusively rejected the request.", True),
    BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized data.", True),
    BranchDefinition(DEFECT, "Local input or credentials are invalid.", True),
)


class _NoDataError(ValueError):
    """The response was well formed but held no data for the symbol."""


Output = TypeVar("Output")






class Client:
    """One redirect-safe HTTP client and call-time credential source."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None:
            raise ValueError("alphavantage configuration is required")
        if credentials is None:
            raise ValueError("alphavantage credential source is required")
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("alphavantage timeout and maxResponseBytes must be positive")
        self.base_url = validate_base_url(config.base_url or _DEFAULT_BASE_URL)
        self.max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(
            config.timeout_seconds or _DEFAULT_TIMEOUT_SECONDS
        )

    def get_global_quote(self) -> "GetGlobalQuoteOperation":
        """Bind the global quote Query to this client."""
        return GetGlobalQuoteOperation(self)

    def get_time_series_daily(self) -> "GetTimeSeriesDailyOperation":
        """Bind the daily series Query to this client."""
        return GetTimeSeriesDailyOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider HTTP client after application shutdown."""
        await self.http_client.aclose()

    async def invoke_json_query(
        self,
        call: Call,
        operation: str,
        path: str,
        query: dict[str, str],
        empty: Output,
        decode: Callable[[Any], Output],
    ) -> QueryAttempt[Output]:
        """Send one GET and classify it exactly like the Go invokeJSONQuery helper.

        Alpha Vantage accepts the key only as the apikey query parameter, and
        reports rate limits and rejections in an HTTP 200 body.
        """
        try:
            credentials = await self.credentials.resolve(call)
        except (ValueError, LookupError, OSError, ReauthorizationRequiredError):
            return _failed(operation, DEFECT, empty, FailureKind.AUTHENTICATION, _CREDENTIAL_MESSAGE)
        key = credentials.api_key.reveal()
        if not key or not is_header_safe_credential(key):
            return _failed(operation, DEFECT, empty, FailureKind.AUTHENTICATION, _CREDENTIAL_MESSAGE)
        endpoint = self.base_url + path + "?" + urlencode(sorted({**query, "apikey": key}.items()))
        try:
            request = self.http_client.build_request("GET", endpoint, headers={"Accept": "application/json"})
            response, _ = await send_traced(self.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _retry(operation, FailureKind.AVAILABILITY, "provider request failed", 0)
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _retry(
                    operation, FailureKind.RATE_LIMIT, "provider rate limited the request", retry_after
                )
            if status == 408 or status >= 500:
                return _retry(
                    operation, FailureKind.AVAILABILITY, "provider is temporarily unavailable", retry_after
                )
            if status == 404:
                return _failed(
                    operation, NOT_FOUND, empty, FailureKind.NOT_FOUND, "provider did not find the resource"
                )
            if status != 200:
                return _failed(
                    operation,
                    PROVIDER_REJECTED,
                    empty,
                    FailureKind.PROVIDER_REJECTION,
                    f"provider rejected the request with HTTP {status}",
                )
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _failed(
                    operation,
                    INVALID_RESPONSE,
                    empty,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "provider response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(operation, FailureKind.AVAILABILITY, "provider response was interrupted", 0)
            try:
                document = json.loads(body)
            except (ValueError, UnicodeDecodeError):
                document = None
            signal = _provider_signal(document)
            if signal is not None:
                is_rate_limit, message = signal
                if is_rate_limit:
                    return _retry(operation, FailureKind.RATE_LIMIT, message, 0)
                return _failed(operation, PROVIDER_REJECTED, empty, FailureKind.PROVIDER_REJECTION, message)
            try:
                value = decode(document)
            except _NoDataError:
                return _failed(
                    operation,
                    NOT_FOUND,
                    empty,
                    FailureKind.NOT_FOUND,
                    "Alpha Vantage returned no data for the symbol",
                )
            except (ValueError, TypeError):
                return _failed(
                    operation,
                    INVALID_RESPONSE,
                    empty,
                    FailureKind.PROTOCOL,
                    "provider returned an invalid response",
                )
            return QueryAttempt.branch_result(FOUND, value, Receipt())
        finally:
            await response.aclose()


class GetGlobalQuoteOperation:
    """Read the latest quote for a stock symbol."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("alphavantage client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return GET_GLOBAL_QUOTE_DEFINITION

    async def invoke(self, call: Call, input: StockInput) -> QueryAttempt[GlobalQuoteOutput]:
        """Validate the symbol, read once, then select a branch or safe retry."""
        operation = GET_GLOBAL_QUOTE_OPERATION_ID
        if not _is_symbol(input):
            return _failed(
                operation, DEFECT, GlobalQuoteOutput(), FailureKind.VALIDATION, "symbol is invalid"
            )
        query = {"function": "GLOBAL_QUOTE", "symbol": input.symbol}
        return await self.client.invoke_json_query(
            call, operation, "/query", query, GlobalQuoteOutput(), _decode_global_quote
        )


class GetTimeSeriesDailyOperation:
    """Read the compact daily price series for a stock symbol."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("alphavantage client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return GET_TIME_SERIES_DAILY_DEFINITION

    async def invoke(self, call: Call, input: StockInput) -> QueryAttempt[DailySeriesOutput]:
        """Validate the symbol, read once, then select a branch or safe retry."""
        operation = GET_TIME_SERIES_DAILY_OPERATION_ID
        if not _is_symbol(input):
            return _failed(
                operation, DEFECT, DailySeriesOutput(), FailureKind.VALIDATION, "symbol is invalid"
            )
        query = {"function": "TIME_SERIES_DAILY", "symbol": input.symbol, "outputsize": "compact"}
        symbol = input.symbol
        return await self.client.invoke_json_query(
            call,
            operation,
            "/query",
            query,
            DailySeriesOutput(),
            lambda document: _decode_daily(document, symbol),
        )


def _is_symbol(input: object) -> bool:
    symbol = getattr(input, "symbol", None)
    return isinstance(input, StockInput) and isinstance(symbol, str) and _SYMBOL.fullmatch(symbol) is not None


_CREDENTIAL_MESSAGE = "connection credentials are unavailable or invalid"


def _failed(
    operation: str, branch: str, value: Output, kind: FailureKind, message: str
) -> QueryAttempt[Output]:
    return QueryAttempt.branch_result(
        branch, value, Receipt(), Failure(kind, CONNECTOR_ID, operation, message)
    )


def _retry(operation: str, kind: FailureKind, message: str, after_seconds: float) -> QueryAttempt[Any]:
    return QueryAttempt.retry(Failure(kind, CONNECTOR_ID, operation, message), after_seconds)


def _provider_signal(document: Any) -> tuple[bool, str] | None:
    """Return (is_rate_limit, message) for an Alpha Vantage notice, or None for data."""
    if not isinstance(document, dict):
        return None
    if "Note" in document:
        return True, "Alpha Vantage rate limit notice"
    if "Information" in document:
        information = document["Information"]
        message = information.lower() if isinstance(information, str) else ""
        if any(phrase in message for phrase in _RATE_LIMIT_PHRASES):
            return True, "Alpha Vantage rate limit notice"
        return False, "Alpha Vantage rejected the request"
    if "Error Message" in document:
        return False, "Alpha Vantage rejected the request"
    return None


def _decode_global_quote(document: Any) -> GlobalQuoteOutput:
    quote = _object(document).get("Global Quote")
    quote = _object({} if quote is None else quote)
    output = GlobalQuoteOutput(
        _string(quote, "01. symbol"), _string(quote, "05. price"), _string(quote, "07. latest trading day")
    )
    if not output.symbol and not output.price and not output.latest_trading_day:
        raise _NoDataError
    if not output.symbol or not output.price or not output.latest_trading_day:
        raise ValueError
    return output


def _decode_daily(document: Any, symbol: str) -> DailySeriesOutput:
    series = _object(document).get("Time Series (Daily)")
    series = _object({} if series is None else series)
    if not series:
        raise _NoDataError
    days = []
    for date, value in series.items():
        day = _object(value)
        fields = [_string(day, name) for name in ("1. open", "2. high", "3. low", "4. close", "5. volume")]
        if not all(fields):
            raise ValueError
        days.append(DailyPrice(date, *fields))
    days.sort(key=lambda price: price.date, reverse=True)
    return DailySeriesOutput(symbol, tuple(days))


def _object(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ValueError
    return value


def _string(document: dict[str, Any], name: str) -> str:
    value = document.get(name)
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError
    return value
