# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""51Tracking status-count transport and Go-compatible decisions."""

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
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import GET_SHIPMENT_STATUS_COUNTS_DEFINITION
from .models import GetShipmentStatusCountsInput, ShipmentStatusCounts


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ShipmentStatusCounts]:
    return QueryAttempt.branch_result(
        branch,
        ShipmentStatusCounts(),
        Receipt(),
        Failure(kind, "fiftyonetracking", "getShipmentStatusCounts", message),
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ShipmentStatusCounts]:
    return QueryAttempt.retry(Failure(kind, "fiftyonetracking", "getShipmentStatusCounts", message), delay)


def _valid_api_key(value: str) -> bool:
    return bool(value) and len(value) <= 2048 and all(0x20 < ord(character) < 0x7F for character in value)


class Client:
    """Own the redirect-safe HTTP transport and resolve credentials for each Query."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("51Tracking configuration and credential source are required")
        if config.timeout_seconds <= 0 or config.max_response_bytes <= 0:
            raise ValueError("51Tracking timeout and response limit must be positive")
        self.base_url = validate_base_url(config.base_url)
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_shipment_status_counts(self) -> "GetShipmentStatusCountsOperation":
        """Bind the aggregate parcel-status Query."""
        return GetShipmentStatusCountsOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP transport when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class GetShipmentStatusCountsOperation:
    """Read non-personal aggregate shipment counts from the 51Tracking API."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("51Tracking client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query identity and branch contract."""
        return GET_SHIPMENT_STATUS_COUNTS_DEFINITION

    async def invoke(
        self, call: Call, input: GetShipmentStatusCountsInput
    ) -> QueryAttempt[ShipmentStatusCounts]:
        """Make one status-count request and classify its provider outcome."""
        try:
            credentials = await self.client.credentials.resolve(call)
            api_key = credentials.api_key.reveal()
            if not _valid_api_key(api_key):
                raise ValueError("invalid API key")
        except (ValueError, LookupError, OSError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "51Tracking API key is unavailable or invalid"
            )
        try:
            request = self.client.http_client.build_request(
                "GET",
                self.client.base_url + "/v3/trackings/status",
                headers={
                    "Accept": "application/json",
                    "Content-Type": "application/json",
                    "Tracking-Api-Key": api_key,
                },
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "status-count request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "51Tracking status-count request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "51Tracking rate limited status-count query", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "51Tracking API is temporarily unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "51Tracking response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "51Tracking response was interrupted")
        finally:
            await response.aclose()
        if status != 200:
            kind = FailureKind.PROVIDER_REJECTION
            if status == 401:
                kind = FailureKind.AUTHENTICATION
            elif status == 403:
                kind = FailureKind.AUTHORIZATION
            elif status in (400, 404, 412):
                kind = FailureKind.VALIDATION
            return _branch(
                "providerRejected", kind, f"51Tracking rejected status-count query with HTTP {status}"
            )
        return _decode_counts(body)


def _decode_counts(body: bytes) -> QueryAttempt[ShipmentStatusCounts]:
    try:
        document: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return _branch(
            "invalidResponse", FailureKind.PROTOCOL, "51Tracking returned an invalid status-count response"
        )
    if not isinstance(document, dict) or type(document.get("code")) is not int:
        return _branch(
            "invalidResponse", FailureKind.PROTOCOL, "51Tracking returned an invalid status-count response"
        )
    if document["code"] != 200:
        return _branch(
            "providerRejected", FailureKind.PROVIDER_REJECTION, "51Tracking rejected status-count query"
        )
    values = document.get("data")
    if not isinstance(values, dict):
        return _branch(
            "invalidResponse", FailureKind.PROTOCOL, "51Tracking returned an invalid status-count response"
        )
    wire_names = (
        "pending",
        "notfound",
        "transit",
        "pickup",
        "delivered",
        "expired",
        "undelivered",
        "exception",
    )
    if any(type(values.get(name)) is not int or values[name] < 0 for name in wire_names):
        return _branch(
            "invalidResponse", FailureKind.PROTOCOL, "51Tracking returned an invalid status-count response"
        )
    counts = ShipmentStatusCounts(
        pending=values["pending"],
        not_found=values["notfound"],
        in_transit=values["transit"],
        pickup=values["pickup"],
        delivered=values["delivered"],
        expired=values["expired"],
        undelivered=values["undelivered"],
        exception=values["exception"],
    )
    return QueryAttempt.branch_result("counted", counts, Receipt())
