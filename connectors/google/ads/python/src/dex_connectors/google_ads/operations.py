# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Google Ads decisions matching Go-recorded provider scenarios."""

from __future__ import annotations

import json
import re
from typing import Any

from dex_connectors.sdk import Call, Failure, FailureKind, QueryAttempt, QueryDefinition, Receipt

from ._generated import definitions
from .client import Client, Exchange
from .models import AccessibleCustomers, ListAccessibleCustomersInput


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "google-ads", "listAccessibleCustomers", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[AccessibleCustomers]:
    return QueryAttempt.branch_result(branch, AccessibleCustomers(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[AccessibleCustomers]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _document(body: bytes) -> dict[str, Any] | None:
    try:
        value: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return None
    return value if isinstance(value, dict) else None


def _read_failure(exchange: Exchange) -> QueryAttempt[AccessibleCustomers] | None:
    if exchange.error == "credential":
        return _branch(
            "defect", FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
        )
    if exchange.error in ("request", "refresh"):
        return _retry(FailureKind.TRANSPORT, "Google Ads request failed")
    if exchange.error == "too_large":
        return _branch(
            "invalidResponse",
            FailureKind.RESPONSE_TOO_LARGE,
            "Google Ads response exceeded the configured limit",
        )
    if exchange.error:
        return _retry(FailureKind.TRANSPORT, "Google Ads response was interrupted")
    if exchange.status == 429:
        return _retry(FailureKind.RATE_LIMIT, "Google Ads rate limited the request", exchange.retry_after)
    if exchange.status == 408 or exchange.status >= 500:
        return _retry(FailureKind.AVAILABILITY, "Google Ads is temporarily unavailable", exchange.retry_after)
    if exchange.status != 200:
        return _branch(
            "providerRejected", FailureKind.PROVIDER_REJECTION, f"Google Ads rejected HTTP {exchange.status}"
        )
    return None


class ListAccessibleCustomersOperation:
    """Read directly accessible customer names with a local result bound."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated customer Query contract."""
        return definitions.LIST_ACCESSIBLE_CUSTOMERS_DEFINITION

    async def invoke(
        self, call: Call, input: ListAccessibleCustomersInput
    ) -> QueryAttempt[AccessibleCustomers]:
        """Fetch and validate bounded customer resource names."""
        if input.max_customers < 0 or input.max_customers > 10000:
            return _branch("defect", FailureKind.VALIDATION, "maximum customer count is invalid")
        max_customers = input.max_customers or 1000
        exchange = await self.client.get(call, "/v26/customers:listAccessibleCustomers")
        failure = _read_failure(exchange)
        if failure is not None:
            return failure
        document = _document(exchange.body)
        if document is None:
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Google Ads returned invalid customer JSON"
            )
        names = document.get("resourceNames")
        if names is None:
            names = []
        if not isinstance(names, list) or len(names) > max_customers:
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Google Ads returned invalid customer JSON"
            )
        seen: set[str] = set()
        for name in names:
            if (
                not isinstance(name, str)
                or re.fullmatch(r"customers/[0-9]{1,20}", name) is None
                or name in seen
            ):
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "Google Ads returned invalid customer JSON"
                )
            seen.add(name)
        return QueryAttempt.branch_result("listed", AccessibleCustomers(tuple(names)), Receipt())
