# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded ticketChanged fetches and compare requests, cursors, and events byte for byte."""

import json
from collections.abc import Callable
from datetime import datetime, timedelta
from pathlib import Path
from typing import Any

import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider, TriggerContext
from dex_connectors.sdk.pollingtrigger import Cursor, RateLimitedError, is_cursor_invalid
from dex_connectors.sdk.projectconfig import encode_go_json_value

from dex_connectors.zendesk_support import (
    Client,
    Config,
    Credentials,
    TicketChangedTriggerConfiguration,
    TicketExportFetcher,
    decode_ticket_changed_event_payload,
)

API_TOKEN = "fixtureZendeskToken0123"
CONNECTION = ConnectionRef("zendesk", "zendesk-ticket-watcher")
CASES: list[dict[str, Any]] = json.loads(
    (Path(__file__).parent / "fixtures/go-ticket-export-cases.json").read_text()
)["cases"]


def canned_responses(case: dict[str, Any], requests: list[str]) -> Callable[[httpx.Request], httpx.Response]:
    """Answer each request with the case's next Go-served response."""
    responses = iter(case["responses"] or [])

    def handle(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/api/v2/incremental/tickets/cursor"
        requests.append(request.url.query.decode())
        response = next(responses)
        headers = {"Content-Type": "application/json"}
        if response.get("retryAfter"):
            headers["Retry-After"] = response["retryAfter"]
        return httpx.Response(response["status"], headers=headers, content=response["body"].encode())

    return handle


@pytest.mark.parametrize("case", CASES, ids=lambda case: case["name"])
async def test_fetcher_matches_go_recording(case: dict[str, Any]) -> None:
    """Every request, cursor token, event ID, and payload equals the Go connector's."""
    requests: list[str] = []
    now = datetime.fromisoformat(case["now"].replace("Z", "+00:00"))
    http_client = httpx.AsyncClient(transport=httpx.MockTransport(canned_responses(case, requests)))
    client = Client(
        Config(subdomain="acme", max_response_bytes=case.get("maxResponseBytes") or 4194304),
        StaticCredentialProvider(
            {CONNECTION: Credentials("admin@acme.example.com", SecretString(API_TOKEN))}
        ),
        http_client,
        api_base_url="https://acme.zendesk.com/api/v2",
        now=lambda: now,
        incremental_export_requests_per_minute=600,
    )
    configuration = TicketChangedTriggerConfiguration(include_deleted=case.get("includeDeleted", False))
    fetcher = TicketExportFetcher(client, CONNECTION, configuration)
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
        await http_client.aclose()
    assert requests == (case["requests"] or [])
    if case.get("error") == "cursorInvalid":
        assert is_cursor_invalid(error)
    elif case.get("error") == "rateLimited":
        assert isinstance(error, RateLimitedError)
        assert error.retry_after == timedelta(seconds=case.get("retryAfterSeconds", 0))
    elif case.get("error") == "failed":
        assert error is not None and not is_cursor_invalid(error) and not isinstance(error, RateLimitedError)
        assert API_TOKEN not in str(error)
        assert case.get("errorContains", "") in str(error)
    else:
        assert error is None
    assert cursor_out == case.get("cursorOut", "")
    assert has_more_pages == case.get("hasMorePages", False)
    expected = case.get("changes") or []
    assert [change.id for change in changes] == [change["id"] for change in expected]
    for change, recorded in zip(changes, expected, strict=True):
        assert change.occurred_at == datetime.fromisoformat(recorded["occurredAt"].replace("Z", "+00:00"))
        # The SDK stores object keys in Go map order and Go in struct order; the values are equal.
        assert json.loads(encode_go_json_value(change.payload)) == json.loads(recorded["payload"])
        payload = recorded["payload"]
        assert decode_ticket_changed_event_payload(payload, payload.encode()) == change.payload
