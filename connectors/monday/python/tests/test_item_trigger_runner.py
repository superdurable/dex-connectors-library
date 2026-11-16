# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""itemChanged runners, ported from the Go connector's item_trigger_runner tests."""

import asyncio
import json
from collections.abc import AsyncIterator, Awaitable, Callable
from contextlib import asynccontextmanager
from datetime import timedelta
from pathlib import Path
from typing import Any, cast

import httpx
import pytest
from dex_connectors.sdk import TriggerCancellation, TriggerContext, TriggerDeliveryAbortedError, TriggerEvent
from dex_connectors.sdk.projectconfig import (
    Configuration,
    InMemoryObjectStore,
    LoadedProject,
    PollCursorKey,
    PollCursorStore,
    Scope,
    TriggerInbox,
    TriggerInboxKey,
)

from dex_connectors.monday import (
    Connection,
    ItemChangedEvent,
    ItemChangedTriggerConfiguration,
    ItemChangedTriggerRoute,
    ItemChangeKind,
    ProjectItemChangedTriggerRoute,
    decode_item_changed_event_payload,
    new_connection,
    new_item_changed_trigger,
    new_item_trigger_runner,
    new_project_item_trigger_runner,
)
from tests.monday_board_fake import MondayBoardFake
from tests.test_item_changed_fetcher import BOARD_ID, REFERENCE, new_client, watermark_token

SCOPE = Scope("monday-trigger-test", "live")
KEY = TriggerInboxKey("monday", REFERENCE.name, "itemChanged", "boardChanges")
BOARD = ItemChangedTriggerConfiguration(BOARD_ID)


class QueueTarget:
    """Consumes each event into a queue."""

    def __init__(self) -> None:
        self.events: asyncio.Queue[TriggerEvent[ItemChangedEvent]] = asyncio.Queue()

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[ItemChangedEvent]) -> None:
        """Record the event."""
        await self.events.put(event)

    async def next_event(self) -> TriggerEvent[ItemChangedEvent]:
        """Wait up to ten seconds for the next event."""
        return await asyncio.wait_for(self.events.get(), 10)


class ProjectStub:
    """The parts of LoadedProject the durable runner reads, over one in-memory object store."""

    def __init__(self, trigger_bindings: tuple[dict[str, Any], ...] = ()) -> None:
        self.objects = InMemoryObjectStore()
        self.configuration = Configuration(SCOPE, 1, (), trigger_bindings)

    def trigger_inbox(self, key: TriggerInboxKey) -> TriggerInbox:
        return TriggerInbox(self.objects, SCOPE, key)

    def poll_cursor_store(self, key: PollCursorKey) -> PollCursorStore:
        return PollCursorStore(self.objects, SCOPE, key)


def saved_binding(configuration: dict[str, Any]) -> dict[str, Any]:
    return {
        "connectorId": "monday",
        "connectionName": REFERENCE.name,
        "triggerName": "itemChanged",
        "bindingName": "boardChanges",
        "configuration": configuration,
    }


@asynccontextmanager
async def connection_for(board: MondayBoardFake) -> AsyncIterator[Connection]:
    """A connection whose requests the board answers."""
    async with httpx.AsyncClient(transport=httpx.MockTransport(board.handle)) as http_client:
        yield new_connection(new_client(http_client), REFERENCE)


async def eventually(condition: Callable[[], Awaitable[bool]]) -> None:
    """Poll condition for up to ten seconds."""
    async with asyncio.timeout(10):
        while not await condition():
            await asyncio.sleep(0.01)


@asynccontextmanager
async def running(runner: Any) -> AsyncIterator[None]:
    """Run a runner until the block exits, then require it to stop through cancellation."""
    cancellation = TriggerCancellation()
    task = asyncio.create_task(runner.run(TriggerContext(cancellation)))
    try:
        yield
    finally:
        cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await task


async def test_runner_rejects_incomplete_routes() -> None:
    board = MondayBoardFake(BOARD_ID)
    target = QueueTarget()
    async with connection_for(board) as connection:
        for routes in {
            "none": [],
            "no binding": [ItemChangedTriggerRoute("", BOARD, target)],
            "no target": [ItemChangedTriggerRoute("boardChanges", BOARD, cast(Any, None))],
            "no board": [ItemChangedTriggerRoute("boardChanges", ItemChangedTriggerConfiguration(), target)],
            "duplicate": [
                ItemChangedTriggerRoute("boardChanges", BOARD, target),
                ItemChangedTriggerRoute("boardChanges", BOARD, target),
            ],
        }.values():
            with pytest.raises(ValueError):
                new_item_trigger_runner(connection, routes)
    with pytest.raises(ValueError, match="monday.com client"):
        new_item_trigger_runner(
            cast(Connection, object()), [ItemChangedTriggerRoute("boardChanges", BOARD, target)]
        )


