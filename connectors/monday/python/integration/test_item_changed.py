# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deliver monday.com item changes through the example's Flow target to a real Dex Worker and Flow."""

import asyncio
import os
import tempfile
import time
from collections.abc import AsyncIterator, Callable
from contextlib import asynccontextmanager
from dataclasses import dataclass, field
from datetime import timedelta
from pathlib import Path
from typing import Any, cast

import dex
import httpx
import pytest
from dex_connectors.sdk import (
    ConnectionRef,
    SecretString,
    StaticCredentialProvider,
    TriggerCancellation,
    TriggerContext,
    TriggerDeliveryAbortedError,
    TriggerEvent,
)
from dex_connectors.sdk.pollingtrigger import PollRunner
from dex_connectors.sdk.projectconfig import (
    Configuration,
    InMemoryObjectStore,
    LoadedProject,
    PollCursorKey,
    PollCursorState,
    PollCursorStore,
    Scope,
    TriggerInbox,
    TriggerInboxKey,
)

from dex_connectors.monday import (
    Client,
    Config,
    Connection,
    Credentials,
    ItemChangedEvent,
    ItemChangeKind,
    new_connection,
)
from examples.item_changed.flow import (
    CONNECTION_NAME,
    ITEM_CHANGED_TRIGGER_BINDING,
    ItemChangeFlow,
    ItemChangeOutcome,
    resolve_flow_id,
)
from examples.item_changed.worker import new_item_change_flow_target, new_item_change_trigger_runner
from tests.monday_board_fake import TOKEN, MondayBoardFake

SCOPE = Scope("monday-item-changed", "live")
INBOX_KEY = TriggerInboxKey("monday", CONNECTION_NAME, "itemChanged", ITEM_CHANGED_TRIGGER_BINDING)


class DurableProject:
    """The project storage two Trigger processes share: hosted storage keeps inboxes and cursors."""

    def __init__(self, board_id: str) -> None:
        self.objects = InMemoryObjectStore()
        binding = {
            "connectorId": "monday",
            "connectionName": CONNECTION_NAME,
            "triggerName": "itemChanged",
            "bindingName": ITEM_CHANGED_TRIGGER_BINDING,
            "configuration": {"boardId": board_id},
        }
        self.configuration = Configuration(SCOPE, 1, (), (binding,))

    def trigger_inbox(self, key: TriggerInboxKey) -> TriggerInbox:
        return TriggerInbox(self.objects, SCOPE, key)

    def poll_cursor_store(self, key: PollCursorKey) -> PollCursorStore:
        return PollCursorStore(self.objects, SCOPE, key)

    async def cursor_state(self) -> PollCursorState | None:
        store = self.poll_cursor_store(PollCursorKey.from_trigger_inbox_key(INBOX_KEY))
        return (await store.read_poll_cursor()).state


@dataclass
class CountingTarget:
    """Wraps the example's Flow target and records each delivered event once, in order."""

    inner: Any
    delivered: dict[str, TriggerEvent[ItemChangedEvent]] = field(default_factory=dict)

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[ItemChangedEvent]) -> None:
        await self.inner.handle_trigger(context, event)
        self.delivered.setdefault(event.id, event)


async def eventually(condition: Callable[[], Any], timeout: float = 60) -> None:
    """Poll a sync or async condition until it holds."""
    deadline = time.monotonic() + timeout
    while True:
        result = condition()
        if asyncio.iscoroutine(result):
            result = await result
        if result:
            return
        assert time.monotonic() < deadline, "condition did not converge"
        await asyncio.sleep(0.05)


@asynccontextmanager
async def trigger_process(runner: PollRunner) -> AsyncIterator[PollRunner]:
    """Run one Trigger process until the block exits; stopping it models a process exit."""
    cancellation = TriggerCancellation()
    task = asyncio.create_task(runner.run(TriggerContext(cancellation)))
    try:
        yield runner
    finally:
        cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await task


@dataclass
class Environment:
    provider: MondayBoardFake
    project: DurableProject
    target: CountingTarget
    dex_client: dex.AsyncClient
    connection: Connection
    # Keeps item IDs, and so Flow IDs, unique per run against a shared Dex server.
    item_prefix: int

    def item_id(self, index: int) -> str:
        return str(self.item_prefix * 100_000 + index)

    def new_runner(self) -> PollRunner:
        return new_item_change_trigger_runner(cast(LoadedProject, self.project), self.connection, self.target)

    async def wait_for_watermark(self, item_id: str) -> None:
        watermark = self.provider.updated_at(item_id).strftime("%Y-%m-%dT%H:%M:%SZ")
        expected = f'{{"boardId":"{self.provider.board_id}","watermark":"{watermark}"}}'

        async def is_at_watermark() -> bool:
            state = await self.project.cursor_state()
            return state is not None and state.token == expected

        await eventually(is_at_watermark)

    async def wait_for_outcomes(self, count: int) -> dict[str, ItemChangeOutcome]:
        """Wait for count deliveries, then each Flow by item ID; a later change replaces an earlier one."""
        await eventually(lambda: len(self.target.delivered) >= count, timeout=90)
        outcomes: dict[str, ItemChangeOutcome] = {}
        for event in list(self.target.delivered.values()):
            result = await asyncio.wait_for(
                self.dex_client.wait_for_flow(resolve_flow_id(event), timedelta(seconds=30)), 40
            )
            assert result.status == dex.FlowStatus.COMPLETED, result.error_message
            outcome = result.single_output(ItemChangeOutcome)
            assert outcome.item_id == event.payload.item.id
            outcomes[outcome.item_id] = outcome
        return outcomes


