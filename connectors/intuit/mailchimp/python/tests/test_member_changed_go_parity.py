# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go memberChanged fetcher's recorded outputs and require identical cursors and event IDs.

The fixture was recorded from the Go connector's memberChangeFetcher: each step lists the requests Go sent
and the responses it received, and each step's cursor token, snapshot entries, and change IDs must match Go
byte for byte, so Go and Python replicas share one poll cursor document.
"""

import hashlib
import json
from dataclasses import replace
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

import httpx
import pytest
from dex import CodecRegistry
from dex_connectors.sdk import (
    ConnectionRef,
    SecretString,
    StaticCredentialProvider,
    TriggerCancellation,
    TriggerContext,
)
from dex_connectors.sdk.pollingtrigger import Cursor, CursorInvalidError, RateLimitedError
from dex_connectors.sdk.projectconfig import KeyedSnapshot

from dex_connectors.intuit_mailchimp import (
    MEMBER_CHANGED_TRIGGER_DEFINITION,
    Client,
    Config,
    Credentials,
    MemberChangedEvent,
    MemberChangedTriggerConfiguration,
)
from dex_connectors.intuit_mailchimp._member_change_fetcher import MemberChangeFetcher

FIXTURE = json.loads(
    (Path(__file__).parent / "fixtures" / "member-changed-go-fetcher-cases.json").read_text()
)
REFERENCE = ConnectionRef("mailchimp", "mailchimp-fetcher-test")
AUDIENCE_ID = "57afe96172"
# Split so secret scanners do not read the fake key as a credential.
FAKE_API_KEY = "0123456789abcdef" + "0123456789abcdef" + "-us6"


def _entries_digest(snapshot: KeyedSnapshot) -> str:
    digest = hashlib.sha256()
    for entry in snapshot.entries:
        digest.update(f"{entry.key}\t{entry.hash}\t{entry.seen}\n".encode())
    return digest.hexdigest()


def _assert_cursor(step: dict[str, Any], cursor: Cursor) -> None:
    expected = step["cursor"]
    assert cursor.token == expected["token"], step["name"]
    entries = cursor.snapshot.entries if cursor.snapshot is not None else ()
    assert len(entries) == expected["entryCount"], step["name"]
    if "entriesSha256" in expected:
        assert cursor.snapshot is not None
        assert _entries_digest(cursor.snapshot) == expected["entriesSha256"], step["name"]
        return
    actual = [{"key": entry.key, "hash": entry.hash, "seen": entry.seen} for entry in entries]
    assert actual == expected.get("entries", []), step["name"]


def _go_rfc3339_nano(instant: datetime) -> str:
    text = instant.astimezone(UTC).strftime("%Y-%m-%dT%H:%M:%S.%f").rstrip("0").rstrip(".")
    return text + "Z"


def _committed(cursor: Cursor, commit_count: int) -> Cursor:
    entries = cursor.snapshot.entries if cursor.snapshot is not None else ()
    committed = tuple(replace(entry, seen=commit_count) if entry.seen == 0 else entry for entry in entries)
    return Cursor(cursor.token, KeyedSnapshot(committed))


def _numbers_as_floats(text: str) -> Any:
    # Go decodes merge field numbers into float64, so compare numbers by value.
    return json.loads(text, parse_int=float)


@pytest.mark.asyncio
async def test_member_change_fetcher_matches_the_go_recording() -> None:
    steps: list[dict[str, Any]] = FIXTURE["steps"]
    pending: list[tuple[str, dict[str, Any]]] = []

    def answer(request: httpx.Request) -> httpx.Response:
        assert pending, "Python sent more requests than Go"
        expected_query, response = pending.pop(0)
        assert request.url.path == f"/3.0/lists/{AUDIENCE_ID}/members"
        assert request.url.query.decode() == expected_query
        headers = {"Content-Type": "application/json"}
        if response.get("retryAfter"):
            headers["Retry-After"] = response["retryAfter"]
        return httpx.Response(response["status"], headers=headers, content=response["body"].encode())

    now = datetime.fromisoformat(FIXTURE["now"].replace("Z", "+00:00"))
    context = TriggerContext(TriggerCancellation())
    cursor = Cursor()
    commit_count = 0
    codec = CodecRegistry().resolve(MemberChangedEvent)
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        client = Client(
            Config(),
            StaticCredentialProvider({REFERENCE: Credentials(SecretString(FAKE_API_KEY))}),
            http_client,
            api_base_url="https://mailchimp.example/3.0",
            now=lambda: now,
        )
        for step in steps:
            pending[:] = list(zip(step["requests"], step["responses"], strict=True))
            configuration = MemberChangedTriggerConfiguration(
                audience_id=AUDIENCE_ID, statuses=tuple(step.get("statuses", ()))
            )
            fetcher = MemberChangeFetcher(
                client, REFERENCE, MEMBER_CHANGED_TRIGGER_DEFINITION.trigger, configuration
            )
            if step.get("seed"):
                next_cursor = await fetcher.seed_cursor(context)
            elif step.get("error"):
                expected_error = {
                    "cursorInvalid": CursorInvalidError,
                    "rateLimited": RateLimitedError,
                    "failed": RuntimeError,
                }[step["error"]]
                with pytest.raises(Exception) as raised:
                    await fetcher.fetch_changes(context, cursor)
                assert type(raised.value) is expected_error, step["name"]
                if isinstance(raised.value, RateLimitedError):
                    assert raised.value.retry_after.total_seconds() == step.get("retryAfterSeconds", 0)
                assert FAKE_API_KEY not in str(raised.value)
                assert not pending, step["name"]
                continue
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
                    assert change.id == expected["id"], step["name"]
                    assert _go_rfc3339_nano(change.occurred_at) == expected["occurredAt"], step["name"]
                    encoded = str(codec.encode(change.payload).data)
                    assert _numbers_as_floats(encoded) == _numbers_as_floats(expected["payload"]), step[
                        "name"
                    ]
                    assert codec.decode(codec.encode(change.payload)) == change.payload
            assert not pending, step["name"]
            _assert_cursor(step, next_cursor)
            commit_count += 1
            cursor = _committed(next_cursor, commit_count)