async def test_durable_runner_requires_a_saved_board() -> None:
    async with connection_for(MondayBoardFake(BOARD_ID)) as connection:
        with pytest.raises(ValueError, match="has no saved board"):
            new_project_item_trigger_runner(
                cast(LoadedProject, ProjectStub()),
                connection,
                [ProjectItemChangedTriggerRoute("boardChanges", QueueTarget())],
            )
        invalid = ProjectStub((saved_binding({"boardId": BOARD_ID, "unknown": True}),))
        with pytest.raises(ValueError, match="boardChanges"):
            new_project_item_trigger_runner(
                cast(LoadedProject, invalid),
                connection,
                [ProjectItemChangedTriggerRoute("boardChanges", QueueTarget())],
            )


async def test_durable_runner_loads_the_saved_board_and_commits_after_recording() -> None:
    board = MondayBoardFake(BOARD_ID)
    board.create("5550001", "Existing request")
    project = ProjectStub((saved_binding({"boardId": BOARD_ID}),))
    target = QueueTarget()
    cursor = project.poll_cursor_store(PollCursorKey.from_trigger_inbox_key(KEY))
    async with connection_for(board) as connection:
        runner = new_project_item_trigger_runner(
            cast(LoadedProject, project), connection, [ProjectItemChangedTriggerRoute("boardChanges", target)]
        )

        async def has_token(token: str, entry_count: int) -> bool:
            state = (await cursor.read_poll_cursor()).state
            return (
                state is not None
                and state.token == token
                and state.snapshot is not None
                and len(state.snapshot.entries) == entry_count
            )

        async with running(runner):
            await eventually(lambda: has_token(watermark_token(board.updated_at("5550001")), 1))
            board.create("5550002", "New request")
            runner.wake_poller("boardChanges")
            event = await target.next_event()
            assert event.payload.item.id == "5550002", "the baseline item is never delivered"
            assert event.payload.change_kind == ItemChangeKind.CREATED
            await eventually(lambda: has_token(watermark_token(board.updated_at("5550002")), 2))
            inbox = project.trigger_inbox(KEY)

            async def is_drained() -> bool:
                return not await inbox.pending()

            await eventually(is_drained)


async def test_single_trigger_records_a_baseline_on_start() -> None:
    board = MondayBoardFake(BOARD_ID)
    board.create("5550001", "Existing request")
    target = QueueTarget()
    async with connection_for(board) as connection:
        # A short interval keeps the test fast; Client rejects one under the 30s minimum.
        connection._client.poll_interval = timedelta(seconds=1)  # type: ignore[union-attr]
        trigger = new_item_changed_trigger(
            connection=connection, binding_name="boardChanges", configuration=BOARD, target=target
        )

        async def has_listed() -> bool:
            return bool(board.requests)

        async with running(trigger):
            await eventually(has_listed)
            assert board.requests[0].operation == "ListBoardItems"
            board.set_status("5550001", "Done")
            event = await target.next_event()
            assert event.payload.change_kind == ItemChangeKind.UPDATED
        with pytest.raises(ValueError, match="boardId"):
            new_item_changed_trigger(
                connection=connection,
                binding_name="boardChanges",
                configuration=ItemChangedTriggerConfiguration(),
                target=target,
            )


def test_inbox_payload_decodes_a_go_written_event() -> None:
    fixture = json.loads(
        (Path(__file__).parent / "fixtures" / "item-changed-go-fetcher-cases.json").read_text()
    )
    go_payload = fixture["cases"][0]["steps"][2]["changes"][1]["payload"]
    event = decode_item_changed_event_payload(go_payload, go_payload.encode())
    assert event.change_kind == ItemChangeKind.UPDATED
    assert event.item.name == 'Fix <b>&</b> "quoted"\u2028 café 😀'
    assert event.item.column_values[0].value == {"index": 2, "post_id": None}
    with pytest.raises(ValueError):
        decode_item_changed_event_payload('{"boardId":"1","changeKind":"archived","item":{"id":"1"}}', b"")
