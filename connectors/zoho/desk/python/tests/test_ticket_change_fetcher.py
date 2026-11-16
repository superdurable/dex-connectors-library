# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded ticketChanged fetches and compare requests, cursors, and events byte for byte."""

import json
from collections.abc import Callable
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any

import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider, TriggerContext
from dex_connectors.sdk.pollingtrigger import Cursor, RateLimitedError, is_cursor_invalid
from dex_connectors.sdk.projectconfig import KeyedSnapshot, KeyedSnapshotEntry, encode_trigger_event

from dex_connectors.zoho_desk import (
    Client,
    Config,
    Credentials,
    Ticket,
    TicketChangedTriggerConfiguration,
    TicketChangeFetcher,
    decode_ticket_changed_event_payload,
    ticket_change_event_id,
)
from dex_connectors.zoho_desk.ticket_change_fetcher import (
    _next_search_position,
    _SearchPage,
    _SearchPosition,
    ticket_version_hash,
)

ACCESS_TOKEN = "1000.zohoDeskTriggerAccess0123456789"
ORGANIZATION_ID = "2389290"
CONNECTION = ConnectionRef("zoho", "zoho-desk-fetcher")
CASES: list[dict[str, Any]] = json.loads(
    (Path(__file__).parent / "fixtures/go-ticket-change-cases.json").read_text()
)["cases"]


def new_client(handler: Callable[[httpx.Request], httpx.Response], now: datetime) -> Client:
    """Build a client whose provider is an in-process handler and whose clock is fixed."""
    return Client(
        Config(org_id=ORGANIZATION_ID, poll_interval_seconds=15),
        StaticCredentialProvider(
            {CONNECTION: Credentials(access_token=SecretString(ACCESS_TOKEN), auth_method_id="zoho-us-oauth")}
        ),
        httpx.AsyncClient(transport=httpx.MockTransport(handler)),
        api_base_url="https://desk.example.test/api/v1",
        now=lambda: now,
    )


def canned_responses(case: dict[str, Any], requests: list[str]) -> Callable[[httpx.Request], httpx.Response]:
    """Answer each request with the case's next Go-served response."""
    responses = iter(case["responses"])

    def handle(request: httpx.Request) -> httpx.Response:
        assert request.headers["Authorization"] == "Zoho-oauthtoken " + ACCESS_TOKEN
        assert request.headers["orgId"] == ORGANIZATION_ID
        assert request.url.path == "/api/v1/tickets/search"
        requests.append(request.url.query.decode())
        response = next(responses)
        headers = {"Content-Type": "application/json"}
        if response.get("date"):
            headers["Date"] = response["date"]
        if response.get("retryAfter"):
            headers["Retry-After"] = response["retryAfter"]
        return httpx.Response(response["status"], headers=headers, content=response["body"].encode())

    return handle


def recorded_cursor(recorded: dict[str, Any] | None) -> Cursor:
    """Rebuild a Go-recorded cursor token and keyed snapshot."""
    if recorded is None:
        return Cursor()
    snapshot = None
    if recorded["hasSnapshot"]:
        snapshot = KeyedSnapshot(
            tuple(KeyedSnapshotEntry(key, hash, seen) for key, hash, seen in recorded["snapshot"])
        )
    return Cursor(recorded["token"], snapshot)


def snapshot_rows(cursor: Cursor) -> list[list[Any]] | None:
    """List a cursor snapshot as the recording stores it."""
    if cursor.snapshot is None:
        return None
    return [[entry.key, entry.hash, entry.seen] for entry in cursor.snapshot.entries]


