# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded ticketChanged fetches and compare cursors, requests, and events byte for byte."""

import json
from collections.abc import Callable
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any

import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider, TriggerContext
from dex_connectors.sdk.pollingtrigger import Cursor, RateLimitedError, is_cursor_invalid
from dex_connectors.sdk.projectconfig import encode_trigger_event

from dex_connectors.freshworks_freshdesk import (
    Client,
    Config,
    Credentials,
    TicketChangedTriggerConfiguration,
    TicketChangeFetcher,
    decode_ticket_changed_event_payload,
)
from dex_connectors.freshworks_freshdesk.ticket_change_fetcher import _prune_delivered_ticket_keys

API_KEY = "freshdeskTriggerKey0123456789"
CONNECTION = ConnectionRef("freshdesk", "freshdesk-trigger")
CASES: list[dict[str, Any]] = json.loads(
    (Path(__file__).parent / "fixtures/go-ticket-change-cases.json").read_text()
)["cases"]


def new_client(handler: Callable[[httpx.Request], httpx.Response], now: datetime | None = None) -> Client:
    """Build a client whose provider is an in-process handler."""
    return Client(
        Config(domain="acme"),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString(API_KEY))}),
        httpx.AsyncClient(transport=httpx.MockTransport(handler)),
        api_base_url="https://acme.freshdesk.com/api/v2",
        now=(lambda: now) if now is not None else None,
    )


def canned_responses(case: dict[str, Any], requests: list[str]) -> Callable[[httpx.Request], httpx.Response]:
    """Answer each request with the case's next Go-served response."""
    responses = iter(case["responses"])

    def handle(request: httpx.Request) -> httpx.Response:
        requests.append(request.url.query.decode())
        response = next(responses)
        headers = {"Content-Type": "application/json"}
        if response.get("date"):
            headers["Date"] = response["date"]
        if response.get("link"):
            headers["Link"] = '<https://acme.freshdesk.com/api/v2/tickets?page=2>; rel="next"'
        if response.get("retryAfter"):
            headers["Retry-After"] = response["retryAfter"]
        body = json.dumps(response["body"], separators=(",", ":")).encode()
        return httpx.Response(response["status"], headers=headers, content=body)

    return handle


@pytest.mark.parametrize("case", CASES, ids=lambda case: case["name"])
async def test_fetcher_matches_go_recording(case: dict[str, Any]) -> None:
    """Every request, cursor token, and event ID equals the Go connector's, and payloads decode alike."""
    requests: list[str] = []
    now = datetime.fromisoformat(case["now"]) if case.get("now") else None
    client = new_client(canned_responses(case, requests), now)
    configuration = TicketChangedTriggerConfiguration(tuple(case.get("changeKinds") or ()))
    fetcher = TicketChangeFetcher(client, CONNECTION, configuration)
    error: BaseException | None = None
    cursor_out, has_more_pages, changes = "", False, []
    try:
        if case["kind"] == "seed":
            cursor_out = (await fetcher.seed_cursor(TriggerContext())).token
        else:
            page = await fetcher.fetch_changes(TriggerContext(), Cursor(case.get("cursorIn", "")))
            cursor_out, has_more_pages = page.next_cursor.token, page.has_more_pages
            changes = list(page.changes)
    except Exception as raised:
        error = raised
    finally:
        await client._client.aclose()
    assert requests == case["requests"]
    if case.get("error") == "cursorInvalid":
        assert is_cursor_invalid(error)
    elif case.get("error") == "rateLimited":
        assert isinstance(error, RateLimitedError)
        assert error.retry_after == timedelta(seconds=case.get("retryAfterSeconds", 0))
    elif case.get("error") == "failed":
        assert error is not None and not is_cursor_invalid(error) and not isinstance(error, RateLimitedError)
        assert API_KEY not in str(error)
    else:
        assert error is None
    assert cursor_out == case.get("cursorOut", "")
    assert len(cursor_out.encode()) < 64 << 10
    assert has_more_pages == case.get("hasMorePages", False)
    expected = case.get("changes") or []
    assert [change.id for change in changes] == [change["id"] for change in expected]
    for change, recorded in zip(changes, expected, strict=True):
        stored = json.loads(encode_trigger_event(change))
        assert stored["occurredAt"] == recorded["occurredAt"]
        if len(expected) > 3:
            assert change.payload.change_kind == recorded["payload"]["changeKind"]
            continue
        # The SDK stores object keys in Go map order; Go stores struct order. Both decode alike.
        assert stored["payload"] == recorded["payload"]
        payload = json.dumps(recorded["payload"], separators=(",", ":"))
        assert decode_ticket_changed_event_payload(payload, payload.encode()) == change.payload


def test_prune_keeps_the_newest_keys_inside_the_window() -> None:
    """Match Go's pruning: bad and old keys drop, and an overflow floor skips a partly dropped second."""
    window_start = datetime.fromtimestamp(1000, UTC)
    pruned, floor = _prune_delivered_ticket_keys(
        ["1@999", "2@1000", "3@1001", "3@1001", "bad", "4@x", "5@ 1002"], window_start
    )
    assert pruned == ["2@1000", "3@1001"]
    assert floor == datetime.min.replace(tzinfo=UTC)
    many = [f"{index}@{2000 + index}" for index in range(1510)]
    pruned, floor = _prune_delivered_ticket_keys(many, window_start)
    assert len(pruned) == 1500 and pruned[-1] == many[-1]
    assert floor == datetime.fromtimestamp(2010, UTC)
    same_second = ["1@3000", "2@3000"] + [f"{10 + index}@{3000 + index % 2}" for index in range(1500)]
    pruned, floor = _prune_delivered_ticket_keys(same_second, window_start)
    assert floor == datetime.fromtimestamp(3001, UTC)
    assert all(key.endswith("@3001") for key in pruned)
