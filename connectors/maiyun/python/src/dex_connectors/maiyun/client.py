# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider Query implementation with Go-compatible classifications."""

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
from ._generated.definitions import (
    DEFECT,
    FOUND,
    INVALID_RESPONSE,
    LOOKUP_IP_LOCATION_DEFINITION,
    PROVIDER_REJECTED,
)
from .models import LookupIPLocationInput, LookupIPLocationOutput

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
    """Own a bounded transport and resolve private credentials per call."""

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
            raise ValueError("MAIYUN configuration and credentials are invalid")
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)

    def lookup_ip_location(self) -> "LookupIPLocationOperation":
        """Bind the read-only provider Query."""
        return LookupIPLocationOperation(self)

    async def aclose(self) -> None:
        """Close the provider transport after Worker shutdown."""
        await self.http_client.aclose()


class LookupIPLocationOperation:
    """Read provider data with typed Go-compatible outcomes."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("MAIYUN client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the stable Query and branch contract."""
        return LOOKUP_IP_LOCATION_DEFINITION

    async def invoke(self, call: Call, input: LookupIPLocationInput) -> QueryAttempt[LookupIPLocationOutput]:
        """Classify one bounded provider request and discard secret response fields."""
        if not isinstance(input, LookupIPLocationInput):
            return _branch(DEFECT, FailureKind.VALIDATION, "MAIYUN requires one public IPv4 address")
        address = _public_ipv4(input.ip_address)
        if address is None:
            return _branch(DEFECT, FailureKind.VALIDATION, "MAIYUN requires one public IPv4 address")
        try:
            credentials = await self.client.credentials.resolve(call)
        except (ValueError, LookupError, OSError):
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "MAIYUN credentials are unavailable or invalid"
            )
        if not isinstance(credentials, Credentials) or not _valid_secret(credentials.token.reveal()):
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "MAIYUN credentials are unavailable or invalid"
            )
        endpoint = "https://lts.maiyun.net/api/service/ip"
        headers = {
            "Accept": "application/json",
            "Content-Type": "application/json",
            "Authorization": "Bearer " + credentials.token.reveal(),
        }
        try:
            request = self.client.http_client.build_request(
                "POST",
                endpoint,
                headers=headers,
                content=json.dumps({"ip": address}, separators=(",", ":")),
            )
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _retry(FailureKind.AVAILABILITY, "MAIYUN request failed")
        try:
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "MAIYUN rate limited the request", retry_after)
            if status == 408 or status >= 500:
                return _retry(
                    FailureKind.AVAILABILITY, "MAIYUN service is temporarily unavailable", retry_after
                )

            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return _branch(PROVIDER_REJECTED, kind, f"MAIYUN rejected the request with HTTP {status}")
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    INVALID_RESPONSE,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "MAIYUN response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "MAIYUN response was interrupted")
            return _decode_location(body, retry_after)
        finally:
            await response.aclose()


def _valid_secret(value: object) -> bool:
    return isinstance(value, str) and 0 < len(value) <= 1024 and all(32 < ord(c) < 127 for c in value)


def _decode_location(body: bytes, delay: float) -> QueryAttempt[LookupIPLocationOutput]:
    try:
        document: Any = json.loads(body, parse_constant=_reject_nonfinite_number)
        if not isinstance(document, dict):
            raise ValueError("invalid envelope")
        wire = {key.lower(): value for key, value in document.items()}
        code = wire["result"]
        if type(code) is not int or not -(1 << 63) <= code < (1 << 63):
            raise ValueError("invalid result code")
        location: dict[str, str] = {}
        for field in ("country", "province", "city", "district"):
            value = wire.get(field)
            if value is not None and not isinstance(value, str):
                raise ValueError("invalid location field")
            location[field] = value or ""
    except (KeyError, ValueError, UnicodeDecodeError):
        return _branch(INVALID_RESPONSE, FailureKind.PROTOCOL, "MAIYUN returned invalid lookup data")
    if code == -429:
        return _retry(FailureKind.RATE_LIMIT, "MAIYUN rate limited the lookup", delay)
    if code <= -500:
        return _retry(FailureKind.AVAILABILITY, "MAIYUN service is temporarily unavailable", delay)
    if code != 1:
        kind = (
            FailureKind.AUTHENTICATION
            if code in (-401, -402)
            else FailureKind.AUTHORIZATION
            if code == -403
            else FailureKind.QUOTA_EXHAUSTED
            if code == -10
            else FailureKind.PROVIDER_REJECTION
        )
        return _branch(PROVIDER_REJECTED, kind, f"MAIYUN rejected lookup with code {code}")
    if not location["country"].strip() or not location["city"].strip():
        return _branch(INVALID_RESPONSE, FailureKind.PROTOCOL, "MAIYUN returned incomplete location data")
    return QueryAttempt.branch_result(
        FOUND,
        LookupIPLocationOutput(
            country=location["country"],
            province=location["province"],
            city=location["city"],
            district=location["district"],
        ),
        Receipt(),
    )


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[LookupIPLocationOutput]:
    return QueryAttempt.branch_result(
        branch, LookupIPLocationOutput(), Receipt(), Failure(kind, "maiyun", "lookupIPLocation", message)
    )


def _retry(kind: FailureKind, message: str, retry_after: float = 0) -> QueryAttempt[LookupIPLocationOutput]:
    return QueryAttempt.retry(Failure(kind, "maiyun", "lookupIPLocation", message), retry_after)


def _reject_nonfinite_number(value: str) -> None:
    raise ValueError(f"Invalid JSON number {value}")
