# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""eventChanged runner wiring, parity with the Go connector's event_trigger_runner_test.go."""

import asyncio
import json
from collections.abc import AsyncIterator, Awaitable, Callable
from pathlib import Path

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
from dex_connectors.sdk.projectconfig import PollCursorKey, TriggerInboxKey, load_from_environment

from dex_connectors.google_calendar import (
    Client,
    Config,
    Connection,
    Credentials,
    EventChangedEvent,
    EventChangedTriggerConfiguration,
    EventChangedTriggerRoute,
    ProjectEventChangedTriggerRoute,
    new_connection,
    new_event_changed_trigger,
    new_event_trigger_runner,
    new_project_event_trigger_runner,
)
from dex_connectors.google_calendar.calendarmock import Mock
from tests.calendar_sync_fake import CalendarSyncFake

SYNC_CALENDAR_ID = "team@group.calendar.google.com"
REFERENCE = ConnectionRef("google", "calendar-sync")


class QueueTarget:
    """Consumes each event into a queue."""

    def __init__(self) -> None:
        self.events: asyncio.Queue[TriggerEvent[EventChangedEvent]] = asyncio.Queue()

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[EventChangedEvent]) -> None:
        await self.events.put(event)


@pytest.fixture
async def calendar_http(request: pytest.FixtureRequest) -> AsyncIterator[tuple[CalendarSyncFake, Connection]]:
    fake = CalendarSyncFake(getattr(request, "param", SYNC_CALENDAR_ID))
    async with httpx.AsyncClient(transport=fake.transport()) as http_client:
        client = Client(
            Config(endpoint="http://127.0.0.1/calendar/v3", poll_interval_seconds=5),
            StaticCredentialProvider({REFERENCE: Credentials(access_token=SecretString("calendar-token"))}),
            http_client,
        )
        yield fake, new_connection(client, REFERENCE)


async def eventually(condition_met: Callable[[], Awaitable[bool]], timeout: float = 10) -> None:
    async with asyncio.timeout(timeout):
        while not await condition_met():
            await asyncio.sleep(0.01)


async def test_new_event_trigger_runner_rejects_incomplete_routes(
    calendar_http: tuple[CalendarSyncFake, Connection],
) -> None:
    _, connection = calendar_http
    target = QueueTarget()
    whitespace = EventChangedTriggerConfiguration(calendar_id="primary ")
    for name, routes in {
        "none": [],
        "no binding": [EventChangedTriggerRoute("", EventChangedTriggerConfiguration(), target)],
        "no target": [EventChangedTriggerRoute("calendarChanges", EventChangedTriggerConfiguration(), None)],  # type: ignore[arg-type]
        "duplicate": [
            EventChangedTriggerRoute("calendarChanges", EventChangedTriggerConfiguration(), target),
            EventChangedTriggerRoute("calendarChanges", EventChangedTriggerConfiguration(), target),
        ],
        "whitespace": [EventChangedTriggerRoute("calendarChanges", whitespace, target)],
    }.items():
        with pytest.raises(ValueError):
            new_event_trigger_runner(connection, routes)
        assert name
    route = EventChangedTriggerRoute("calendarChanges", EventChangedTriggerConfiguration(), target)
    with pytest.raises(ValueError, match="mock connection"):
        new_event_trigger_runner(Mock(REFERENCE.name).connection(), [route])


def write_project(directory: Path) -> Path:
    """Write the connections.json Dex Web saves, with a saved calendar for teamChanges."""
    path = directory / "connections.json"
    connection = {
        "connectorId": "google-calendar",
        "connectionName": REFERENCE.name,
        "modulePath": "github.com/superdurable/dex-connectors-library/connectors/google/calendar/go",
        "provider": "google",
        "authMethodId": "google-oauth",
        "configuration": {},
        "credentials": {"oauth_client_id": "inert", "access_token": "inert", "refresh_token": "inert"},
    }
    binding = {
        "connectorId": "google-calendar",
        "connectionName": REFERENCE.name,
        "triggerName": "eventChanged",
        "bindingName": "teamChanges",
        "configuration": {"calendarId": SYNC_CALENDAR_ID, "calendarName": "Ops Team"},
    }
    document = {
        "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
        "connections": [connection],
        "triggerBindings": [binding],
    }
    path.write_text(json.dumps(document))
    path.chmod(0o600)
    return path


async def test_project_runner_loads_the_saved_calendar_and_commits_after_recording(
    calendar_http: tuple[CalendarSyncFake, Connection], tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    fake, connection = calendar_http
    fake.insert("existing001", "Existing")
    monkeypatch.setenv("DEX_CONNECTOR_CONFIG_FILE", str(write_project(tmp_path)))
    project = await load_from_environment()
    target = QueueTarget()
    runner = new_project_event_trigger_runner(
        project, connection, [ProjectEventChangedTriggerRoute("teamChanges", target)]
    )
    key = TriggerInboxKey("google-calendar", REFERENCE.name, "eventChanged", "teamChanges")
    cursor = project.poll_cursor_store(PollCursorKey.from_trigger_inbox_key(key))

    async def committed_token() -> str:
        record = await cursor.read_poll_cursor()
        return record.state.token if record.state is not None else ""

    context = TriggerContext()
    running = asyncio.create_task(runner.run(context))
    try:
        await eventually(
            lambda: _equals(
                committed_token(), '{"calendarId":"' + SYNC_CALENDAR_ID + '","syncToken":"sync-1"}'
            )
        )
        fake.insert("kickoff0001", "Kickoff")
        runner.wake_poller("teamChanges")
        event = await asyncio.wait_for(target.events.get(), 10)
        assert event.payload.event.id == "kickoff0001", "the baseline event is never delivered"
        assert event.payload.change_kind == "created"
        await eventually(
            lambda: _equals(
                committed_token(), '{"calendarId":"' + SYNC_CALENDAR_ID + '","syncToken":"sync-2"}'
            )
        )
        inbox = project.trigger_inbox(key)
        await eventually(lambda: _equals(_pending_count(inbox), 0))
    finally:
        context.cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await running
        await project.close()


async def _equals(value: Awaitable[object], expected: object) -> bool:
    return await value == expected


async def _pending_count(inbox: object) -> int:
    return len(await inbox.pending())  # type: ignore[attr-defined]


@pytest.mark.parametrize("calendar_http", ["primary"], indirect=True)
async def test_single_binding_trigger_baselines_and_delivers_the_next_change(
    calendar_http: tuple[CalendarSyncFake, Connection],
) -> None:
    fake, connection = calendar_http
    fake.insert("existing001", "Existing")
    target = QueueTarget()
    trigger = new_event_changed_trigger(
        connection=connection,
        binding_name="primaryChanges",
        configuration=EventChangedTriggerConfiguration(),
        target=target,
    )
    context = TriggerContext()
    running = asyncio.create_task(trigger.run(context))
    try:
        await eventually(lambda: _equals(_request_count(fake), 1))
        fake.cancel("existing001")
        # The next poll comes one jittered 5s interval later.
        event = await asyncio.wait_for(target.events.get(), 20)
        assert event.payload.change_kind == "cancelled"
        assert event.payload.calendar_id == "primary"
    finally:
        context.cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await running


async def _request_count(fake: CalendarSyncFake) -> int:
    return len(fake.requests())
