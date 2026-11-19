# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""APISpace current-weather requests and Go-compatible decisions."""

import json
import math
from datetime import UTC, datetime
from typing import Any, cast

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
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import GET_CURRENT_WEATHER_DEFINITION
from .models import CurrentWeather, GetCurrentWeatherInput


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[CurrentWeather]:
    return QueryAttempt.branch_result(
        branch, CurrentWeather(), Receipt(), Failure(kind, "apispace", "getCurrentWeather", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[CurrentWeather]:
    return QueryAttempt.retry(Failure(kind, "apispace", "getCurrentWeather", message), delay)


def _valid_area_code(code: object) -> bool:
    return (
        isinstance(code, str)
        and 1 <= len(code) <= 32
        and all(
            "0" <= character <= "9" or "a" <= character <= "z" or "A" <= character <= "Z"
            for character in code
        )
    )


class Client:
    """Own an HTTP transport and resolve the APISpace token for each Query attempt."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("APISpace configuration and credential source are required")
        if config.timeout_seconds <= 0 or config.max_response_bytes <= 0:
            raise ValueError("APISpace timeout and response limit must be positive")
        self.base_url = validate_base_url(config.base_url)
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_current_weather(self) -> "GetCurrentWeatherOperation":
        """Bind the current-weather Query to this client."""
        return GetCurrentWeatherOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP transport when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class GetCurrentWeatherOperation:
    """Read public observations for one documented APISpace area code."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("APISpace client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated current-weather Query identity and retry policy."""
        return GET_CURRENT_WEATHER_DEFINITION

    async def invoke(self, call: Call, input: GetCurrentWeatherInput) -> QueryAttempt[CurrentWeather]:
        """Make one provider request and classify its observed result."""
        area_code = input.area_code if isinstance(input, GetCurrentWeatherInput) else None
        if not _valid_area_code(area_code):
            return _branch(
                "defect", FailureKind.VALIDATION, "APISpace area code must be 1 to 32 ASCII letters or digits"
            )
        try:
            credentials = await self.client.credentials.resolve(call)
            token = credentials.api_token.reveal()
            if len(token) > 2048 or not is_header_safe_credential(token):
                raise ValueError("invalid token")
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "APISpace API token is unavailable or invalid"
            )
        try:
            request = self.client.http_client.build_request(
                "GET",
                f"{self.client.base_url}/456456/weather/v001/now?areacode={area_code}",
                headers={
                    "Accept": "application/json",
                    "Content-Type": "application/json",
                    "Authorization-Type": "apikey",
                    "X-APISpace-Token": token,
                },
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "weather request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "APISpace weather request failed")
        try:
            status = response.status_code
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status in (429, 413):
                return _retry(FailureKind.RATE_LIMIT, "APISpace rate limited weather requests", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "APISpace weather is temporarily unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "APISpace response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "APISpace response was interrupted")
        finally:
            await response.aclose()
        if status != 200:
            kind = FailureKind.PROVIDER_REJECTION
            if status == 401:
                kind = FailureKind.AUTHENTICATION
            elif status in (403, 416):
                kind = FailureKind.AUTHORIZATION
            elif status in (400, 404):
                kind = FailureKind.VALIDATION
            return _branch("providerRejected", kind, f"APISpace rejected weather query with HTTP {status}")
        return _decode_weather(body)


def _decode_weather(body: bytes) -> QueryAttempt[CurrentWeather]:
    try:
        wire: Any = json.loads(body, parse_constant=lambda _: (_ for _ in ()).throw(ValueError()))
    except (ValueError, UnicodeDecodeError):
        return _branch(
            "invalidResponse", FailureKind.PROTOCOL, "APISpace returned an invalid weather response"
        )
    if (
        not isinstance(wire, dict)
        or type(wire.get("status")) is not int
        or not -(1 << 63) <= wire["status"] < (1 << 63)
    ):
        return _branch(
            "invalidResponse", FailureKind.PROTOCOL, "APISpace returned an invalid weather response"
        )
    status = wire["status"]
    if status != 0:
        return _branch(
            "providerRejected",
            FailureKind.PROVIDER_REJECTION,
            f"APISpace rejected weather query with status {status}",
        )
    result = wire.get("result")
    if not isinstance(result, dict):
        return _branch(
            "invalidResponse", FailureKind.PROTOCOL, "APISpace returned an invalid weather response"
        )
    location, realtime = result.get("location"), result.get("realtime")
    if not isinstance(location, dict) or not isinstance(realtime, dict):
        return _branch(
            "invalidResponse", FailureKind.PROTOCOL, "APISpace returned an invalid weather response"
        )
    city, condition = location.get("name"), realtime.get("text")
    temperature, humidity, updated = realtime.get("temp"), realtime.get("rh"), result.get("last_update")
    if (
        not isinstance(city, str)
        or not city
        or not isinstance(condition, str)
        or not condition
        or type(temperature) not in (int, float)
        or not math.isfinite(cast(float, temperature))
        or type(humidity) is not int
        or not 0 <= humidity <= 100
        or not isinstance(updated, str)
        or not updated
    ):
        return _branch(
            "invalidResponse", FailureKind.PROTOCOL, "APISpace returned an invalid weather response"
        )
    return QueryAttempt.branch_result(
        "found",
        CurrentWeather(city, condition, float(cast(float, temperature)), humidity, updated),
        Receipt(),
    )
