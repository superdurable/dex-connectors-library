# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""OpenWeatherMap provider reads and Go-compatible branch classification."""

import json
import math
from collections.abc import Callable
from dataclasses import dataclass as dataclass
from datetime import UTC, datetime
from datetime import timedelta as timedelta
from decimal import Decimal
from typing import Any, TypeVar
from urllib.parse import urlencode

import httpx
from dex_connectors.sdk import BranchDefinition as BranchDefinition
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    QueryAttempt,
    QueryDefinition,
    ReauthorizationRequiredError,
    Receipt,
)
from dex_connectors.sdk import OperationRef as OperationRef
from dex_connectors.sdk import SecretString as SecretString
from dex_connectors.sdk import StepDefaults as StepDefaults
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
from dex_connectors.sdk.retry_policy import (
    default_connector_step_execute_retry_policy as default_connector_step_execute_retry_policy,
)

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
    GET_CURRENT_WEATHER_DEFINITION as GET_CURRENT_WEATHER_DEFINITION,
)
from ._generated.definitions import (
    GET_CURRENT_WEATHER_OPERATION_ID as GET_CURRENT_WEATHER_OPERATION_ID,
)
from ._generated.definitions import (
    GET_FORECAST_DEFINITION as GET_FORECAST_DEFINITION,
)
from ._generated.definitions import (
    GET_FORECAST_OPERATION_ID as GET_FORECAST_OPERATION_ID,
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
from .models import CurrentWeatherOutput, ForecastEntry, ForecastOutput, WeatherInput

UNITS = ("standard", "metric", "imperial")
_DEFAULT_BASE_URL = "https://api.openweathermap.org"
_DEFAULT_TIMEOUT_SECONDS = 15
_DEFAULT_MAX_RESPONSE_BYTES = 1_048_576

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
            raise ValueError("openweathermap configuration is required")
        if credentials is None:
            raise ValueError("openweathermap credential source is required")
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("openweathermap timeout and maxResponseBytes must be positive")
        self.base_url = validate_base_url(config.base_url or _DEFAULT_BASE_URL)
        self.max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(
            config.timeout_seconds or _DEFAULT_TIMEOUT_SECONDS
        )

    def get_current_weather(self) -> "GetCurrentWeatherOperation":
        """Bind the current weather Query to this client."""
        return GetCurrentWeatherOperation(self)

    def get_forecast(self) -> "GetForecastOperation":
        """Bind the forecast Query to this client."""
        return GetForecastOperation(self)

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

        OpenWeatherMap accepts the key only as the appid query parameter.
        """
        try:
            credentials = await self.credentials.resolve(call)
        except (ValueError, LookupError, OSError, ReauthorizationRequiredError):
            return _failed(operation, DEFECT, empty, FailureKind.AUTHENTICATION, _CREDENTIAL_MESSAGE)
        key = credentials.api_key.reveal()
        if not key or not is_header_safe_credential(key):
            return _failed(operation, DEFECT, empty, FailureKind.AUTHENTICATION, _CREDENTIAL_MESSAGE)
        endpoint = self.base_url + path + "?" + urlencode(sorted({**query, "appid": key}.items()))
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
                value = decode(json.loads(body))
            except (ValueError, TypeError, UnicodeDecodeError):
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


class GetCurrentWeatherOperation:
    """Read the current weather at a coordinate."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("openweathermap client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return GET_CURRENT_WEATHER_DEFINITION

    async def invoke(self, call: Call, input: WeatherInput) -> QueryAttempt[CurrentWeatherOutput]:
        """Validate the coordinate, read once, then select a branch or safe retry."""
        operation = GET_CURRENT_WEATHER_OPERATION_ID
        query = weather_query(input)
        if isinstance(query, str):
            return _failed(operation, DEFECT, CurrentWeatherOutput(), FailureKind.VALIDATION, query)
        return await self.client.invoke_json_query(
            call, operation, "/data/2.5/weather", query, CurrentWeatherOutput(), _decode_current_weather
        )


class GetForecastOperation:
    """Read the five-day, three-hour forecast at a coordinate."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("openweathermap client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return GET_FORECAST_DEFINITION

    async def invoke(self, call: Call, input: WeatherInput) -> QueryAttempt[ForecastOutput]:
        """Validate the coordinate, read once, then select a branch or safe retry."""
        operation = GET_FORECAST_OPERATION_ID
        query = weather_query(input)
        if isinstance(query, str):
            return _failed(operation, DEFECT, ForecastOutput(), FailureKind.VALIDATION, query)
        return await self.client.invoke_json_query(
            call, operation, "/data/2.5/forecast", query, ForecastOutput(), _decode_forecast
        )


def weather_query(input: WeatherInput) -> dict[str, str] | str:
    """Return the coordinate query, or the validation message the Go connector reports."""
    if not isinstance(input, WeatherInput) or any(
        type(value) not in (int, float) or not math.isfinite(value)
        for value in (input.latitude, input.longitude)
    ):
        return "latitude or longitude is outside its degree range"
    if not -90 <= input.latitude <= 90 or not -180 <= input.longitude <= 180:
        return "latitude or longitude is outside its degree range"
    units = input.units or "standard"
    if units not in UNITS:
        return "units must be standard, metric, or imperial"
    return {"lat": _format_degrees(input.latitude), "lon": _format_degrees(input.longitude), "units": units}


_CREDENTIAL_MESSAGE = "connection credentials are unavailable or invalid"


def _format_degrees(value: float) -> str:
    """Match Go strconv.FormatFloat(value, 'f', -1, 64): shortest digits, no exponent."""
    text = format(Decimal(repr(float(value))), "f")
    if "." in text:
        text = text.rstrip("0").rstrip(".")
    return "0" if text == "-0" else text


def _failed(
    operation: str, branch: str, value: Output, kind: FailureKind, message: str
) -> QueryAttempt[Output]:
    return QueryAttempt.branch_result(
        branch, value, Receipt(), Failure(kind, CONNECTOR_ID, operation, message)
    )


def _retry(operation: str, kind: FailureKind, message: str, after_seconds: float) -> QueryAttempt[Any]:
    return QueryAttempt.retry(Failure(kind, CONNECTOR_ID, operation, message), after_seconds)


def _decode_current_weather(document: Any) -> CurrentWeatherOutput:
    document = _object(document)
    temperature = _number(_object(document.get("main") or {}), "temp")
    weather = document.get("weather")
    if temperature is None or not isinstance(weather, list) or not weather:
        raise ValueError
    return CurrentWeatherOutput(
        _string(document, "name"), temperature, _string(_object(weather[0]), "description")
    )


def _decode_forecast(document: Any) -> ForecastOutput:
    document = _object(document)
    points = document.get("list")
    if not isinstance(points, list):
        raise ValueError
    entries = []
    for point in map(_object, points):
        timestamp = point.get("dt")
        temperature = _number(_object(point.get("main") or {}), "temp")
        if type(timestamp) is not int or timestamp <= 0 or temperature is None:
            raise ValueError
        entries.append(ForecastEntry(timestamp, temperature))
    return ForecastOutput(_string(_object(document.get("city") or {}), "name"), tuple(entries))


def _object(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ValueError
    return value


def _number(document: dict[str, Any], name: str) -> float | None:
    value = document.get(name)
    if value is None:
        return None
    if type(value) not in (int, float):
        raise ValueError
    return float(value)


def _string(document: dict[str, Any], name: str) -> str:
    value = document.get(name)
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError
    return value


Config.__doc__ = "OpenWeatherMap HTTP settings; zero or blank values use manifest defaults."
Credentials.__doc__ = "OpenWeatherMap API key, resolved anew for every provider attempt."
