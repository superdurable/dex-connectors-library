# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""LinkedIn ad account decisions matching Go-recorded provider scenarios."""

from __future__ import annotations

import json
from typing import Any
from urllib.parse import urlencode

from dex_connectors.sdk import Call, Failure, FailureKind, QueryAttempt, QueryDefinition, Receipt

from ._generated import definitions
from .client import Client, Exchange
from .models import AdAccount, AdAccountsPage, ListAdAccountsInput


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "linkedin-ads", "listAdAccounts", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[AdAccountsPage]:
    return QueryAttempt.branch_result(branch, AdAccountsPage(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[AdAccountsPage]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _document(body: bytes) -> dict[str, Any] | None:
    try:
        value: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return None
    return value if isinstance(value, dict) else None


def _safe_page_token(token: str) -> bool:
    return len(token) <= 1024 and all(33 <= ord(character) <= 126 for character in token)


def _read_failure(exchange: Exchange) -> QueryAttempt[AdAccountsPage] | None:
    if exchange.error == "credential":
        return _branch(
            "defect", FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
        )
    if exchange.error in ("request", "refresh"):
        return _retry(FailureKind.TRANSPORT, "LinkedIn Ads request failed")
    if exchange.status == 429:
        return _retry(FailureKind.RATE_LIMIT, "LinkedIn Ads rate limited the request", exchange.retry_after)
    if exchange.status == 408 or exchange.status >= 500:
        return _retry(
            FailureKind.AVAILABILITY, "LinkedIn Ads is temporarily unavailable", exchange.retry_after
        )
    if exchange.error == "too_large":
        return _branch(
            "invalidResponse", FailureKind.RESPONSE_TOO_LARGE,
            "LinkedIn Ads response exceeded the configured limit",
        )
    if exchange.error:
        return _retry(FailureKind.TRANSPORT, "LinkedIn Ads response was interrupted")
    if exchange.status != 200:
        return _branch(
            "providerRejected", FailureKind.PROVIDER_REJECTION,
            f"LinkedIn Ads rejected HTTP {exchange.status}",
        )
    return None


class ListAdAccountsOperation:
    """Read one cursor-based page of LinkedIn ad account identities."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated ad account Query contract."""
        return definitions.LIST_AD_ACCOUNTS_DEFINITION

    async def invoke(self, call: Call, input: ListAdAccountsInput) -> QueryAttempt[AdAccountsPage]:
        """Fetch and validate one bounded ad account page."""
        if input.page_size < 0 or input.page_size > 1000 or not _safe_page_token(input.page_token):
            return _branch("defect", FailureKind.VALIDATION, "account page size or cursor is invalid")
        page_size = input.page_size or 100
        query = {"q": "search", "pageSize": str(page_size)}
        if input.page_token:
            query["pageToken"] = input.page_token
        exchange = await self.client.get(call, "/rest/adAccounts?" + urlencode(sorted(query.items())))
        failure = _read_failure(exchange)
        if failure is not None:
            return failure
        document = _document(exchange.body)
        if document is None:
            return _branch("invalidResponse", FailureKind.PROTOCOL, "invalid account JSON")
        elements = document.get("elements")
        metadata = document.get("metadata", {})
        next_token = metadata.get("nextPageToken", "") if isinstance(metadata, dict) else None
        if (
            not isinstance(elements, list) or len(elements) > page_size
            or not isinstance(next_token, str) or not _safe_page_token(next_token)
        ):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "invalid account JSON")
        accounts: list[AdAccount] = []
        seen: set[int] = set()
        for item in elements:
            account_id = item.get("id") if isinstance(item, dict) else None
            name = item.get("name") if isinstance(item, dict) else None
            if (not isinstance(account_id, int) or isinstance(account_id, bool) or account_id <= 0 or
                    account_id > 9_007_199_254_740_991 or not isinstance(name, str) or
                    not name.strip() or account_id in seen):
                return _branch("invalidResponse", FailureKind.PROTOCOL, "invalid account JSON")
            seen.add(account_id)
            accounts.append(AdAccount(str(account_id), name))
        return QueryAttempt.branch_result("listed", AdAccountsPage(tuple(accounts), next_token), Receipt())
