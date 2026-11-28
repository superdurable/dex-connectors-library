# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Seniverse current-weather Query with bounded public output."""

import json
import math
import re
from calendar import monthrange
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
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import GET_CURRENT_WEATHER_DEFINITION
from .models import GetCurrentWeatherInput, GetCurrentWeatherOutput

_ALLOWED_LANGUAGES = frozenset(
    ("zh-Hans", "zh-Hant", "en", "ja", "de", "fr", "es", "pt", "hi", "id", "ru", "th", "ar")
)
_AUTH_FAILURES = frozenset(("AP010003", "AP010004"))
_PERMISSION_FAILURES = frozenset(("AP010002", "AP010006", "AP010008", "AP010009"))
_QUOTA_FAILURES = frozenset(("AP010012", "AP010013"))
_INPUT_FAILURES = frozenset(("AP010001", "AP010010", "AP010011", "AP010017"))


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[GetCurrentWeatherOutput]:
    return QueryAttempt.branch_result(
        branch, GetCurrentWeatherOutput(), Receipt(), Failure(kind, "seniverse", "getCurrentWeather", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[GetCurrentWeatherOutput]:
    return QueryAttempt.retry(Failure(kind, "seniverse", "getCurrentWeather", message), delay)


def _valid_key(value: str) -> bool:
    return 0 < len(value) <= 512 and value.strip() == value and all(0x20 < ord(c) < 0x7F for c in value)


def _error_kind(code: str) -> FailureKind:
    if code in _AUTH_FAILURES:
        return FailureKind.AUTHENTICATION
    if code in _PERMISSION_FAILURES:
        return FailureKind.AUTHORIZATION
    if code in _QUOTA_FAILURES:
        return FailureKind.QUOTA_EXHAUSTED
    if code in _INPUT_FAILURES:
        return FailureKind.VALIDATION
    return FailureKind.PROVIDER_REJECTION


def _fields_by_name(fields: dict[str, object]) -> dict[str, object]:
    return {name.lower(): value for name, value in fields.items()}


def _valid_time(text: str) -> bool:
    parts = re.fullmatch(
        r"([0-9]{4})-([0-9]{2})-([0-9]{2})T([0-9]{1,2}):([0-9]{2}):([0-9]{2})(?:[.,][0-9]+)?(?:Z|[+-]([0-9]{2}):([0-9]{2}))",
        text,
    )
    if not parts:
        return False
    year, month, day, hour, minute, second, zone_hour, zone_minute = (int(v or 0) for v in parts.groups())
    return (
        1 <= month <= 12
        and 1 <= day <= monthrange(year, month)[1]
        and hour < 24
        and minute < 60
        and second < 60
        and zone_hour <= 24
        and zone_minute <= 60
    )


_TEMPERATURE = re.compile(
    r"[+-]?(?:(?:[0-9](?:_?[0-9])*(?:\.(?:[0-9](?:_?[0-9])*)?)?|\.[0-9](?:_?[0-9])*)(?:[Ee][+-]?[0-9](?:_?[0-9])*)?|0[xX](?:_?[0-9A-Fa-f](?:_?[0-9A-Fa-f])*(?:\.(?:[0-9A-Fa-f](?:_?[0-9A-Fa-f])*)?)?|\.[0-9A-Fa-f](?:_?[0-9A-Fa-f])*)[pP][+-]?[0-9](?:_?[0-9])*)"
)


def _valid_temperature(text: str) -> bool:
    if not _TEMPERATURE.fullmatch(text):
        return False
    literal = text.replace("_", "")
    try:
        value = float.fromhex(literal) if literal.lstrip("+-").lower().startswith("0x") else float(literal)
    except OverflowError:
        return False
    return math.isfinite(value)


class Client:
    """Own Seniverse API origin, language, unit, and bounded HTTP transport."""

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
            or config.max_response_bytes <= 0
        ):
            raise ValueError("Seniverse configuration and credential source are required")
        if config.language not in _ALLOWED_LANGUAGES or config.unit not in ("c", "f"):
            raise ValueError("Seniverse language or unit is invalid")
        base_url = validate_base_url(config.base_url)
        self.base_url = base_url.rstrip("/")
        self.language = config.language
        self.unit = config.unit
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_current_weather(self) -> "GetCurrentWeatherOperation":
        """Bind the current-weather Query to this client."""
        return GetCurrentWeatherOperation(self)

    async def aclose(self) -> None:
        """Close only a transport this client created."""
        if self._owns_client:
            await self.http_client.aclose()


class GetCurrentWeatherOperation:
    """Read one weather observation for a documented Seniverse location."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Seniverse client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated weather Query contract."""
        return GET_CURRENT_WEATHER_DEFINITION

    async def invoke(
        self, call: Call, input: GetCurrentWeatherInput
    ) -> QueryAttempt[GetCurrentWeatherOutput]:
        """Classify the provider response without persisting the secret query key."""
        location = input.location
        if (
            not isinstance(location, str)
            or not location
            or location.strip() != location
            or len(location.encode("utf-8")) > 256
        ):
            return _branch("defect", FailureKind.VALIDATION, "location is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
            key = credentials.private_key.reveal()
            if not _valid_key(key):
                raise ValueError("invalid private key")
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
            )
        try:
            request = self.client.http_client.build_request(
                "GET",
                self.client.base_url + "/v3/weather/now.json",
                params={
                    "key": key,
                    "location": location,
                    "language": self.client.language,
                    "unit": self.client.unit,
                },
                headers={"Accept": "application/json"},
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "weather request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "Seniverse request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if response.status_code == 429:
                return _retry(FailureKind.RATE_LIMIT, "Seniverse rate limited the request", delay)
            if response.status_code == 408 or response.status_code >= 500:
                return _retry(FailureKind.AVAILABILITY, "Seniverse is temporarily unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse", FailureKind.RESPONSE_TOO_LARGE, "Seniverse response exceeded limit"
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Seniverse response was interrupted")
            if response.status_code != 200:
                try:
                    provider_code = json.loads(body, object_hook=_fields_by_name).get("status_code", "")
                    if not isinstance(provider_code, str):
                        provider_code = ""
                except (ValueError, AttributeError, TypeError):
                    provider_code = ""
                if provider_code == "AP010014":
                    return _retry(FailureKind.RATE_LIMIT, "Seniverse rate limited the request", delay)
                return _branch(
                    "providerRejected",
                    _error_kind(provider_code),
                    f"Seniverse rejected with HTTP {response.status_code}",
                )
            try:
                wire = json.loads(
                    body,
                    object_hook=_fields_by_name,
                    parse_constant=lambda _: (_ for _ in ()).throw(ValueError()),
                )
                results = wire["results"]
                if not isinstance(results, list) or len(results) != 1:
                    raise ValueError("invalid result count")
                result = results[0]
                place, now = result["location"], result["now"]
                values = (
                    place["id"],
                    place["name"],
                    now["text"],
                    now["code"],
                    now["temperature"],
                    result["last_update"],
                )
                if any(not isinstance(value, str) or not value for value in values):
                    raise ValueError("missing weather field")
                country = place.get("country")
                if country is None:
                    country = ""
                if not isinstance(country, str) or any(key in field for field in (*values, country)):
                    raise ValueError("invalid weather fields")
                if not _valid_temperature(now["temperature"]) or not _valid_time(result["last_update"]):
                    raise ValueError("invalid temperature or timestamp")
                output = GetCurrentWeatherOutput(
                    place["id"],
                    place["name"],
                    country,
                    now["text"],
                    now["code"],
                    now["temperature"],
                    self.client.unit,
                    result["last_update"],
                )
            except (ValueError, KeyError, TypeError, UnicodeDecodeError, OverflowError):
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "Seniverse returned an invalid response"
                )
            return QueryAttempt.branch_result("fetched", output, Receipt())
        finally:
            await response.aclose()
