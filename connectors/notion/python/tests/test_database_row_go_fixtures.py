# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go databaseRowChanged fetcher's recorded polls: same requests, cursor bytes, and events."""

import hashlib
import json
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

from dex_connectors.notion import Client, Config, Credentials, decode_database_row_changed_event_payload
from dex_connectors.notion._database_row_cursor import (
    DATABASE_ROW_CURSOR_VERSION,
    DatabaseRowCursor,
    SeenRows,
    encode_database_row_cursor,
)
from dex_connectors.notion._database_row_fetcher import DatabaseRowFetcher
from dex_connectors.notion._go_time import parse_go_time

FIXTURES = json.loads(
    (Path(__file__).parent / "fixtures" / "go-database-row-changed-fixtures.json").read_text()
)
TOKEN = "ntn_fake_rows_token"
CONNECTION = ConnectionRef("notion", "rows")
DATA_SOURCE_ID = "2a4d6f80-1b3c-4d5e-8f70-112233445566"


def ordered(value: Any) -> Any:
    """Parse JSON text keeping member order, so equality also checks Go's field order."""
    return json.loads(value if isinstance(value, str) else json.dumps(value), object_pairs_hook=list)


class RecordedNotion:
    """Answers each request with the next recorded Go exchange after checking the request body."""

    def __init__(self) -> None:
        self.exchanges: list[dict[str, Any]] = []

    def handle(self, request: httpx.Request) -> httpx.Response:
        assert self.exchanges, "the Python fetcher sent a request the Go fetcher did not"
        exchange = self.exchanges.pop(0)
        assert request.headers["Authorization"] == "Bearer " + TOKEN
        assert ordered(request.content.decode()) == ordered(exchange["requestBody"])
        headers = {"Content-Type": "application/json"}
        if exchange.get("retryAfter"):
            headers["Retry-After"] = exchange["retryAfter"]
        return httpx.Response(exchange["status"], headers=headers, content=exchange["responseBody"].encode())


def new_fetcher(scenario: dict[str, Any], provider: RecordedNotion) -> DatabaseRowFetcher:
    credentials = StaticCredentialProvider({CONNECTION: Credentials(SecretString(TOKEN))})
    client = Client(
        Config(
            endpoint="https://api.notion.com", poll_interval_seconds=5, poll_page_size=scenario["pageSize"]
        ),
        credentials,
        httpx.AsyncClient(transport=httpx.MockTransport(provider.handle)),
    )
    return DatabaseRowFetcher(client, CONNECTION, scenario["dataSourceId"])


@pytest.mark.parametrize("scenario", FIXTURES["scenarios"], ids=lambda scenario: scenario["name"])
async def test_python_fetcher_matches_the_go_recording(scenario: dict[str, Any]) -> None:
    provider = RecordedNotion()
    fetcher = new_fetcher(scenario, provider)
    context = TriggerContext(TriggerCancellation())
    for index, step in enumerate(scenario["steps"]):
        provider.exchanges = list(step["exchanges"] or [])
        where = f"step {index} ({step['call']})"
        try:
            if step["call"] == "seed":
                token, has_more, events = (await fetcher.seed_cursor(context)).token, False, []
            else:
                page = await fetcher.fetch_changes(context, Cursor(step["cursor"]))
                token, has_more, events = page.next_cursor.token, page.has_more_pages, list(page.changes)
        except Exception as error:
            expected = step.get("error")
            assert expected, f"{where} raised {error!r}"
            assert is_cursor_invalid(error) == (expected == "cursorInvalid"), where
            assert isinstance(error, RateLimitedError) == (expected == "rateLimited"), where
            if expected == "rateLimited":
                assert isinstance(error, RateLimitedError)
                assert error.retry_after.total_seconds() == step["retryAfterSeconds"], where
            assert not provider.exchanges, f"{where} sent fewer requests than Go"
            continue
        assert not step.get("error"), f"{where} succeeded where Go failed"
        assert not provider.exchanges, f"{where} sent fewer requests than Go"
        assert token == step["token"], where
        assert has_more == step.get("hasMorePages", False), where
        expected_events = step.get("events") or []
        assert [event.id for event in events] == [event["id"] for event in expected_events], where
        for event, expected in zip(events, expected_events, strict=True):
            go_time = parse_go_time(expected["occurredAt"])
            assert event.occurred_at == go_time.to_datetime(), where
            payload = json.dumps(expected["payload"])
            assert decode_database_row_changed_event_payload(payload, payload.encode()) == event.payload, (
                where
            )


def seen_rows_inputs(name: str) -> list[tuple[str, str]]:
    """Rebuild the Go generator's rows, as each fixture's description states."""
    base = parse_go_time("2026-11-02T09:00:00Z")
    if name == "sharedSeconds":
        rows = [(base.add((index % 90) * 1_000_000_000), f"row-{index}") for index in range(2100)]
        rows.append((parse_go_time("2026-11-02T08:55:00Z"), "too-old"))
    else:
        rows = [(base.add(index * 50_000_000), f"row-{index}") for index in range(2000)]
    return [(time.format_rfc3339_nano(), row_id) for time, row_id in rows]


@pytest.mark.parametrize("fixture", FIXTURES["seenRows"], ids=lambda fixture: fixture["name"])
def test_seen_rows_are_bounded_and_encoded_as_go_encodes_them(fixture: dict[str, Any]) -> None:
    seen = SeenRows([])
    for at, row_id in seen_rows_inputs(fixture["name"]):
        seen.add(parse_go_time(at), row_id)
    high_water = parse_go_time(fixture["highWater"])
    token = encode_database_row_cursor(
        DatabaseRowCursor(
            DATABASE_ROW_CURSOR_VERSION, DATA_SOURCE_ID, high_water, seen=seen.groups_within(high_water)
        )
    )
    assert token.startswith(fixture["tokenPrefix"])
    assert len(token.encode()) == fixture["tokenBytes"] < 64 * 1024
    assert hashlib.sha256(token.encode()).hexdigest() == fixture["tokenSha256"]
