# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""17TRACK registered shipment Query matching the Go provider contract."""

import json
from datetime import UTC, datetime
from urllib.parse import urlparse

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
from ._generated.definitions import LIST_TRACKED_SHIPMENTS_DEFINITION
from .models import ListTrackedShipmentsInput, TrackedShipment, TrackedShipmentPage


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[TrackedShipmentPage]:
    return QueryAttempt.branch_result(
        branch,
        TrackedShipmentPage(),
        Receipt(),
        Failure(kind, "seventeentrack", "listTrackedShipments", message),
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[TrackedShipmentPage]:
    return QueryAttempt.retry(Failure(kind, "seventeentrack", "listTrackedShipments", message), delay)


def _valid_key(value: str) -> bool:
    return (
        bool(value)
        and len(value.encode()) <= 2048
        and value.strip() == value
        and all(0x20 < ord(c) < 0x7F for c in value)
    )


class Client:
    """Own bounded HTTP settings and resolve the current 17TRACK key per attempt."""

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
            raise ValueError("17TRACK configuration and credentials are required")
        parsed = urlparse(config.base_url)
        if (
            parsed.scheme != "https"
            or not parsed.hostname
            or parsed.username
            or parsed.password
            or parsed.query
            or parsed.fragment
        ):
            raise ValueError("17TRACK base URL is invalid")
        self.base_url = config.base_url.rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def list_tracked_shipments(self) -> "ListTrackedShipmentsOperation":
        """Bind a read-only registered-shipment page Query."""
        return ListTrackedShipmentsOperation(self)

    async def aclose(self) -> None:
        """Close the transport only if this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class ListTrackedShipmentsOperation:
    """Read one bounded page of registered tracking records."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("17TRACK client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated tracking-list Query identity."""
        return LIST_TRACKED_SHIPMENTS_DEFINITION

    async def invoke(self, call: Call, input: ListTrackedShipmentsInput) -> QueryAttempt[TrackedShipmentPage]:
        """Classify one provider page and safe Retry outcomes."""
        if input.page_number < 0 or input.page_number > 100000:
            return _branch(
                "defect", FailureKind.VALIDATION, "17TRACK page number must be between 1 and 100000"
            )
        page_number = input.page_number or 1
        try:
            credentials = await self.client.credentials.resolve(call)
            api_key = credentials.api_key.reveal()
            if not _valid_key(api_key):
                raise ValueError("invalid API key")
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch("defect", FailureKind.AUTHENTICATION, "17TRACK API key is unavailable or invalid")
        try:
            request = self.client.http_client.build_request(
                "POST",
                self.client.base_url + "/track/v2.4/gettracklist",
                content=json.dumps({"page_no": page_number}, separators=(",", ":")),
                headers={
                    "Accept": "application/json",
                    "Content-Type": "application/json",
                    "17token": api_key,
                },
            )
        except (httpx.HTTPError, ValueError, TypeError):
            return _branch("defect", FailureKind.VALIDATION, "tracking-list request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "17TRACK tracking-list request failed")
        try:
            status = response.status_code
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "17TRACK rate limited tracking-list query", delay)
            if status == 408 or status >= 500:
                return _retry(
                    FailureKind.AVAILABILITY, "17TRACK tracking API is temporarily unavailable", delay
                )
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "17TRACK response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "17TRACK response was interrupted")
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.VALIDATION
                    if status in (400, 404, 412)
                    else FailureKind.PROVIDER_REJECTION
                )
                return _branch(
                    "providerRejected", kind, f"17TRACK rejected tracking-list query with HTTP {status}"
                )
        finally:
            await response.aclose()
        try:
            wire = json.loads(body, parse_constant=lambda _: (_ for _ in ()).throw(ValueError()))
            if not isinstance(wire, dict) or type(wire.get("code")) is not int:
                raise ValueError("invalid 17TRACK JSON")
            if wire["code"] != 0:
                return _branch(
                    "providerRejected", FailureKind.PROVIDER_REJECTION, "17TRACK rejected tracking-list query"
                )
            data, page = wire.get("data"), wire.get("page")
            if not isinstance(data, dict):
                raise ValueError("missing data")
            if data.get("errors"):
                return _branch(
                    "providerRejected", FailureKind.PROVIDER_REJECTION, "17TRACK rejected tracking-list query"
                )
            accepted = data.get("accepted")
            if (
                not isinstance(page, dict)
                or not isinstance(accepted, list)
                or len(accepted) > 40
                or type(page.get("page_no")) is not int
                or page["page_no"] < 0
                or type(page.get("page_size")) is not int
                or page["page_size"] > 40
                or type(page.get("data_total")) is not int
                or page["data_total"] < 0
            ):
                raise ValueError("invalid page")
            shipments: list[TrackedShipment] = []
            for item in accepted:
                if (
                    not isinstance(item, dict)
                    or not isinstance(item.get("number"), str)
                    or not item["number"]
                    or type(item.get("carrier")) is not int
                    or item["carrier"] <= 0
                ):
                    raise ValueError("invalid shipment")
                shipments.append(
                    TrackedShipment(
                        number=item["number"],
                        carrier=item["carrier"],
                        package_status=item.get("package_status") or "",
                        tracking_status=item.get("tracking_status") or "",
                    )
                )
            output = TrackedShipmentPage(
                page_number=page["page_no"], total=page["data_total"], shipments=tuple(shipments)
            )
        except (ValueError, TypeError, KeyError, UnicodeDecodeError):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "17TRACK returned an invalid tracking-list response"
            )
        return QueryAttempt.branch_result("listed", output, Receipt())
