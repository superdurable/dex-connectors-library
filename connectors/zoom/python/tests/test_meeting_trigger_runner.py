# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""meetingChanged runners, ported from the Go connector's meeting_trigger_runner tests."""

import asyncio
import json
from collections.abc import AsyncIterator, Awaitable, Callable
from contextlib import asynccontextmanager
from datetime import timedelta
from typing import Any, cast

import httpx
import pytest
from dex_connectors.sdk import (
    TriggerCancellation,
    TriggerContext,
    TriggerDeliveryAbortedError,
    TriggerEvent,
)
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

from dex_connectors.zoom import (
    Connection,
    MeetingChangedEvent,
    MeetingChangedTriggerConfiguration,
    MeetingChangedTriggerRoute,
    ProjectMeetingChangedTriggerRoute,
    decode_meeting_changed_event_payload,
    new_connection,
    new_meeting_changed_trigger,
    new_meeting_trigger_runner,
    new_project_meeting_trigger_runner,
)
from tests.test_meeting_list_fetcher import HOST_ID, REFERENCE, new_client
from tests.zoom_meetings_fake import ZoomMeetingsFake

SCOPE = Scope("zoom-test", "preview")
KEY = TriggerInboxKey("zoom", REFERENCE.name, "meetingChanged", "hostMeetings")


class QueueTarget:
    """Consumes each event into a queue."""

    def __init__(self) -> None:
        self.events: asyncio.Queue[TriggerEvent[MeetingChangedEvent]] = asyncio.Queue()

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[MeetingChangedEvent]) -> None:
        """Record the event."""
        await self.events.put(event)

    async def next_event(self) -> TriggerEvent[MeetingChangedEvent]:
        """Wait up to ten seconds for the next event."""
        return await asyncio.wait_for(self.events.get(), 10)


class ProjectStub:
    """The parts of LoadedProject the durable runner reads, over one in-memory object store."""

    def __init__(self, trigger_bindings: tuple[dict[str, Any], ...]) -> None:
        self.objects = InMemoryObjectStore()
        self.configuration = Configuration(SCOPE, 1, (), trigger_bindings)

    def trigger_inbox(self, key: TriggerInboxKey) -> TriggerInbox:
        return TriggerInbox(self.objects, SCOPE, key)

    def poll_cursor_store(self, key: PollCursorKey) -> PollCursorStore:
        return PollCursorStore(self.objects, SCOPE, key)


@asynccontextmanager
async def connection_for(fake: ZoomMeetingsFake) -> AsyncIterator[Connection]:
    """A connection whose requests the fake answers."""
    async with httpx.AsyncClient(transport=httpx.MockTransport(fake.handle)) as http_client:
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
    fake = ZoomMeetingsFake(HOST_ID)
    target = QueueTarget()
    async with connection_for(fake) as connection:
        for routes in {
            "none": [],
            "no binding": [MeetingChangedTriggerRoute("", target)],
            "no target": [MeetingChangedTriggerRoute("meetingChanges", cast(Any, None))],
            "bad kinds": [
                MeetingChangedTriggerRoute(
                    "meetingChanges", target, MeetingChangedTriggerConfiguration(change_kinds=("ended",))
                )
            ],
            "duplicate": [MeetingChangedTriggerRoute("a", target), MeetingChangedTriggerRoute("a", target)],
        }.values():
            with pytest.raises(ValueError):
                new_meeting_trigger_runner(connection, routes)
    mock = cast(Connection, object())
    with pytest.raises(ValueError, match="Zoom client"):
        new_meeting_trigger_runner(mock, [MeetingChangedTriggerRoute("meetingChanges", target)])