@asynccontextmanager
async def item_changed_environment() -> AsyncIterator[Environment]:
    """Serve the example Flow on a real Dex Worker against a fake monday.com board."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    board_id = str(time.time_ns() % 1_000_000_000_000 + 1_000_000_000_000)
    provider = MondayBoardFake(board_id)
    reference = ConnectionRef("monday", CONNECTION_NAME)
    credentials = StaticCredentialProvider(
        {reference: Credentials("personal-api-token", SecretString(TOKEN))}
    )
    async with httpx.AsyncClient(transport=httpx.MockTransport(provider.handle)) as http_client:
        client = Client(
            Config(poll_interval_seconds=30), credentials, http_client, endpoint="https://monday.example/v2"
        )
        connection = new_connection(client, reference)
        flow = ItemChangeFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="monday-item-changed-blobs-") as directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(directory) / "blobs"), 1 << 26))
            worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
            worker_task = asyncio.create_task(worker.start())
            try:
                await worker.wait_pull_registered(timeout=30)
                options = dex.ClientOptions(server_address=server_address)
                async with dex.AsyncClient(registry, cache, options) as dex_client:
                    yield Environment(
                        provider,
                        DurableProject(board_id),
                        CountingTarget(new_item_change_flow_target(dex_client, flow)),
                        dex_client,
                        connection,
                        time.time_ns() % 1_000_000_000,
                    )
            finally:
                await worker.stop()
                await worker_task
                cache.close()


@pytest.mark.asyncio
async def test_item_changes_start_one_flow_each_across_a_restart_with_real_dex() -> None:
    """The baseline starts nothing; each change starts one Flow; changes made while stopped arrive later."""
    async with item_changed_environment() as environment:
        provider = environment.provider
        existing, second_existing, new, offline = (environment.item_id(index) for index in (1, 2, 3, 4))
        provider.create(existing, "Existing request")
        provider.create(second_existing, "Second existing request")
        async with trigger_process(environment.new_runner()) as first:
            await environment.wait_for_watermark(second_existing)
            provider.create(new, "New request")
            provider.set_status(existing, "Done")
            first.wake_poller(ITEM_CHANGED_TRIGGER_BINDING)
            outcomes = await environment.wait_for_outcomes(2)
        assert outcomes[new].change_kind == ItemChangeKind.CREATED
        assert outcomes[new].current.name == "New request"
        assert outcomes[existing].change_kind == ItemChangeKind.UPDATED
        assert outcomes[existing].current.column_values[0].text == "Done", "the Flow reads the item back"

        provider.create(offline, "Filed while stopped")
        provider.set_status(new, "Stuck")
        provider.rate_limit_next_requests(1, 1)
        async with trigger_process(environment.new_runner()) as second:
            outcomes = await environment.wait_for_outcomes(4)
            await environment.wait_for_watermark(new)
            second.wake_poller(ITEM_CHANGED_TRIGGER_BINDING)
            deadline = time.monotonic() + 2
            while time.monotonic() < deadline:
                assert len(environment.target.delivered) == 4, "no change twice and never the baseline"
                await asyncio.sleep(0.1)
        assert outcomes[offline].current.name == "Filed while stopped"
        assert outcomes[new].change_kind == ItemChangeKind.UPDATED
        assert any(request.status == 429 for request in provider.requests), "the restart was rate limited"


@pytest.mark.asyncio
async def test_an_oversized_burst_records_a_new_baseline_with_real_dex() -> None:
    """More changes than one scan reads reseed without delivering the gap; the next change starts a Flow."""
    async with item_changed_environment() as environment:
        provider = environment.provider
        provider.create(environment.item_id(1), "Existing request")
        async with trigger_process(environment.new_runner()):
            await environment.wait_for_watermark(environment.item_id(1))

        for index in range(2, 10_003):
            provider.create(environment.item_id(index), "Bulk import")
        async with trigger_process(environment.new_runner()) as second:

            async def is_reset() -> bool:
                state = await environment.project.cursor_state()
                return state is not None and state.reset_count == 1

            await eventually(is_reset)
            await environment.wait_for_watermark(environment.item_id(10_002))
            after_reset = environment.item_id(10_003)
            provider.create(after_reset, "After the reset")
            second.wake_poller(ITEM_CHANGED_TRIGGER_BINDING)
            outcomes = await environment.wait_for_outcomes(1)
        assert list(outcomes) == [after_reset], "items changed in the gap are not delivered"
        assert outcomes[after_reset].current.name == "After the reset"
