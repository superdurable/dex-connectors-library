# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""GEOVIS Earth current-weather Query with Go-compatible decisions."""

import json
import math
import re
from datetime import UTC, datetime
from urllib.parse import urlencode, urlsplit

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

_AREA_CODE = re.compile(r"^WTX_CH[0-9]{9}$")


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[GetCurrentWeatherOutput]:
    return QueryAttempt.branch_result(
        branch,
        GetCurrentWeatherOutput(),
        Receipt(),
        Failure(kind, "geovisearth", "getCurrentWeather", message),
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[GetCurrentWeatherOutput]:
    return QueryAttempt.retry(Failure(kind, "geovisearth", "getCurrentWeather", message), delay)


def _valid_location(location: str) -> bool:
    if _AREA_CODE.fullmatch(location):
        return True
    if location.strip() != location:
        return False
    parts = location.split(",")
    if len(parts) != 2:
        return False
    try:
        longitude, latitude = (float(part) for part in parts)
    except ValueError:
        return False
    return (
        math.isfinite(longitude)
        and -180 <= longitude <= 180
        and math.isfinite(latitude)
        and -90 <= latitude <= 90
    )


def _valid_token(value: str) -> bool:
    return bool(value) and len(value) <= 512 and all(0x21 <= ord(c) <= 0x7E for c in value)


class Client:
    """Own weather settings and resolve the token for each request."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("GEOVIS Earth configuration and credential source are required")
        parsed = urlsplit(config.base_url)
        if (
            parsed.scheme != "https"
            or parsed.netloc != "api.open.geovisearth.com"
            or parsed.path not in ("", "/")
            or parsed.query
            or parsed.fragment
            or config.timeout_seconds <= 0
            or not 0 < config.max_response_bytes <= 16 << 20
        ):
            raise ValueError("GEOVIS Earth configuration is invalid")
        self.base_url = config.base_url.rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_current_weather(self) -> "GetCurrentWeatherOperation":
        """Bind the current-weather Query."""
        return GetCurrentWeatherOperation(self)

    async def aclose(self) -> None:
        """Close an HTTP client this instance created."""
        if self._owns_client:
            await self.http_client.aclose()


class GetCurrentWeatherOperation:
    """Read one current weather observation."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("GEOVIS Earth client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query identity and retry policy."""
        return GET_CURRENT_WEATHER_DEFINITION

    async def invoke(
        self, call: Call, input: GetCurrentWeatherInput
    ) -> QueryAttempt[GetCurrentWeatherOutput]:
        """Send one weather request and classify its observed outcome."""
        location = input.location if isinstance(input, GetCurrentWeatherInput) else ""
        if not _valid_location(location):
            return _branch(
                "defect",
                FailureKind.VALIDATION,
                "location must be an area code or longitude,latitude coordinate",
            )
        try:
            credentials = await self.client.credentials.resolve(call)
            token = credentials.token.reveal()
            if not _valid_token(token):
                raise ValueError("invalid token")
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "GEOVIS Earth token is unavailable or invalid"
            )
        query = urlencode({"token": token, "location": location})
        try:
            request = self.client.http_client.build_request(
                "GET",
                f"{self.client.base_url}/v2/cn/area/basic?{query}",
                headers={"Accept": "application/json"},
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "weather request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "GEOVIS Earth request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "GEOVIS Earth rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "GEOVIS Earth temporarily unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "weather response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "weather response was interrupted")
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else (FailureKind.AUTHORIZATION if status == 403 else FailureKind.PROVIDER_REJECTION)
                )
                return _branch(
                    "providerRejected", kind, f"GEOVIS Earth rejected the request with HTTP {status}"
                )
        finally:
            await response.aclose()
        return _decode_weather(body)


def _decode_weather(body: bytes) -> QueryAttempt[GetCurrentWeatherOutput]:
    try:
        document = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "GEOVIS Earth returned invalid weather data")
    if not isinstance(document, dict) or type(document.get("status")) is not int:
        return _branch("invalidResponse", FailureKind.PROTOCOL, "GEOVIS Earth returned invalid weather data")
    if document["status"] != 0:
        return _branch(
            "providerRejected",
            FailureKind.PROVIDER_REJECTION,
            f"GEOVIS Earth rejected the weather request, status {document['status']}",
        )
    result, date, place = document.get("result"), document.get("date"), document.get("location")
    if not isinstance(result, dict) or not isinstance(date, dict):
        return _branch(
            "invalidResponse", FailureKind.PROTOCOL, "GEOVIS Earth returned incomplete weather observation"
        )
    weather, code = result.get("wp"), result.get("wp_code")
    temperature, humidity, pressure = result.get("tem"), result.get("rh"), result.get("prs")
    direction, force, observed = result.get("wd_desc"), result.get("ws_desc"), date.get("time")
    if (
        not isinstance(weather, str)
        or not weather
        or not isinstance(code, str)
        or not code
        or not isinstance(temperature, (int, float))
        or isinstance(temperature, bool)
        or not math.isfinite(temperature)
        or not isinstance(humidity, int)
        or isinstance(humidity, bool)
        or not 0 <= humidity <= 100
        or not isinstance(pressure, (int, float))
        or isinstance(pressure, bool)
        or not math.isfinite(pressure)
        or pressure <= 0
        or not isinstance(direction, str)
        or not direction
        or not isinstance(force, str)
        or not force
        or not isinstance(observed, str)
        or len(observed) != 14
    ):
        return _branch(
            "invalidResponse", FailureKind.PROTOCOL, "GEOVIS Earth returned incomplete weather observation"
        )
    try:
        datetime.strptime(observed, "%Y%m%d%H%M%S")
    except ValueError:
        return _branch(
            "invalidResponse", FailureKind.PROTOCOL, "GEOVIS Earth returned invalid observation time"
        )
    area_code = place.get("areaCode", "") if isinstance(place, dict) else ""
    if not isinstance(area_code, str):
        area_code = ""
    output = GetCurrentWeatherOutput(
        area_code, weather, code, float(temperature), humidity, direction, force, float(pressure), observed
    )
    return QueryAttempt.branch_result("fetched", output, Receipt())
