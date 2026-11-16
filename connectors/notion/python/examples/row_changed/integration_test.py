# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deliver Notion row changes through the databaseRowChanged poll Trigger into real Dex Flows."""

import asyncio
import json
import os
import tempfile
import uuid
from collections.abc import AsyncIterator, Awaitable, Callable
from contextlib import asynccontextmanager
from dataclasses import dataclass, field
from datetime import UTC, datetime, timedelta
from pathlib import Path

import dex
import pytest
from dex_connectors.sdk import (
    ConnectionRef,
    SecretString,
    StaticCredentialProvider,
    TriggerCancellation,
    TriggerContext,
    TriggerDeliveryAbortedError,
    TriggerEvent,
    new_dex_flow_trigger_target,
)
from dex_connectors.sdk.pollingtrigger import PollRunner
from dex_connectors.sdk.projectconfig import (
    InMemoryObjectStore,
    PollCursorKey,
    PollCursorState,
    PollCursorStore,
    Scope,
    TriggerInbox,
    TriggerInboxKey,
    new_durable_trigger_target,
)

from dex_connectors.notion import (
    Client,
    Config,
    Connection,
    Credentials,
    DatabaseRowChangedEvent,
    DatabaseRowChangedTriggerConfiguration,
    DatabaseRowChangedTriggerRoute,
    DatabaseRowTriggerRunnerConfig,
    decode_database_row_changed_event_payload,
    new_connection,
    new_database_row_trigger_runner,
)
from dex_connectors.notion._go_time import parse_go_time
from tests.notion_rows_fake import ENDPOINT, TOKEN, NotionRowsFake

from .flow import (
    CONNECTION_NAME,
    DATABASE_ROW_CHANGED_TRIGGER_BINDING,
    ChangeOutcome,
    DatabaseRowChangeFlow,
    accept_every_row_change,
    map_to_flow_input,
    resolve_flow_id,
)

SCOPE = Scope("notion-row-changed", "live")
INBOX_KEY = TriggerInboxKey(
    "notion", CONNECTION_NAME, "databaseRowChanged", DATABASE_ROW_CHANGED_TRIGGER_BINDING
)
ROW_STANDUP = "2a4d6f80-1b3c-4d5e-8f70-0000000000a1"
ROW_REVIEW = "2a4d6f80-1b3c-4d5e-8f70-0000000000b2"
ROW_KICKOFF = "2a4d6f80-1b3c-4d5e-8f70-0000000000c3"
ROW_OFFLINE = "2a4d6f80-1b3c-4d5e-8f70-0000000000d4"


async def eventually(condition_met: Callable[[], Awaitable[bool]], timeout: float = 30) -> None:
    """Poll a condition until it holds."""
    async with asyncio.timeout(timeout):
        while not await condition_met():
            await asyncio.sleep(0.05)


def unique_notion_id() -> str:
    return str(uuid.uuid4())


