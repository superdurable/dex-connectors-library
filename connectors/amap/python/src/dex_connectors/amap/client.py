# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Amap Web Service geocoding and Go-compatible outcome classification."""

import json
import math
import re
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
from ._generated.definitions import CONNECTOR_ID as CONNECTOR_ID
from ._generated.definitions import DEFECT as DEFECT
from ._generated.definitions import GEOCODE_ADDRESS_DEFINITION as GEOCODE_ADDRESS_DEFINITION
from ._generated.definitions import GEOCODE_ADDRESS_OPERATION_ID as GEOCODE_ADDRESS_OPERATION_ID
from ._generated.definitions import GEOCODED as GEOCODED
from ._generated.definitions import INVALID_RESPONSE as INVALID_RESPONSE
from ._generated.definitions import PROVIDER_REJECTED as PROVIDER_REJECTED
from .models import GeocodeAddressInput, GeocodeAddressOutput, GeocodedLocation


class Client:
    """One redirect-safe HTTP transport that resolves the private key for every call."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Amap configuration and credential source are required")
        if config.timeout_seconds <= 0 or config.max_response_bytes <= 0:
            raise ValueError("Amap timeout and maxResponseBytes must be positive")
        self.base_url = validate_base_url(config.base_url)
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)

    def geocode_address(self) -> "GeocodeAddressOperation":
        """Bind the geocodeAddress Query to this client."""
        return GeocodeAddressOperation(self)

    async def aclose(self) -> None:
        """Close the provider HTTP transport after Worker shutdown."""
        await self.http_client.aclose()


class GeocodeAddressOperation:
    """Read one structured Chinese address from Amap Web Service."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Amap client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the stable geocoding operation and branch contract."""
        return GEOCODE_ADDRESS_DEFINITION

    async def invoke(self, call: Call, input: GeocodeAddressInput) -> QueryAttempt[GeocodeAddressOutput]:
        """Send one bounded lookup and classify provider and transport outcomes."""
        if (
            not isinstance(input, GeocodeAddressInput)
            or not isinstance(input.address, str)
            or not isinstance(input.city, str)
        ):
            return _branch(DEFECT, FailureKind.VALIDATION, "address or city is invalid")
        if (
            not input.address.strip()
            or len(input.address.encode("utf-8")) > 512
            or len(input.city.encode("utf-8")) > 128
        ):
            return _branch(DEFECT, FailureKind.VALIDATION, "address or city is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
        except Exception:
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
            )
        if not isinstance(credentials, Credentials) or credentials.api_key is None:
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
            )
        api_key = credentials.api_key.reveal()
        if not _is_valid_api_key(api_key):
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
            )
        query = {"address": input.address, "key": api_key, "output": "JSON"}
        if input.city:
            query["city"] = input.city
        endpoint = self.client.base_url + "/v3/geocode/geo?" + urlencode(sorted(query.items()))
        try:
            request = self.client.http_client.build_request(
                "GET", endpoint, headers={"Accept": "application/json"}
            )
        except (httpx.HTTPError, ValueError):
            return _branch(DEFECT, FailureKind.VALIDATION, "geocoding request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError):
            return _retry(FailureKind.AVAILABILITY, "Amap request failed")
        try:
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Amap rate limited the request", retry_after)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Amap is temporarily unavailable", retry_after)
            if status != 200:
                return _branch(
                    PROVIDER_REJECTED,
                    FailureKind.PROVIDER_REJECTION,
                    f"Amap rejected the request with HTTP {status}",
                )
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    INVALID_RESPONSE,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Amap response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Amap response was interrupted")
            try:
                output, infocode = _decode_response(body)
            except (ValueError, TypeError, UnicodeDecodeError):
                return _branch(INVALID_RESPONSE, FailureKind.PROTOCOL, "Amap returned an invalid response")
            if infocode in ("10004", "10014"):
                return _retry(FailureKind.RATE_LIMIT, "Amap rate limited the request", retry_after)
            if infocode != "10000":
                return _branch(
                    PROVIDER_REJECTED, _classify_infocode(infocode), "Amap rejected the geocoding request"
                )
            return QueryAttempt.branch_result(GEOCODED, output, Receipt())
        finally:
            await response.aclose()


