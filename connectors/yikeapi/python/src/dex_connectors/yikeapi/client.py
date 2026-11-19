# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Yikeapi provider transport and Go-compatible Query decisions."""

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


def _opaque(value: str, limit: int = 1024) -> bool:
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
            raise ValueError("Yikeapi configuration and credentials are required")
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
    """Query Yikeapi and classify the provider outcome."""

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
            Failure(kind, "yikeapi", "lookupIPLocation", "Yikeapi request failed"),
        )

    def _retry(self, kind: FailureKind, delay: float = 0) -> QueryAttempt[LookupIPLocationOutput]:
        return QueryAttempt.retry(
            Failure(kind, "yikeapi", "lookupIPLocation", "Yikeapi request failed"), delay
        )

    def _request(
        self, input: LookupIPLocationInput, credentials: Credentials
    ) -> httpx.Request | QueryAttempt[LookupIPLocationOutput]:
        import ipaddress

        try:
            address = ipaddress.IPv4Address(input.ip_address)
        except (ipaddress.AddressValueError, TypeError):
            return self._branch("defect", FailureKind.VALIDATION)
        if (
            address.is_private
            or address.is_loopback
            or address.is_link_local
            or address.is_unspecified
            or address.is_multicast
        ):
            return self._branch("defect", FailureKind.VALIDATION)
        app_id = credentials.app_id.reveal()
        app_secret = credentials.app_secret.reveal()
        if not _opaque(app_id) or not _opaque(app_secret):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        return self.client.http_client.build_request(
            "GET",
            "https://apia.yikeapi.com/ip/",
            params={"appid": app_id, "appsecret": app_secret, "ip": str(address)},
            headers={"Accept": "application/json"},
        )

    def _decode(
        self, wire: object, input: LookupIPLocationInput
    ) -> LookupIPLocationOutput | QueryAttempt[LookupIPLocationOutput] | None:
        if not isinstance(wire, dict) or type(wire.get("errcode")) is not int:
            return None
        if wire["errcode"] != 0:
            return self._branch("providerRejected", FailureKind.PROVIDER_REJECTION)
        import ipaddress

        if (
            wire.get("ip") != str(ipaddress.IPv4Address(input.ip_address))
            or not isinstance(wire.get("country"), str)
            or not wire["country"].strip()
            or not isinstance(wire.get("city"), str)
            or not wire["city"].strip()
        ):
            return None
        for field in ("province", "district"):
            if not isinstance(wire.get(field, ""), str):
                return None
        return LookupIPLocationOutput(
            country=wire["country"],
            province=wire.get("province", ""),
            city=wire["city"],
            district=wire.get("district", ""),
        )

    async def invoke(self, call: Call, input: LookupIPLocationInput) -> QueryAttempt[LookupIPLocationOutput]:
        """Read one bounded provider response without persisting credentials."""
        if not isinstance(input, LookupIPLocationInput):
            return self._branch("defect", FailureKind.VALIDATION)
        try:
            credentials = await self.client.credentials.resolve(call)
        except (ValueError, LookupError, OSError, AttributeError):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        request = self._request(input, credentials)
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
            wire = json.loads(body)
        except (ValueError, UnicodeDecodeError):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        result = self._decode(wire, input)
        if isinstance(result, QueryAttempt):
            return result
        if result is None:
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        return QueryAttempt.branch_result("found", result, Receipt())
