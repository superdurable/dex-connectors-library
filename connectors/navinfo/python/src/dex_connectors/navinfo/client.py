# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Navinfo provider transport and Go-compatible Query decisions."""

import ipaddress
import json
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
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import LOOKUP_IP_LOCATION_DEFINITION
from .models import LookupIPLocationInput, LookupIPLocationOutput


def _opaque(value: str, limit: int) -> bool:
    return (
        isinstance(value, str) and 0 < len(value) <= limit and all(0x20 < ord(char) < 0x7F for char in value)
    )


class Client:
    """Resolve credentials for each lookupIPLocation request."""

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
            raise ValueError("Navinfo configuration and credentials are required")
        self.config = config
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_http_client = http_client is None

    def lookup_ip_location(self) -> "LookupIPLocationOperation":
        """Bind the lookupIPLocation Query."""
        return LookupIPLocationOperation(self)

    async def aclose(self) -> None:
        """Close the owned HTTP transport."""
        if self._owns_http_client:
            await self.http_client.aclose()


class LookupIPLocationOperation:
    """Query Navinfo and classify the provider outcome."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query definition."""
        return LOOKUP_IP_LOCATION_DEFINITION

    def _branch(self, branch: str, kind: FailureKind) -> QueryAttempt[LookupIPLocationOutput]:
        return QueryAttempt.branch_result(
            branch,
            LookupIPLocationOutput(),
            Receipt(),
            Failure(kind, "navinfo", "lookupIPLocation", "Navinfo request failed"),
        )

    def _retry(self, kind: FailureKind, delay: float = 0) -> QueryAttempt[LookupIPLocationOutput]:
        return QueryAttempt.retry(
            Failure(kind, "navinfo", "lookupIPLocation", "Navinfo request failed"), delay
        )

    def _request(
        self, address: ipaddress.IPv4Address, credentials: Credentials
    ) -> httpx.Request | QueryAttempt[LookupIPLocationOutput]:
        token = credentials.ak.reveal()
        if not _opaque(token, 1024):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        return self.client.http_client.build_request(
            "GET",
            "https://lbs.navinfo.com/open-api/position/v1/ip",
            params={"ak": token, "ip": str(address)},
            headers={"Accept": "application/json"},
        )

    def _decode(
        self, wire: object, credentials: Credentials
    ) -> LookupIPLocationOutput | QueryAttempt[LookupIPLocationOutput] | None:
        if not isinstance(wire, dict):
            return None
        response = {key.lower(): value for key, value in wire.items()}
        status = response.get("status")
        if type(status) is not int or not -(1 << 63) <= status < (1 << 63):
            return None
        data = response.get("data")
        if data is not None and not isinstance(data, dict):
            return None
        fields = {key.lower(): value for key, value in (data or {}).items()}
        location: dict[str, str] = {}
        for name in ("prov", "city", "adcode"):
            value = fields.get(name)
            if value is not None and not isinstance(value, str):
                return None
            location[name] = value or ""
        if status in (1008, 1010):
            return self._retry(FailureKind.RATE_LIMIT)
        if status != 200:
            return self._branch("providerRejected", FailureKind.PROVIDER_REJECTION)
        province, city, adcode = location["prov"], location["city"], location["adcode"]
        if (
            data is None
            or len(province.encode()) > 128
            or len(city.encode()) > 128
            or len(adcode.encode()) > 16
            or any(char in province + city + adcode for char in "\r\n\x00")
            or credentials.ak.reveal() in province + city + adcode
        ):
            return None
        return LookupIPLocationOutput(province=province, city=city, adcode=adcode)

    async def invoke(self, call: Call, input: LookupIPLocationInput) -> QueryAttempt[LookupIPLocationOutput]:
        """Read one bounded provider response without persisting secrets."""
        if not isinstance(input, LookupIPLocationInput):
            return self._branch("defect", FailureKind.VALIDATION)
        try:
            address = ipaddress.ip_address(input.ip_address)
        except ValueError:
            return self._branch("defect", FailureKind.VALIDATION)
        if isinstance(address, ipaddress.IPv6Address):
            if address.scope_id is not None or address.ipv4_mapped is None:
                return self._branch("defect", FailureKind.VALIDATION)
            address = address.ipv4_mapped
        try:
            credentials = await self.client.credentials.resolve(call)
        except (ValueError, LookupError, OSError, AttributeError):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        request = self._request(address, credentials)
        if isinstance(request, QueryAttempt):
            return request
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return self._retry(FailureKind.AVAILABILITY)
        try:
            status = response.status_code
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return self._retry(FailureKind.RATE_LIMIT, delay)
            if status == 408 or status >= 500:
                return self._retry(FailureKind.AVAILABILITY, delay)
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return self._branch("providerRejected", kind)
            try:
                body = await read_bounded_body(response, self.client.config.max_response_bytes)
            except BodyTooLargeError:
                return self._branch("invalidResponse", FailureKind.RESPONSE_TOO_LARGE)
            except ProviderTransportError:
                return self._retry(FailureKind.AVAILABILITY)
        finally:
            await response.aclose()
        try:
            wire = json.loads(body.decode("utf-8", errors="replace"), parse_constant=_reject_nonfinite_number)
        except (ValueError, UnicodeDecodeError):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        result = self._decode(wire, credentials)
        if isinstance(result, QueryAttempt):
            return result
        if result is None:
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        return QueryAttempt.branch_result("found", result, Receipt())


def _reject_nonfinite_number(value: str) -> None:
    raise ValueError(f"Invalid JSON number {value}")
