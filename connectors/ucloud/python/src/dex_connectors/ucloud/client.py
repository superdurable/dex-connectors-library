# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""UCloud GetRegion signing, transport, and Go-compatible Query decisions."""

import hashlib
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
from ._generated.definitions import GET_REGION_DEFINITION
from .models import GetRegionInput, GetRegionOutput, RegionInfo


def _credential(value: str) -> bool:
    return isinstance(value, str) and 0 < len(value) <= 512 and all(0x20 < ord(ch) < 0x7F for ch in value)


class Client:
    """Resolve rotated UCloud keys per request and own the HTTP transport."""

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
            or config.max_response_bytes <= 0
        ):
            raise ValueError("UCloud configuration and credentials are required")
        if not config.base_url.startswith("https://") or not config.base_url.removeprefix("https://"):
            raise ValueError("UCloud base URL must be HTTPS")
        self.config = config
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_http_client = http_client is None

    def get_region(self) -> "GetRegionOperation":
        """Bind the getRegion Query."""
        return GetRegionOperation(self)

    async def aclose(self) -> None:
        """Close the owned HTTP transport."""
        if self._owns_http_client:
            await self.http_client.aclose()


class GetRegionOperation:
    """Read account-visible regions and availability zones."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query definition."""
        return GET_REGION_DEFINITION

    def _branch(self, branch: str, kind: FailureKind) -> QueryAttempt[GetRegionOutput]:
        return QueryAttempt.branch_result(
            branch,
            GetRegionOutput(),
            Receipt(),
            Failure(kind, "ucloud", "getRegion", "UCloud region query failed"),
        )

    def _retry(self, kind: FailureKind, delay: float = 0) -> QueryAttempt[GetRegionOutput]:
        return QueryAttempt.retry(Failure(kind, "ucloud", "getRegion", "UCloud request failed"), delay)

    async def invoke(self, call: Call, input: GetRegionInput) -> QueryAttempt[GetRegionOutput]:
        """Sign one bounded GetRegion request without exposing its private key."""
        if not isinstance(input, GetRegionInput):
            return self._branch("defect", FailureKind.VALIDATION)
        try:
            credentials = await self.client.credentials.resolve(call)
        except (ValueError, LookupError, OSError, AttributeError):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        public_key, private_key = credentials.public_key.reveal(), credentials.private_key.reveal()
        if not _credential(public_key) or not _credential(private_key):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        signature = hashlib.sha1(("ActionGetRegionPublicKey" + public_key + private_key).encode()).hexdigest()
        request = self.client.http_client.build_request(
            "POST",
            self.client.config.base_url,
            content=json.dumps(
                {"Action": "GetRegion", "PublicKey": public_key, "Signature": signature},
                separators=(",", ":"),
            ).encode(),
            headers={"Accept": "application/json", "Content-Type": "application/json"},
        )
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
        except (UnicodeDecodeError, ValueError):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        if (
            not isinstance(wire, dict)
            or wire.get("Action") != "GetRegionResponse"
            or not isinstance(wire.get("RetCode"), int)
            or isinstance(wire["RetCode"], bool)
        ):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        code = wire["RetCode"]
        if code in (110, 130, 150):
            return self._retry(FailureKind.AVAILABILITY, delay)
        if code == 153:
            return self._retry(FailureKind.RATE_LIMIT, delay)
        if code in (170, 171, 172, 174):
            return self._branch("providerRejected", FailureKind.AUTHENTICATION)
        if code in (293, 294, 295, 299):
            return self._branch("providerRejected", FailureKind.AUTHORIZATION)
        if code != 0:
            return self._branch("providerRejected", FailureKind.PROVIDER_REJECTION)
        entries = wire.get("Regions", [])
        if not isinstance(entries, list):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        regions: list[RegionInfo] = []
        for entry in entries:
            if not isinstance(entry, dict):
                return self._branch("invalidResponse", FailureKind.PROTOCOL)
            region_id, name, default, bits, region, zone = (
                entry.get("RegionId"),
                entry.get("RegionName"),
                entry.get("IsDefault"),
                entry.get("BitMaps"),
                entry.get("Region"),
                entry.get("Zone"),
            )
            if (
                not isinstance(region_id, int)
                or isinstance(region_id, bool)
                or not isinstance(name, str)
                or not name
                or not isinstance(default, bool)
                or not isinstance(bits, str)
                or not isinstance(region, str)
                or not region
                or not isinstance(zone, str)
                or not zone
            ):
                return self._branch("invalidResponse", FailureKind.PROTOCOL)
            regions.append(RegionInfo(region_id, name, default, bits, region, zone))
        return QueryAttempt.branch_result("found", GetRegionOutput(tuple(regions)), Receipt())
