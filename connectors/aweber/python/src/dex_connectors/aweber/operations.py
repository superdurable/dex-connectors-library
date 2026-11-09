# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""AWeber decisions matching the Go-recorded provider scenarios."""

from __future__ import annotations

import json
from typing import Any
from urllib.parse import urlencode

from dex_connectors.sdk import Call, Failure, FailureKind, QueryAttempt, QueryDefinition, Receipt

from ._generated import definitions
from .client import Client, Exchange
from .models import Account, AccountsPage, ListAccountsInput


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "aweber", "listAccounts", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[AccountsPage]:
    return QueryAttempt.branch_result(branch, AccountsPage(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[AccountsPage]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _document(body: bytes) -> dict[str, Any] | None:
    try:
        value: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return None
    return value if isinstance(value, dict) else None


def _is_rate_limit(body: bytes) -> bool:
    document = _document(body)
    error = document.get("error") if document else None
    return (
        isinstance(error, dict)
        and error.get("type") == "ForbiddenError"
        and isinstance(error.get("message"), str)
        and error["message"].lower() == "rate limit error"
    )


def _read_failure(exchange: Exchange) -> QueryAttempt[AccountsPage] | None:
    if exchange.error == "credential":
        return _branch(
            "defect", FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
        )
    if exchange.error in ("request", "refresh"):
        return _retry(FailureKind.TRANSPORT, "AWeber request failed")
    # Retryable statuses win over their body, so an oversized 429 or 5xx page still retries.
    if exchange.status == 429:
        return _retry(FailureKind.RATE_LIMIT, "AWeber rate limited the request", exchange.retry_after)
    if exchange.status == 408 or exchange.status >= 500:
        return _retry(FailureKind.AVAILABILITY, "AWeber is temporarily unavailable", exchange.retry_after)
    if exchange.error == "too_large":
        return _branch(
            "invalidResponse", FailureKind.RESPONSE_TOO_LARGE, "AWeber response exceeded the configured limit"
        )
    if exchange.error:
        return _retry(FailureKind.TRANSPORT, "AWeber response was interrupted")
    if exchange.status == 403 and _is_rate_limit(exchange.body):
        return _retry(FailureKind.RATE_LIMIT, "AWeber rate limited the request", exchange.retry_after)
    if exchange.status != 200:
        return _branch(
            "providerRejected", FailureKind.PROVIDER_REJECTION, f"AWeber rejected HTTP {exchange.status}"
        )
    return None


class ListAccountsOperation:
    """Read one page of the OAuth-authorized customer's accounts."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated account Query contract."""
        return definitions.LIST_ACCOUNTS_DEFINITION

    async def invoke(self, call: Call, input: ListAccountsInput) -> QueryAttempt[AccountsPage]:
        """Project account IDs and company names from a bounded response."""
        if input.offset < 0 or input.limit < 0 or input.limit > 100:
            return _branch("defect", FailureKind.VALIDATION, "offset or limit is invalid")
        limit = input.limit or 100
        query = urlencode(sorted({"ws.start": input.offset, "ws.size": limit}.items()))
        exchange = await self.client.get(call, "/1.0/accounts?" + query)
        failure = _read_failure(exchange)
        if failure is not None:
            return failure
        document = _document(exchange.body)
        if document is None:
            return _branch("invalidResponse", FailureKind.PROTOCOL, "AWeber returned invalid account JSON")
        entries = document.get("entries")
        start, total = document.get("start"), document.get("total_size")
        if (
            not isinstance(entries, list)
            or not isinstance(start, int)
            or isinstance(start, bool)
            or start != input.offset
            or not isinstance(total, int)
            or isinstance(total, bool)
            or total < len(entries)
        ):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "AWeber returned invalid account JSON")
        accounts: list[Account] = []
        for entry in entries:
            account_id = entry.get("id") if isinstance(entry, dict) else None
            company = entry.get("company") if isinstance(entry, dict) else None
            if (
                not isinstance(account_id, int)
                or isinstance(account_id, bool)
                or account_id <= 0
                or account_id > 2_147_483_647
                or not isinstance(company, str)
            ):
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "AWeber returned invalid account JSON"
                )
            accounts.append(Account(str(account_id), company))
        return QueryAttempt.branch_result(
            "listed", AccountsPage(tuple(accounts), input.offset, limit, total), Receipt()
        )
