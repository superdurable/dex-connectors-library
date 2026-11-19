# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Baidu Maps geocoding Query with Go-compatible provider decisions."""

import json
import math
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
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import (
    DEFECT,
    GEOCODE_ADDRESS_DEFINITION,
    GEOCODED,
    INVALID_RESPONSE,
    PROVIDER_REJECTED,
)
from .models import GeocodeAddressInput, GeocodeAddressOutput


class Client:
    """Own a redirect-safe transport and resolve the private AK per lookup."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Baidu Maps configuration and credentials are required")
        if config.timeout_seconds <= 0 or config.max_response_bytes <= 0:
            raise ValueError("Baidu Maps timeout and response limit must be positive")
        self.base_url = validate_base_url(config.base_url)
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)

    def geocode_address(self) -> "GeocodeAddressOperation":
        """Bind the structured-address geocoding Query."""
        return GeocodeAddressOperation(self)

    async def aclose(self) -> None:
        """Close the provider transport after Worker shutdown."""
        await self.http_client.aclose()


class GeocodeAddressOperation:
    """Read one Baidu Maps BD-09 coordinate and precision measures."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Baidu Maps client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the stable geocoding operation and branch contract."""
        return GEOCODE_ADDRESS_DEFINITION

    async def invoke(self, call: Call, input: GeocodeAddressInput) -> QueryAttempt[GeocodeAddressOutput]:
        """Query geocoding v3 and classify its bounded provider response."""
        if (
            not isinstance(input, GeocodeAddressInput)
            or not isinstance(input.address, str)
            or not isinstance(input.city, str)
            or not input.address.strip()
            or len(input.address.encode("utf-8")) > 128
            or len(input.city.encode("utf-8")) > 128
        ):
            return _branch(DEFECT, FailureKind.VALIDATION, "address or city is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
        except (ValueError, LookupError, OSError):
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
            )
        if not isinstance(credentials, Credentials) or credentials.access_key is None:
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
            )
        access_key = credentials.access_key.reveal()
        if not _valid_access_key(access_key):
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
            )
        query = {"ak": access_key, "address": input.address, "output": "json"}
        if input.city:
            query["city"] = input.city
        endpoint = self.client.base_url + "/geocoding/v3/?" + urlencode(sorted(query.items()))
        try:
            request = self.client.http_client.build_request(
                "GET", endpoint, headers={"Accept": "application/json"}
            )
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _retry(FailureKind.AVAILABILITY, "Baidu Maps request failed")
        try:
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Baidu Maps rate limited the request", retry_after)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Baidu Maps is temporarily unavailable", retry_after)
            if status != 200:
                return _branch(
                    PROVIDER_REJECTED,
                    FailureKind.PROVIDER_REJECTION,
                    f"Baidu Maps rejected the request with HTTP {status}",
                )
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    INVALID_RESPONSE,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Baidu Maps response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Baidu Maps response was interrupted")
            try:
                output, provider_status = _decode_response(body)
            except (UnicodeDecodeError, ValueError, TypeError):
                return _branch(
                    INVALID_RESPONSE, FailureKind.PROTOCOL, "Baidu Maps returned an invalid response"
                )
            if provider_status == 1:
                return _retry(FailureKind.AVAILABILITY, "Baidu Maps reported a server error", retry_after)
            if provider_status != 0:
                return _branch(
                    PROVIDER_REJECTED,
                    _classify_status(provider_status),
                    "Baidu Maps rejected the geocoding request",
                )
            return QueryAttempt.branch_result(GEOCODED, output, Receipt())
        finally:
            await response.aclose()


def _valid_access_key(value: object) -> bool:
    return isinstance(value, str) and 0 < len(value) <= 256 and all(32 < ord(char) < 127 for char in value)


def _decode_response(body: bytes) -> tuple[GeocodeAddressOutput, int]:
    document: Any = json.loads(
        body, parse_constant=lambda _: (_ for _ in ()).throw(ValueError("invalid number"))
    )
    if not isinstance(document, dict) or type(document.get("status")) is not int:
        raise ValueError("invalid Baidu Maps envelope")
    provider_status = document["status"]
    if provider_status != 0:
        return GeocodeAddressOutput(), provider_status
    result = document.get("result")
    if not isinstance(result, dict) or not isinstance(result.get("location"), dict):
        raise ValueError("incomplete result")
    location = result["location"]
    longitude = location.get("lng")
    latitude = location.get("lat")
    precise = result.get("precise")
    confidence = result.get("confidence")
    comprehension = result.get("comprehension")
    if (
        type(longitude) not in (int, float)
        or type(latitude) not in (int, float)
        or not math.isfinite(longitude)
        or not math.isfinite(latitude)
        or not -180 <= longitude <= 180
        or not -90 <= latitude <= 90
        or type(precise) is not int
        or precise not in (0, 1)
        or type(confidence) is not int
        or not 0 <= confidence <= 100
        or type(comprehension) is not int
        or not 0 <= comprehension <= 100
        or not isinstance(result.get("level", ""), str)
    ):
        raise ValueError("invalid result values")
    return GeocodeAddressOutput(
        float(longitude), float(latitude), precise == 1, confidence, comprehension, result.get("level", "")
    ), 0


def _classify_status(status: int) -> FailureKind:
    if status == 2:
        return FailureKind.VALIDATION
    if status in (3, 102, 240) or 200 <= status < 300:
        return FailureKind.AUTHORIZATION
    if status == 4 or 300 <= status < 400:
        return FailureKind.QUOTA_EXHAUSTED
    if status in (5, 101):
        return FailureKind.AUTHENTICATION
    return FailureKind.PROVIDER_REJECTION


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[GeocodeAddressOutput]:
    return QueryAttempt.branch_result(
        branch, GeocodeAddressOutput(), Receipt(), Failure(kind, "baidu-maps", "geocodeAddress", message)
    )


def _retry(kind: FailureKind, message: str, retry_after: float = 0) -> QueryAttempt[GeocodeAddressOutput]:
    return QueryAttempt.retry(Failure(kind, "baidu-maps", "geocodeAddress", message), retry_after)
