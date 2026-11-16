# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deliver Google Calendar changes from a fake sync feed through the example to a real Dex Worker and Flow."""

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
import httpx
import pytest
from dex_connectors.sdk import (
    ConnectionRef,
    SecretString,
    StaticCredentialProvider,
    TriggerContext,
    TriggerDeliveryAbortedError,
    TriggerEvent,
)
from dex_connectors.sdk.pollingtrigger import PollRunner
from dex_connectors.sdk.projectconfig import (
    InMemoryObjectStore,
    PollCursorKey,
    PollCursorStore,
    Scope,
    TriggerInbox,
    TriggerInboxKey,
    new_durable_trigger_target,
)

from dex_connectors.google_calendar import (
    Client,
    Config,
    Connection,
    Credentials,
    EventChangedEvent,
    EventChangedTriggerConfiguration,
    EventChangedTriggerRoute,
    new_connection,
    new_event_trigger_runner,
)
from dex_connectors.google_calendar.triggers import _decode_event_changed_payload
from tests.calendar_sync_fake import CalendarSyncFake

from .flow import (
    CONNECTION_NAME,
    EVENT_CHANGED_TRIGGER_BINDING,
    ChangeOutcome,
    EventChangeFlow,
    resolve_flow_id,
)
from .routing import new_event_change_target

SCOPE = Scope("google-calendar-event-changed", "live")
INBOX_KEY = TriggerInboxKey("google-calendar", CONNECTION_NAME, "eventChanged", EVENT_CHANGED_TRIGGER_BINDING)


@dataclass
class EventChangedEnvironment:
    """Storage that survives the Trigger process, plus the Dex client and the deliveries it saw."""

    fake: CalendarSyncFake
    connection: Connection
    dex_client: dex.AsyncClient
    flow: EventChangeFlow
    objects: InMemoryObjectStore = field(default_factory=InMemoryObjectStore)
    delivered: dict[str, TriggerEvent[EventChangedEvent]] = field(default_factory=dict)

    def cursor_store(self) -> PollCursorStore:
        return PollCursorStore(self.objects, SCOPE, PollCursorKey.from_trigger_inbox_key(INBOX_KEY))

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[EventChangedEvent]) -> None:
        """Start the example's Flow, then count the delivery."""
        await new_event_change_target(self.dex_client, self.flow).handle_trigger(context, event)
        self.delivered[event.id] = event

    def new_runner(self) -> PollRunner:
        """Wire the example target behind the durable inbox and cursor, as the project runner does."""
        target = new_durable_trigger_target(
            TriggerInbox(self.objects, SCOPE, INBOX_KEY), self, decode_payload=_decode_event_changed_payload
        )
        configuration = EventChangedTriggerConfiguration(calendar_id=self.fake.calendar_id)
        route = EventChangedTriggerRoute(
            EVENT_CHANGED_TRIGGER_BINDING, configuration, target, self.cursor_store()
        )
        return new_event_trigger_runner(self.connection, [route])

    async def cursor_state_token(self) -> str:
        state = (await self.cursor_store().read_poll_cursor()).state
        return state.token if state is not None else ""

    async def wait_for_sync_token(self, sync_token: str) -> None:
        expected = '{"calendarId":"' + self.fake.calendar_id + '","syncToken":"' + sync_token + '"}'

        async def is_committed() -> bool:
            return await self.cursor_state_token() == expected

        await eventually(is_committed)

    async def wait_for_outcomes(self, count: int) -> dict[str, ChangeOutcome]:
        """Wait for count deliveries, then each one's Flow, keyed by event ID."""

        async def has_count() -> bool:
            return len(self.delivered) >= count

        await eventually(has_count)
        outcomes: dict[str, ChangeOutcome] = {}
        for event in list(self.delivered.values()):
            result = await self.dex_client.wait_for_flow(resolve_flow_id(event), timedelta(seconds=60))
            assert result.status == dex.FlowStatus.COMPLETED, result.error_message
            outcome = result.single_output(ChangeOutcome)
            assert outcome.event_id == event.payload.event.id
            outcomes[outcome.event_id] = outcome
        return outcomes


async def eventually(condition_met: Callable[[], Awaitable[bool]], timeout: float = 30) -> None:
    async with asyncio.timeout(timeout):
        while not await condition_met():
            await asyncio.sleep(0.05)


@asynccontextmanager
async def trigger_process(environment: EventChangedEnvironment) -> AsyncIterator[PollRunner]:
    """One run of the application's Trigger side; leaving the block models a process exit."""
    runner = environment.new_runner()
    context = TriggerContext()
    running = asyncio.create_task(runner.run(context))
    try:
        yield runner
    finally:
        context.cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await running


