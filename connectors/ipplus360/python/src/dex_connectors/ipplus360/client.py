# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""IPPlus360 v2 transport and Go-compatible location decisions."""

import ipaddress
import json
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
from ._generated.definitions import LOOKUP_IP_LOCATION_DEFINITION
from .models import LookupIPLocationInput, LookupIPLocationOutput

_ENDPOINT = "https://api.ipplus360.com/ip/geo/v1/district/"


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[LookupIPLocationOutput]:
    return QueryAttempt.branch_result(
        branch, LookupIPLocationOutput(), Receipt(), Failure(kind, "ipplus360", "lookupIPLocation", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[LookupIPLocationOutput]:
    return QueryAttempt.retry(Failure(kind, "ipplus360", "lookupIPLocation", message), delay)


class Client:
    """Resolve IPPlus360 credentials for each Query and own a redirect-safe HTTP transport."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("IPPlus360 configuration and credential source are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("IPPlus360 timeout or response limit is invalid")
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def lookup_ip_location(self) -> "LookupIPLocationOperation":
        """Bind the IP location Query to this client."""
        return LookupIPLocationOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP transport when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class LookupIPLocationOperation:
    """Read the default coarse location fields for a public IPv4 address."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("IPPlus360 client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated IPPlus360 Query identity."""
        return LOOKUP_IP_LOCATION_DEFINITION

    async def invoke(self, call: Call, input: LookupIPLocationInput) -> QueryAttempt[LookupIPLocationOutput]:
        """Make one IPPlus360 request and classify its provider outcome."""
        try:
            address = ipaddress.ip_address(input.ip_address)
            if not isinstance(address, ipaddress.IPv4Address) or not address.is_global:
                raise ValueError("not a public IPv4 address")
        except ValueError:
            return _branch("defect", FailureKind.VALIDATION, "IPPlus360 requires one public IPv4 address")
        try:
            credentials = await self.client.credentials.resolve(call)
            api_key = credentials.api_key.reveal()
            if not api_key or len(api_key) > 1024 or any(not 0x20 < ord(c) < 0x7F for c in api_key):
                raise ValueError("invalid API key")
        except (ValueError, LookupError, OSError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "IPPlus360 credentials are unavailable or invalid"
            )
        try:
            endpoint = _ENDPOINT + "?" + urlencode({"ip": str(address), "key": api_key})
            request = self.client.http_client.build_request(
                "GET", endpoint, headers={"Accept": "application/json"}
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "IPPlus360 location request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "IPPlus360 location request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "IPPlus360 rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(
                    FailureKind.AVAILABILITY, "IPPlus360 location service is temporarily unavailable", delay
                )
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return _branch(
                    "providerRejected", kind, f"IPPlus360 rejected location request with HTTP {status}"
                )
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "IPPlus360 response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "IPPlus360 response was interrupted")
        finally:
            await response.aclose()
        return _decode_location(body)


def _decode_location(body: bytes) -> QueryAttempt[LookupIPLocationOutput]:
    try:
        wire: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "IPPlus360 returned invalid lookup data")
    if not isinstance(wire, dict) or not isinstance(wire.get("code"), str) or not wire["code"]:
        return _branch("invalidResponse", FailureKind.PROTOCOL, "IPPlus360 returned invalid lookup data")
    code = wire["code"]
    if code == "NoData":
        return _branch("providerRejected", FailureKind.NOT_FOUND, "IPPlus360 returned no district data")
    if code != "Success":
        kind = FailureKind.PROVIDER_REJECTION
        if code in {"KeyNotFound", "KeyExpired"}:
            kind = FailureKind.AUTHENTICATION
        elif code in {"KeyUnauthorized", "KeyDisabled"}:
            kind = FailureKind.AUTHORIZATION
        elif code in {"KeyNotEnoughBalance", "DailyLimited"}:
            kind = FailureKind.QUOTA_EXHAUSTED
        safe_code = (
            code
            if code
            in {
                "NoData",
                "MissingParameter",
                "InvalidParameter",
                "DailyLimited",
                "UnknownError",
                "KeyNotFound",
                "KeyUnauthorized",
                "KeyExpired",
                "KeyNotEnoughBalance",
                "KeyDisabled",
            }
            else "unknown"
        )
        return _branch("providerRejected", kind, f"IPPlus360 rejected lookup with code {safe_code}")
    location = wire.get("data")
    if not isinstance(location, dict):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "IPPlus360 returned incomplete location data")
    fields = ("country", "prov", "city", "district", "isp")
    if (
        any(not isinstance(location.get(field, ""), str) for field in fields)
        or not location.get("country", "").strip()
        or not location.get("city", "").strip()
        or not location.get("district", "").strip()
    ):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "IPPlus360 returned incomplete location data")
    return QueryAttempt.branch_result(
        "found",
        LookupIPLocationOutput(
            country=location["country"],
            province=location.get("prov", ""),
            city=location["city"],
            district=location["district"],
            operator=location.get("isp", ""),
        ),
        Receipt(),
    )
