# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deliver Zoom meeting changes through the example's Flow target to a real Dex Worker and Flow."""

import asyncio
import json
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

from dex_connectors.zoom import (
    Client,
    Config,
    Credentials,
    MeetingChangedEvent,
    ProjectMeetingChangedTriggerRoute,
    new_connection,
    new_project_meeting_trigger_runner,
)
from tests.zoom_meetings_fake import ACCESS_TOKEN, ZoomMeetingsFake

from .flow import BINDING_NAME, CONNECTION_NAME, MeetingChangeFlow, MeetingChangeOutcome, resolve_flow_id
from .worker import new_meeting_change_flow_target

SCOPE = Scope("zoom-meeting-changed", "live")
INBOX_KEY = TriggerInboxKey("zoom", CONNECTION_NAME, "meetingChanged", BINDING_NAME)


class DurableProject:
    """The project storage two Trigger processes share: hosted storage keeps inboxes and cursors."""

    def __init__(self) -> None:
        self.objects = InMemoryObjectStore()
        self.configuration = Configuration(SCOPE, 1, ())

    def trigger_inbox(self, key: TriggerInboxKey) -> TriggerInbox:
        return TriggerInbox(self.objects, SCOPE, key)

    def poll_cursor_store(self, key: PollCursorKey) -> PollCursorStore:
        return PollCursorStore(self.objects, SCOPE, key)

    async def cursor_state(self) -> PollCursorState | None:
        return (
            await self.poll_cursor_store(PollCursorKey.from_trigger_inbox_key(INBOX_KEY)).read_poll_cursor()
        ).state


@dataclass
class CountingTarget:
    """Wraps the example's Flow target and records each delivered event once, in order."""

    inner: Any
    delivered: dict[str, TriggerEvent[MeetingChangedEvent]] = field(default_factory=dict)

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[MeetingChangedEvent]) -> None:
        await self.inner.handle_trigger(context, event)
        self.delivered.setdefault(event.id, event)


async def eventually(condition: Callable[[], Any], timeout: float = 30) -> None:
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
    """Run one Trigger process until the block exits."""
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
    provider: ZoomMeetingsFake
    project: DurableProject
    target: CountingTarget
    dex_client: dex.AsyncClient
    connection: Any
    first_meeting_id: int

    def meeting_id(self, offset: int) -> int:
        return self.first_meeting_id + offset

    def new_runner(self) -> PollRunner:
        routes = [ProjectMeetingChangedTriggerRoute(BINDING_NAME, self.target)]
        return new_project_meeting_trigger_runner(cast(LoadedProject, self.project), self.connection, routes)

    async def wait_for_snapshot(self, meeting_count: int) -> None:
        async def has_count() -> bool:
            state = await self.project.cursor_state()
            return (
                state is not None
                and state.snapshot is not None
                and len(state.snapshot.entries) == meeting_count
            )

        await eventually(has_count)

    async def wait_for_outcomes(self, count: int) -> dict[int, MeetingChangeOutcome]:
        await eventually(lambda: len(self.target.delivered) >= count)
        outcomes: dict[int, MeetingChangeOutcome] = {}
        for event in list(self.target.delivered.values()):
            result = await asyncio.wait_for(
                self.dex_client.wait_for_flow(resolve_flow_id(event), timedelta(seconds=30)), 40
            )
            assert result.status == dex.FlowStatus.COMPLETED, result.error_message
            outcome = result.single_output(MeetingChangeOutcome)
            assert outcome.meeting_id == event.payload.meeting.id
            outcomes[outcome.meeting_id] = outcome
        return outcomes


