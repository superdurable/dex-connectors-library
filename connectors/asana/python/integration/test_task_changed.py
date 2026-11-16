# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deliver Asana task events through the taskChanged poll Trigger into real Dex Flows."""

import asyncio
import os
import tempfile
import time
from collections.abc import AsyncIterator, Awaitable, Callable
from contextlib import asynccontextmanager
from dataclasses import dataclass, field
from datetime import timedelta
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

from dex_connectors.asana import (
    Client,
    Config,
    Connection,
    Credentials,
    TaskChangedEvent,
    TaskChangedTriggerConfiguration,
    TaskChangedTriggerRoute,
    TaskTriggerRunnerConfig,
    decode_task_changed_event_payload,
    new_connection,
    new_task_trigger_runner,
)
from examples.task_changed.flow import (
    CONNECTION_NAME,
    TASK_CHANGED_TRIGGER_BINDING,
    ChangeOutcome,
    TaskChangedFlow,
    accept_every_task_change,
    map_to_flow_input,
    resolve_flow_id,
)
from tests.asana_events_fake import ACCESS_TOKEN, ENDPOINT, AsanaEventsFake

SCOPE = Scope("asana-task-changed", "live")
INBOX_KEY = TriggerInboxKey("asana", CONNECTION_NAME, "taskChanged", TASK_CHANGED_TRIGGER_BINDING)


async def eventually(condition_met: Callable[[], Awaitable[bool]], timeout: float = 30) -> None:
    """Poll a condition until it holds."""
    async with asyncio.timeout(timeout):
        while not await condition_met():
            await asyncio.sleep(0.05)


@dataclass
class TaskChangedEnvironment:
    """A real Dex Worker and Client, the Asana fake, and storage that outlives each Trigger process."""

    provider: AsanaEventsFake
    connection: Connection
    flow: TaskChangedFlow
    dex_client: dex.AsyncClient
    objects: InMemoryObjectStore = field(default_factory=InMemoryObjectStore)
    delivered: dict[str, TriggerEvent[TaskChangedEvent]] = field(default_factory=dict)
    order: list[str] = field(default_factory=list)

    def cursor_store(self) -> PollCursorStore:
        return PollCursorStore(self.objects, SCOPE, PollCursorKey.from_trigger_inbox_key(INBOX_KEY))

    async def cursor_state(self) -> PollCursorState | None:
        return (await self.cursor_store().read_poll_cursor()).state

    async def wait_for_sync_token(self, sync_token: str) -> None:
        """Wait until the committed cursor holds sync_token."""
        expected = f'{{"projectId":"{self.provider.project_id}","sync":"{sync_token}"}}'

        async def is_committed() -> bool:
            state = await self.cursor_state()
            return state is not None and state.token == expected

        await eventually(is_committed)

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[TaskChangedEvent]) -> None:
        """Start the event's Flow, then count the delivery."""
        await self._start.handle_trigger(context, event)
        if event.id not in self.delivered:
            self.order.append(event.id)
        self.delivered[event.id] = event

    def __post_init__(self) -> None:
        self._start = new_dex_flow_trigger_target(
            self.dex_client,
            self.flow,
            accepts_event=accept_every_task_change,
            resolve_flow_id=resolve_flow_id,
            map_to_flow_input=map_to_flow_input,
        )

    def new_runner(self) -> PollRunner:
        """Wire the example's target behind the durable inbox and cursor, as the project runner does."""
        target = new_durable_trigger_target(
            TriggerInbox(self.objects, SCOPE, INBOX_KEY),
            self,
            decode_payload=decode_task_changed_event_payload,
        )
        configuration = TaskChangedTriggerConfiguration(
            project_id=self.provider.project_id, project_name="Facilities"
        )
        route = TaskChangedTriggerRoute(
            TASK_CHANGED_TRIGGER_BINDING, configuration, target, self.cursor_store()
        )
        return new_task_trigger_runner(TaskTriggerRunnerConfig(self.connection, [route]))

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

    async def wait_for_outcomes(self, count: int) -> list[ChangeOutcome]:
        """Wait for count deliveries, then return each event's Flow outcome in delivery order."""

        async def has_deliveries() -> bool:
            return len(self.delivered) >= count

        await eventually(has_deliveries)
        outcomes = []
        for event_id in self.order:
            event = self.delivered[event_id]
            result = await self.dex_client.wait_for_flow(resolve_flow_id(event), timedelta(seconds=30))
            assert result.status == dex.FlowStatus.COMPLETED, result.error_message
            outcome = result.single_output(ChangeOutcome)
            assert outcome.task_id == event.payload.task.id
            outcomes.append(outcome)
        return outcomes


