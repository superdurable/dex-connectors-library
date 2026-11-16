# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""ticketChanged runners, ported from the Go connector's ticket_trigger_runner tests."""

import asyncio
from collections.abc import AsyncIterator, Awaitable, Callable
from contextlib import asynccontextmanager
from typing import Any, cast

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

from dex_connectors.gorgias import (
    Client,
    Config,
    Connection,
    Credentials,
    ProjectTicketChangedTriggerRoute,
    ProjectTicketTriggerRunnerConfig,
    TicketChangedEvent,
    TicketChangedTriggerConfiguration,
    TicketChangedTriggerRoute,
    TicketTriggerRunnerConfig,
    decode_ticket_changed_event_payload,
    new_connection,
    new_project_ticket_trigger_runner,
    new_ticket_changed_trigger,
    new_ticket_trigger_runner,
    validate_ticket_changed_trigger_configuration,
)
from tests.gorgias_events_fake import API_KEY, EMAIL, GorgiasEventsFake

SCOPE = Scope("gorgias-trigger-test", "live")
REFERENCE = ConnectionRef("gorgias", "gorgias-ticket-watcher")
KEY = TriggerInboxKey("gorgias", REFERENCE.name, "ticketChanged", "closedTickets")


class QueueTarget:
    """Consumes each event into a queue."""

    def __init__(self) -> None:
        self.events: asyncio.Queue[TriggerEvent[TicketChangedEvent]] = asyncio.Queue()

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[TicketChangedEvent]) -> None:
        """Record the event."""
        await self.events.put(event)

    async def next_event(self, timeout: float = 10) -> TriggerEvent[TicketChangedEvent]:
        """Wait for the next event."""
        return await asyncio.wait_for(self.events.get(), timeout)


class ProjectStub:
    """The parts of LoadedProject the durable runner reads, over one in-memory object store."""

    def __init__(self, trigger_bindings: tuple[dict[str, Any], ...] = ()) -> None:
        self.objects = InMemoryObjectStore()
        self.configuration = Configuration(SCOPE, 1, (), trigger_bindings)

    def trigger_inbox(self, key: TriggerInboxKey) -> TriggerInbox:
        return TriggerInbox(self.objects, SCOPE, key)

    def poll_cursor_store(self, key: PollCursorKey) -> PollCursorStore:
        return PollCursorStore(self.objects, SCOPE, key)


@asynccontextmanager
async def connection_for(
    fake: GorgiasEventsFake, poll_interval_seconds: float = 60
) -> AsyncIterator[Connection]:
    """A connection whose requests the fake answers."""
    credentials = StaticCredentialProvider({REFERENCE: Credentials(EMAIL, SecretString(API_KEY))})
    async with httpx.AsyncClient(transport=httpx.MockTransport(fake.handle)) as http_client:
        config = Config(domain="acme", poll_interval_seconds=poll_interval_seconds)
        yield new_connection(Client(config, credentials, http_client), REFERENCE)


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
    target = QueueTarget()
    async with connection_for(GorgiasEventsFake()) as connection:
        unknown = TicketChangedTriggerConfiguration(event_types=("ticket-message-created",))
        for routes in {
            "none": [],
            "no binding": [TicketChangedTriggerRoute("", TicketChangedTriggerConfiguration(), target)],
            "no target": [
                TicketChangedTriggerRoute(
                    "ticketEvents", TicketChangedTriggerConfiguration(), cast(Any, None)
                )
            ],
            "duplicate": [
                TicketChangedTriggerRoute("ticketEvents", TicketChangedTriggerConfiguration(), target),
                TicketChangedTriggerRoute("ticketEvents", TicketChangedTriggerConfiguration(), target),
            ],
            "unknown type": [TicketChangedTriggerRoute("ticketEvents", unknown, target)],
        }.values():
            with pytest.raises(ValueError):
                new_ticket_trigger_runner(TicketTriggerRunnerConfig(connection, routes))
    route = TicketChangedTriggerRoute("ticketEvents", TicketChangedTriggerConfiguration(), target)
    with pytest.raises(ValueError, match="new_connection"):
        new_ticket_trigger_runner(TicketTriggerRunnerConfig(cast(Connection, None), [route]))


def test_configuration_validation_matches_go() -> None:
    validate_ticket_changed_trigger_configuration(TicketChangedTriggerConfiguration())
    validate_ticket_changed_trigger_configuration(
        TicketChangedTriggerConfiguration(event_types=("ticket-created", "ticket-closed"))
    )
    with pytest.raises(ValueError, match='"ticket-message-created" is not a ticket event type'):
        validate_ticket_changed_trigger_configuration(
            TicketChangedTriggerConfiguration(event_types=("ticket-message-created",))
        )
    with pytest.raises(ValueError, match='lists "ticket-created" twice'):
        validate_ticket_changed_trigger_configuration(
            TicketChangedTriggerConfiguration(event_types=("ticket-created", "ticket-created"))
        )


