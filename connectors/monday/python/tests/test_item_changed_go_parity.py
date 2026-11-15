# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go itemChanged fetcher's recorded outputs and require identical cursors and event IDs.

The fixture was recorded from the Go connector's itemChangeFetcher: each step serves raw GraphQL pages
to one fetcher that lives across the case's steps, and each step's cursor token and snapshot entries
must match Go byte for byte, so Go and Python replicas share one poll cursor document.
"""

import hashlib
import json
from dataclasses import replace
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
from dex_connectors.sdk.pollingtrigger import Cursor
from dex_connectors.sdk.projectconfig import KeyedSnapshot

from dex_connectors.monday import Client, Config, Credentials, ItemChangedTriggerConfiguration
from dex_connectors.monday._item_changed_fetcher import ItemChangeFetcher
from dex_connectors.monday.triggers import ITEM_CHANGED_TRIGGER_DEFINITION

FIXTURE = json.loads((Path(__file__).parent / "fixtures" / "item-changed-go-fetcher-cases.json").read_text())
REFERENCE = ConnectionRef("monday", "monday-board-watch")


def _entries_digest(snapshot: KeyedSnapshot) -> str:
    lines = "".join(
        f"{entry.key}\t{entry.hash}\t{entry.seen}\t{(entry.value or b'').decode()}\n"
        for entry in snapshot.entries
    )
    return hashlib.sha256(lines.encode()).hexdigest()


def _assert_cursor(step: dict[str, Any], cursor: Cursor) -> None:
    expected = step["cursor"]
    assert cursor.token == expected["token"], step["name"]
    entries = cursor.snapshot.entries if cursor.snapshot is not None else ()
    assert len(entries) == expected["entryCount"], step["name"]
    if "entriesSha256" in expected:
        assert cursor.snapshot is not None
        assert _entries_digest(cursor.snapshot) == expected["entriesSha256"], step["name"]
        return
    actual = [
        {"key": entry.key, "hash": entry.hash, "seen": entry.seen, "value": (entry.value or b"").decode()}
        for entry in entries
    ]
    assert actual == expected.get("entries", []), step["name"]


def _committed(cursor: Cursor, commit_count: int) -> Cursor:
    entries = cursor.snapshot.entries if cursor.snapshot is not None else ()
    stamped = tuple(replace(entry, seen=commit_count) if entry.seen == 0 else entry for entry in entries)
    return Cursor(cursor.token, KeyedSnapshot(stamped))


def _go_rfc3339(instant: datetime) -> str:
    return instant.astimezone(UTC).strftime("%Y-%m-%dT%H:%M:%S.%f").rstrip("0").rstrip(".") + "Z"


@pytest.mark.parametrize("case", FIXTURE["cases"], ids=lambda case: case["name"])
async def test_item_change_fetcher_matches_the_go_recording(case: dict[str, Any]) -> None:
    served: list[dict[str, Any]] = []
    step: dict[str, Any] = {}

    def answer(request: httpx.Request) -> httpx.Response:
        body = json.loads(request.content)
        served.append({"operation": body["query"].split()[1].split("(")[0], "variables": body["variables"]})
        return httpx.Response(200, content=step["pages"][len(served) - 1].encode())

    context = TriggerContext(TriggerCancellation())
    credentials = Credentials("personal-api-token", SecretString("monday-fake-token"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        client = Client(
            Config(poll_interval_seconds=30),
            StaticCredentialProvider({REFERENCE: credentials}),
            http_client,
            endpoint="https://monday.example/v2",
        )
        fetcher = ItemChangeFetcher(
            client,
            REFERENCE,
            ITEM_CHANGED_TRIGGER_DEFINITION.trigger,
            ItemChangedTriggerConfiguration(FIXTURE["boardId"]),
        )
        cursor = Cursor()
        for index, step in enumerate(case["steps"]):
            served.clear()
            if step.get("seed"):
                next_cursor = await fetcher.seed_cursor(context)
            else:
                page = await fetcher.fetch_changes(context, cursor)
                next_cursor = page.next_cursor
                assert page.has_more_pages == step["hasMorePages"], step["name"]
                assert len(page.changes) == step["changeCount"], step["name"]
                listed = list(page.changes)
                if len(listed) > 10:
                    listed = listed[:3] + listed[-1:]
                assert len(listed) == len(step["changes"]), step["name"]
                for change, expected in zip(listed, step["changes"], strict=True):
                    go_payload = json.loads(expected["payload"])
                    assert change.id == expected["id"], step["name"]
                    assert _go_rfc3339(change.occurred_at) == expected["occurredAt"], step["name"]
                    assert change.payload.board_id == go_payload["boardId"]
                    assert change.payload.change_kind == go_payload["changeKind"]
                    assert change.payload.item.id == go_payload["item"]["id"]
                    assert change.payload.item.name == go_payload["item"]["name"]
                    assert [column.value for column in change.payload.item.column_values] == [
                        column.get("value") for column in go_payload["item"].get("columnValues", [])
                    ]
            assert served == step["requests"], step["name"]
            _assert_cursor(step, next_cursor)
            cursor = _committed(next_cursor, index + 1)
