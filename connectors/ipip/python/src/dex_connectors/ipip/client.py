# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""IPIP v2 transport and Go-compatible location decisions."""

import ipaddress
import json
from datetime import UTC, datetime
from typing import Any
from urllib.parse import quote, urlencode

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

_ENDPOINT = "https://ipapi.ipip.net/v2/query/"


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[LookupIPLocationOutput]:
    return QueryAttempt.branch_result(
        branch, LookupIPLocationOutput(), Receipt(), Failure(kind, "ipip", "lookupIPLocation", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[LookupIPLocationOutput]:
    return QueryAttempt.retry(Failure(kind, "ipip", "lookupIPLocation", message), delay)


class Client:
    """Resolve IPIP credentials for each Query and own a redirect-safe HTTP transport."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("IPIP configuration and credential source are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("IPIP timeout or response limit is invalid")
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
    """Read the default coarse location fields for an IPv4 or IPv6 address."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("IPIP client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated IPIP Query identity."""
        return LOOKUP_IP_LOCATION_DEFINITION

    async def invoke(self, call: Call, input: LookupIPLocationInput) -> QueryAttempt[LookupIPLocationOutput]:
        """Make one IPIP request and classify its provider outcome."""
        try:
            address = ipaddress.ip_address(input.ip_address)
            if isinstance(address, ipaddress.IPv6Address):
                if address.scope_id is not None:
                    raise ValueError("scoped IPv6 address")
                address = address.ipv4_mapped or address
        except ValueError:
            return _branch("defect", FailureKind.VALIDATION, "IPIP requires one valid IP address")
        try:
            credentials = await self.client.credentials.resolve(call)
            token = credentials.token.reveal()
            if not token or len(token) > 1024 or any(not 0x20 < ord(c) < 0x7F for c in token):
                raise ValueError("invalid token")
        except (ValueError, LookupError, OSError):
            return _branch("defect", FailureKind.AUTHENTICATION, "IPIP token is unavailable or invalid")
        try:
            endpoint = _ENDPOINT + quote(str(address), safe=":") + "?" + urlencode({"token": token})
            request = self.client.http_client.build_request(
                "GET", endpoint, headers={"Accept": "application/json"}
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "IPIP location request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "IPIP location request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "IPIP rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(
                    FailureKind.AVAILABILITY, "IPIP location service is temporarily unavailable", delay
                )
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return _branch("providerRejected", kind, f"IPIP rejected location request with HTTP {status}")
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "IPIP response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "IPIP response was interrupted")
        finally:
            await response.aclose()
        return _decode_location(body)


def _decode_location(body: bytes) -> QueryAttempt[LookupIPLocationOutput]:
    try:
        wire: Any = json.loads(
            body.decode("utf-8", errors="replace"), parse_constant=_reject_nonfinite_number
        )
    except (ValueError, UnicodeDecodeError):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "IPIP returned invalid location data")
    if not isinstance(wire, dict):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "IPIP returned invalid location data")
    fields = ("country_name", "region_name", "city_name", "country_code")
    response = {key.lower(): value for key, value in wire.items()}
    values: dict[str, Any] = response
    for level in ("data", "info"):
        nested = values.get(level)
        if nested is not None and not isinstance(nested, dict):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "IPIP returned invalid location data")
        values = {key.lower(): value for key, value in (nested or {}).items()}
    info = values
    if any(info.get(field) is not None and not isinstance(info[field], str) for field in fields):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "IPIP returned invalid location data")
    if response.get("ret") == "err":
        return _branch(
            "providerRejected", FailureKind.PROVIDER_REJECTION, "IPIP rejected the location request"
        )
    if (
        response.get("ret") != "ok"
        or any(not isinstance(info.get(field), str) for field in fields)
        or not info["country_name"]
        or not info["country_code"]
    ):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "IPIP returned incomplete location data")
    return QueryAttempt.branch_result(
        "found",
        LookupIPLocationOutput(
            country_name=info["country_name"],
            region_name=info["region_name"],
            city_name=info["city_name"],
            country_code=info["country_code"],
        ),
        Receipt(),
    )


def _reject_nonfinite_number(value: str) -> None:
    raise ValueError(f"Invalid JSON number {value}")
