# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Datadog provider reads and Go-compatible branch classification."""

import json
from collections.abc import Callable
from dataclasses import dataclass as dataclass
from datetime import UTC, datetime
from datetime import timedelta as timedelta
from typing import Any, TypeVar
from urllib.parse import urlencode

import httpx
from dex_connectors.sdk import BranchDefinition as BranchDefinition
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    QueryAttempt,
    QueryDefinition,
    ReauthorizationRequiredError,
    Receipt,
)
from dex_connectors.sdk import OperationRef as OperationRef
from dex_connectors.sdk import SecretString as SecretString
from dex_connectors.sdk import StepDefaults as StepDefaults
from dex_connectors.sdk.providerhttp import (
    BodyTooLargeError,
    ProviderTransportError,
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
    validate_base_url,
)
from dex_connectors.sdk.retry_policy import (
    default_connector_step_execute_retry_policy as default_connector_step_execute_retry_policy,
)

from ._generated.config import (
    Config as Config,
)
from ._generated.config import (
    Credentials as Credentials,
)
from ._generated.definitions import (
    CONNECTOR_ID as CONNECTOR_ID,
)
from ._generated.definitions import (
    DEFECT as DEFECT,
)
from ._generated.definitions import (
    INVALID_RESPONSE as INVALID_RESPONSE,
)
from ._generated.definitions import (
    LIST_MONITORS_DEFINITION as LIST_MONITORS_DEFINITION,
)
from ._generated.definitions import (
    LIST_MONITORS_OPERATION_ID as LIST_MONITORS_OPERATION_ID,
)
from ._generated.definitions import (
    LISTED as LISTED,
)
from ._generated.definitions import (
    PROVIDER_REJECTED as PROVIDER_REJECTED,
)
from ._generated.definitions import (
    QUERIED as QUERIED,
)
from ._generated.definitions import (
    QUERY_METRICS_DEFINITION as QUERY_METRICS_DEFINITION,
)
from ._generated.definitions import (
    QUERY_METRICS_OPERATION_ID as QUERY_METRICS_OPERATION_ID,
)
from .models import ListMonitorsInput, MetricSeries, MetricsQuery, Monitor, MonitorsPage, QueryMetricsInput

PROVIDER_NAME = "Datadog"
_DEFAULT_BASE_URL = "https://api.datadoghq.com"
_DEFAULT_TIMEOUT_SECONDS = 15
_DEFAULT_MAX_RESPONSE_BYTES = 2_097_152
_STATUS_KINDS = {
    401: FailureKind.AUTHENTICATION,
    403: FailureKind.AUTHORIZATION,
    404: FailureKind.NOT_FOUND,
    400: FailureKind.VALIDATION,
    422: FailureKind.VALIDATION,
}

Output = TypeVar("Output")


