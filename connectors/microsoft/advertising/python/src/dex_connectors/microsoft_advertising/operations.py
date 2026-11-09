# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Microsoft Advertising campaign Query decisions matching Go recordings."""

from __future__ import annotations

import json
import re
from typing import Any, cast

from dex_connectors.sdk import Call, Failure, FailureKind, QueryAttempt, QueryDefinition, Receipt

from ._generated import definitions
from .client import Client, Exchange
from .models import CampaignsPage, CampaignSummary, ListCampaignsInput

_ADVERTISING_ID_PATTERN = re.compile(r"[1-9][0-9]{0,18}\Z")
_MAX_SIGNED_64 = (1 << 63) - 1


def valid_advertising_id(value: object) -> bool:
    """Accept positive decimal identifiers that fit Microsoft Advertising's long type."""
    return (
        isinstance(value, str)
        and _ADVERTISING_ID_PATTERN.fullmatch(value) is not None
        and int(value) <= _MAX_SIGNED_64
    )


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "microsoft-advertising", "listCampaigns", message)


def _empty_page() -> CampaignsPage:
    return CampaignsPage(())


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[CampaignsPage]:
    return QueryAttempt.branch_result(branch, _empty_page(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[CampaignsPage]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _classify(exchange: Exchange) -> QueryAttempt[CampaignsPage] | None:
    if exchange.error == "credential":
        return _branch(
            "defect",
            FailureKind.AUTHENTICATION,
            "Microsoft Advertising connection credentials are unavailable",
        )
    if exchange.error in ("request", "refresh"):
        return _retry(FailureKind.TRANSPORT, "Microsoft Advertising campaign request failed")
    if exchange.status == 429:
        return _retry(
            FailureKind.RATE_LIMIT, "Microsoft Advertising rate limited the request", exchange.retry_after
        )
    if exchange.status == 408 or exchange.status >= 500:
        return _retry(
            FailureKind.AVAILABILITY, "Microsoft Advertising is temporarily unavailable", exchange.retry_after
        )
    if exchange.status != 200:
        return _branch(
            "providerRejected",
            FailureKind.PROVIDER_REJECTION,
            f"Microsoft Advertising rejected HTTP {exchange.status}",
        )
    if exchange.error == "too_large":
        return _branch(
            "invalidResponse",
            FailureKind.RESPONSE_TOO_LARGE,
            "Microsoft Advertising campaign response exceeded the configured limit",
        )
    if exchange.error:
        return _retry(FailureKind.TRANSPORT, "Microsoft Advertising campaign response was interrupted")
    return None


def _decode_page(body: bytes) -> CampaignsPage:
    document: Any = json.loads(body)
    if not isinstance(document, dict) or not isinstance(document.get("Campaigns"), list):
        raise ValueError("campaign list is missing")
    campaigns: list[CampaignSummary] = []
    for entry in document["Campaigns"]:
        if not isinstance(entry, dict):
            raise ValueError("campaign record is invalid")
        identifier = entry.get("Id")
        name = entry.get("Name")
        campaign_type = entry.get("CampaignType")
        status = entry.get("Status")
        if (
            not valid_advertising_id(identifier)
            or not isinstance(name, str)
            or not name
            or not isinstance(campaign_type, str)
            or not campaign_type
            or not isinstance(status, str)
            or not status
        ):
            raise ValueError("campaign record is incomplete")
        campaigns.append(CampaignSummary(cast(str, identifier), name, campaign_type, status))
    return CampaignsPage(tuple(campaigns))


class ListCampaignsOperation:
    """List IDs, names, types, and statuses in one advertising account."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated campaign Query contract."""
        return definitions.LIST_CAMPAIGNS_DEFINITION

    async def invoke(self, call: Call, input: ListCampaignsInput) -> QueryAttempt[CampaignsPage]:
        """Query the account and project only selected campaign fields."""
        if not valid_advertising_id(input.account_id):
            return _branch("defect", FailureKind.VALIDATION, "Microsoft Advertising account ID is invalid")
        exchange = await self.client.post_campaigns(call, input.account_id)
        failure = _classify(exchange)
        if failure is not None:
            return failure
        try:
            page = _decode_page(exchange.body)
        except (ValueError, UnicodeDecodeError):
            return _branch(
                "invalidResponse",
                FailureKind.PROTOCOL,
                "Microsoft Advertising returned invalid campaign JSON",
            )
        return QueryAttempt.branch_result("listed", page, Receipt())
