# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable poll Trigger: a change-log provider polled with a durable cursor into a durable inbox.

It runs offline: the provider is an httpx mock transport and project storage is in memory. An
application binds `project.poll_cursor_store(key)` and `project.trigger_inbox(key)` instead, so a
restarted replica resumes from the committed cursor, and routes events with
`new_dex_flow_trigger_target`, which starts one Flow per event ID.
"""

import asyncio
import json
import logging
from collections.abc import Awaitable, Callable
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from typing import Any

import httpx

from dex_connectors.sdk import (
    ConnectionRef,
    TriggerBindingRef,
    TriggerCancellation,
    TriggerContext,
    TriggerDefinition,
    TriggerDeliveryAbortedError,
    TriggerEvent,
    TriggerRef,
)
from dex_connectors.sdk.pollingtrigger import (
    BindingPollerConfig,
    ChangePage,
    Cursor,
    PollRunner,
    PollRunnerConfig,
    PollSchedule,
    mark_cursor_invalid,
    mark_rate_limited,
    new_binding_poller,
    wait_for_poll_request,
)
from dex_connectors.sdk.projectconfig import (
    InMemoryObjectStore,
    PollCursorKey,
    PollCursorStore,
    Scope,
    TriggerInbox,
    TriggerInboxKey,
    new_durable_trigger_target,
    parse_trigger_payload_json,
)
from dex_connectors.sdk.providerhttp import parse_retry_after

CONNECTION = ConnectionRef("changelog", "main")
DEFINITION = TriggerDefinition(TriggerRef("changelog", "itemChanged"), "Receive a change-log item change.")
BINDING = TriggerBindingRef(CONNECTION, DEFINITION.trigger, "sync")
ItemChanged = dict[str, str | int]


class ChangeLogProvider:
    """A provider with sequence-numbered changes behind `/latest` and `/changes?after=&limit=`."""

    def __init__(self) -> None:
        self.changes: list[ItemChanged] = []

    def add_change(self, item_id: str) -> None:
        self.changes.append({"itemId": item_id, "sequence": len(self.changes) + 1})

    def handle(self, request: httpx.Request) -> httpx.Response:
        if request.url.path == "/latest":
            return httpx.Response(200, json={"latest": len(self.changes)})
        after = int(request.url.params.get("after", "-1"))
        if after < 0 or after > len(self.changes):
            return httpx.Response(410)
        end = min(after + int(request.url.params["limit"]), len(self.changes))
        return httpx.Response(
            200, json={"changes": self.changes[after:end], "hasMore": end < len(self.changes)}
        )


@dataclass(frozen=True, slots=True)
class ChangeLogFetcher:
    """The connector side: maps provider statuses to the kit's signals; event IDs are pure."""

    client: httpx.AsyncClient
    page_size: int = 100

    async def seed_cursor(self, context: TriggerContext) -> Cursor:
        """Start at the provider's latest sequence, so existing changes are not delivered."""
        latest = await self._get(context, "/latest", {})
        return Cursor(str(latest["latest"]))

    async def fetch_changes(
        self, context: TriggerContext, cursor: Cursor
    ) -> ChangePage[TriggerEvent[ItemChanged]]:
        """Return the changes after the cursor's sequence; each event ID is "<itemId>@<sequence>"."""
        listed = await self._get(context, "/changes", {"after": cursor.token, "limit": str(self.page_size)})
        events = [
            TriggerEvent(f"{change['itemId']}@{change['sequence']}", datetime.now(UTC), change)
            for change in listed["changes"]
        ]
        next_token = str(listed["changes"][-1]["sequence"]) if listed["changes"] else cursor.token
        return ChangePage(events, Cursor(next_token), bool(listed["hasMore"]))

    async def _get(self, context: TriggerContext, path: str, params: dict[str, str]) -> dict[str, Any]:
        await wait_for_poll_request(context)
        response = await self.client.get(path, params=params)
        if response.status_code == 410:
            raise mark_cursor_invalid(RuntimeError("change-log cursor expired"))
        if response.status_code == 429:
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            raise mark_rate_limited(RuntimeError("change log rate limited"), timedelta(seconds=retry_after))
        response.raise_for_status()
        listed: dict[str, Any] = json.loads(response.content)
        return listed