def test_client_rejects_a_short_poll_interval() -> None:
    with pytest.raises(ValueError, match="pollInterval must be at least 10s"):
        Client(Config(domain="acme", poll_interval_seconds=5), StaticCredentialProvider({}))
    # A zero pollInterval, as a project may save it, keeps the one-minute default as Go does.
    defaulted = Client(Config(domain="acme", poll_interval_seconds=0), StaticCredentialProvider({}))
    assert defaulted.poll_interval_seconds == 60


def test_stored_payloads_written_by_go_decode() -> None:
    payload = (
        '{"eventId":9003,"eventType":"ticket-created","ticketId":7002,'
        '"occurredAt":"2026-11-02T09:00:10.000001Z","userId":12,"context":"abc"}'
    )
    decoded = decode_ticket_changed_event_payload(payload, payload.encode())
    assert (decoded.event_id, decoded.ticket_id, decoded.user_id, decoded.context) == (9003, 7002, 12, "abc")
    assert decoded.occurred_at.microsecond == 1
    with pytest.raises(ValueError):
        decode_ticket_changed_event_payload("[]", b"[]")


async def test_durable_runner_loads_the_saved_types_and_commits_after_recording() -> None:
    fake = GorgiasEventsFake()
    fake.record_ticket_event("ticket-created", 7001)
    project = ProjectStub(
        (
            {
                "connectorId": "gorgias",
                "connectionName": REFERENCE.name,
                "triggerName": "ticketChanged",
                "bindingName": "closedTickets",
                "configuration": {"eventTypes": ["ticket-closed"]},
            },
        )
    )
    target = QueueTarget()
    cursor = project.poll_cursor_store(PollCursorKey.from_trigger_inbox_key(KEY))

    async def committed_token() -> str:
        state = (await cursor.read_poll_cursor()).state
        return state.token if state is not None else ""

    async with connection_for(fake) as connection:
        runner = new_project_ticket_trigger_runner(
            cast(LoadedProject, project),
            connection,
            ProjectTicketTriggerRunnerConfig([ProjectTicketChangedTriggerRoute("closedTickets", target)]),
        )
        async with running(runner):

            async def has_baseline() -> bool:
                return await committed_token() != ""

            await eventually(has_baseline)
            baseline = await committed_token()
            fake.record_ticket_event("ticket-updated", 7001, 12)
            closed = fake.record_ticket_event("ticket-closed", 7001, 12)
            runner.wake_poller("closedTickets")
            event = await target.next_event()
            assert event.id == f"gorgias-event-{closed}", (
                "the saved configuration delivers only ticket-closed"
            )
            assert event.payload.ticket_id == 7001 and event.payload.user_id == 12

            async def has_moved() -> bool:
                return await committed_token() != baseline

            await eventually(has_moved)

            async def is_drained() -> bool:
                return not await project.trigger_inbox(KEY).pending()

            await eventually(is_drained)
        assert target.events.empty(), "the skipped update is never delivered"


async def test_durable_runner_rejects_an_invalid_saved_configuration() -> None:
    project = ProjectStub(
        (
            {
                "connectorId": "gorgias",
                "connectionName": REFERENCE.name,
                "triggerName": "ticketChanged",
                "bindingName": "closedTickets",
                "configuration": {"eventTypes": ["ticket-message-created"]},
            },
        )
    )
    async with connection_for(GorgiasEventsFake()) as connection:
        with pytest.raises(ValueError, match="ticket-message-created"):
            new_project_ticket_trigger_runner(
                cast(LoadedProject, project),
                connection,
                ProjectTicketTriggerRunnerConfig(
                    [ProjectTicketChangedTriggerRoute("closedTickets", QueueTarget())]
                ),
            )


async def test_generated_trigger_baselines_and_delivers_the_next_event() -> None:
    fake = GorgiasEventsFake()
    fake.record_ticket_event("ticket-created", 7001)
    target = QueueTarget()
    async with connection_for(fake, poll_interval_seconds=10) as connection:
        trigger = new_ticket_changed_trigger(
            connection=connection,
            binding_name="allTicketEvents",
            configuration=TicketChangedTriggerConfiguration(),
            target=target,
        )
        cancellation = TriggerCancellation()
        task = asyncio.create_task(trigger.run(TriggerContext(cancellation)))
        try:

            async def has_polled() -> bool:
                return fake.event_request_count > 0

            await eventually(has_polled)
            reopened = fake.record_ticket_event("ticket-reopened", 7001, 12)
            event = await target.next_event(timeout=30)
            assert event.id == f"gorgias-event-{reopened}"
            assert event.payload.event_type == "ticket-reopened"
        finally:
            cancellation.cancel()
            with pytest.raises(TriggerDeliveryAbortedError):
                await task