async def test_durable_runner_loads_the_saved_kinds_and_commits_after_recording() -> None:
    fake = ZoomMeetingsFake(HOST_ID)
    fake.schedule(92674392836, "Consultation", 1)
    project = ProjectStub(
        (
            {
                "connectorId": "zoom",
                "connectionName": REFERENCE.name,
                "triggerName": "meetingChanged",
                "bindingName": "hostMeetings",
                "configuration": {"changeKinds": ["scheduled", "updated"]},
            },
        )
    )
    target = QueueTarget()
    cursor = project.poll_cursor_store(PollCursorKey.from_trigger_inbox_key(KEY))
    async with connection_for(fake) as connection:
        runner = new_project_meeting_trigger_runner(
            cast(LoadedProject, project),
            connection,
            [ProjectMeetingChangedTriggerRoute("hostMeetings", target)],
        )

        async def commit_count() -> int:
            state = (await cursor.read_poll_cursor()).state
            return state.commit_count if state is not None else 0

        async with running(runner):
            await eventually(lambda: _at_least(commit_count, 1))
            state = (await cursor.read_poll_cursor()).state
            assert state is not None and json.loads(state.token) == {"version": 1, "hostId": HOST_ID}
            # A rename, a rename back, and the same rename again are three changes with three event IDs.
            seen: set[str] = set()
            for index, topic in enumerate(["Consultation moved", "Consultation", "Consultation moved"]):
                fake.rename(92674392836, topic)
                runner.wake_poller("hostMeetings")
                event = await target.next_event()
                assert event.payload.change_kind == "updated" and event.payload.meeting.topic == topic
                assert event.id not in seen, f"change {index} reuses an earlier event ID"
                seen.add(event.id)
                await eventually(lambda expected=index + 2: _at_least(commit_count, expected))

            fake.delete(92674392836)
            fake.schedule(92674392850, "Kickoff", 4)
            runner.wake_poller("hostMeetings")
            event = await target.next_event()
            assert event.payload.change_kind == "scheduled", "the saved kinds skip the removal"
            assert event.payload.meeting.id == 92674392850
            inbox = project.trigger_inbox(KEY)

            async def is_drained() -> bool:
                return not await inbox.pending()

            await eventually(is_drained)


async def _at_least(read: Callable[[], Awaitable[int]], expected: int) -> bool:
    return await read() >= expected


async def test_durable_runner_rejects_an_invalid_saved_configuration() -> None:
    fake = ZoomMeetingsFake(HOST_ID)
    project = ProjectStub(
        (
            {
                "connectorId": "zoom",
                "connectionName": REFERENCE.name,
                "triggerName": "meetingChanged",
                "bindingName": "hostMeetings",
                "configuration": {"changeKinds": ["scheduled"], "unknown": True},
            },
        )
    )
    async with connection_for(fake) as connection:
        with pytest.raises(ValueError, match="hostMeetings"):
            new_project_meeting_trigger_runner(
                cast(LoadedProject, project),
                connection,
                [ProjectMeetingChangedTriggerRoute("hostMeetings", QueueTarget())],
            )


async def test_single_trigger_baselines_and_delivers_the_next_change() -> None:
    fake = ZoomMeetingsFake(HOST_ID)
    fake.schedule(92674392836, "Consultation", 1)
    target = QueueTarget()
    async with connection_for(fake) as connection:
        # A short interval keeps the test fast; Client rejects one under the 30s minimum.
        connection._client.poll_interval = timedelta(seconds=1)
        trigger = new_meeting_changed_trigger(
            connection=connection, binding_name="hostMeetings", target=target
        )
        async with running(trigger):
            await eventually(lambda: _listed_at_least(fake, 1))
            fake.rename(92674392836, "Consultation moved")
            event = await target.next_event()
            assert event.payload.change_kind == "updated"
            assert event.payload.previous_meeting is not None
            assert event.payload.previous_meeting.topic == "Consultation"


async def _listed_at_least(fake: ZoomMeetingsFake, count: int) -> bool:
    return fake.listing_request_count() >= count


def test_inbox_payload_decodes_a_go_written_event() -> None:
    go_payload = (
        '{"changeKind":"updated","meeting":{"id":1001,"uuid":"uuid-1001==","hostId":"zoom-host-1",'
        '"topic":"Consultation \\u003cmoved\\u003e","type":2,"startTime":"2036-02-24T09:00:00.12+05:30",'
        '"joinUrl":"https://us05web.zoom.us/j/1001"},"previousMeeting":{"id":1001,"topic":"Consultation"}}'
    )
    event = decode_meeting_changed_event_payload(go_payload, go_payload.encode())
    assert event.meeting.topic == "Consultation <moved>"
    assert event.previous_meeting is not None and event.previous_meeting.topic == "Consultation"
    with pytest.raises(ValueError):
        decode_meeting_changed_event_payload('{"changeKind":"ended"}', b"")