@dataclass
class RowChangedEnvironment:
    """A real Dex Worker and Client, the Notion fake, and storage that outlives each Trigger process."""

    provider: NotionRowsFake
    connection: Connection
    flow: DatabaseRowChangeFlow
    dex_client: dex.AsyncClient
    now: datetime = field(default_factory=lambda: datetime.now(UTC).replace(second=0, microsecond=0))
    objects: InMemoryObjectStore = field(default_factory=InMemoryObjectStore)
    delivered: dict[str, TriggerEvent[DatabaseRowChangedEvent]] = field(default_factory=dict)
    order: list[str] = field(default_factory=list)

    def __post_init__(self) -> None:
        self._start = new_dex_flow_trigger_target(
            self.dex_client,
            self.flow,
            accepts_event=accept_every_row_change,
            resolve_flow_id=resolve_flow_id,
            map_to_flow_input=map_to_flow_input,
        )

    def cursor_store(self) -> PollCursorStore:
        return PollCursorStore(self.objects, SCOPE, PollCursorKey.from_trigger_inbox_key(INBOX_KEY))

    async def cursor_state(self) -> PollCursorState | None:
        return (await self.cursor_store().read_poll_cursor()).state

    async def wait_for_committed_high_water(self, high_water: datetime) -> None:
        """Wait until the committed cursor watches this data source up to high_water with no open query."""

        async def is_committed() -> bool:
            state = await self.cursor_state()
            if state is None:
                return False
            position = json.loads(state.token)
            return (
                position.get("dataSourceId") == self.provider.data_source_id
                and parse_go_time(position["highWater"]).to_datetime() == high_water
                and not position.get("startCursor")
            )

        await eventually(is_committed)

    async def handle_trigger(
        self, context: TriggerContext, event: TriggerEvent[DatabaseRowChangedEvent]
    ) -> None:
        """Start the change's Flow, then count the delivery."""
        await self._start.handle_trigger(context, event)
        if event.id not in self.delivered:
            self.order.append(event.id)
        self.delivered[event.id] = event

    def new_runner(self) -> PollRunner:
        """Wire the example's target behind the durable inbox and cursor, as the project runner does."""
        target = new_durable_trigger_target(
            TriggerInbox(self.objects, SCOPE, INBOX_KEY),
            self,
            decode_payload=decode_database_row_changed_event_payload,
        )
        configuration = DatabaseRowChangedTriggerConfiguration(
            data_source_id=self.provider.data_source_id, data_source_name="Requests"
        )
        route = DatabaseRowChangedTriggerRoute(
            DATABASE_ROW_CHANGED_TRIGGER_BINDING, configuration, target, self.cursor_store()
        )
        return new_database_row_trigger_runner(DatabaseRowTriggerRunnerConfig(self.connection, [route]))

    @asynccontextmanager
    async def trigger_process(self) -> AsyncIterator[PollRunner]:
        """Run one Trigger process until the block exits, as a restartable replica."""
        runner, cancellation = self.new_runner(), TriggerCancellation()
        running = asyncio.create_task(runner.run(TriggerContext(cancellation)))
        try:
            yield runner
        finally:
            cancellation.cancel()
            with pytest.raises(TriggerDeliveryAbortedError):
                await running

    async def wait_for_queries(self, count: int) -> None:
        """Wait for count more provider requests than were recorded when it was called."""
        baseline = len(self.provider.requests)

        async def has_queried() -> bool:
            return len(self.provider.requests) >= baseline + count

        await eventually(has_queried)

    async def wait_for_outcomes(self, count: int) -> dict[str, ChangeOutcome]:
        """Wait for count deliveries, then each change's Flow outcome keyed by the title it was read with."""

        async def has_deliveries() -> bool:
            return len(self.delivered) >= count

        await eventually(has_deliveries)
        outcomes = {}
        for event_id in list(self.order):
            event = self.delivered[event_id]
            result = await self.dex_client.wait_for_flow(resolve_flow_id(event), timedelta(seconds=30))
            assert result.status == dex.FlowStatus.COMPLETED, result.error_message
            outcome = result.single_output(ChangeOutcome)
            assert outcome.row_id == event.payload.row.id
            outcomes[event.payload.row.title] = outcome
        return outcomes


@asynccontextmanager
async def row_changed_environment() -> AsyncIterator[RowChangedEnvironment]:
    """Run the Worker and Client against DEX_FLOW_SERVICE_ADDRESS with a unique watched data source."""
    address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
    provider = NotionRowsFake(unique_notion_id(), unique_notion_id())
    reference = ConnectionRef("notion", CONNECTION_NAME)
    credentials = StaticCredentialProvider({reference: Credentials(SecretString(TOKEN))})
    client = Client(Config(endpoint=ENDPOINT, poll_interval_seconds=5), credentials, provider.http_client())
    connection = new_connection(client, reference)
    flow = DatabaseRowChangeFlow(connection)
    registry = dex.Registry([flow], allow_async_handlers=True)
    with tempfile.TemporaryDirectory(prefix="notion-python-row-changed-") as directory:
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(directory) / "blobs"), 1 << 26))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=30)
            async with dex.AsyncClient(
                registry, cache, dex.ClientOptions(server_address=address)
            ) as dex_client:
                yield RowChangedEnvironment(provider, connection, flow, dex_client)
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()


