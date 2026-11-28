# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Haimou IP location transport and Go-compatible Query decisions."""

import ipaddress
import json
from datetime import UTC, datetime
from typing import Any

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

_ENDPOINT = "https://hmkj4399.com/v1/ip"


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[LookupIPLocationOutput]:
    return QueryAttempt.branch_result(
        branch,
        LookupIPLocationOutput(),
        Receipt(),
        Failure(kind, "haimou", "lookupIPLocation", message),
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[LookupIPLocationOutput]:
    return QueryAttempt.retry(Failure(kind, "haimou", "lookupIPLocation", message), delay)


def _valid_api_key(value: str) -> bool:
    return bool(value) and len(value) <= 1024 and all(0x20 < ord(character) < 0x7F for character in value)


_PRIVATE_NETWORKS = tuple(
    ipaddress.ip_network(value) for value in ("10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16")
)


def _public_ipv4(value: object) -> str | None:
    if not isinstance(value, str) or "%" in value:
        return None
    try:
        parsed = ipaddress.ip_address(value)
        address = parsed.ipv4_mapped if isinstance(parsed, ipaddress.IPv6Address) else parsed
    except ValueError:
        return None
    if (
        not isinstance(address, ipaddress.IPv4Address)
        or address.is_loopback
        or address.is_link_local
        or address.is_unspecified
        or address.is_multicast
        or any(address in network for network in _PRIVATE_NETWORKS)
    ):
        return None
    return str(address)


class Client:
    """Own the redirect-safe HTTP transport and resolve credentials for every IP lookup."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Haimou configuration and credential source are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("Haimou timeout or response limit is invalid")
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
    """Read country, region, and network fields for a public IPv4 address."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Haimou client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated IP location Query identity."""
        return LOOKUP_IP_LOCATION_DEFINITION

    async def invoke(self, call: Call, input: LookupIPLocationInput) -> QueryAttempt[LookupIPLocationOutput]:
        """Make one IP location request and classify its provider outcome."""
        address = _public_ipv4(input.ip_address)
        if address is None:
            return _branch("defect", FailureKind.VALIDATION, "HAIMOU requires one public IPv4 address")
        try:
            credentials = await self.client.credentials.resolve(call)
            api_key = credentials.api_key.reveal()
            if not _valid_api_key(api_key):
                raise ValueError("invalid API Key")
        except (ValueError, LookupError, OSError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "HAIMOU credentials are unavailable or invalid"
            )
        try:
            request = self.client.http_client.build_request(
                "GET",
                _ENDPOINT,
                params={"apikey": api_key, "ip": address},
                headers={"Accept": "application/json"},
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "HAIMOU lookup request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "HAIMOU lookup request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "HAIMOU rate limited the lookup", delay)
            if status == 408 or status >= 500:
                return _retry(
                    FailureKind.AVAILABILITY, "HAIMOU lookup service is temporarily unavailable", delay
                )
            if status != 200:
                kind = FailureKind.PROVIDER_REJECTION
                if status == 401:
                    kind = FailureKind.AUTHENTICATION
                elif status == 403:
                    kind = FailureKind.AUTHORIZATION
                return _branch("providerRejected", kind, f"HAIMOU rejected lookup with HTTP {status}")
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "HAIMOU response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "HAIMOU response was interrupted")
        finally:
            await response.aclose()
        return _decode_location(body, delay)


def _decode_location(body: bytes, delay: float) -> QueryAttempt[LookupIPLocationOutput]:
    try:
        document: Any = json.loads(body, parse_constant=_reject_nonfinite_number)
        if not isinstance(document, dict):
            raise ValueError("invalid envelope")
        wire = {key.lower(): value for key, value in document.items()}
        code = wire["code"]
        if type(code) is not int or not -(1 << 63) <= code < (1 << 63):
            raise ValueError("invalid result code")
        nested = wire.get("data")
        if nested is not None and not isinstance(nested, dict):
            raise ValueError("invalid location envelope")
        wire = {key.lower(): value for key, value in (nested or {}).items()}
        location: dict[str, str] = {}
        for field in ("country", "province", "city", "district", "isp"):
            value = wire.get(field)
            if value is not None and not isinstance(value, str):
                raise ValueError("invalid location field")
            location[field] = value or ""
    except (KeyError, ValueError, UnicodeDecodeError):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "HAIMOU returned invalid lookup data")
    if code in (10006, 429):
        return _retry(FailureKind.RATE_LIMIT, "HAIMOU rate limited the lookup", delay)
    if 500 <= code <= 599:
        return _retry(FailureKind.AVAILABILITY, "HAIMOU lookup service is temporarily unavailable", delay)
    if code != 200:
        kind = FailureKind.PROVIDER_REJECTION
        if code in (10001, 10002):
            kind = FailureKind.AUTHENTICATION
        elif code == 10003:
            kind = FailureKind.AUTHORIZATION
        elif code == 10005:
            kind = FailureKind.QUOTA_EXHAUSTED
        return _branch("providerRejected", kind, f"HAIMOU rejected lookup with code {code}")
    if not location["country"].strip() or not location["city"].strip():
        return _branch("invalidResponse", FailureKind.PROTOCOL, "HAIMOU returned incomplete location data")
    return QueryAttempt.branch_result(
        "found",
        LookupIPLocationOutput(
            country=location["country"],
            province=location["province"],
            city=location["city"],
            district=location["district"],
            operator=location["isp"],
        ),
        Receipt(),
    )


def _reject_nonfinite_number(value: str) -> None:
    raise ValueError(f"Invalid JSON number {value}")
