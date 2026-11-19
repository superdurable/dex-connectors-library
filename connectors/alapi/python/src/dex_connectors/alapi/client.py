# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""ALAPI IP lookup with Go-compatible provider decisions."""

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
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import LOOKUP_IP_LOCATION_DEFINITION
from .models import LookupIPLocationInput, LookupIPLocationOutput

_ENDPOINT = "https://v3.alapi.cn/api/ip"
_PRIVATE_NETWORKS = tuple(
    ipaddress.ip_network(value) for value in ("10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16")
)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[LookupIPLocationOutput]:
    failure = Failure(kind, "alapi", "lookupIPLocation", message)
    return QueryAttempt.branch_result(branch, LookupIPLocationOutput(), Receipt(), failure)


def _retry(kind: FailureKind, message: str, retry_after: float = 0) -> QueryAttempt[LookupIPLocationOutput]:
    return QueryAttempt.retry(Failure(kind, "alapi", "lookupIPLocation", message), retry_after)


def _public_ipv4(value: object) -> str | None:
    if not isinstance(value, str):
        return None
    try:
        address = ipaddress.ip_address(value)
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
    """Own an HTTP transport and resolve the ALAPI token for every attempt."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("ALAPI configuration and credential source are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("ALAPI configuration is invalid")
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def lookup_ip_location(self) -> "LookupIPLocationOperation":
        """Bind the public IPv4 location Query."""
        return LookupIPLocationOperation(self)

    async def aclose(self) -> None:
        """Close the transport when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class LookupIPLocationOperation:
    """Read coarse location fields for one public IPv4 address."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("ALAPI client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query identity and retry defaults."""
        return LOOKUP_IP_LOCATION_DEFINITION

    async def invoke(self, call: Call, input: LookupIPLocationInput) -> QueryAttempt[LookupIPLocationOutput]:
        """Make one provider request and classify the observed result."""
        address = _public_ipv4(input.ip_address) if isinstance(input, LookupIPLocationInput) else None
        if address is None:
            return _branch("defect", FailureKind.VALIDATION, "ALAPI requires one public IPv4 address")
        try:
            credentials = await self.client.credentials.resolve(call)
            token = credentials.token.reveal()
            if len(token) > 1024 or not is_header_safe_credential(token):
                raise ValueError("invalid token")
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "ALAPI credentials are unavailable or invalid"
            )
        try:
            request = self.client.http_client.build_request(
                "POST",
                _ENDPOINT,
                headers={"Accept": "application/json", "Content-Type": "application/json", "token": token},
                content=json.dumps({"ip": address}, ensure_ascii=False, separators=(",", ":")).encode(),
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "ALAPI lookup request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "ALAPI lookup request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "ALAPI rate limited the lookup", delay)
            if status == 408 or status >= 500:
                return _retry(
                    FailureKind.AVAILABILITY, "ALAPI lookup service is temporarily unavailable", delay
                )
            if status != 200:
                kind = FailureKind.PROVIDER_REJECTION
                if status == 401:
                    kind = FailureKind.AUTHENTICATION
                elif status == 403:
                    kind = FailureKind.AUTHORIZATION
                return _branch("providerRejected", kind, f"ALAPI rejected lookup with HTTP {status}")
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "ALAPI response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "ALAPI response was interrupted")
        finally:
            await response.aclose()
        return _decode_response(body, delay)


def _decode_response(body: bytes, retry_after: float) -> QueryAttempt[LookupIPLocationOutput]:
    try:
        document: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "ALAPI returned invalid lookup data")
    if (
        not isinstance(document, dict)
        or type(document.get("code")) is not int
        or type(document.get("success")) is not bool
    ):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "ALAPI returned invalid lookup data")
    code = document["code"]
    if code == 429:
        return _retry(FailureKind.RATE_LIMIT, "ALAPI rate limited the lookup", retry_after)
    if code != 200 or not document["success"]:
        kind = FailureKind.PROVIDER_REJECTION
        if code in (10001, 10002, 10008):
            kind = FailureKind.AUTHENTICATION
        elif code in (10004, 10009):
            kind = FailureKind.AUTHORIZATION
        elif code in (10005, 10010):
            kind = FailureKind.QUOTA_EXHAUSTED
        return _branch("providerRejected", kind, f"ALAPI rejected lookup with code {code}")
    data = document.get("data")
    if (
        not isinstance(data, dict)
        or any(
            field in data and data[field] is not None and not isinstance(data[field], str)
            for field in ("country", "province", "city", "district", "isp")
        )
        or not isinstance(data.get("country"), str)
        or not isinstance(data.get("city"), str)
        or not data["country"].strip()
        or not data["city"].strip()
    ):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "ALAPI returned incomplete location data")
    return QueryAttempt.branch_result(
        "found",
        LookupIPLocationOutput(
            data["country"],
            data.get("province") or "",
            data["city"],
            data.get("district") or "",
            data.get("isp") or "",
        ),
        Receipt(),
    )
