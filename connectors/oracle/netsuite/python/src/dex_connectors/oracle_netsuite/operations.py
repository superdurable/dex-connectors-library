# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Oracle NetSuite customer Query decisions matching Go-recorded provider scenarios."""

from __future__ import annotations

import json
import re
from typing import Any
from urllib.parse import quote

from dex_connectors.sdk import Call, Failure, FailureKind, QueryAttempt, QueryDefinition, Receipt

from ._generated import definitions
from .client import Client, Exchange
from .models import CustomerSummary, GetCustomerInput

_CUSTOMER_ID_PATTERN = re.compile(r"[1-9][0-9]*\Z")


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "oracle-netsuite", "getCustomer", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[CustomerSummary]:
    return QueryAttempt.branch_result(branch, CustomerSummary(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[CustomerSummary]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _document(body: bytes) -> dict[str, Any] | None:
    try:
        value: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return None
    return value if isinstance(value, dict) else None


def _read_failure(exchange: Exchange) -> QueryAttempt[CustomerSummary] | None:
    if exchange.error == "credential":
        return _branch("defect", FailureKind.AUTHENTICATION, "Oracle NetSuite connection needs authorization")
    if exchange.error in ("request", "refresh"):
        return _retry(FailureKind.TRANSPORT, "Oracle NetSuite customer request failed")
    if exchange.status == 429:
        return _retry(
            FailureKind.RATE_LIMIT, "Oracle NetSuite rate limited the request", exchange.retry_after
        )
    if exchange.status == 408 or exchange.status >= 500:
        return _retry(
            FailureKind.AVAILABILITY, "Oracle NetSuite is temporarily unavailable", exchange.retry_after
        )
    if exchange.status == 404:
        return _branch("notFound", FailureKind.NOT_FOUND, "Oracle NetSuite customer was not found")
    if exchange.error == "too_large":
        return _branch(
            "invalidResponse", FailureKind.RESPONSE_TOO_LARGE,
            "Oracle NetSuite customer response exceeded the configured limit",
        )
    if exchange.status != 200:
        return _branch(
            "providerRejected", FailureKind.PROVIDER_REJECTION,
            f"Oracle NetSuite rejected HTTP {exchange.status}",
        )
    if exchange.error:
        return _retry(FailureKind.TRANSPORT, "Oracle NetSuite customer response was interrupted")
    return None


class GetCustomerOperation:
    """Read one customer by Oracle NetSuite internal ID."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated customer Query contract."""
        return definitions.GET_CUSTOMER_DEFINITION

    async def invoke(self, call: Call, input: GetCustomerInput) -> QueryAttempt[CustomerSummary]:
        """Project only the stable internal and entity IDs into the Flow."""
        if not _CUSTOMER_ID_PATTERN.fullmatch(input.customer_id):
            return _branch("defect", FailureKind.VALIDATION, "customer internal ID is invalid")
        path = "/services/rest/record/v1/customer/" + quote(input.customer_id, safe="")
        exchange = await self.client.get(call, path)
        failure = _read_failure(exchange)
        if failure is not None:
            return failure
        document = _document(exchange.body)
        if document is None:
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Oracle NetSuite returned invalid customer JSON"
            )
        customer_id, entity_id = document.get("id"), document.get("entityId")
        if customer_id != input.customer_id or not isinstance(entity_id, str) or not entity_id:
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Oracle NetSuite returned invalid customer JSON"
            )
        return QueryAttempt.branch_result("found", CustomerSummary(customer_id, entity_id), Receipt())
