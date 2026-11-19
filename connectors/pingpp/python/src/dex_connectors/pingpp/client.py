# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Ping++ read-only Charge status with Go-compatible classifications."""

import base64
import ipaddress
import json
import re
from datetime import UTC, datetime
from typing import Any
from urllib.parse import urlsplit

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
from ._generated.definitions import GET_CHARGE_STATUS_DEFINITION
from .models import GetChargeStatusInput, GetChargeStatusOutput


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[GetChargeStatusOutput]:
    return QueryAttempt.branch_result(
        branch, GetChargeStatusOutput(), Receipt(), Failure(kind, "pingpp", "getChargeStatus", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[GetChargeStatusOutput]:
    return QueryAttempt.retry(Failure(kind, "pingpp", "getChargeStatus", message), delay)


def _validated_base_url(value: str, *, origin_only: bool = False) -> str:
    if not value or any(ord(char) <= 0x20 or ord(char) > 0x7E for char in value):
        raise ValueError("provider base URL must be printable ASCII")
    try:
        parsed = urlsplit(value)
        host = parsed.hostname
    except ValueError as error:
        raise ValueError("provider base URL is invalid") from error
    if not host or parsed.username or parsed.password or parsed.query or parsed.fragment:
        raise ValueError("provider base URL is invalid")
    if origin_only and parsed.path not in ("", "/"):
        raise ValueError("provider base URL must be an origin")
    if parsed.scheme != "https":
        try:
            is_loopback = host == "localhost" or ipaddress.ip_address(host).is_loopback
        except ValueError:
            is_loopback = host == "localhost"
        if parsed.scheme != "http" or not is_loopback:
            raise ValueError("provider base URL must use HTTPS")
    return value.rstrip("/")


class Client:
    """Own Ping++ settings and resolve its secret API Key for each Query."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Ping++ configuration and credentials are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("Ping++ configuration is invalid")
        self.base_url = _validated_base_url(config.base_url, origin_only=False)
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_charge_status(self) -> "GetChargeStatusOperation":
        """Bind the generated Charge status Query."""
        return GetChargeStatusOperation(self)

    async def aclose(self) -> None:
        """Close only an HTTP client this instance created."""
        if self._owns_client:
            await self.http_client.aclose()


class GetChargeStatusOperation:
    """Read payment flags without returning customer or credential fields."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Ping++ client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Charge status contract."""
        return GET_CHARGE_STATUS_DEFINITION

    async def invoke(self, call: Call, input: GetChargeStatusInput) -> QueryAttempt[GetChargeStatusOutput]:
        """Make one read-only Charge lookup and classify its outcome."""
        charge_id = input.charge_id if isinstance(input, GetChargeStatusInput) else ""
        if not re.fullmatch(r"ch_[A-Za-z0-9_-]{1,125}", charge_id):
            return _branch("defect", FailureKind.VALIDATION, "Charge ID is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
            key = credentials.api_key.reveal()
            if not key or len(key) > 512 or any(ord(c) <= 0x20 or ord(c) >= 0x7F for c in key):
                raise ValueError("invalid key")
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch("defect", FailureKind.AUTHENTICATION, "Ping++ API Key is unavailable or invalid")
        basic = base64.b64encode((key + ":").encode()).decode()
        try:
            request = self.client.http_client.build_request(
                "GET",
                self.client.base_url + "/v1/charges/" + charge_id,
                headers={"Accept": "application/json", "Authorization": "Basic " + basic},
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "Charge request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "Ping++ request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status in (403, 429):
                return _retry(FailureKind.RATE_LIMIT, "Ping++ rate or concurrency limit reached", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Ping++ is temporarily unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Ping++ response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Ping++ response was interrupted")
        finally:
            await response.aclose()
        if status == 404:
            return _branch("notFound", FailureKind.PROVIDER_REJECTION, "Ping++ Charge was not found")
        if status != 200:
            kind = (
                FailureKind.AUTHENTICATION
                if status == 401
                else FailureKind.VALIDATION
                if status == 400
                else FailureKind.PROVIDER_REJECTION
            )
            return _branch("providerRejected", kind, "Ping++ rejected the Charge lookup")
        try:
            document: Any = json.loads(body)
        except (ValueError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Ping++ returned invalid Charge status")
        if (
            not isinstance(document, dict)
            or document.get("id") != charge_id
            or document.get("object") != "charge"
            or any(
                type(document.get(name)) is not bool for name in ("paid", "refunded", "reversed", "livemode")
            )
        ):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Ping++ returned invalid Charge status")
        return QueryAttempt.branch_result(
            "fetched",
            GetChargeStatusOutput(
                charge_id, document["paid"], document["refunded"], document["reversed"], document["livemode"]
            ),
            Receipt(),
        )
