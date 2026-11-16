# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded pageChanged fetcher scenarios and require identical requests, cursors, and events."""

import json
from datetime import timedelta
from pathlib import Path
from typing import Any

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider, TriggerContext
from dex_connectors.sdk.pollingtrigger import Cursor, RateLimitedError, is_cursor_invalid
from dex_connectors.sdk.projectconfig import KeyedSnapshot, KeyedSnapshotEntry

from dex_connectors.atlassian_confluence import (
    Client,
    Config,
    Credentials,
    PageChangedEvent,
    PageChangedTriggerConfiguration,
    PageSummary,
    decode_page_changed_event_payload,
    page_change_event_id,
)
from dex_connectors.atlassian_confluence._go_time import parse_go_time
from dex_connectors.atlassian_confluence._page_change_cursor import (
    SeenPageVersion,
    encode_seen_page_version,
    seen_page_hash,
)
from dex_connectors.atlassian_confluence._page_change_fetcher import PageChangeFetcher

GO_CASES = json.loads((Path(__file__).parent / "fixtures/go-page-change-cases.json").read_text())
CONNECTION = ConnectionRef("atlassian", "confluence-trigger-test")
# The Go recordings use this bearer token; the fake answers echo it to test secret reflection.
ACCESS_TOKEN = "fixture-" + "access"


class ScriptedSite:
    """Answers each request with the next recorded response and records the request."""

    def __init__(self) -> None:
        self.responses: list[dict[str, Any]] = []
        self.requests: list[dict[str, str]] = []

    def handle(self, request: httpx.Request) -> httpx.Response:
        self.requests.append({"path": request.url.path, "query": request.url.query.decode()})
        if not self.responses:
            return httpx.Response(599)
        response = self.responses.pop(0)
        return httpx.Response(
            response["status"], headers=response.get("headers") or {}, content=response["body"].encode()
        )


def to_cursor(recorded: dict[str, Any]) -> Cursor:
    entries = tuple(
        KeyedSnapshotEntry(
            entry["key"], entry["hash"], entry["seen"], entry.get("value", "").encode() or None
        )
        for entry in recorded["snapshot"]
    )
    return Cursor(recorded["token"], KeyedSnapshot(entries))


def from_cursor(cursor: Cursor) -> dict[str, Any]:
    entries = cursor.snapshot.entries if cursor.snapshot is not None else ()
    snapshot = [{"key": entry.key, "hash": entry.hash, "seen": entry.seen} for entry in entries]
    for recorded, entry in zip(snapshot, entries, strict=True):
        if entry.value is not None:
            recorded["value"] = entry.value.decode()
    return {"token": cursor.token, "snapshot": snapshot}


def payload_json(payload: PageChangedEvent) -> Any:
    return json.loads(str(dex.CodecRegistry().resolve(PageChangedEvent).encode(payload).data))


def microsecond_payload(payload: dict[str, Any]) -> dict[str, Any]:
    """Python datetimes keep microseconds, so a nanosecond page time is compared truncated."""
    instant = parse_go_time(payload["page"]["lastModifiedAt"])
    page = {**payload["page"], "lastModifiedAt": dex_time(instant.to_datetime())}
    return {**payload, "page": page}


def dex_time(moment: Any) -> str:
    """Encode a datetime as the SDK codec writes a payload time."""
    encoded = dex.CodecRegistry().resolve(PageSummary).encode(PageSummary(last_modified_at=moment)).data
    return str(json.loads(str(encoded))["lastModifiedAt"])


async def run_step(
    site: ScriptedSite, client: Client, fetcher: PageChangeFetcher, step: dict[str, Any]
) -> Any:
    site.responses, site.requests = list(step["responses"]), []
    clock = parse_go_time(step["now"]).to_datetime()
    client.now = lambda: clock
    context = TriggerContext()
    try:
        if step["action"] == "seed":
            return {"cursor": from_cursor(await fetcher.seed_cursor(context))}
        page = await fetcher.fetch_changes(context, to_cursor(step["cursor"]))
    except RateLimitedError as error:
        result: dict[str, Any] = {"error": "rateLimited"}
        if error.retry_after:
            result["retryAfterMs"] = error.retry_after // timedelta(milliseconds=1)
        return result
    except Exception as error:
        return {"error": "cursorInvalid" if is_cursor_invalid(error) else "failed"}
    result = {"cursor": from_cursor(page.next_cursor)}
    if page.has_more_pages:
        result["hasMore"] = True
    if page.changes:
        result["events"] = [
            {
                "id": change.id,
                "occurredAt": dex_time(change.occurred_at),
                "payload": payload_json(change.payload),
            }
            for change in page.changes
        ]
    return result


@pytest.mark.parametrize("scenario", GO_CASES["scenarios"], ids=lambda scenario: scenario["name"])
async def test_fetcher_matches_the_go_recording(scenario: dict[str, Any]) -> None:
    site = ScriptedSite()
    credentials = Credentials(
        "client-id", SecretString("client-secret"), SecretString(ACCESS_TOKEN), SecretString("r")
    )
    client = Client(
        Config(scenario["cloudId"], endpoint="http://127.0.0.1:12345"),
        StaticCredentialProvider({CONNECTION: credentials}),
        httpx.AsyncClient(transport=httpx.MockTransport(site.handle)),
    )
    fetcher = PageChangeFetcher(
        client, CONNECTION, PageChangedTriggerConfiguration(space_key=scenario["spaceKey"])
    )
    for index, step in enumerate(scenario["steps"]):
        result = await run_step(site, client, fetcher, step)
        expected = dict(step["result"])
        if "events" in expected:
            expected["events"] = [
                {
                    **event,
                    "occurredAt": dex_time(parse_go_time(event["occurredAt"]).to_datetime()),
                    "payload": microsecond_payload(event["payload"]),
                }
                for event in expected["events"]
            ]
        assert site.requests == step["requests"], f"step {index} requests"
        assert result == expected, f"step {index} result"
        for event in step["result"].get("events", []):
            decoded = decode_page_changed_event_payload(json.dumps(event["payload"]), b"")
            assert decoded.page.id == event["payload"]["page"]["id"]


@pytest.mark.parametrize("item", GO_CASES["seenItems"], ids=lambda item: item["pageId"])
def test_seen_versions_and_event_ids_match_go(item: dict[str, Any]) -> None:
    instant = parse_go_time(item["lastModifiedAt"])
    seen = SeenPageVersion(item["versionNumber"], instant.utc())
    assert seen_page_hash(item["pageId"], item["versionNumber"]) == item["hash"]
    assert encode_seen_page_version(seen).decode() == item["value"]
    cloud_id = "11223344-a1b2-4b33-8c44-def123456789"
    assert page_change_event_id(cloud_id, item["pageId"], item["versionNumber"]) == item["eventId"]
