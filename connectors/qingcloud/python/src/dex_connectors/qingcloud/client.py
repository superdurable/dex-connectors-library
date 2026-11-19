# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Signed QingCloud zone Query with Go-compatible failure decisions."""

import base64
import hashlib
import hmac
import json
from collections.abc import Callable
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
from ._generated.definitions import DESCRIBE_ZONES_DEFINITION
from .models import DescribeZonesInput, Zone, ZonesPage


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ZonesPage]:
    return QueryAttempt.branch_result(
        branch, ZonesPage(), Receipt(), Failure(kind, "qingcloud", "describeZones", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ZonesPage]:
    return QueryAttempt.retry(Failure(kind, "qingcloud", "describeZones", message), delay)


class Client:
    """Resolve rotated keys and sign each QingCloud API request."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        clock: Callable[[], datetime] | None = None,
    ) -> None:
        if (
            config is None
            or credentials is None
            or config.timeout_seconds <= 0
            or not 0 < config.max_response_bytes <= 16 << 20
        ):
            raise ValueError("QingCloud configuration and credentials are invalid")
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.base_url = config.base_url.rstrip("/")
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None
        self.clock = clock or (lambda: datetime.now(UTC))

    def describe_zones(self) -> "DescribeZonesOperation":
        """Bind the accessible-zones Query."""
        return DescribeZonesOperation(self)

    async def aclose(self) -> None:
        """Close the transport if this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class DescribeZonesOperation:
    """List accessible IaaS zones without exposing credentials."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("QingCloud client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the stable generated Query definition."""
        return DESCRIBE_ZONES_DEFINITION

    async def invoke(self, call: Call, input: DescribeZonesInput) -> QueryAttempt[ZonesPage]:
        """Make one signed request and classify its bounded response."""
        if not isinstance(input, DescribeZonesInput):
            return _branch("defect", FailureKind.VALIDATION, "QingCloud input is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
            access_key = credentials.access_key_id
            secret = credentials.secret_access_key.reveal()
            if (
                not access_key
                or len(access_key) > 128
                or any(ord(c) < 33 or ord(c) > 126 for c in access_key)
                or not secret
            ):
                raise ValueError("invalid credential")
        except (ValueError, LookupError, OSError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "QingCloud credentials are unavailable or invalid"
            )
        timestamp = self.client.clock().astimezone(UTC).strftime("%Y-%m-%dT%H:%M:%SZ")
        params = {
            "access_key_id": access_key,
            "action": "DescribeZones",
            "limit": "100",
            "signature_method": "HmacSHA256",
            "signature_version": "1",
            "time_stamp": timestamp,
            "version": "1",
        }
        canonical_query = urlencode(sorted(params.items()))
        signature = base64.b64encode(
            hmac.new(secret.encode(), ("GET\n/iaas/\n" + canonical_query).encode(), hashlib.sha256).digest()
        ).decode()
        params["signature"] = signature
        try:
            request = self.client.http_client.build_request(
                "GET",
                self.client.base_url + "/iaas/",
                params=params,
                headers={"Accept": "application/json"},
            )
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _retry(FailureKind.TRANSPORT, "QingCloud request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), self.client.clock())
            if response.status_code == 429:
                return _retry(FailureKind.RATE_LIMIT, "QingCloud rate limited the request", delay)
            if response.status_code == 408 or response.status_code >= 500:
                return _retry(FailureKind.AVAILABILITY, "QingCloud is temporarily unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "QingCloud response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.TRANSPORT, "QingCloud response was interrupted")
            if response.status_code != 200:
                return _branch(
                    "providerRejected",
                    FailureKind.PROVIDER_REJECTION,
                    f"QingCloud rejected HTTP {response.status_code}",
                )
            try:
                data: Any = json.loads(
                    body, parse_constant=lambda _: (_ for _ in ()).throw(ValueError("invalid number"))
                )
            except (UnicodeDecodeError, ValueError):
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "QingCloud returned invalid zones JSON"
                )
            if not isinstance(data, dict) or type(data.get("ret_code")) is not int:
                return _branch("invalidResponse", FailureKind.PROTOCOL, "QingCloud omitted ret_code")
            code = data["ret_code"]
            if code in {5000, 5100, 5200, 5300}:
                return _retry(FailureKind.AVAILABILITY, "QingCloud reported a temporary server error", delay)
            if code != 0:
                return _branch(
                    "providerRejected",
                    FailureKind.PROVIDER_REJECTION,
                    f"QingCloud rejected the query with code {code}",
                )
            items = data.get("zone_set")
            total = data.get("total_count")
            if (
                data.get("action") != "DescribeZonesResponse"
                or type(total) is not int
                or total < 0
                or not isinstance(items, list)
                or len(items) > 100
                or total < len(items)
            ):
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "QingCloud returned inconsistent zones metadata"
                )
            zones: list[Zone] = []
            for item in items:
                if (
                    not isinstance(item, dict)
                    or not isinstance(item.get("zone_id"), str)
                    or not item["zone_id"]
                    or item.get("status") not in {"active", "faulty", "defunct"}
                ):
                    return _branch(
                        "invalidResponse", FailureKind.PROTOCOL, "QingCloud returned an invalid zone"
                    )
                zones.append(Zone(item["zone_id"], item["status"]))
            return QueryAttempt.branch_result("listed", ZonesPage(tuple(zones), total), Receipt())
        finally:
            await response.aclose()
