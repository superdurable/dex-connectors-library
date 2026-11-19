# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""QWeather current conditions Query."""

import json
import math
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
from ._generated.definitions import GET_CURRENT_WEATHER_DEFINITION
from .models import GetCurrentWeatherInput, GetCurrentWeatherOutput


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[GetCurrentWeatherOutput]:
    return QueryAttempt.branch_result(
        branch, GetCurrentWeatherOutput(), Receipt(), Failure(kind, "qweather", "getCurrentWeather", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[GetCurrentWeatherOutput]:
    return QueryAttempt.retry(Failure(kind, "qweather", "getCurrentWeather", message), delay)


def _valid_key(value: str) -> bool:
    return 0 < len(value) <= 1024 and value.strip() == value and all(0x20 < ord(c) < 0x7F for c in value)


def _valid_host(host: str) -> bool:
    if not 1 <= len(host) <= 253 or not host.endswith(".qweatherapi.com") or host == "qweatherapi.com":
        return False
    return all(
        0 < len(label) <= 63
        and label[0] != "-"
        and label[-1] != "-"
        and all(c.isascii() and (c.islower() or c.isdigit() or c == "-") for c in label)
        for label in host.split(".")
    )


class Client:
    """Own QWeather HTTP limits and resolve the account host and key per attempt."""

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
            raise ValueError("QWeather configuration and credential source are required")
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_current_weather(self) -> "GetCurrentWeatherOperation":
        """Bind the current-weather Query to this client."""
        return GetCurrentWeatherOperation(self)

    async def aclose(self) -> None:
        """Close the transport only when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class GetCurrentWeatherOperation:
    """Read one current-weather observation without modifying provider state."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("QWeather client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated weather Query contract."""
        return GET_CURRENT_WEATHER_DEFINITION

    async def invoke(
        self, call: Call, input: GetCurrentWeatherInput
    ) -> QueryAttempt[GetCurrentWeatherOutput]:
        """Validate coordinates and classify the one provider request."""
        if not isinstance(input, GetCurrentWeatherInput) or not all(
            (
                isinstance(input.latitude, (int, float)),
                isinstance(input.longitude, (int, float)),
                math.isfinite(input.latitude),
                math.isfinite(input.longitude),
                -90 <= input.latitude <= 90,
                -180 <= input.longitude <= 180,
            )
        ):
            return _branch("defect", FailureKind.VALIDATION, "latitude or longitude is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
            host, key = credentials.api_host.reveal(), credentials.api_key.reveal()
            if not _valid_host(host) or not _valid_key(key):
                raise ValueError("invalid host or key")
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch("defect", FailureKind.AUTHENTICATION, "API Host or key is unavailable or invalid")
        path = f"/weather/v1/current/{input.latitude:.6f}/{input.longitude:.6f}"
        try:
            request = self.client.http_client.build_request(
                "GET", f"https://{host}{path}", headers={"X-QW-Api-Key": key, "Accept": "application/json"}
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "weather request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "QWeather request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if response.status_code == 408 or response.status_code >= 500:
                return _retry(FailureKind.AVAILABILITY, "QWeather is temporarily unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                if response.status_code == 429:
                    return _retry(FailureKind.RATE_LIMIT, "QWeather rate limited the request", delay)
                return _branch(
                    "invalidResponse", FailureKind.RESPONSE_TOO_LARGE, "QWeather response exceeded limit"
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "QWeather response was interrupted")
            if response.status_code == 429:
                try:
                    problem = json.loads(body)
                    if "over-monthly-limit" in str(problem.get("error", {}).get("type", "")).lower():
                        return _retry(FailureKind.QUOTA_EXHAUSTED, "QWeather monthly limit reached", delay)
                except (ValueError, AttributeError, TypeError):
                    pass
                return _retry(FailureKind.RATE_LIMIT, "QWeather rate limited the request", delay)
            if response.status_code != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if response.status_code == 401
                    else (
                        FailureKind.AUTHORIZATION
                        if response.status_code == 403
                        else FailureKind.PROVIDER_REJECTION
                    )
                )
                return _branch(
                    "providerRejected", kind, f"QWeather rejected with HTTP {response.status_code}"
                )
            try:
                wire = json.loads(body, parse_constant=lambda _: (_ for _ in ()).throw(ValueError()))
                condition, temperature = wire["condition"], wire["temperature"]
                text, code = condition["text"], condition["code"]
                value, unit = temperature["value"], temperature["unit"]
                valid_time = wire["validTime"]
                attributions = wire["metadata"]["attributions"]
                if not all(isinstance(x, str) and x for x in (text, code, unit, valid_time)):
                    raise ValueError("invalid weather strings")
                if not isinstance(value, (int, float)) or not math.isfinite(value):
                    raise ValueError("invalid temperature")
                datetime.fromisoformat(valid_time.replace("Z", "+00:00"))
                if (
                    not isinstance(attributions, list)
                    or not 1 <= len(attributions) <= 16
                    or any(
                        not isinstance(x, str) or not x or len(x) > 500 or key in x or host in x
                        for x in attributions
                    )
                    or key in text
                    or host in text
                ):
                    raise ValueError("invalid attributions")
                output = GetCurrentWeatherOutput(
                    text, code, float(value), unit, valid_time, tuple(attributions)
                )
            except (ValueError, KeyError, TypeError, UnicodeDecodeError, OverflowError):
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "QWeather returned invalid weather data"
                )
            return QueryAttempt.branch_result("found", output, Receipt())
        finally:
            await response.aclose()