class Client:
    """One redirect-safe HTTP client and call-time credential source."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None:
            raise ValueError("Datadog configuration is required")
        if credentials is None:
            raise ValueError("Datadog credential source is required")
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("Datadog timeout and maxResponseBytes must be positive")
        self.base_url = validate_base_url(config.base_url or _DEFAULT_BASE_URL)
        self.max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(
            config.timeout_seconds or _DEFAULT_TIMEOUT_SECONDS
        )

    def query_metrics(self) -> "QueryMetricsOperation":
        """Bind the metric query Query to this client."""
        return QueryMetricsOperation(self)

    def list_monitors(self) -> "ListMonitorsOperation":
        """Bind the monitor page Query to this client."""
        return ListMonitorsOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider HTTP client after application shutdown."""
        await self.http_client.aclose()

    async def execute_query(
        self,
        call: Call,
        operation: str,
        path: str,
        empty: Output,
        success: str,
        decode: Callable[[Any], Output],
    ) -> QueryAttempt[Output]:
        """Send one GET and classify it exactly like the Go executeQuery helper."""
        try:
            credentials = await self.credentials.resolve(call)
        except (ValueError, LookupError, OSError, ReauthorizationRequiredError):
            return _failed(
                operation, DEFECT, empty, FailureKind.AUTHENTICATION, "connection credentials are unavailable"
            )
        api_key = credentials.api_key.reveal()
        application_key = credentials.application_key.reveal()
        if (
            not api_key
            or not application_key
            or not is_header_safe_credential(api_key)
            or not is_header_safe_credential(application_key)
        ):
            return _failed(
                operation, DEFECT, empty, FailureKind.AUTHENTICATION, "connection credentials are invalid"
            )
        try:
            request = self.http_client.build_request(
                "GET",
                self.base_url + path,
                headers={
                    "DD-API-KEY": api_key,
                    "DD-APPLICATION-KEY": application_key,
                    "Accept": "application/json",
                },
            )
        except (httpx.HTTPError, ValueError):
            return _failed(operation, DEFECT, empty, FailureKind.LOCAL_DEFECT, "request could not be built")
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError:
            return _retry(operation, FailureKind.AVAILABILITY, f"{PROVIDER_NAME} request failed", 0)
        try:
            status = response.status_code
            if status in (408, 409, 429) or status >= 500:
                kind = FailureKind.RATE_LIMIT if status == 429 else FailureKind.AVAILABILITY
                retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
                return _retry(operation, kind, f"{PROVIDER_NAME} returned HTTP {status}", retry_after)
            if status != 200:
                kind = _STATUS_KINDS.get(status, FailureKind.PROVIDER_REJECTION)
                return _failed(
                    operation, PROVIDER_REJECTED, empty, kind, f"{PROVIDER_NAME} returned HTTP {status}"
                )
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _failed(
                    operation,
                    INVALID_RESPONSE,
                    empty,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(operation, FailureKind.AVAILABILITY, "response was interrupted", 0)
            try:
                value = decode(json.loads(body))
            except (ValueError, TypeError, UnicodeDecodeError):
                return _failed(
                    operation, INVALID_RESPONSE, empty, FailureKind.PROTOCOL, "response was malformed"
                )
            return QueryAttempt.branch_result(success, value, Receipt())
        finally:
            await response.aclose()


class QueryMetricsOperation:
    """Query metric timeseries over an explicit Unix-second interval."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Datadog client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return QUERY_METRICS_DEFINITION

    async def invoke(self, call: Call, input: QueryMetricsInput) -> QueryAttempt[MetricsQuery]:
        """Validate the interval and query, read once, then select a branch or safe retry."""
        operation = QUERY_METRICS_OPERATION_ID
        if (
            not isinstance(input, QueryMetricsInput)
            or type(input.from_seconds) is not int
            or type(input.to_seconds) is not int
            or not isinstance(input.query, str)
            or input.from_seconds <= 0
            or input.to_seconds <= input.from_seconds
            or not input.query.strip()
        ):
            return _failed(
                operation,
                DEFECT,
                MetricsQuery(),
                FailureKind.VALIDATION,
                "from, to, and query are required; to must follow from",
            )
        query = urlencode(
            sorted(
                {"from": str(input.from_seconds), "to": str(input.to_seconds), "query": input.query}.items()
            )
        )
        return await self.client.execute_query(
            call, operation, "/api/v1/query?" + query, MetricsQuery(), QUERIED, _decode_metrics
        )


class ListMonitorsOperation:
    """Read one page of Datadog monitors."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Datadog client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return LIST_MONITORS_DEFINITION

    async def invoke(self, call: Call, input: ListMonitorsInput) -> QueryAttempt[MonitorsPage]:
        """Validate the page, read once, then select a branch or safe retry."""
        operation = LIST_MONITORS_OPERATION_ID
        if (
            not isinstance(input, ListMonitorsInput)
            or type(input.page) is not int
            or type(input.page_size) is not int
            or input.page < 0
            or not 0 <= input.page_size <= 1000
        ):
            return _failed(
                operation,
                DEFECT,
                MonitorsPage(),
                FailureKind.VALIDATION,
                "page must be nonnegative and pageSize at most 1000",
            )
        query = urlencode({"page": str(input.page), "page_size": str(input.page_size or 100)})
        return await self.client.execute_query(
            call, operation, "/api/v1/monitor?" + query, MonitorsPage(), LISTED, _decode_monitors
        )


def _failed(
    operation: str, branch: str, value: Output, kind: FailureKind, message: str
) -> QueryAttempt[Output]:
    return QueryAttempt.branch_result(
        branch, value, Receipt(), Failure(kind, CONNECTOR_ID, operation, message)
    )


def _retry(operation: str, kind: FailureKind, message: str, after_seconds: float) -> QueryAttempt[Any]:
    return QueryAttempt.retry(Failure(kind, CONNECTOR_ID, operation, message), after_seconds)


def _decode_metrics(document: Any) -> MetricsQuery:
    document = _object(document)
    series = document.get("series")
    if _string(document, "status") != "ok" or not isinstance(series, list):
        raise ValueError
    return MetricsQuery("ok", tuple(_decode_series(item) for item in series))


def _decode_series(document: Any) -> MetricSeries:
    document = _object(document)
    points = document.get("pointlist")
    if points is None:
        points = []
    if not isinstance(points, list):
        raise ValueError
    return MetricSeries(_string(document, "metric"), tuple(_decode_point(point) for point in points))


def _decode_point(point: Any) -> tuple[float | None, ...]:
    if point is None:
        return ()
    if not isinstance(point, list) or any(
        value is not None and type(value) not in (int, float) for value in point
    ):
        raise ValueError
    return tuple(None if value is None else float(value) for value in point)


def _decode_monitors(document: Any) -> MonitorsPage:
    if not isinstance(document, list):
        raise ValueError
    monitors = []
    for item in map(_object, document):
        monitor_id = item.get("id")
        if monitor_id is None:
            monitor_id = 0
        if type(monitor_id) is not int:
            raise ValueError
        monitors.append(
            Monitor(monitor_id, _string(item, "name"), _string(item, "type"), _string(item, "overall_state"))
        )
    return MonitorsPage(tuple(monitors))


def _object(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ValueError
    return value


def _string(document: dict[str, Any], name: str) -> str:
    value = document.get(name)
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError
    return value


Config.__doc__ = (
    "Datadog HTTP settings; base_url selects the Datadog site; zero or blank values use defaults."
)
Credentials.__doc__ = "Datadog API and application keys, resolved anew for every provider attempt."