def _is_valid_api_key(api_key: object) -> bool:
    return (
        isinstance(api_key, str)
        and 0 < len(api_key) <= 256
        and all("!" <= character <= "~" for character in api_key)
    )


def _decode_response(body: bytes) -> tuple[GeocodeAddressOutput, str]:
    document = json.loads(
        body, parse_constant=lambda _: (_ for _ in ()).throw(ValueError("invalid constant"))
    )
    if not isinstance(document, dict):
        raise ValueError("invalid envelope")
    status = document.get("status")
    infocode = document.get("infocode")
    if status not in ("0", "1") or not isinstance(infocode, str) or not infocode:
        raise ValueError("invalid envelope")
    if status == "0":
        return GeocodeAddressOutput(), infocode
    geocodes = document.get("geocodes")
    if infocode != "10000" or not isinstance(geocodes, list):
        raise ValueError("invalid success")
    count = document.get("count")
    if not isinstance(count, str) or re.fullmatch(r"[+-]?[0-9]+", count) is None:
        raise ValueError("invalid count")
    try:
        parsed_count = int(count)
    except ValueError as error:
        raise ValueError("invalid count") from error
    if parsed_count != len(geocodes) or parsed_count > 100:
        raise ValueError("invalid count")
    locations: list[GeocodedLocation] = []
    for item in geocodes:
        if not isinstance(item, dict):
            raise ValueError("invalid geocode")
        longitude, latitude = _parse_location(item.get("location"))
        locations.append(
            GeocodedLocation(
                formatted_address=_go_typed_string(item.get("formatted_address")),
                province=_go_string(item.get("province")),
                city=_go_string(item.get("city")),
                district=_go_string(item.get("district")),
                adcode=_go_typed_string(item.get("adcode")),
                longitude=longitude,
                latitude=latitude,
                level=_go_typed_string(item.get("level")),
            )
        )
    return GeocodeAddressOutput(tuple(locations)), infocode


def _parse_location(value: Any) -> tuple[float, float]:
    if not isinstance(value, str):
        raise ValueError("invalid location")
    parts = value.split(",")
    if len(parts) != 2:
        raise ValueError("invalid location")
    if any(
        re.fullmatch(r"[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?|[+-]?(?:Inf|NaN)", part)
        is None
        for part in parts
    ):
        raise ValueError("invalid location")
    coordinates = tuple(float(part) for part in parts)
    longitude, latitude = coordinates
    if not math.isfinite(longitude) or not -180 <= longitude <= 180:
        raise ValueError("invalid longitude")
    if not math.isfinite(latitude) or not -90 <= latitude <= 90:
        raise ValueError("invalid latitude")
    return longitude, latitude


def _go_string(value: Any) -> str:
    return value if isinstance(value, str) else ""


def _go_typed_string(value: Any) -> str:
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError("invalid geocode text")
    return value


def _classify_infocode(infocode: str) -> FailureKind:
    if infocode in ("10001", "10013"):
        return FailureKind.AUTHENTICATION
    if infocode in ("10002", "10005", "10007", "10009", "10012"):
        return FailureKind.AUTHORIZATION
    if infocode in ("10003", "10010"):
        return FailureKind.QUOTA_EXHAUSTED
    if infocode in ("20000", "20001"):
        return FailureKind.VALIDATION
    return FailureKind.PROVIDER_REJECTION


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[GeocodeAddressOutput]:
    return QueryAttempt.branch_result(
        branch,
        GeocodeAddressOutput(),
        Receipt(),
        Failure(kind, CONNECTOR_ID, GEOCODE_ADDRESS_OPERATION_ID, message),
    )


def _retry(kind: FailureKind, message: str, after_seconds: float = 0) -> QueryAttempt[GeocodeAddressOutput]:
    return QueryAttempt.retry(
        Failure(kind, CONNECTOR_ID, GEOCODE_ADDRESS_OPERATION_ID, message), after_seconds
    )


Config.__doc__ = "Amap HTTPS origin, request timeout in seconds, and response-size bound in bytes."
Credentials.__doc__ = "Private Amap Web Service key resolved again for each attempt."
