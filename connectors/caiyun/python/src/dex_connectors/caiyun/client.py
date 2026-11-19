# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Caiyun v2.6 signed realtime weather Query and Go-compatible decisions."""

import base64
import hashlib
import hmac
import json
import math
import secrets
from collections.abc import Callable
from datetime import UTC, datetime
from decimal import Decimal
from typing import Any, TypeGuard

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
from ._generated.definitions import (
    DEFECT,
    FETCHED,
    GET_CURRENT_WEATHER_DEFINITION,
    INVALID_RESPONSE,
    PROVIDER_REJECTED,
)
from .models import GetCurrentWeatherInput, GetCurrentWeatherOutput


class Client:
    """Own Caiyun settings and resolve signed-request credentials per call."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        *,
        clock: Callable[[], datetime] = lambda: datetime.now(UTC),
        nonce_source: Callable[[int], bytes] = secrets.token_bytes,
    ) -> None:
        if config is None or credentials is None or clock is None or nonce_source is None:
            raise ValueError("Caiyun configuration, credentials, clock, and nonce source are required")
        if config.timeout_seconds <= 0 or config.max_response_bytes <= 0:
            raise ValueError("Caiyun timeout and response limit must be positive")
        self.base_url = validate_base_url(config.base_url)
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self.clock = clock
        self.nonce_source = nonce_source

    def get_current_weather(self) -> "GetCurrentWeatherOperation":
        """Bind one realtime weather Query."""
        return GetCurrentWeatherOperation(self)

    async def aclose(self) -> None:
        """Close the provider HTTP client after Worker shutdown."""
        await self.http_client.aclose()


class GetCurrentWeatherOperation:
    """Read and sanitize one metered realtime observation."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Caiyun client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest Query contract."""
        return GET_CURRENT_WEATHER_DEFINITION

    async def invoke(
        self, call: Call, input: GetCurrentWeatherInput
    ) -> QueryAttempt[GetCurrentWeatherOutput]:
        """Sign one request and classify the bounded provider response."""
        if (
            not isinstance(input, GetCurrentWeatherInput)
            or not _finite_in_range(input.longitude, -180, 180)
            or not _finite_in_range(input.latitude, -90, 90)
        ):
            return _branch(DEFECT, FailureKind.VALIDATION, "longitude or latitude is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
        except (ValueError, LookupError, OSError):
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
            )
        if not isinstance(credentials, Credentials):
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
            )
        app_key, app_secret = credentials.app_key.reveal(), credentials.app_secret.reveal()
        if not _valid_app_key(app_key) or not _valid_app_secret(app_secret):
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
            )
        try:
            nonce_bytes = self.client.nonce_source(16)
            if len(nonce_bytes) != 16:
                raise ValueError("nonce length")
        except (OSError, ValueError, TypeError):
            return _retry(FailureKind.AVAILABILITY, "Caiyun request nonce could not be generated")
        nonce = nonce_bytes.hex()
        timestamp = str(int(self.client.clock().timestamp()))
        path = f"/v2.6/{app_key}/{_go_float(input.longitude)},{_go_float(input.latitude)}/realtime"
        signature = _sign_weather_request(app_key, app_secret, path, "", nonce, timestamp)
        try:
            request = self.client.http_client.build_request(
                "GET",
                self.client.base_url + path,
                headers={
                    "Accept": "application/json",
                    "x-cy-nonce": nonce,
                    "x-cy-timestamp": timestamp,
                    "x-cy-signature": signature,
                },
            )
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _retry(
                FailureKind.AVAILABILITY,
                "Caiyun request failed; a prior attempt may consume another plan call",
            )
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), self.client.clock())
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Caiyun is temporarily unavailable", retry_after)
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Caiyun rate or quota limit reached", retry_after)
            if status != 200:
                return _branch(
                    PROVIDER_REJECTED, _classify_http(status), "Caiyun rejected the weather request"
                )
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    INVALID_RESPONSE,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Caiyun response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Caiyun response was interrupted")
            try:
                output = _decode_weather(body)
            except (UnicodeDecodeError, ValueError, TypeError):
                return _branch(
                    INVALID_RESPONSE, FailureKind.PROTOCOL, "Caiyun returned invalid realtime weather data"
                )
            return QueryAttempt.branch_result(FETCHED, output, Receipt())
        finally:
            await response.aclose()


def _sign_weather_request(
    app_key: str, app_secret: str, path: str, canonical_query: str, nonce: str, timestamp: str
) -> str:
    signed = ":".join(("GET", path, canonical_query, app_key, nonce, timestamp))
    return base64.urlsafe_b64encode(
        hmac.new(app_secret.encode(), signed.encode(), hashlib.sha256).digest()
    ).decode()


def _go_float(value: float) -> str:
    text = format(Decimal(str(value)), "f")
    return text.rstrip("0").rstrip(".") if "." in text else text


def _finite_in_range(value: object, minimum: float, maximum: float) -> TypeGuard[int | float]:
    return (
        isinstance(value, (int, float))
        and not isinstance(value, bool)
        and math.isfinite(value)
        and minimum <= value <= maximum
    )


def _valid_app_key(value: object) -> bool:
    return (
        isinstance(value, str)
        and 0 < len(value) <= 256
        and all(char.isascii() and (char.isalnum() or char in "-_") for char in value)
    )


def _valid_app_secret(value: object) -> bool:
    return isinstance(value, str) and 0 < len(value) <= 512 and all(0x20 < ord(char) < 0x7F for char in value)


def _classify_http(status: int) -> FailureKind:
    if status in (400, 401):
        return FailureKind.AUTHENTICATION
    if status == 403:
        return FailureKind.AUTHORIZATION
    if status == 422:
        return FailureKind.VALIDATION
    return FailureKind.PROVIDER_REJECTION


def _decode_weather(body: bytes) -> GetCurrentWeatherOutput:
    document: Any = json.loads(
        body.decode("utf-8"), parse_constant=lambda _: (_ for _ in ()).throw(ValueError("invalid number"))
    )
    if not isinstance(document, dict) or document.get("status") != "ok":
        raise ValueError("invalid Caiyun status")
    result = document.get("result")
    realtime = result.get("realtime") if isinstance(result, dict) else None
    if not isinstance(realtime, dict) or realtime.get("status") != "ok":
        raise ValueError("invalid Caiyun realtime")
    wind = realtime.get("wind")
    if not isinstance(wind, dict):
        raise ValueError("invalid wind")
    temperature, humidity, speed, sky = (
        realtime.get("temperature"),
        realtime.get("humidity"),
        wind.get("speed"),
        realtime.get("skycon"),
    )
    if (
        not _finite_in_range(temperature, -150, 150)
        or not _finite_in_range(humidity, 0, 1)
        or not _finite_in_range(speed, 0, 200)
        or not isinstance(sky, str)
        or not sky
    ):
        raise ValueError("invalid weather fields")
    return GetCurrentWeatherOutput(float(temperature), float(humidity), sky, float(speed))


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[GetCurrentWeatherOutput]:
    return QueryAttempt.branch_result(
        branch, GetCurrentWeatherOutput(), Receipt(), Failure(kind, "caiyun", "getCurrentWeather", message)
    )


def _retry(kind: FailureKind, message: str, retry_after: float = 0) -> QueryAttempt[GetCurrentWeatherOutput]:
    return QueryAttempt.retry(Failure(kind, "caiyun", "getCurrentWeather", message), retry_after)