@asynccontextmanager
async def event_changed_environment() -> AsyncIterator[EventChangedEnvironment]:
    """Start a Worker against real Dex and a fake calendar unique to this run, so Flow IDs are unique."""
    server_address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
    fake = CalendarSyncFake(f"team-{time.time_ns()}@group.calendar.google.com")
    reference = ConnectionRef("google", CONNECTION_NAME)
    async with httpx.AsyncClient(transport=fake.transport()) as provider_http_client:
        client = Client(
            Config(endpoint="http://127.0.0.1/calendar/v3", poll_interval_seconds=5),
            StaticCredentialProvider({reference: Credentials(access_token=SecretString("calendar-token"))}),
            provider_http_client,
        )
        connection = new_connection(client, reference)
        flow = EventChangeFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="google-calendar-python-event-changed-") as directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(directory) / "blobs"), 1 << 24))
            worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
            worker_task = asyncio.create_task(worker.start())
            try:
                await worker.wait_pull_registered(timeout=30)
                client_options = dex.ClientOptions(server_address=server_address)
                async with dex.AsyncClient(registry, cache, client_options) as dex_client:
                    yield EventChangedEnvironment(fake, connection, dex_client, flow)
            finally:
                await worker.stop()
                await worker_task
                cache.close()


@pytest.mark.asyncio
async def test_event_changes_start_one_flow_each_across_a_restart_with_real_dex() -> None:
    """Existing events are a baseline, each later change starts one Flow, and offline changes arrive."""
    async with event_changed_environment() as environment:
        fake = environment.fake
        fake.insert("standup0001", "Standup")
        fake.insert("review00001", "Review")
        async with trigger_process(environment) as first:
            await environment.wait_for_sync_token(fake.sync_token())
            fake.insert("kickoff0001", "Kickoff")
            fake.update("standup0001", "Standup moved")
            fake.cancel("review00001")
            first.wake_poller(EVENT_CHANGED_TRIGGER_BINDING)
            outcomes = await environment.wait_for_outcomes(3)
        assert outcomes["kickoff0001"].change_kind == "created"
        assert outcomes["kickoff0001"].current is not None
        assert outcomes["kickoff0001"].current.summary == "Kickoff"
        assert outcomes["standup0001"].change_kind == "updated"
        assert outcomes["standup0001"].current is not None
        assert outcomes["standup0001"].current.summary == "Standup moved", "the Flow reads the event back"
        assert outcomes["review00001"].change_kind == "cancelled"
        assert outcomes["review00001"].current is None, "a cancelled event is not read back"

        fake.insert("offline0001", "Planned while stopped")
        fake.update("kickoff0001", "Kickoff moved")
        async with trigger_process(environment) as second:
            second.wake_poller(EVENT_CHANGED_TRIGGER_BINDING)
            outcomes = await environment.wait_for_outcomes(5)
            assert outcomes["offline0001"].current is not None
            assert outcomes["offline0001"].current.summary == "Planned while stopped"
            assert outcomes["kickoff0001"].change_kind == "updated"
            await environment.wait_for_sync_token(fake.sync_token())
        assert len(environment.delivered) == 5, "no change is delivered twice and the baseline never is"


@pytest.mark.asyncio
async def test_expired_sync_token_records_a_new_baseline_with_real_dex() -> None:
    """A 410 Gone reseeds without delivering the gap."""
    async with event_changed_environment() as environment:
        fake = environment.fake
        fake.insert("standup0001", "Standup")
        async with trigger_process(environment):
            await environment.wait_for_sync_token(fake.sync_token())

        fake.insert("gapevent001", "Lost in the gap")
        fake.expire_sync_tokens()
        async with trigger_process(environment) as second:

            async def has_reset_once() -> bool:
                state = (await environment.cursor_store().read_poll_cursor()).state
                return state is not None and state.reset_count == 1

            await eventually(has_reset_once)
            await environment.wait_for_sync_token(fake.sync_token())
            fake.insert("afterreset1", "After the reset")
            second.wake_poller(EVENT_CHANGED_TRIGGER_BINDING)
            outcomes = await environment.wait_for_outcomes(1)
        assert outcomes["afterreset1"].current is not None
        assert outcomes["afterreset1"].current.summary == "After the reset"
        assert "gapevent001" not in outcomes, (
            "changes between the expired token and the new baseline are lost"
        )
