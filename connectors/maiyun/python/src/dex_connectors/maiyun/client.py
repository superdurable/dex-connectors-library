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
        try:
            address = ipaddress.ip_address(input.ip_address)
            if address.version != 4 or not address.is_global:
                raise ValueError("not a public IPv4")
        except ValueError:
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
                content=json.dumps({"ip": input.ip_address}, separators=(",", ":")),
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
            try:
                data: Any = json.loads(
                    body, parse_constant=lambda _: (_ for _ in ()).throw(ValueError("invalid JSON number"))
                )
                result = _decode(data, input)
            except (ValueError, TypeError, KeyError, OverflowError):
                return _branch(INVALID_RESPONSE, FailureKind.PROTOCOL, "MAIYUN returned invalid data")
            if isinstance(result, QueryAttempt):
                return result
            return QueryAttempt.branch_result(FOUND, result, Receipt())
        finally:
            await response.aclose()


def _valid_secret(value: object) -> bool:
    return isinstance(value, str) and 0 < len(value) <= 1024 and all(32 < ord(c) < 127 for c in value)


def _decode(
    data: Any, input: LookupIPLocationInput
) -> LookupIPLocationOutput | QueryAttempt[LookupIPLocationOutput]:
    if not isinstance(data, dict) or type(data.get("result")) is not int:
        raise ValueError("invalid envelope")
    code = data["result"]
    if code == -429:
        return _retry(FailureKind.RATE_LIMIT, "MAIYUN rate limited the lookup")
    if code <= -500:
        return _retry(FailureKind.AVAILABILITY, "MAIYUN service is temporarily unavailable")
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
    for field in ("country", "province", "city", "district"):
        if not isinstance(data.get(field, ""), str):
            raise ValueError("invalid location field")
    if not data["country"].strip() or not data["city"].strip():
        raise ValueError("incomplete location")
    return LookupIPLocationOutput(
        data["country"], data.get("province", ""), data["city"], data.get("district", "")
    )


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[LookupIPLocationOutput]:
    return QueryAttempt.branch_result(
        branch, LookupIPLocationOutput(), Receipt(), Failure(kind, "maiyun", "lookupIPLocation", message)
    )


def _retry(kind: FailureKind, message: str, retry_after: float = 0) -> QueryAttempt[LookupIPLocationOutput]:
    return QueryAttempt.retry(Failure(kind, "maiyun", "lookupIPLocation", message), retry_after)
