# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go ticketChanged fetcher's recorded exchanges and require identical requests and cursors.

The fixture was recorded from the Go connector's ticketEventFetcher: each step lists the GET /api/events
requests Go sent with the responses it received, the cursor token it committed, and the Trigger events it
delivered. Python must send the same query strings, commit the same tokens byte for byte, and deliver the
same events, so Go and Python replicas share one poll cursor document.
"""

import base64
import json
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

import httpx
import pytest
from dex_connectors.sdk import (
    ConnectionRef,
    SecretString,
    StaticCredentialProvider,
    TriggerCancellation,
    TriggerContext,
)
from dex_connectors.sdk.pollingtrigger import Cursor, RateLimitedError, is_cursor_invalid

from dex_connectors.gorgias import Client, Config, Credentials, TicketChangedTriggerConfiguration
from dex_connectors.gorgias._go_time import parse_go_time
from dex_connectors.gorgias._ticket_event_fetcher import TicketEventFetcher

FIXTURE: dict[str, Any] = json.loads(
    (Path(__file__).parent / "fixtures" / "go-ticket-changed-fixtures.json").read_text()
)
REFERENCE = ConnectionRef("gorgias", "gorgias-ticket-watcher")
SCENARIOS: list[dict[str, Any]] = FIXTURE["scenarios"]


def _go_rfc3339_nano(instant: datetime) -> str:
    text = instant.astimezone(UTC).strftime("%Y-%m-%dT%H:%M:%S.%f").rstrip("0").rstrip(".")
    return text + "Z"


def _microsecond_rfc3339(go_text: str) -> str:
    # A Python datetime holds microseconds, Gorgias's documented precision; Go keeps any finer digits.
    return parse_go_time(go_text).to_datetime().strftime("%Y-%m-%dT%H:%M:%S.%f").rstrip("0").rstrip(".") + "Z"


class RecordedProvider:
    """Answers each request with the next recorded response and checks its query against Go's."""

    def __init__(self) -> None:
        self.exchanges: list[dict[str, Any]] = []
        self.step_name = ""

    def serve(self, step_name: str, exchanges: list[dict[str, Any]]) -> None:
        self.step_name, self.exchanges = step_name, list(exchanges or [])

    def handle(self, request: httpx.Request) -> httpx.Response:
        assert self.exchanges, f"{self.step_name}: Python sent more requests than Go"
        exchange = self.exchanges.pop(0)
        assert request.method == "GET" and request.url.path == "/api/events", self.step_name
        assert request.url.query.decode() == exchange["query"], self.step_name
        basic = base64.b64encode(f"{FIXTURE['email']}:{FIXTURE['apiKey']}".encode()).decode()
        assert request.headers["Authorization"] == "Basic " + basic
        headers = {"Content-Type": "application/json"}
        for name, key in (("Date", "date"), ("Retry-After", "retryAfter")):
            if exchange.get(key):
                headers[name] = exchange[key]
        if exchange.get("apiCallLimit"):
            headers["X-Gorgias-Account-Api-Call-Limit"] = exchange["apiCallLimit"]
        return httpx.Response(exchange["status"], headers=headers, content=exchange["responseBody"].encode())


@pytest.mark.parametrize("scenario", SCENARIOS, ids=[scenario["name"] for scenario in SCENARIOS])
async def test_ticket_event_fetcher_matches_the_go_recording(scenario: dict[str, Any]) -> None:
    provider = RecordedProvider()
    now = parse_go_time(scenario["now"])
    context = TriggerContext(TriggerCancellation())
    credentials = Credentials(FIXTURE["email"], SecretString(FIXTURE["apiKey"]))
    async with httpx.AsyncClient(transport=httpx.MockTransport(provider.handle)) as http_client:
        client = Client(
            Config(domain="acme"),
            StaticCredentialProvider({REFERENCE: credentials}),
            http_client,
            now=now.to_datetime,
        )
        configuration = TicketChangedTriggerConfiguration(event_types=tuple(scenario.get("eventTypes") or ()))
        fetcher = TicketEventFetcher(client, REFERENCE, configuration)
        # Go reads its clock at nanosecond precision; replay the recorded instant exactly.
        fetcher._local_now = lambda: now
        for index, step in enumerate(scenario["steps"]):
            name = f"{scenario['name']} step {index}"
            provider.serve(name, step["exchanges"])
            try:
                if step["call"] == "seed":
                    cursor = await fetcher.seed_cursor(context)
                    page = None
                else:
                    page = await fetcher.fetch_changes(context, Cursor(step["cursor"]))
                    cursor = page.next_cursor
            except Exception as error:
                assert step.get("error"), f"{name}: Go succeeded but Python raised {error!r}"
                _assert_error(name, step, error)
            else:
                assert not step.get("error"), f"{name}: Go failed with {step.get('error')}"
                assert cursor.token == step["token"], name
                if page is not None:
                    _assert_page(name, step, page)
            assert not provider.exchanges, f"{name}: Python sent fewer requests than Go"


def _assert_error(name: str, step: dict[str, Any], error: Exception) -> None:
    expected = step["error"]
    if expected == "cursorInvalid":
        assert is_cursor_invalid(error), name
        return
    assert not is_cursor_invalid(error), name
    rate_limited = _rate_limited(error)
    if expected == "rateLimited":
        assert rate_limited is not None, name
        assert rate_limited.retry_after.total_seconds() == step.get("retryAfterSeconds", 0), name
    else:
        assert expected == "failed" and rate_limited is None, name


def _rate_limited(error: BaseException) -> RateLimitedError | None:
    while error is not None:
        if isinstance(error, RateLimitedError):
            return error
        error = error.__cause__  # type: ignore[assignment]
    return None


def _assert_page(name: str, step: dict[str, Any], page: Any) -> None:
    assert page.has_more_pages == step.get("hasMorePages", False), name
    expected_events = step.get("events") or []
    assert len(page.changes) == len(expected_events), name
    for change, expected in zip(page.changes, expected_events, strict=True):
        payload = expected["payload"]
        assert change.id == expected["id"], name
        assert _go_rfc3339_nano(change.occurred_at) == _microsecond_rfc3339(expected["occurredAt"]), name
        assert change.payload.event_id == payload["eventId"], name
        assert change.payload.event_type == payload["eventType"], name
        assert change.payload.ticket_id == payload["ticketId"], name
        assert _go_rfc3339_nano(change.payload.occurred_at) == _microsecond_rfc3339(payload["occurredAt"]), (
            name
        )
        assert change.payload.user_id == payload.get("userId", 0), name
        assert change.payload.context == payload.get("context", ""), name