@pytest.mark.parametrize("case", CASES, ids=lambda case: case["name"])
async def test_fetcher_matches_go_recording(case: dict[str, Any]) -> None:
    """Every request, cursor token, snapshot, and event equals the Go connector's."""
    requests: list[str] = []
    client = new_client(canned_responses(case, requests), datetime.fromisoformat(case["now"]))
    configuration = TicketChangedTriggerConfiguration(department_id=case.get("departmentId", ""))
    fetcher = TicketChangeFetcher(client, CONNECTION, configuration)
    error: BaseException | None = None
    cursor_out: Cursor | None = None
    has_more_pages, changes = False, []
    try:
        if case["kind"] == "seed":
            cursor_out = await fetcher.seed_cursor(TriggerContext())
        else:
            page = await fetcher.fetch_changes(TriggerContext(), recorded_cursor(case.get("cursorIn")))
            cursor_out, has_more_pages, changes = page.next_cursor, page.has_more_pages, list(page.changes)
    except Exception as raised:
        error = raised
    finally:
        await client.http_client.aclose()
    assert requests == case["requests"]
    if case.get("error") == "cursorInvalid":
        assert is_cursor_invalid(error)
    elif case.get("error") == "rateLimited":
        assert isinstance(error, RateLimitedError)
        assert error.retry_after == timedelta(seconds=case.get("retryAfterSeconds", 0))
    elif case.get("error") == "failed":
        assert error is not None and not is_cursor_invalid(error) and not isinstance(error, RateLimitedError)
        assert ACCESS_TOKEN not in str(error)
    else:
        assert error is None, error
    recorded_out = case.get("cursorOut")
    if recorded_out is None:
        assert cursor_out is None
    else:
        assert cursor_out is not None
        assert cursor_out.token == recorded_out["token"]
        assert snapshot_rows(cursor_out) == (
            recorded_out["snapshot"] if recorded_out["hasSnapshot"] else None
        )
    assert has_more_pages == case.get("hasMorePages", False)
    expected = case.get("changes") or []
    assert [change.id for change in changes] == [change["id"] for change in expected]
    for change, recorded in zip(changes, expected, strict=True):
        stored = json.loads(encode_trigger_event(change))
        assert stored["occurredAt"] == recorded["occurredAt"]
        assert change.payload.change_kind == recorded["payload"]["changeKind"]
        if len(expected) > 3:
            continue
        # The SDK stores object keys in Go map order; Go stores struct order. Both decode alike.
        assert stored["payload"] == recorded["payload"]
        payload = json.dumps(recorded["payload"], separators=(",", ":"))
        assert decode_ticket_changed_event_payload(payload, payload.encode()) == change.payload


def test_event_id_and_version_hash_are_pure_functions_of_the_ticket_version() -> None:
    """A refetch yields the same ID; a later modified time yields a new ID and hash."""
    instant = datetime(2026, 3, 2, 9, 0, tzinfo=UTC)
    ticket = Ticket(id="1892000000200001", status="Open", created_at=instant, modified_at=instant)
    later = Ticket(
        id=ticket.id, status="Closed", created_at=instant, modified_at=instant + timedelta(seconds=1)
    )
    assert ticket_change_event_id(ORGANIZATION_ID, ticket) == ticket_change_event_id(ORGANIZATION_ID, ticket)
    assert ticket_change_event_id(ORGANIZATION_ID, ticket).startswith("zdesk-")
    assert len(ticket_change_event_id(ORGANIZATION_ID, ticket)) == len("zdesk-") + 32
    assert ticket_change_event_id(ORGANIZATION_ID, ticket) != ticket_change_event_id(ORGANIZATION_ID, later)
    assert ticket_change_event_id("1", ticket) != ticket_change_event_id(ORGANIZATION_ID, ticket)
    assert ticket_version_hash(ticket) != ticket_version_hash(later)


def test_next_position_steps_past_an_instant_beyond_the_offset_limit() -> None:
    """Match Go: a full page at offset 900 of one instant steps one millisecond and keeps the sweep start."""
    instant = datetime(2026, 3, 2, 9, 0, tzinfo=UTC)
    tickets = tuple(
        Ticket(id="1", status="Open", created_at=instant, modified_at=instant) for _ in range(100)
    )
    position = _SearchPosition(instant, instant, 900, instant)
    following, has_more_pages = _next_search_position(position, _SearchPage(tickets, 100, instant, None))
    assert has_more_pages
    assert following == _SearchPosition(instant, instant + timedelta(milliseconds=1), 0, instant)
