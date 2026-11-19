# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Newayz provider transport and Go-compatible Query decisions."""

import json
import re
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


def _opaque(value: str, limit: int) -> bool:
    return (
        isinstance(value, str) and 0 < len(value) <= limit and all(0x20 < ord(char) < 0x7F for char in value)
    )


class Client:
    """Resolve credentials for each getCurrentWeather request."""

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
            raise ValueError("Newayz configuration and credentials are required")
        pass
        self.config = config
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_http_client = http_client is None

    def get_current_weather(self) -> "GetCurrentWeatherOperation":
        """Bind the getCurrentWeather Query."""
        return GetCurrentWeatherOperation(self)

    async def aclose(self) -> None:
        """Close the owned HTTP transport."""
        if self._owns_http_client:
            await self.http_client.aclose()


class GetCurrentWeatherOperation:
    """Query Newayz and classify the provider outcome."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query definition."""
        return GET_CURRENT_WEATHER_DEFINITION

    def _branch(self, branch: str, kind: FailureKind) -> QueryAttempt[GetCurrentWeatherOutput]:
        return QueryAttempt.branch_result(
            branch,
            GetCurrentWeatherOutput(),
            Receipt(),
            Failure(kind, "newayz", "getCurrentWeather", "Newayz request failed"),
        )

    def _retry(self, kind: FailureKind, delay: float = 0) -> QueryAttempt[GetCurrentWeatherOutput]:
        return QueryAttempt.retry(
            Failure(kind, "newayz", "getCurrentWeather", "Newayz request failed"), delay
        )

    def _request(
        self, input: GetCurrentWeatherInput, credentials: Credentials
    ) -> httpx.Request | QueryAttempt[GetCurrentWeatherOutput]:
        if not re.fullmatch(r"[0-9]{6}", input.city_code):
            return self._branch("defect", FailureKind.VALIDATION)
        token = credentials.access_key.reveal()
        if not _opaque(token, 1024):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        return self.client.http_client.build_request(
            "GET",
            "https://api.newayz.com/location/scenario/v1/weather",
            params={"access_key": token, "city": input.city_code, "extensions": "base"},
            headers={"Accept": "application/json"},
        )

    def _decode(
        self, wire: object, input: GetCurrentWeatherInput, credentials: Credentials
    ) -> GetCurrentWeatherOutput | QueryAttempt[GetCurrentWeatherOutput] | None:
        if not isinstance(wire, dict):
            return None
        status = wire.get("status")
        if not isinstance(status, str) or not status:
            return None
        if status != "1" or wire.get("infocode") != "10000":
            return self._branch("providerRejected", FailureKind.PROVIDER_REJECTION)
        lives = wire.get("lives")
        if not isinstance(lives, list) or len(lives) != 1 or not isinstance(lives[0], dict):
            return None
        live = lives[0]
        if (
            live.get("adcode") != input.city_code
            or not live.get("city")
            or not live.get("weather")
            or not live.get("temperature")
        ):
            return None
        values = {
            "province": "province",
            "city": "city",
            "city_code": "adcode",
            "weather": "weather",
            "temperature_celsius": "temperature",
            "wind_direction": "winddirection",
            "wind_power": "windpower",
            "humidity_percent": "humidity",
            "report_time": "reporttime",
        }
        if any(not isinstance(live.get(source, ""), str) for source in values.values()):
            return None
        return GetCurrentWeatherOutput(**{target: live.get(source, "") for target, source in values.items()})

    async def invoke(
        self, call: Call, input: GetCurrentWeatherInput
    ) -> QueryAttempt[GetCurrentWeatherOutput]:
        """Read one bounded provider response without persisting secrets."""
        if not isinstance(input, GetCurrentWeatherInput):
            return self._branch("defect", FailureKind.VALIDATION)
        try:
            credentials = await self.client.credentials.resolve(call)
        except (ValueError, LookupError, OSError, AttributeError):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        request = self._request(input, credentials)
        if isinstance(request, QueryAttempt):
            return request
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
                body = await read_bounded_body(response, self.client.config.max_response_bytes)
            except BodyTooLargeError:
                return self._branch("invalidResponse", FailureKind.RESPONSE_TOO_LARGE)
            except ProviderTransportError:
                return self._retry(FailureKind.AVAILABILITY)
        finally:
            await response.aclose()
        try:
            wire = json.loads(body)
        except (ValueError, UnicodeDecodeError):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        result = self._decode(wire, input, credentials)
        if isinstance(result, QueryAttempt):
            return result
        if result is None:
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        return QueryAttempt.branch_result("fetched", result, Receipt())
