# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider transport and Go-compatible Query decisions."""

import hashlib
import json
import re
from datetime import UTC, date, datetime
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
from ._generated.definitions import GET_FLIGHT_STATUS_DEFINITION
from ._generated.models import FlightStatus
from .models import GetFlightStatusInput, GetFlightStatusOutput

_PROVIDER = "variflight"
_OPERATION = "getFlightStatus"


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[GetFlightStatusOutput]:
    return QueryAttempt.branch_result(
        branch, GetFlightStatusOutput(), Receipt(), Failure(kind, _PROVIDER, _OPERATION, message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[GetFlightStatusOutput]:
    return QueryAttempt.retry(Failure(kind, _PROVIDER, _OPERATION, message), delay)


class Client:
    """Own the provider HTTP transport and resolve rotating credentials per Query."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("VariFlight configuration and credentials are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("VariFlight configuration is invalid")

        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_flight_status(self) -> "GetFlightStatusOperation":
        """Bind the getFlightStatus Query."""
        return GetFlightStatusOperation(self)

    async def aclose(self) -> None:
        """Close this client's own transport."""
        if self._owns_client:
            await self.http_client.aclose()


class GetFlightStatusOperation:
    """Run the getFlightStatus provider Query."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("VariFlight client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query identity."""
        return GET_FLIGHT_STATUS_DEFINITION

    async def invoke(self, call: Call, input: GetFlightStatusInput) -> QueryAttempt[GetFlightStatusOutput]:
        """Send one request and classify its provider outcome."""
        prepared = await _prepare(self.client, call, input)
        if isinstance(prepared, QueryAttempt):
            return prepared
        try:
            response, _ = await send_traced(self.client.http_client, prepared)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "VariFlight flight request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "VariFlight rate limited flight request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "VariFlight API is temporarily unavailable", delay)
            if status != 200 and not False:
                return _http_rejection(status)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "VariFlight flight response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "VariFlight flight response was interrupted")
            if status != 200:
                return _http_rejection(status)
        finally:
            await response.aclose()
        return _decode(body, input, delay)


def _valid_credential(value: str) -> bool:
    return (
        bool(value)
        and len(value) <= 1024
        and all(32 <= ord(c) < 127 for c in value)
        and value.strip() == value
    )


async def _prepare(
    client: Client, call: Call, input: GetFlightStatusInput
) -> httpx.Request | QueryAttempt[GetFlightStatusOutput]:
    if not re.fullmatch(r"[A-Z0-9]{3,8}", input.flight_number):
        return _branch("defect", FailureKind.VALIDATION, "VariFlight flight number is invalid")
    try:
        parsed_date = date.fromisoformat(input.departure_date)
        if parsed_date.isoformat() != input.departure_date:
            raise ValueError("date format")
    except ValueError:
        return _branch("defect", FailureKind.VALIDATION, "VariFlight departure date must be YYYY-MM-DD")
    try:
        credentials = await client.credentials.resolve(call)
        app_id = credentials.app_id.reveal()
        security = credentials.app_security.reveal()
        if not _valid_credential(app_id) or not _valid_credential(security):
            raise ValueError("invalid credentials")
    except (ValueError, LookupError, OSError):
        return _branch(
            "defect", FailureKind.AUTHENTICATION, "VariFlight partner credentials are unavailable or invalid"
        )
    params = {"appid": app_id, "date": input.departure_date, "fnum": input.flight_number, "lang": "EN"}
    signature_source = (
        "&".join(f"{key}={params[key]}" for key in ("appid", "date", "fnum", "lang")) + security
    )
    first = hashlib.md5(signature_source.encode()).hexdigest()
    params["token"] = hashlib.md5(first.encode()).hexdigest()
    try:
        return client.http_client.build_request(
            "GET",
            "https://open-al.variflight.com/api/flight?" + urlencode(params),
            headers={"Accept": "application/json"},
        )
    except (httpx.HTTPError, ValueError):
        return _branch("defect", FailureKind.VALIDATION, "VariFlight flight request is invalid")


def _http_rejection(status: int) -> QueryAttempt[GetFlightStatusOutput]:
    return _branch(
        "providerRejected",
        FailureKind.PROVIDER_REJECTION,
        f"VariFlight rejected flight request with HTTP {status}",
    )


def _decode(body: bytes, input: GetFlightStatusInput, delay: float) -> QueryAttempt[GetFlightStatusOutput]:
    body = body.strip()
    if not body:
        return _branch("invalidResponse", FailureKind.PROTOCOL, "VariFlight returned empty flight data")
    try:
        document: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "VariFlight returned invalid flight data")
    if body.startswith(b"{"):
        if not isinstance(document, dict) or type(document.get("error_code")) is not int:
            return _branch("invalidResponse", FailureKind.PROTOCOL, "VariFlight returned invalid error data")
        code = document["error_code"]
        if code == 10:
            return QueryAttempt.branch_result("noData", GetFlightStatusOutput(), Receipt())
        if code == 500:
            return _retry(FailureKind.AVAILABILITY, "VariFlight reported temporary error")
        return _branch(
            "providerRejected",
            FailureKind.PROVIDER_REJECTION,
            f"VariFlight rejected flight request with code {code}",
        )
    if not isinstance(document, list):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "VariFlight returned invalid flight data")
    flights: list[FlightStatus] = []
    for record in document:
        if (
            not isinstance(record, dict)
            or not isinstance(record.get("FlightNo"), str)
            or not record["FlightNo"]
            or not isinstance(record.get("FlightState"), str)
            or not record["FlightState"]
            or type(record.get("FlightStateNum")) is not int
            or not isinstance(record.get("FlightDeptimePlanDate", ""), str)
            or len(record.get("FlightDeptimePlanDate", "")) > 64
            or not isinstance(record.get("FlightDeptimeDate", ""), str)
            or len(record.get("FlightDeptimeDate", "")) > 64
            or any(c in record["FlightState"] for c in "\r\n\x00")
        ):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "VariFlight returned malformed flight status"
            )
        if len(flights) < 20:
            flights.append(
                FlightStatus(
                    record["FlightNo"],
                    record["FlightState"],
                    record["FlightStateNum"],
                    record.get("FlightDeptimePlanDate", ""),
                    record.get("FlightDeptimeDate", ""),
                )
            )
    return QueryAttempt.branch_result(
        "found", GetFlightStatusOutput(len(document), tuple(flights)), Receipt()
    )
