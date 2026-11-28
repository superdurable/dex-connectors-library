# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Juhe provider transport and Go-compatible Query decisions."""

import json
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
from ._generated.definitions import GET_CURRENT_WEATHER_DEFINITION
from .models import GetCurrentWeatherInput, GetCurrentWeatherOutput


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[GetCurrentWeatherOutput]:
    return QueryAttempt.branch_result(
        branch, GetCurrentWeatherOutput(), Receipt(), Failure(kind, "juhe", "getCurrentWeather", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[GetCurrentWeatherOutput]:
    return QueryAttempt.retry(Failure(kind, "juhe", "getCurrentWeather", message), delay)


def _ascii_value(value: str, limit: int, allow_empty: bool = False) -> bool:
    return (
        isinstance(value, str)
        and (allow_empty or bool(value))
        and len(value.encode()) <= limit
        and all(0x20 < ord(c) < 0x7F for c in value)
    )


class Client:
    """Resolve credentials per getCurrentWeather request and own a redirect-safe HTTP client."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Juhe configuration and credentials are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("Juhe timeout or response limit is invalid")
        self.base_url = validate_base_url(config.base_url)
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_current_weather(self) -> "GetCurrentWeatherOperation":
        """Bind the getCurrentWeather Query to this client."""
        return GetCurrentWeatherOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client if this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class GetCurrentWeatherOperation:
    """Perform one getCurrentWeather read and filter the provider response."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Juhe client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Go-derived Query identity."""
        return GET_CURRENT_WEATHER_DEFINITION

    async def invoke(
        self, call: Call, input: GetCurrentWeatherInput
    ) -> QueryAttempt[GetCurrentWeatherOutput]:
        """Classify transport and provider outcomes without exposing secrets."""
        try:
            if (
                not input.city
                or any(c.isspace() and c not in "\x1c\x1d\x1e\x1f" for c in (input.city[0], input.city[-1]))
                or len(input.city.encode("utf-8")) > 128
            ):
                return _branch("defect", FailureKind.VALIDATION, "city is invalid")
            credentials = await self.client.credentials.resolve(call)
            key = credentials.api_key.reveal()
            if not _ascii_value(key, 512):
                return _branch(
                    "defect", FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
                )
            request = self.client.http_client.build_request(
                "POST",
                self.client.base_url + "/simpleWeather/query",
                data={"key": key, "city": input.city},
                headers={"Accept": "application/json", "Content-Type": "application/x-www-form-urlencoded"},
            )
        except (ValueError, LookupError, OSError, httpx.HTTPError):
            return _branch("defect", FailureKind.AUTHENTICATION, "Juhe credentials or request are invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "Juhe request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Juhe rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Juhe is temporarily unavailable", delay)

            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Juhe response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Juhe response was interrupted")
        finally:
            await response.aclose()
        if status != 200:
            return _branch(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                f"Juhe rejected request with HTTP {status}",
            )
        try:
            document: Any = json.loads(
                body.decode("utf-8", errors="replace"), parse_constant=_reject_nonfinite_number
            )
        except (ValueError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Juhe returned invalid JSON")
        if not isinstance(document, dict):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Juhe returned an invalid response")
        envelope = {key.lower(): value for key, value in document.items()}
        code = envelope.get("error_code")
        result_value = envelope.get("result")
        if (
            type(code) is not int
            or not -(1 << 63) <= code < (1 << 63)
            or (result_value is not None and not isinstance(result_value, dict))
        ):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Juhe returned invalid weather data")
        result = {key.lower(): value for key, value in (result_value or {}).items()}
        realtime_value = result.get("realtime")
        if (result.get("city") is not None and not isinstance(result["city"], str)) or (
            realtime_value is not None and not isinstance(realtime_value, dict)
        ):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Juhe returned invalid weather data")
        realtime = {key.lower(): value for key, value in (realtime_value or {}).items()}
        for name in ("info", "wid", "temperature", "humidity", "direct", "power", "aqi"):
            value = realtime.get(name)
            if value is not None and not isinstance(value, str):
                return _branch("invalidResponse", FailureKind.PROTOCOL, "Juhe returned invalid weather data")
            realtime[name] = value or ""
        if code in (207303, 10014, 10020):
            return _retry(FailureKind.AVAILABILITY, "Juhe weather service is temporarily unavailable")
        if code == 10011:
            return _retry(FailureKind.RATE_LIMIT, "Juhe limited the caller IP")
        if code != 0:
            kind = FailureKind.PROVIDER_REJECTION
            if code in (10001, 10003, 10009):
                kind = FailureKind.AUTHENTICATION
            elif code in (10002, 10005, 10007, 10008):
                kind = FailureKind.AUTHORIZATION
            elif code in (10012, 10013):
                kind = FailureKind.QUOTA_EXHAUSTED
            elif code in (207301, 207302):
                kind = FailureKind.VALIDATION
            return _branch("providerRejected", kind, "Juhe rejected the weather request")
        if not result.get("city") or not realtime["info"] or not realtime["wid"]:
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Juhe response lacks current weather fields"
            )
        return QueryAttempt.branch_result(
            "fetched",
            GetCurrentWeatherOutput(
                city=result["city"],
                weather_text=realtime["info"],
                weather_code=realtime["wid"],
                temperature_celsius=realtime.get("temperature", ""),
                humidity_percent=realtime.get("humidity", ""),
                wind_direction=realtime.get("direct", ""),
                wind_power=realtime.get("power", ""),
                air_quality_index=realtime.get("aqi", ""),
            ),
            Receipt(),
        )


def _reject_nonfinite_number(value: str) -> None:
    raise ValueError(f"Invalid JSON number {value}")
