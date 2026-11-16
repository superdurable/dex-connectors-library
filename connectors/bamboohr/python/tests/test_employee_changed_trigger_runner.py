# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""employeeChanged runners, ported from the Go connector's employee_changed_trigger_runner tests."""

import asyncio
from collections.abc import AsyncIterator, Awaitable, Callable
from contextlib import asynccontextmanager
from datetime import timedelta
from typing import Any, cast

import httpx
import pytest
from dex_connectors.sdk import (
    StaticCredentialProvider,
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

from dex_connectors.bamboohr import (
    EMPLOYEE_CHANGE_SETTLE_WINDOW,
    Client,
    Config,
    Connection,
    EmployeeChangedEvent,
    EmployeeChangedTriggerConfiguration,
    EmployeeChangedTriggerRoute,
    ProjectEmployeeChangedTriggerRoute,
    decode_employee_changed_event_payload,
    new_connection,
    new_employee_changed_trigger,
    new_employee_changed_trigger_runner,
    new_project_employee_changed_trigger_runner,
)
from tests.bamboohr_fake import START, BambooHRFake
from tests.test_employee_change_fetcher import REFERENCE, new_client

SCOPE = Scope("bamboohr-test", "preview")
KEY = TriggerInboxKey("bamboohr", REFERENCE.name, "employeeChanged", "hires")


class QueueTarget:
    """Consumes each event into a queue."""

    def __init__(self) -> None:
        self.events: asyncio.Queue[TriggerEvent[EmployeeChangedEvent]] = asyncio.Queue()

    async def handle_trigger(
        self, context: TriggerContext, event: TriggerEvent[EmployeeChangedEvent]
    ) -> None:
        """Record the event."""
        await self.events.put(event)

    async def next_event(self) -> TriggerEvent[EmployeeChangedEvent]:
        """Wait up to fifteen seconds for the next event."""
        return await asyncio.wait_for(self.events.get(), 15)


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
    """One saved employeeChanged binding named hires."""
    return {
        "connectorId": "bamboohr",
        "connectionName": REFERENCE.name,
        "triggerName": "employeeChanged",
        "bindingName": "hires",
        "configuration": configuration,
    }


@asynccontextmanager
async def connection_for(fake: BambooHRFake) -> AsyncIterator[Connection]:
    """A connection whose requests the fake answers."""
    async with httpx.AsyncClient(transport=httpx.MockTransport(fake.handle)) as http_client:
        yield new_connection(new_client(http_client), REFERENCE)


async def eventually(condition: Callable[[], Awaitable[bool]]) -> None:
    """Poll condition for up to fifteen seconds."""
    async with asyncio.timeout(15):
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


def test_client_rejects_a_short_poll_interval() -> None:
    with pytest.raises(ValueError, match="pollInterval must be at least 10s"):
        Client(Config(company_domain="acme", poll_interval_seconds=9), StaticCredentialProvider({}))
    client = Client(Config(company_domain="acme", poll_interval_seconds=0), StaticCredentialProvider({}))
    assert client.poll_interval == timedelta(minutes=1), "a blank pollInterval keeps the manifest default"


async def test_runner_rejects_incomplete_routes() -> None:
    fake = BambooHRFake()
    target = QueueTarget()
    async with connection_for(fake) as connection:
        for routes in (
            [],
            [EmployeeChangedTriggerRoute("", target)],
            [EmployeeChangedTriggerRoute("hires", cast(Any, None))],
            [EmployeeChangedTriggerRoute("hires", target), EmployeeChangedTriggerRoute("hires", target)],
            [EmployeeChangedTriggerRoute("hires", target, EmployeeChangedTriggerConfiguration("all"))],
        ):
            with pytest.raises(ValueError):
                new_employee_changed_trigger_runner(connection, routes)
    with pytest.raises(ValueError, match="BambooHR client"):
        new_employee_changed_trigger_runner(
            cast(Connection, object()), [EmployeeChangedTriggerRoute("hires", target)]
        )
    with pytest.raises(ValueError, match="loaded project configuration"):
        new_project_employee_changed_trigger_runner(cast(LoadedProject, None), connection, [])


async def test_durable_runner_loads_the_saved_change_type_and_commits_after_recording() -> None:
    fake = BambooHRFake()
    fake.insert("140")
    project = ProjectStub((saved_binding({"changeType": "inserted"}),))
    target = QueueTarget()
    cursor = project.poll_cursor_store(PollCursorKey.from_trigger_inbox_key(KEY))

    async def committed_token() -> str:
        state = (await cursor.read_poll_cursor()).state
        return state.token if state is not None else ""

    async with connection_for(fake) as connection:
        runner = new_project_employee_changed_trigger_runner(
            cast(LoadedProject, project), connection, [ProjectEmployeeChangedTriggerRoute("hires", target)]
        )
        async with running(runner):

            async def is_seeded() -> bool:
                return await committed_token() != ""

            await eventually(is_seeded)
            assert await committed_token() == '{"v":1,"since":"2026-10-13T09:00:01Z","changeType":"inserted"}'
            fake.update("140")
            fake.insert("141")
            fake.advance(EMPLOYEE_CHANGE_SETTLE_WINDOW)
            runner.wake_poller("hires")
            event = await target.next_event()
            assert event.payload.employee_id == "141", "the seeded and updated employees are not delivered"
            assert event.payload.action == "Inserted"
            expected = (
                '{"v":1,"since":"2026-10-13T09:00:03Z","afterEmployeeId":"141","changeType":"inserted"}'
            )

            async def is_committed() -> bool:
                return await committed_token() == expected

            await eventually(is_committed)
            inbox = project.trigger_inbox(KEY)

            async def is_drained() -> bool:
                return not await inbox.pending()

            await eventually(is_drained)


async def test_durable_runner_rejects_an_invalid_saved_configuration() -> None:
    for configuration, message in (
        ({"changeType": "all"}, "changeType must be inserted"),
        ({"changeType": "inserted", "unknown": True}, 'binding "hires" configuration'),
    ):
        project = ProjectStub((saved_binding(configuration),))
        routes = [ProjectEmployeeChangedTriggerRoute("hires", QueueTarget())]
        async with connection_for(BambooHRFake()) as connection:
            with pytest.raises(ValueError, match=message):
                new_project_employee_changed_trigger_runner(cast(LoadedProject, project), connection, routes)


async def test_unsaved_binding_delivers_every_change_type() -> None:
    fake = BambooHRFake()
    project = ProjectStub()
    target = QueueTarget()
    async with connection_for(fake) as connection:
        runner = new_project_employee_changed_trigger_runner(
            cast(LoadedProject, project), connection, [ProjectEmployeeChangedTriggerRoute("hires", target)]
        )
        async with running(runner):

            async def has_polled() -> bool:
                return len(fake.change_list_queries) >= 1

            await eventually(has_polled)
            fake.delete("77")
            fake.advance(EMPLOYEE_CHANGE_SETTLE_WINDOW)
            runner.wake_poller("hires")
            event = await target.next_event()
    assert event.payload.action == "Deleted"
    assert all("type" not in query for query in fake.change_list_queries)


async def test_single_trigger_seeds_and_delivers_the_next_change() -> None:
    fake = BambooHRFake()
    fake.insert("140")
    target = QueueTarget()
    async with connection_for(fake) as connection:
        trigger = new_employee_changed_trigger(connection=connection, target=target)
        async with running(trigger):

            async def has_polled() -> bool:
                return len(fake.change_list_queries) >= 1

            await eventually(has_polled)
            fake.delete("140")
            fake.advance(EMPLOYEE_CHANGE_SETTLE_WINDOW)
            # The single Trigger has no wake hook; it polls again after the 10-second interval.
            event = await target.next_event()
        assert event.payload == EmployeeChangedEvent(
            "140", action="Deleted", last_changed=START + timedelta(seconds=2)
        )
        with pytest.raises(ValueError):
            new_employee_changed_trigger(
                connection=connection,
                target=target,
                configuration=EmployeeChangedTriggerConfiguration("Deleted"),
            )


def test_inbox_payload_decodes_a_go_written_event() -> None:
    go_payload = '{"employeeId":"140","action":"Updated","lastChanged":"2026-10-13T09:00:01Z"}'
    event = decode_employee_changed_event_payload(go_payload, go_payload.encode())
    assert event == EmployeeChangedEvent("140", action="Updated", last_changed=START + timedelta(seconds=1))
    with pytest.raises(ValueError):
        decode_employee_changed_event_payload('{"employeeId":140}', b"")