@pytest.mark.asyncio
async def test_row_changes_start_one_flow_each_across_a_restart() -> None:
    """Existing rows are a baseline, each later row version starts exactly one Flow, a trashed row none,
    and edits made while the process is stopped arrive after a restart."""
    async with row_changed_environment() as environment:
        provider, now = environment.provider, environment.now
        provider.create(ROW_STANDUP, "Standup", now - timedelta(hours=1))
        provider.create(ROW_REVIEW, "Review", now - timedelta(minutes=30))
        async with environment.trigger_process() as first:
            await environment.wait_for_committed_high_water(now - timedelta(minutes=30))
            provider.create(ROW_KICKOFF, "Kickoff", now)
            provider.edit(ROW_STANDUP, "Standup moved", now)
            provider.trash(ROW_REVIEW, now)
            first.wake_poller(DATABASE_ROW_CHANGED_TRIGGER_BINDING)
            outcomes = await environment.wait_for_outcomes(2)
            assert outcomes["Kickoff"].change_kind == "created"
            assert outcomes["Standup moved"].change_kind == "updated"
            assert outcomes["Standup moved"].current.id == ROW_STANDUP, "the Flow reads the row back"

        provider.create(ROW_OFFLINE, "Planned while stopped", now + timedelta(minutes=1))
        provider.edit(ROW_KICKOFF, "Kickoff moved", now + timedelta(minutes=1))
        async with environment.trigger_process() as second:
            second.wake_poller(DATABASE_ROW_CHANGED_TRIGGER_BINDING)
            outcomes = await environment.wait_for_outcomes(4)
            assert outcomes["Planned while stopped"].change_kind == "created"
            assert outcomes["Kickoff moved"].change_kind == "updated"
            await environment.wait_for_committed_high_water(now + timedelta(minutes=1))
            second.wake_poller(DATABASE_ROW_CHANGED_TRIGGER_BINDING)
            await environment.wait_for_queries(1)
        assert len(environment.delivered) == 4, "no row version twice, the baseline and the trashed row never"
        assert await TriggerInbox(environment.objects, SCOPE, INBOX_KEY).pending() == ()


@pytest.mark.asyncio
async def test_rate_limit_and_foreign_cursor_record_a_new_baseline() -> None:
    """A 429 pauses the poll for Retry-After, and a stored cursor for another data source, as after the
    binding was re-picked, reseeds without a backlog."""
    async with row_changed_environment() as environment:
        provider, now = environment.provider, environment.now
        provider.create(ROW_STANDUP, "Existing", now - timedelta(hours=1))
        store = environment.cursor_store()
        record = await store.read_poll_cursor()
        foreign = (
            '{"v":1,"dataSourceId":"2a4d6f80-1b3c-4d5e-8f70-999999999999","highWater":"2026-01-01T00:00:00Z"}'
        )
        await store.replace_poll_cursor(record.revision, PollCursorState(token=foreign))
        provider.rate_limit_next_requests(1, "1")
        async with environment.trigger_process() as process:

            async def is_reset() -> bool:
                state = await environment.cursor_state()
                return state is not None and state.reset_count == 1

            await eventually(is_reset)
            await environment.wait_for_committed_high_water(now - timedelta(hours=1))
            assert provider.requests[0].status == 429
            provider.create(ROW_KICKOFF, "After the reset", now)
            process.wake_poller(DATABASE_ROW_CHANGED_TRIGGER_BINDING)
            outcomes = await environment.wait_for_outcomes(1)
        assert outcomes["After the reset"].change_kind == "created"
        assert "Existing" not in outcomes, "the new baseline delivers no existing row"
