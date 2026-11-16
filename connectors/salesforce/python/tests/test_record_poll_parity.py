# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay a Go fetcher scenario: cursor tokens and event IDs byte for byte, payloads by value."""

import json
from collections.abc import AsyncIterator
from datetime import timedelta
from pathlib import Path
from typing import Any

import httpx
import pytest
from dex import CodecRegistry
from dex_connectors.sdk.pollingtrigger import Cursor

from dex_connectors.salesforce import (
    RecordModifiedEvent,
    RecordModifiedTriggerConfiguration,
    decode_record_modified_event_payload,
)
from tests.test_record_modified_fetcher import CONTEXT, new_poll_test_fetcher

FIXTURE = json.loads((Path(__file__).parent / "fixtures" / "go-record-poll-cursors.json").read_text())


@pytest.fixture
async def http_clients() -> AsyncIterator[list[httpx.AsyncClient]]:
    clients: list[httpx.AsyncClient] = []
    yield clients
    for client in clients:
        await client.aclose()


def go_compact(value: Any) -> str:
    """Re-serialize a fixture value compactly, keeping Go's member order."""
    return json.dumps(value, separators=(",", ":"), ensure_ascii=False)


async def test_python_fetcher_replays_the_go_scenario_byte_for_byte(
    http_clients: list[httpx.AsyncClient],
) -> None:
    configuration = RecordModifiedTriggerConfiguration("Opportunity", ("Name", "StageName"))
    fetcher, org, clock = new_poll_test_fetcher(http_clients, 2, configuration)
    existing = org.insert("Opportunity", {"Name": "Renewal", "StageName": "Prospecting"})
    seed = await fetcher.seed_cursor(CONTEXT)
    assert seed.token == FIXTURE["seed"]
    org.insert("Opportunity", {"Name": "Expansion", "StageName": "Qualification"})
    org.update(existing, {"StageName": "Closed Won"})
    for record_id in org.insert_batch("Opportunity", 3):
        org.records[record_id].fields["Name"] = "Bulk"
    clock.advance(timedelta(seconds=1))
    codec = CodecRegistry().resolve(RecordModifiedEvent)
    for recorded in FIXTURE["pages"]:
        page = await fetcher.fetch_changes(CONTEXT, Cursor(recorded["cursor"]))
        assert page.next_cursor.token == recorded["next"]
        assert page.has_more_pages == recorded["hasMorePages"]
        assert len(page.changes) == len(recorded["events"])
        for change, expected in zip(page.changes, recorded["events"], strict=True):
            assert change.id == expected["id"]
            assert change.occurred_at.strftime("%Y-%m-%dT%H:%M:%SZ") == expected["occurredAt"]
            payload_json = go_compact(expected["payload"])
            # The Dex codec sorts members, so the payload matches Go's by value rather than by byte.
            assert json.loads(str(codec.encode(change.payload).data)) == expected["payload"]
            assert decode_record_modified_event_payload(payload_json, payload_json.encode()) == change.payload