def decode_stored_change(payload_json: str, payload_bytes: bytes) -> ItemChanged:
    """Decode a replayed change; anything but an item ID and sequence is refused."""
    payload = parse_trigger_payload_json(payload_bytes)
    if not isinstance(payload, dict) or not isinstance(payload.get("itemId"), str):
        raise ValueError("stored change payload is invalid")
    return {"itemId": payload["itemId"], "sequence": int(payload["sequence"])}


class ItemSyncRecorder:
    """Stands in for `new_dex_flow_trigger_target`, which would start one itemSyncFlow per event."""

    def __init__(self) -> None:
        self.event_ids: list[str] = []

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[ItemChanged]) -> None:
        """Consume the event."""
        self.event_ids.append(event.id)


def new_item_runner(
    client: httpx.AsyncClient, recorder: ItemSyncRecorder
) -> tuple[PollRunner, TriggerInbox, PollCursorStore]:
    """Wire one durable binding poller and the runner of its connection."""
    objects, scope = InMemoryObjectStore(), Scope("example", "preview")
    key = TriggerInboxKey("changelog", CONNECTION.name, DEFINITION.trigger.trigger_name, BINDING.name)
    # An application uses project.trigger_inbox(key) and project.poll_cursor_store(...).
    inbox = TriggerInbox(objects, scope, key)
    cursor = PollCursorStore(objects, scope, PollCursorKey.from_trigger_inbox_key(key))
    target = new_durable_trigger_target(inbox, recorder, decode_payload=decode_stored_change)
    poller = new_binding_poller(
        BindingPollerConfig(DEFINITION, BINDING, cursor, ChangeLogFetcher(client), target)
    )
    schedule = PollSchedule(timedelta(seconds=1), maximum_backoff=timedelta(minutes=1))
    return PollRunner(PollRunnerConfig(CONNECTION, schedule, [poller])), inbox, cursor


async def wait_until(condition_met: Callable[[], Awaitable[bool]]) -> None:
    """Poll a condition for up to ten seconds."""
    async with asyncio.timeout(10):
        while not await condition_met():
            await asyncio.sleep(0.01)


async def main() -> None:
    """Seed past an existing change, deliver two new ones, and leave the inbox empty."""
    logging.basicConfig()
    logging.getLogger("dex_connectors.sdk.trigger").setLevel(logging.INFO)
    provider = ChangeLogProvider()
    provider.add_change("item-0")
    transport = httpx.MockTransport(provider.handle)
    async with httpx.AsyncClient(transport=transport, base_url="https://changelog.example") as client:
        recorder = ItemSyncRecorder()
        runner, inbox, cursor = new_item_runner(client, recorder)
        cancellation = TriggerCancellation()
        # Replays the inbox, seeds on first start, then polls until cancelled.
        running = asyncio.create_task(runner.run(TriggerContext(cancellation)))

        async def is_seeded() -> bool:
            return (await cursor.read_poll_cursor()).state is not None

        async def is_delivered() -> bool:
            return len(recorder.event_ids) >= 2 and not await inbox.pending()

        try:
            await wait_until(is_seeded)
            provider.add_change("item-1")
            provider.add_change("item-2")
            await wait_until(is_delivered)
            if recorder.event_ids != ["item-1@2", "item-2@3"] or await inbox.pending():
                raise RuntimeError(f"poll Trigger example delivered {recorder.event_ids}")
            print("delivered", *recorder.event_ids)
        finally:
            cancellation.cancel()
            try:
                await running
            except TriggerDeliveryAbortedError:
                pass


if __name__ == "__main__":
    asyncio.run(main())