@asynccontextmanager
async def task_changed_environment() -> AsyncIterator[TaskChangedEnvironment]:
    """Run the Worker and Client against DEX_FLOW_SERVICE_ADDRESS with a unique watched project."""
    address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
    provider = AsanaEventsFake(str(time.time_ns()))
    reference = ConnectionRef("asana", CONNECTION_NAME)
    credentials = StaticCredentialProvider({reference: Credentials(SecretString(ACCESS_TOKEN))})
    client = Client(Config(endpoint=ENDPOINT, poll_interval_seconds=5), credentials, provider.http_client())
    connection = new_connection(client, reference)
    flow = TaskChangedFlow(connection)
    registry = dex.Registry([flow], allow_async_handlers=True)
    with tempfile.TemporaryDirectory(prefix="asana-python-task-changed-") as directory:
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(directory) / "blobs"), 1 << 26))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=30)
            async with dex.AsyncClient(
                registry, cache, dex.ClientOptions(server_address=address)
            ) as dex_client:
                yield TaskChangedEnvironment(provider, connection, flow, dex_client)
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()


@pytest.mark.asyncio
async def test_task_changes_start_one_flow_each_across_a_restart() -> None:
    """Existing tasks are a baseline, each later task event starts one Flow, stories start none, and events
    made while the process is stopped arrive after a restart whose first poll is rate limited."""
    async with task_changed_environment() as environment:
        provider = environment.provider
        provider.add_task("1204000000000001", "Inspect door sensors")
        provider.add_task("1204000000000002", "Order spare badges")
        async with environment.trigger_process() as first:
            await environment.wait_for_sync_token(provider.sync_token())
            provider.add_task("1204000000000003", "Replace badge reader")
            provider.complete_task("1204000000000001")
            provider.add_story("1205000000000001")
            provider.delete_task("1204000000000002")
            first.wake_poller(TASK_CHANGED_TRIGGER_BINDING)
            outcomes = await environment.wait_for_outcomes(3)
            assert outcomes[0].action == "added"
            assert outcomes[0].current is not None and outcomes[0].current.name == "Replace badge reader"
            assert (outcomes[1].action, outcomes[1].changed_field) == ("changed", "completed")
            assert outcomes[1].current is not None and outcomes[1].current.is_completed
            assert outcomes[2].action == "deleted" and outcomes[2].current is None

        provider.add_task("1204000000000004", "Planned while stopped")
        provider.rename_task("1204000000000003", "Replace badge reader at door 4")
        provider.rate_limit_next(1, "1")
        async with environment.trigger_process() as second:
            second.wake_poller(TASK_CHANGED_TRIGGER_BINDING)
            outcomes = await environment.wait_for_outcomes(5)
            assert outcomes[3].current is not None and outcomes[3].current.name == "Planned while stopped"
            assert outcomes[4].changed_field == "name"
            assert (
                outcomes[4].current is not None
                and outcomes[4].current.name == "Replace badge reader at door 4"
            )
            await environment.wait_for_sync_token(provider.sync_token())
        assert len(environment.delivered) == 5, "no event is delivered twice and the baseline never"
        assert any(request.status == 429 for request in provider.requests), "the restarted poller waited"
        assert await TriggerInbox(environment.objects, SCOPE, INBOX_KEY).pending() == ()


@pytest.mark.asyncio
async def test_expired_sync_token_records_a_new_baseline() -> None:
    """A 412 for the stored token reseeds without delivering the gap."""
    async with task_changed_environment() as environment:
        provider = environment.provider
        provider.add_task("1204000000000001", "Inspect door sensors")
        async with environment.trigger_process():
            await environment.wait_for_sync_token(provider.sync_token())

        provider.add_task("1204000000000009", "Lost in the gap")
        provider.expire_sync_tokens()
        async with environment.trigger_process() as second:

            async def is_reset() -> bool:
                state = await environment.cursor_state()
                return state is not None and state.reset_count == 1

            await eventually(is_reset)
            await environment.wait_for_sync_token(provider.sync_token())
            provider.add_task("1204000000000010", "After the reset")
            second.wake_poller(TASK_CHANGED_TRIGGER_BINDING)
            outcomes = await environment.wait_for_outcomes(1)
        assert len(outcomes) == 1, "events between the expired token and the new baseline are not delivered"
        assert outcomes[0].current is not None and outcomes[0].current.name == "After the reset"
