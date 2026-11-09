# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Search Console site decisions matching Go provider recordings."""

from __future__ import annotations

import json
from typing import TYPE_CHECKING, Any

from dex_connectors.sdk import Call, Failure, FailureKind, QueryAttempt, QueryDefinition, Receipt

from ._generated import definitions
from .models import ListSitesInput, Site, SitesPage

if TYPE_CHECKING:
    from .client import Client


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "google-search-console", "listSites", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[SitesPage]:
    return QueryAttempt.branch_result(branch, SitesPage(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[SitesPage]:
    return QueryAttempt.retry(_failure(kind, message), delay)


class ListSitesOperation:
    """Read properties visible to the connected Google account."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated site Query contract."""
        return definitions.LIST_SITES_DEFINITION

    async def invoke(self, call: Call, _input: ListSitesInput) -> QueryAttempt[SitesPage]:
        """Return one bounded list of site URLs and permissions."""
        exchange = await self.client.request(call, "GET", "/webmasters/v3/sites")
        if exchange.error == "credential":
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
            )
        if exchange.error in ("request", "refresh"):
            return _retry(FailureKind.TRANSPORT, "Search Console request failed")
        if exchange.status == 429:
            return _retry(FailureKind.RATE_LIMIT, "Google rate limited the request", exchange.retry_after)
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(
                FailureKind.AVAILABILITY, "Search Console is temporarily unavailable", exchange.retry_after
            )
        if exchange.status != 200:
            return _branch(
                "providerRejected", FailureKind.PROVIDER_REJECTION, f"Google rejected HTTP {exchange.status}"
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "Search Console response exceeded the configured limit",
            )
        if exchange.error:
            return _retry(FailureKind.TRANSPORT, "Search Console response was interrupted")
        try:
            document: Any = json.loads(exchange.body)
        except (ValueError, UnicodeDecodeError):
            document = None
        if not isinstance(document, dict):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Google returned invalid property JSON")
        entries = document.get("siteEntry", [])
        if not isinstance(entries, list) or len(entries) > 1000:
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Google returned invalid property JSON")
        sites: list[Site] = []
        for entry in entries:
            if (
                not isinstance(entry, dict)
                or not isinstance(entry.get("siteUrl"), str)
                or not entry["siteUrl"]
                or entry.get("permissionLevel")
                not in ("siteOwner", "siteFullUser", "siteRestrictedUser", "siteUnverifiedUser")
            ):
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "Google returned incomplete property metadata"
                )
            sites.append(Site(entry["siteUrl"], entry["permissionLevel"]))
        return QueryAttempt.branch_result("listed", SitesPage(tuple(sites)), Receipt())
