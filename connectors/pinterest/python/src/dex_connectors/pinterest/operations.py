# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Pinterest decisions matching the Go-recorded provider scenarios."""

from __future__ import annotations

import json
import re
from typing import Any
from urllib.parse import urlencode

from dex_connectors.sdk import Call, Failure, FailureKind, QueryAttempt, QueryDefinition, Receipt

from ._generated import definitions
from .client import Client, Exchange
from .models import ListPinsInput, Pin, PinsPage


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "pinterest", "listPins", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[PinsPage]:
    return QueryAttempt.branch_result(branch, PinsPage(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[PinsPage]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _document(body: bytes) -> dict[str, Any] | None:
    try:
        value: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return None
    return value if isinstance(value, dict) else None


def _read_failure(exchange: Exchange) -> QueryAttempt[PinsPage] | None:
    if exchange.error == "credential":
        return _branch(
            "defect", FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
        )
    if exchange.error in ("request", "refresh"):
        return _retry(FailureKind.TRANSPORT, "Pinterest request failed")
    if exchange.error == "too_large":
        return _branch(
            "invalidResponse",
            FailureKind.RESPONSE_TOO_LARGE,
            "Pinterest response exceeded the configured limit",
        )
    if exchange.error:
        return _retry(FailureKind.TRANSPORT, "Pinterest response was interrupted")
    if exchange.status == 429:
        return _retry(FailureKind.RATE_LIMIT, "Pinterest rate limited the request", exchange.retry_after)
    if exchange.status == 408 or exchange.status >= 500:
        return _retry(FailureKind.AVAILABILITY, "Pinterest is temporarily unavailable", exchange.retry_after)
    if exchange.status != 200:
        return _branch(
            "providerRejected", FailureKind.PROVIDER_REJECTION, f"Pinterest rejected HTTP {exchange.status}"
        )
    return None


class ListPinsOperation:
    """Read one cursor-based page of the authorized user's Pins."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Pin Query contract."""
        return definitions.LIST_PINS_DEFINITION

    async def invoke(self, call: Call, input: ListPinsInput) -> QueryAttempt[PinsPage]:
        """Project Pin IDs, board IDs, titles, and the next bookmark."""
        if input.page_size < 0 or input.page_size > 250 or not _valid_bookmark(input.bookmark):
            return _branch("defect", FailureKind.VALIDATION, "page size or bookmark is invalid")
        page_size = input.page_size or 25
        parameters = {"page_size": str(page_size)}
        if input.bookmark:
            parameters["bookmark"] = input.bookmark
        query = urlencode(sorted(parameters.items()))
        exchange = await self.client.get(call, "/v5/pins?" + query)
        failure = _read_failure(exchange)
        if failure is not None:
            return failure
        document = _document(exchange.body)
        if document is None:
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Pinterest returned invalid Pin JSON")
        entries = document.get("items")
        bookmark = document.get("bookmark")
        if not isinstance(entries, list) or len(entries) > page_size or (
            bookmark is not None and (not isinstance(bookmark, str) or not _valid_bookmark(bookmark))
        ):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Pinterest returned invalid Pin JSON")
        pins: list[Pin] = []
        for entry in entries:
            pin_id = entry.get("id") if isinstance(entry, dict) else None
            board_id = entry.get("board_id") if isinstance(entry, dict) else None
            title = entry.get("title") if isinstance(entry, dict) else None
            if (
                not isinstance(pin_id, str)
                or re.fullmatch(r"[0-9]{1,32}", pin_id) is None
                or not isinstance(board_id, str)
                or re.fullmatch(r"[0-9]{1,32}", board_id) is None
                or (title is not None and not isinstance(title, str))
            ):
                return _branch("invalidResponse", FailureKind.PROTOCOL, "Pinterest returned invalid Pin JSON")
            pins.append(Pin(pin_id, board_id, title or ""))
        return QueryAttempt.branch_result("listed", PinsPage(tuple(pins), bookmark or ""), Receipt())


def _valid_bookmark(bookmark: str) -> bool:
    return len(bookmark.encode()) <= 4096 and "\r" not in bookmark and "\n" not in bookmark
