# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""GEOVIS Earth current-weather Query with Go-compatible decisions."""

import json
import math
import re
from calendar import monthrange
from datetime import UTC, datetime
from typing import Any
from urllib.parse import urlencode

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


_COORDINATE = re.compile(
    r"[+-]?(?:(?:[0-9](?:_?[0-9])*(?:\.(?:[0-9](?:_?[0-9])*)?)?|\.[0-9](?:_?[0-9])*)(?:[Ee][+-]?[0-9](?:_?[0-9])*)?|0[xX](?:_?[0-9A-Fa-f](?:_?[0-9A-Fa-f])*(?:\.(?:[0-9A-Fa-f](?:_?[0-9A-Fa-f])*)?)?|\.[0-9A-Fa-f](?:_?[0-9A-Fa-f])*)[pP][+-]?[0-9](?:_?[0-9])*)"
)


def _valid_coordinate(text: str, maximum: float) -> bool:
    if not _COORDINATE.fullmatch(text):
        return False
    literal = text.replace("_", "")
    try:
        value = float.fromhex(literal) if literal.lstrip("+-").lower().startswith("0x") else float(literal)
    except OverflowError:
        return False
    return abs(value) <= maximum


def _valid_location(location: str) -> bool:
    if not isinstance(location, str):
        return False
    if _AREA_CODE.fullmatch(location):
        return True
    pair = location.split(",")
    return len(pair) == 2 and _valid_coordinate(pair[0], 180) and _valid_coordinate(pair[1], 90)


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
        origin = config.base_url.rstrip("/")
        if (
            origin != "https://api.open.geovisearth.com"
            or config.timeout_seconds <= 0
            or not 0 < config.max_response_bytes <= 16 << 20
        ):
            raise ValueError("GEOVIS Earth configuration is invalid")
        self.base_url = origin
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
        return _decode_weather(body, token)


def _fields_by_name(fields: dict[str, Any]) -> dict[str, Any]:
    return {name.lower(): value for name, value in fields.items()}


def _int64(value: object) -> bool:
    return type(value) is int and -(1 << 63) <= value < (1 << 63)


def _finite_number(value: object) -> bool:
    if not isinstance(value, (int, float)) or isinstance(value, bool):
        return False
    try:
        return math.isfinite(float(value))
    except OverflowError:
        return False


def _valid_time(text: str) -> bool:
    if len(text) != 14 or not text.isascii() or not text.isdecimal():
        return False
    year = int(text[:4])
    month = int(text[4:6])
    day = int(text[6:8])
    return (
        1 <= month <= 12
        and 1 <= day <= monthrange(year, month)[1]
        and int(text[8:10]) < 24
        and int(text[10:12]) < 60
        and int(text[12:]) < 60
    )


def _invalid_weather() -> QueryAttempt[GetCurrentWeatherOutput]:
    return _branch("invalidResponse", FailureKind.PROTOCOL, "GEOVIS Earth returned invalid weather data")


def _invalid_json_number(text: str) -> None:
    raise ValueError("invalid JSON number")


def _decode_weather(body: bytes, token: str) -> QueryAttempt[GetCurrentWeatherOutput]:
    try:
        data = json.loads(
            body.decode("utf-8", errors="replace"),
            object_hook=_fields_by_name,
            parse_constant=_invalid_json_number,
        )
    except (ValueError, RecursionError):
        return _invalid_weather()
    if not isinstance(data, dict) or not _int64(data.get("status")):
        return _invalid_weather()
    result, date, location = data.get("result"), data.get("date"), data.get("location")
    if any(item is not None and not isinstance(item, dict) for item in (result, date, location)):
        return _invalid_weather()
    result, date, location = result or {}, date or {}, location or {}
    area, time = location.get("areacode"), date.get("time")
    weather, code, direction, force = (result.get(name) for name in ("wp", "wp_code", "wd_desc", "ws_desc"))
    humidity, temperature, pressure = result.get("rh"), result.get("tem"), result.get("prs")
    if any(
        value is not None and not isinstance(value, str)
        for value in (area, time, weather, code, direction, force)
    ):
        return _invalid_weather()
    if (humidity is not None and not _int64(humidity)) or any(
        value is not None and not _finite_number(value) for value in (temperature, pressure)
    ):
        return _invalid_weather()
    if data["status"] != 0:
        return _branch(
            "providerRejected", FailureKind.PROVIDER_REJECTION, "GEOVIS Earth rejected the weather request"
        )
    if (
        not weather
        or not code
        or not direction
        or not force
        or not time
        or not _valid_time(time)
        or humidity is None
        or not 0 <= humidity <= 100
        or pressure is None
        or pressure <= 0
    ):
        return _invalid_weather()
    if any(token in field for field in (area or "", time, weather, code, direction, force)):
        return _invalid_weather()
    return QueryAttempt.branch_result(
        "fetched",
        GetCurrentWeatherOutput(
            area or "",
            weather,
            code,
            float(temperature or 0),
            humidity,
            direction,
            force,
            float(pressure),
            time,
        ),
        Receipt(),
    )
