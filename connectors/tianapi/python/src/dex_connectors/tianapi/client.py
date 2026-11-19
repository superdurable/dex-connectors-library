# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""tianapi provider transport and Go-compatible Query decisions."""

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
from ._generated.definitions import GET_HOLIDAY_STATUS_DEFINITION
from .models import GetHolidayStatusInput, GetHolidayStatusOutput


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[GetHolidayStatusOutput]:
    return QueryAttempt.branch_result(
        branch, GetHolidayStatusOutput(), Receipt(), Failure(kind, "tianapi", "getHolidayStatus", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[GetHolidayStatusOutput]:
    return QueryAttempt.retry(Failure(kind, "tianapi", "getHolidayStatus", message), delay)


class Client:
    """Resolve credentials per getHolidayStatus request and own a redirect-safe HTTP client."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("tianapi configuration and credentials are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("tianapi timeout or response limit is invalid")
        self.config = config
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_holiday_status(self) -> "GetHolidayStatusOperation":
        """Bind the getHolidayStatus Query to this client."""
        return GetHolidayStatusOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client if this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class GetHolidayStatusOperation:
    """Perform the getHolidayStatus read and return bounded provider data."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("tianapi client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Go-derived Query identity."""
        return GET_HOLIDAY_STATUS_DEFINITION

    async def invoke(self, call: Call, input: GetHolidayStatusInput) -> QueryAttempt[GetHolidayStatusOutput]:
        """Classify transport and provider outcomes without exposing secrets."""
        try:
            credentials = await self.client.credentials.resolve(call)
            request = self._build_request(input, credentials)
        except (ValueError, LookupError, OSError, httpx.HTTPError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "connection credentials or request are invalid"
            )
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "tianapi request failed")
        return await self._process_response(response, input, credentials)

    async def _process_response(
        self, response: httpx.Response, input: GetHolidayStatusInput, credentials: Credentials
    ) -> QueryAttempt[GetHolidayStatusOutput]:
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "tianapi rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "tianapi is temporarily unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "tianapi response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "tianapi response was interrupted")
        finally:
            await response.aclose()
        if status != 200:
            kind = (
                FailureKind.AUTHENTICATION
                if status == 401
                else FailureKind.AUTHORIZATION
                if status == 403
                else FailureKind.PROVIDER_REJECTION
            )
            return _branch("providerRejected", kind, f"tianapi rejected request with HTTP {status}")
        try:
            document: Any = json.loads(body)
        except (ValueError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "tianapi returned invalid JSON")
        return self._decode(input, document)

    def _build_request(self, input: GetHolidayStatusInput, credentials: Credentials) -> httpx.Request:
        from datetime import date

        if date.fromisoformat(input.date).isoformat() != input.date or not credentials.key.reveal():
            raise ValueError("invalid date or key")
        return self.client.http_client.build_request(
            "POST",
            "https://apis.tianapi.com/jiejiari/index",
            data={"key": credentials.key.reveal(), "date": input.date, "type": "0"},
            headers={"Content-Type": "application/x-www-form-urlencoded", "Accept": "application/json"},
        )

    def _decode(self, input: GetHolidayStatusInput, document: Any) -> QueryAttempt[GetHolidayStatusOutput]:
        if not isinstance(document, dict) or type(document.get("code")) is not int:
            return _branch("invalidResponse", FailureKind.PROTOCOL, "invalid holiday data")
        code = document["code"]
        if code == 130:
            return _retry(FailureKind.RATE_LIMIT, "holiday rate limit reached")
        if code in (100, 120):
            return _retry(FailureKind.AVAILABILITY, "holiday service unavailable")
        if code != 200:
            return _branch("providerRejected", FailureKind.PROVIDER_REJECTION, "holiday request rejected")
        result = document.get("result")
        items = result.get("list") if isinstance(result, dict) else None
        if not isinstance(items, list) or len(items) != 1 or not isinstance(items[0], dict):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "invalid holiday list")
        item = items[0]
        if (
            item.get("date") != input.date
            or type(item.get("daycode")) is not int
            or item["daycode"] not in range(4)
            or type(item.get("isnotwork")) is not int
            or item["isnotwork"] not in (0, 1)
        ):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "invalid holiday status")
        return QueryAttempt.branch_result(
            "found",
            GetHolidayStatusOutput(date=input.date, day_code=item["daycode"], is_not_work=item["isnotwork"]),
            Receipt(),
        )