@asynccontextmanager
async def meeting_changed_environment() -> AsyncIterator[Environment]:
    """Serve the example Flow on a real Dex Worker against a fake Zoom user's meetings."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    provider = ZoomMeetingsFake("zoom-host-1")
    reference = ConnectionRef("zoom", CONNECTION_NAME)
    empty = SecretString("")
    credentials = Credentials("", empty, SecretString(ACCESS_TOKEN), empty)
    async with httpx.AsyncClient(transport=httpx.MockTransport(provider.handle)) as http_client:
        client = Client(
            Config(endpoint="https://zoom.example/v2", poll_interval_seconds=30),
            StaticCredentialProvider({reference: credentials}),
            http_client,
        )
        connection = new_connection(client, reference)
        flow = MeetingChangeFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="zoom-meeting-changed-blobs-") as directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(directory) / "blobs"), 1 << 26))
            worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
            worker_task = asyncio.create_task(worker.start())
            try:
                await worker.wait_pull_registered(timeout=30)
                options = dex.ClientOptions(server_address=server_address)
                async with dex.AsyncClient(registry, cache, options) as dex_client:
                    yield Environment(
                        provider,
                        DurableProject(),
                        CountingTarget(new_meeting_change_flow_target(dex_client, flow)),
                        dex_client,
                        connection,
                        10_000_000_000 + (time.time_ns() % 1_000_000_000) * 10,
                    )
            finally:
                await worker.stop()
                await worker_task
                cache.close()


@pytest.mark.asyncio
async def test_meeting_changes_start_one_flow_each_across_a_restart_with_real_dex() -> None:
    """The baseline starts nothing; each change starts one Flow; changes made while stopped arrive later."""
    async with meeting_changed_environment() as environment:
        provider = environment.provider
        consultation, review, kickoff, offline = (environment.meeting_id(offset) for offset in range(4))
        provider.schedule(consultation, "Consultation", 1)
        provider.schedule(review, "Team review", 2)
        async with trigger_process(environment.new_runner()) as first:
            await environment.wait_for_snapshot(2)
            provider.schedule(kickoff, "Kickoff", 3)
            provider.reschedule(consultation, 2)
            provider.delete(review)
            first.wake_poller(BINDING_NAME)
            outcomes = await environment.wait_for_outcomes(3)
        assert outcomes[kickoff].change_kind == "scheduled"
        assert outcomes[kickoff].current is not None and outcomes[kickoff].current.topic == "Kickoff"
        assert outcomes[kickoff].current.settings.waiting_room, "the Flow reads the full meeting back"
        assert outcomes[consultation].change_kind == "updated"
        assert outcomes[review].change_kind == "removed" and outcomes[review].current is None

        provider.schedule(offline, "Planned while stopped", 4)
        provider.rename(kickoff, "Kickoff moved")
        async with trigger_process(environment.new_runner()) as second:
            second.wake_poller(BINDING_NAME)
            outcomes = await environment.wait_for_outcomes(5)
            await environment.wait_for_snapshot(3)
        assert outcomes[offline].current is not None
        assert outcomes[offline].current.topic == "Planned while stopped"
        assert outcomes[kickoff].change_kind == "updated"
        assert outcomes[kickoff].current is not None and outcomes[kickoff].current.topic == "Kickoff moved"
        assert len(environment.target.delivered) == 5, "no change twice and never the baseline"


@pytest.mark.asyncio
async def test_another_zoom_users_listing_records_a_new_baseline_with_real_dex() -> None:
    """A listing of another user's meetings reseeds; meetings in the gap belong to the new baseline."""
    async with meeting_changed_environment() as environment:
        provider = environment.provider
        provider.schedule(environment.meeting_id(0), "Consultation", 1)
        async with trigger_process(environment.new_runner()):
            await environment.wait_for_snapshot(1)

        provider.host_id = "zoom-host-2"
        provider.schedule(environment.meeting_id(1), "Lost in the gap", 2)
        async with trigger_process(environment.new_runner()) as second:

            async def is_reset() -> bool:
                state = await environment.project.cursor_state()
                return state is not None and state.reset_count == 1

            await eventually(is_reset)
            await environment.wait_for_snapshot(2)
            after_reset = environment.meeting_id(2)
            provider.schedule(after_reset, "After the reset", 3)
            second.wake_poller(BINDING_NAME)
            outcomes = await environment.wait_for_outcomes(1)
        assert list(outcomes) == [after_reset], "meetings in the gap are part of the new baseline"
        assert outcomes[after_reset].current is not None
        assert outcomes[after_reset].current.topic == "After the reset"
        state = await environment.project.cursor_state()
        assert state is not None
        assert json.loads(state.token) == {"version": 1, "hostId": "zoom-host-2", "sequence": 1}
