# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go meetingChanged fetcher's recorded outputs and require identical cursors and event IDs.

The fixture was recorded from the Go connector's meetingListFetcher: each step serves raw List meetings
pages, and each step's cursor token and snapshot entries must match Go byte for byte, so Go and Python
replicas share one poll cursor document.
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

from dex_connectors.zoom import (
    MEETING_CHANGED_TRIGGER_DEFINITION,
    Client,
    Config,
    Credentials,
    MeetingChangedTriggerConfiguration,
)
from dex_connectors.zoom._meeting_list_fetcher import MeetingListFetcher

FIXTURE = json.loads(
    (Path(__file__).parent / "fixtures" / "meeting-changed-go-fetcher-cases.json").read_text()
)
REFERENCE = ConnectionRef("zoom", "zoom-watcher")


def _credentials() -> Credentials:
    empty = SecretString("")
    return Credentials("", empty, SecretString("zoom-trigger-token"), empty)


def _entries_digest(snapshot: KeyedSnapshot) -> str:
    digest = hashlib.sha256()
    for entry in snapshot.entries:
        value = (entry.value or b"").decode()
        digest.update(f"{entry.key}\t{entry.hash}\t{entry.seen}\t{value}\n".encode())
    return digest.hexdigest()


def _assert_cursor(step: dict[str, Any], cursor: Cursor) -> None:
    expected = step["cursor"]
    assert cursor.token == expected["token"], step["name"]
    assert cursor.snapshot is not None
    assert len(cursor.snapshot.entries) == expected["entryCount"], step["name"]
    if "entriesSha256" in expected:
        assert _entries_digest(cursor.snapshot) == expected["entriesSha256"], step["name"]
        return
    actual = [
        {"key": entry.key, "hash": entry.hash, "seen": entry.seen, "value": (entry.value or b"").decode()}
        for entry in cursor.snapshot.entries
    ]
    assert actual == expected.get("entries", []), step["name"]


def _go_rfc3339_nano(instant: datetime) -> str:
    text = instant.astimezone(UTC).strftime("%Y-%m-%dT%H:%M:%S.%f").rstrip("0").rstrip(".")
    return text + "Z"


def _committed(cursor: Cursor, commit_count: int) -> Cursor:
    assert cursor.snapshot is not None
    entries = tuple(
        replace(entry, seen=commit_count) if entry.seen == 0 else entry for entry in cursor.snapshot.entries
    )
    return Cursor(cursor.token, KeyedSnapshot(entries))


@pytest.mark.asyncio
async def test_meeting_list_fetcher_matches_the_go_recording() -> None:
    steps: list[dict[str, Any]] = FIXTURE["steps"]
    current = 0

    def answer(request: httpx.Request) -> httpx.Response:
        token = request.url.params.get("next_page_token", "")
        assert request.url.params["type"] == "scheduled"
        assert request.url.params["page_size"] == "300"
        page = int(token.removeprefix("p")) if token else 0
        return httpx.Response(200, content=steps[current]["pages"][page].encode())

    observed_at = datetime.fromisoformat(FIXTURE["observedAt"].replace("Z", "+00:00"))
    context = TriggerContext(TriggerCancellation())
    cursor = Cursor()
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        client = Client(
            Config(endpoint="https://zoom.example/v2"),
            StaticCredentialProvider({REFERENCE: _credentials()}),
            http_client,
            now=lambda: observed_at,
        )
        for index, step in enumerate(steps):
            current = index
            configuration = MeetingChangedTriggerConfiguration(
                change_kinds=tuple(step.get("changeKinds", ()))
            )
            fetcher = MeetingListFetcher(
                client, REFERENCE, MEETING_CHANGED_TRIGGER_DEFINITION.trigger, configuration
            )
            if step.get("seed"):
                next_cursor = await fetcher.seed_cursor(context)
            else:
                page = await fetcher.fetch_changes(context, cursor)
                next_cursor = page.next_cursor
                assert page.has_more_pages == step["hasMorePages"], step["name"]
                assert len(page.changes) == step["changeCount"], step["name"]
                expected_changes = step["changes"] or []
                listed = list(page.changes)
                if len(listed) > 10:
                    listed = listed[:3] + listed[-1:]
                assert len(listed) == len(expected_changes), step["name"]
                for change, expected in zip(listed, expected_changes, strict=True):
                    go_payload = json.loads(expected["payload"])
                    assert change.id == expected["id"], step["name"]
                    assert _go_rfc3339_nano(change.occurred_at) == expected["occurredAt"], step["name"]
                    assert change.payload.change_kind == go_payload["changeKind"]
                    assert change.payload.meeting.id == go_payload["meeting"]["id"]
                    assert change.payload.meeting.topic == go_payload["meeting"]["topic"]
                    assert change.payload.meeting.start_time == go_payload["meeting"].get("startTime")
                    assert change.payload.meeting.join_url == go_payload["meeting"].get("joinUrl", "")
                    previous = go_payload.get("previousMeeting")
                    if previous is None:
                        assert change.payload.previous_meeting is None
                    else:
                        assert change.payload.previous_meeting is not None
                        assert change.payload.previous_meeting.topic == previous["topic"]
            _assert_cursor(step, next_cursor)
            cursor = _committed(next_cursor, index + 1)
