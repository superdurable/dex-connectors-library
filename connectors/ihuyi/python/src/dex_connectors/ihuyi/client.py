# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Ihuyi time-bound IP lookup transport and Go-compatible decisions."""

import hashlib
import ipaddress
import json
from collections.abc import Callable
from datetime import UTC, datetime, timedelta
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

_ENDPOINT = "https://api.ihuyi.com/appdev/ip/Submit.json"


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[LookupIPLocationOutput]:
    return QueryAttempt.branch_result(
        branch, LookupIPLocationOutput(), Receipt(), Failure(kind, "ihuyi", "lookupIPLocation", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[LookupIPLocationOutput]:
    return QueryAttempt.retry(Failure(kind, "ihuyi", "lookupIPLocation", message), delay)


def _valid_credential(value: str) -> bool:
    return (
        bool(value)
        and len(value) <= 1024
        and value.strip() == value
        and all(0x20 < ord(character) < 0x7F for character in value)
    )


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
    """Resolve Ihuyi credentials per Query and own a redirect-safe transport."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        now: Callable[[], datetime] | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Ihuyi configuration and credential source are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("Ihuyi timeout or response limit is invalid")
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None
        self.now = now or (lambda: datetime.now(UTC))

    def lookup_ip_location(self) -> "LookupIPLocationOperation":
        """Bind the IP lookup Query to this client."""
        return LookupIPLocationOperation(self)

    async def aclose(self) -> None:
        """Close the transport when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class LookupIPLocationOperation:
    """Read the location of one public IPv4 address."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Ihuyi client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated IP lookup Query identity."""
        return LOOKUP_IP_LOCATION_DEFINITION

    async def invoke(self, call: Call, input: LookupIPLocationInput) -> QueryAttempt[LookupIPLocationOutput]:
        """Send a time-bound signed lookup and classify its provider outcome."""
        address = _public_ipv4(input.ip_address)
        if address is None:
            return _branch("defect", FailureKind.VALIDATION, "Ihuyi requires one public IPv4 address")
        try:
            credentials = await self.client.credentials.resolve(call)
            api_id = credentials.api_id
            api_key = credentials.api_key.reveal()
            if not _valid_credential(api_id) or not _valid_credential(api_key):
                raise ValueError("invalid credentials")
        except (ValueError, LookupError, OSError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Ihuyi credentials are unavailable or invalid"
            )
        elapsed = self.client.now().astimezone(UTC) - datetime(1970, 1, 1, tzinfo=UTC)
        timestamp = str(elapsed // timedelta(seconds=1))
        digest = hashlib.md5((api_id + api_key + timestamp).encode(), usedforsecurity=False).hexdigest()
        try:
            request = self.client.http_client.build_request(
                "POST",
                _ENDPOINT,
                content=urlencode(
                    {"account": api_id, "password": digest, "ip": str(address), "time": timestamp}
                ),
                headers={"Content-Type": "application/x-www-form-urlencoded", "Accept": "application/json"},
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "Ihuyi lookup request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "Ihuyi lookup request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Ihuyi rate limited the lookup", delay)
            if status == 408 or status >= 500:
                return _retry(
                    FailureKind.AVAILABILITY, "Ihuyi lookup service is temporarily unavailable", delay
                )
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return _branch("providerRejected", kind, f"Ihuyi rejected lookup with HTTP {status}")
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Ihuyi response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Ihuyi response was interrupted")
        finally:
            await response.aclose()
        return _decode_location(body)


def _decode_location(body: bytes) -> QueryAttempt[LookupIPLocationOutput]:
    try:
        document: Any = json.loads(body, parse_constant=_reject_nonfinite_number)
        if not isinstance(document, dict):
            raise ValueError("invalid envelope")
        wire = {key.lower(): value for key, value in document.items()}
        code = wire["code"]
        if type(code) is not int or not -(1 << 63) <= code < (1 << 63):
            raise ValueError("invalid result code")
        nested = wire.get("result")
        if nested is not None and not isinstance(nested, dict):
            raise ValueError("invalid location envelope")
        wire = {key.lower(): value for key, value in (nested or {}).items()}
        location: dict[str, str] = {}
        for field in ("country", "porovince", "city", "district", "operator"):
            value = wire.get(field)
            if value is not None and not isinstance(value, str):
                raise ValueError("invalid location field")
            location[field] = value or ""
    except (KeyError, ValueError, UnicodeDecodeError):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "Ihuyi returned invalid lookup data")
    if code != 2:
        kind = (
            FailureKind.AUTHENTICATION
            if code in (405, 4050, 40501, 40502)
            else FailureKind.AUTHORIZATION
            if code in (400, 4052)
            else FailureKind.PROVIDER_REJECTION
        )
        return _branch("providerRejected", kind, f"Ihuyi rejected lookup with code {code}")
    if not location["porovince"].strip() or not location["city"].strip():
        return _branch("invalidResponse", FailureKind.PROTOCOL, "Ihuyi returned incomplete location data")
    return QueryAttempt.branch_result(
        "found",
        LookupIPLocationOutput(
            country=location["country"],
            province=location["porovince"],
            city=location["city"],
            district=location["district"],
            operator=location["operator"],
        ),
        Receipt(),
    )


def _reject_nonfinite_number(value: str) -> None:
    raise ValueError(f"Invalid JSON number {value}")
