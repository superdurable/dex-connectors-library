# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Basecamp decisions matching the Go-recorded provider scenarios."""

from __future__ import annotations

import json
from typing import Any

from dex_connectors.sdk import Call, Failure, FailureKind, QueryAttempt, QueryDefinition, Receipt

from ._generated import definitions
from .client import Client, Exchange
from .models import Account, AccountsPage, ListAccountsInput


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "basecamp", "listAccounts", message)


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


def _read_failure(exchange: Exchange) -> QueryAttempt[AccountsPage] | None:
    if exchange.error == "credential":
        return _branch(
            "defect", FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
        )
    if exchange.error in ("request", "refresh"):
        return _retry(FailureKind.TRANSPORT, "Basecamp request failed")
    if exchange.error == "too_large":
        return _branch(
            "invalidResponse",
            FailureKind.RESPONSE_TOO_LARGE,
            "Basecamp response exceeded the configured limit",
        )
    if exchange.error:
        return _retry(FailureKind.TRANSPORT, "Basecamp response was interrupted")
    if exchange.status == 429:
        return _retry(FailureKind.RATE_LIMIT, "Basecamp rate limited the request", exchange.retry_after)
    if exchange.status == 408 or exchange.status >= 500:
        return _retry(FailureKind.AVAILABILITY, "Basecamp is temporarily unavailable", exchange.retry_after)
    if exchange.status != 200:
        return _branch(
            "providerRejected", FailureKind.PROVIDER_REJECTION, f"Basecamp rejected HTTP {exchange.status}"
        )
    return None


class ListAccountsOperation:
    """Read a bounded local page of OAuth-authorized Basecamp accounts."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated account Query contract."""
        return definitions.LIST_ACCOUNTS_DEFINITION

    async def invoke(self, call: Call, input: ListAccountsInput) -> QueryAttempt[AccountsPage]:
        """Project account IDs and names without persisting the identity profile."""
        if input.offset < 0 or input.limit < 0 or input.limit > 100:
            return _branch("defect", FailureKind.VALIDATION, "offset or limit is invalid")
        limit = input.limit or 100
        exchange = await self.client.get(call, "/authorization.json")
        failure = _read_failure(exchange)
        if failure is not None:
            return failure
        document = _document(exchange.body)
        if document is None:
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Basecamp returned invalid account JSON")
        entries = document.get("accounts")
        if not isinstance(entries, list):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Basecamp returned invalid account JSON")
        accounts: list[Account] = []
        seen: set[int] = set()
        for entry in entries:
            if not isinstance(entry, dict):
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "Basecamp returned invalid account JSON"
                )
            if entry.get("product") != "bc3":
                continue
            account_id = entry.get("id") if isinstance(entry, dict) else None
            company = entry.get("name") if isinstance(entry, dict) else None
            if (
                not isinstance(account_id, int)
                or isinstance(account_id, bool)
                or account_id <= 0
                or account_id > 9_007_199_254_740_991
                or not isinstance(company, str)
                or not company
                or account_id in seen
            ):
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "Basecamp returned invalid account JSON"
                )
            seen.add(account_id)
            accounts.append(Account(str(account_id), company))
        selected_accounts = tuple(accounts[input.offset : input.offset + limit])
        page = AccountsPage(selected_accounts, input.offset, limit, len(accounts))
        return QueryAttempt.branch_result("listed", page, Receipt())
