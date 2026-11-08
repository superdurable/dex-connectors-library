# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""GA4 operation decisions matching Go provider recordings."""

from __future__ import annotations

import json
import re
from datetime import datetime
from typing import TYPE_CHECKING, Any, TypeVar
from urllib.parse import urlencode

from dex_connectors.sdk import Call, Failure, FailureKind, QueryAttempt, QueryDefinition, Receipt

from ._generated import definitions
from .client import Exchange
from .models import (
    Audience,
    AudiencesPage,
    GetPropertyInput,
    ListAudiencesInput,
    Property,
    ReportPage,
    ReportRow,
    RunReportInput,
)

if TYPE_CHECKING:
    from .client import Client

Output = TypeVar("Output")
_PROPERTY_ID = re.compile(r"^[0-9]{1,30}$")
_ANALYTICS_NAME = re.compile(r"^[A-Za-z][A-Za-z0-9_:]{0,127}$")


def _failure(operation: str, kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "google-analytics-4", operation, message)


def _branch(
    operation: str, branch: str, kind: FailureKind, message: str, empty: Output
) -> QueryAttempt[Output]:
    return QueryAttempt.branch_result(branch, empty, Receipt(), _failure(operation, kind, message))


def _retry(operation: str, kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[Any]:
    return QueryAttempt.retry(_failure(operation, kind, message), delay)


def _read_failure(
    operation: str, exchange: Exchange, empty: Output, *, not_found: bool = False
) -> QueryAttempt[Output] | None:
    if exchange.error == "credential":
        return _branch(
            operation,
            "defect",
            FailureKind.AUTHENTICATION,
            "connection credentials are unavailable or invalid",
            empty,
        )
    if exchange.error in ("request", "refresh"):
        return _retry(operation, FailureKind.TRANSPORT, "Google Analytics request failed")
    if exchange.status == 429:
        return _retry(
            operation, FailureKind.RATE_LIMIT, "Google rate limited the request", exchange.retry_after
        )
    if exchange.status == 408 or exchange.status >= 500:
        return _retry(
            operation,
            FailureKind.AVAILABILITY,
            "Google Analytics is temporarily unavailable",
            exchange.retry_after,
        )
    if exchange.status == 404 and not_found:
        return _branch(
            operation, "notFound", FailureKind.NOT_FOUND, "Google did not find the property", empty
        )
    if exchange.status != 200:
        return _branch(
            operation,
            "providerRejected",
            FailureKind.PROVIDER_REJECTION,
            f"Google rejected HTTP {exchange.status}",
            empty,
        )
    if exchange.error == "too_large":
        return _branch(
            operation,
            "invalidResponse",
            FailureKind.RESPONSE_TOO_LARGE,
            "Google response exceeded the configured limit",
            empty,
        )
    if exchange.error:
        return _retry(operation, FailureKind.TRANSPORT, "Google response was interrupted")
    return None


def _document(body: bytes) -> dict[str, Any] | None:
    try:
        value: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return None
    return value if isinstance(value, dict) else None


def _valid_property_id(value: str) -> bool:
    return bool(_PROPERTY_ID.fullmatch(value))


def _valid_date_range(start: str, end: str) -> bool:
    try:
        first = datetime.strptime(start, "%Y-%m-%d")
        last = datetime.strptime(end, "%Y-%m-%d")
    except ValueError:
        return False
    return first.strftime("%Y-%m-%d") == start and last.strftime("%Y-%m-%d") == end and last >= first


def _valid_names(names: tuple[str, ...], *, required: bool) -> bool:
    return (
        len(names) <= 10
        and (not required or bool(names))
        and all(_ANALYTICS_NAME.fullmatch(name) for name in names)
    )


class RunReportOperation:
    """Run one bounded read-only report using the GA4 Data API."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated report Query contract."""
        return definitions.RUN_REPORT_DEFINITION

    async def invoke(self, call: Call, input: RunReportInput) -> QueryAttempt[ReportPage]:
        """Read requested dimensions and metrics in one offset page."""
        empty = ReportPage()
        if (
            not _valid_property_id(input.property_id)
            or not _valid_date_range(input.start_date, input.end_date)
            or not _valid_names(input.dimensions, required=False)
            or not _valid_names(input.metrics, required=True)
            or input.limit < 0
            or input.limit > 10000
            or input.offset < 0
            or input.offset > 1_000_000_000
        ):
            return _branch("runReport", "defect", FailureKind.VALIDATION, "report input is invalid", empty)
        limit = input.limit or 100
        payload = {
            "dateRanges": [{"startDate": input.start_date, "endDate": input.end_date}],
            "dimensions": [{"name": name} for name in input.dimensions],
            "metrics": [{"name": name} for name in input.metrics],
            "limit": str(limit),
            "offset": str(input.offset),
        }
        exchange = await self.client.request(
            call, "POST", "data", f"/v1beta/properties/{input.property_id}:runReport", payload=payload
        )
        failure = _read_failure("runReport", exchange, empty)
        if failure is not None:
            return failure
        document = _document(exchange.body)
        if (
            document is None
            or not isinstance(document.get("rows", []), list)
            or len(document.get("rows", [])) > limit
        ):
            return _branch(
                "runReport",
                "invalidResponse",
                FailureKind.PROTOCOL,
                "Google returned invalid report JSON",
                empty,
            )
        row_count = document.get("rowCount", 0)
        if (
            isinstance(row_count, bool)
            or not isinstance(row_count, (int, str))
            or not str(row_count).isdigit()
        ):
            return _branch(
                "runReport",
                "invalidResponse",
                FailureKind.PROTOCOL,
                "Google returned invalid report row count",
                empty,
            )
        rows: list[ReportRow] = []
        for row in document.get("rows", []):
            if not isinstance(row, dict):
                return _branch(
                    "runReport",
                    "invalidResponse",
                    FailureKind.PROTOCOL,
                    "Google returned invalid report JSON",
                    empty,
                )
            dimensions = row.get("dimensionValues", [])
            metrics = row.get("metricValues", [])
            if (
                not isinstance(dimensions, list)
                or len(dimensions) != len(input.dimensions)
                or not isinstance(metrics, list)
                or len(metrics) != len(input.metrics)
                or any(
                    not isinstance(value, dict) or not isinstance(value.get("value"), str)
                    for value in dimensions + metrics
                )
            ):
                return _branch(
                    "runReport",
                    "invalidResponse",
                    FailureKind.PROTOCOL,
                    "Google returned mismatched report columns",
                    empty,
                )
            rows.append(
                ReportRow(
                    tuple(value["value"] for value in dimensions), tuple(value["value"] for value in metrics)
                )
            )
        return QueryAttempt.branch_result("reported", ReportPage(tuple(rows), int(row_count)), Receipt())


class GetPropertyOperation:
    """Read one GA4 property's display metadata."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated property Query contract."""
        return definitions.GET_PROPERTY_DEFINITION

    async def invoke(self, call: Call, input: GetPropertyInput) -> QueryAttempt[Property]:
        """Read one property without exposing private API fields."""
        empty = Property()
        if not _valid_property_id(input.property_id):
            return _branch(
                "getProperty", "defect", FailureKind.VALIDATION, "property ID must be numeric", empty
            )
        exchange = await self.client.request(call, "GET", "admin", f"/v1beta/properties/{input.property_id}")
        failure = _read_failure("getProperty", exchange, empty, not_found=True)
        if failure is not None:
            return failure
        document = _document(exchange.body)
        if (
            document is None
            or document.get("name") != f"properties/{input.property_id}"
            or not isinstance(document.get("displayName"), str)
            or not document["displayName"]
            or not isinstance(document.get("timeZone"), str)
            or not document["timeZone"]
            or not isinstance(document.get("currencyCode", ""), str)
        ):
            return _branch(
                "getProperty",
                "invalidResponse",
                FailureKind.PROTOCOL,
                "Google returned invalid property JSON",
                empty,
            )
        property = Property(
            document["name"], document["displayName"], document["timeZone"], document.get("currencyCode", "")
        )
        return QueryAttempt.branch_result("found", property, Receipt())


class ListAudiencesOperation:
    """Read one page of GA4 audience identities and names."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated audience Query contract."""
        return definitions.LIST_AUDIENCES_DEFINITION

    async def invoke(self, call: Call, input: ListAudiencesInput) -> QueryAttempt[AudiencesPage]:
        """List a bounded page without retaining filter definitions."""
        empty = AudiencesPage()
        if (
            not _valid_property_id(input.property_id)
            or input.page_size < 0
            or input.page_size > 200
            or len(input.page_token) > 4096
            or "\r" in input.page_token
            or "\n" in input.page_token
        ):
            return _branch(
                "listAudiences", "defect", FailureKind.VALIDATION, "audience page input is invalid", empty
            )
        page_size = input.page_size or 50
        query = [("pageSize", str(page_size))]
        if input.page_token:
            query.append(("pageToken", input.page_token))
        exchange = await self.client.request(
            call, "GET", "admin", f"/v1alpha/properties/{input.property_id}/audiences?{urlencode(query)}"
        )
        failure = _read_failure("listAudiences", exchange, empty)
        if failure is not None:
            return failure
        document = _document(exchange.body)
        if document is None or not isinstance(document.get("audiences", []), list):
            return _branch(
                "listAudiences",
                "invalidResponse",
                FailureKind.PROTOCOL,
                "Google returned invalid audience JSON",
                empty,
            )
        entries = document.get("audiences", [])
        next_page_token = document.get("nextPageToken", "")
        if len(entries) > page_size or not isinstance(next_page_token, str) or len(next_page_token) > 4096:
            return _branch(
                "listAudiences",
                "invalidResponse",
                FailureKind.PROTOCOL,
                "Google returned invalid audience JSON",
                empty,
            )
        audiences: list[Audience] = []
        for entry in entries:
            if (
                not isinstance(entry, dict)
                or not isinstance(entry.get("name"), str)
                or not entry["name"].startswith(f"properties/{input.property_id}/audiences/")
                or not isinstance(entry.get("displayName"), str)
                or not entry["displayName"]
                or not isinstance(entry.get("description", ""), str)
            ):
                return _branch(
                    "listAudiences",
                    "invalidResponse",
                    FailureKind.PROTOCOL,
                    "Google returned incomplete audience metadata",
                    empty,
                )
            audiences.append(Audience(entry["name"], entry["displayName"], entry.get("description", "")))
        return QueryAttempt.branch_result(
            "listed", AudiencesPage(tuple(audiences), next_page_token), Receipt()
        )
