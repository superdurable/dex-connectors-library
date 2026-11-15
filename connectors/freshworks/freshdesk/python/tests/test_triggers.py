# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""ticketChanged runner wiring, saved configuration, and the in-memory Trigger."""

import asyncio
import json
import time
from collections.abc import Awaitable, Callable
from datetime import UTC, datetime, timedelta
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

from dex_connectors.freshworks_freshdesk import (
    TICKET_CHANGE_CREATED,
    TICKET_CHANGE_UPDATED,
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
    decode_ticket_changed_trigger_configuration,
    new_connection,
    new_project_connection,
    new_project_ticket_trigger_runner,
    new_ticket_changed_trigger,
    new_ticket_trigger_runner,
    validate_ticket_changed_trigger_configuration,
)
from dex_connectors.freshworks_freshdesk.freshdeskmock import Mock
from examples.ticket_changed.ticket_list_fake import FreshdeskTicketFake

API_KEY = "freshdeskTriggerKey0123456789"
CONNECTION = ConnectionRef("freshdesk", "freshdesk-trigger")
MODULE_PATH = "github.com/superdurable/dex-connectors-library/connectors/freshworks/freshdesk/go"


class QueueTarget:
    """Consume each event into a queue."""

    def __init__(self) -> None:
        self.events: asyncio.Queue[TriggerEvent[TicketChangedEvent]] = asyncio.Queue()

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[TicketChangedEvent]) -> None:
        await self.events.put(event)


def new_fake_connection(fake: FreshdeskTicketFake, poll_interval_seconds: float = 60) -> Connection:
    """Connect a client to the ticket fake."""
    client = Client(
        Config(domain="acme", poll_interval_seconds=poll_interval_seconds),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString(API_KEY))}),
        httpx.AsyncClient(transport=fake.transport()),
        api_base_url=fake.base_url,
    )
    return new_connection(client, CONNECTION)


async def eventually(condition: Callable[[], Awaitable[bool]], timeout: float = 10) -> None:
    """Poll condition until it holds."""
    deadline = time.monotonic() + timeout
    while not await condition():
        assert time.monotonic() < deadline, "condition did not converge"
        await asyncio.sleep(0.01)


def test_configuration_validation_and_strict_decoding() -> None:
    """Reject unknown and repeated kinds, and unknown saved fields, as Go does."""
    validate_ticket_changed_trigger_configuration(TicketChangedTriggerConfiguration())
    validate_ticket_changed_trigger_configuration(
        TicketChangedTriggerConfiguration((TICKET_CHANGE_CREATED, TICKET_CHANGE_UPDATED))
    )
    with pytest.raises(ValueError, match="created or updated"):
        validate_ticket_changed_trigger_configuration(TicketChangedTriggerConfiguration(("deleted",)))
    with pytest.raises(ValueError, match="twice"):
        validate_ticket_changed_trigger_configuration(
            TicketChangedTriggerConfiguration((TICKET_CHANGE_CREATED, TICKET_CHANGE_CREATED))
        )
    assert decode_ticket_changed_trigger_configuration({"changeKinds": ["created"]}).change_kinds == (
        "created",
    )
    with pytest.raises(ValueError):
        decode_ticket_changed_trigger_configuration({"changeKind": ["created"]})


def test_client_rejects_a_poll_interval_under_ten_seconds() -> None:
    """Keep the poll loop inside Freshdesk's Tickets List sublimit, as Go's New does."""
    with pytest.raises(ValueError, match="pollInterval"):
        Client(Config(domain="acme", poll_interval_seconds=9), StaticCredentialProvider({}))


def test_client_keeps_the_default_for_a_zero_poll_interval() -> None:
    """A zero pollInterval, as a project may store it, polls each minute, as Go's withConfigDefaults does."""
    client = Client(Config(domain="acme", poll_interval_seconds=0), StaticCredentialProvider({}))
    assert client.config.poll_interval_seconds == 60


def test_runner_rejects_incomplete_routes() -> None:
    """Missing routes, names, targets, duplicates, unknown kinds, and mock connections fail at wiring."""
    connection = new_fake_connection(FreshdeskTicketFake(API_KEY))
    target = QueueTarget()
    for routes in (
        [],
        [TicketChangedTriggerRoute("", target)],
        [TicketChangedTriggerRoute("ticketChanges", None)],  # type: ignore[arg-type]
        [
            TicketChangedTriggerRoute("ticketChanges", target),
            TicketChangedTriggerRoute("ticketChanges", target),
        ],
        [TicketChangedTriggerRoute("ticketChanges", target, TicketChangedTriggerConfiguration(("merged",)))],
    ):
        with pytest.raises(ValueError):
            new_ticket_trigger_runner(TicketTriggerRunnerConfig(connection, routes))
    mock_connection = Mock().connection()
    with pytest.raises(ValueError, match="mock"):
        new_ticket_trigger_runner(
            TicketTriggerRunnerConfig(mock_connection, [TicketChangedTriggerRoute("ticketChanges", target)])
        )


def write_project(directory: Path, binding_name: str, configuration: dict[str, object]) -> Path:
    """Write the connections.json Dex Web saves for one connection and binding."""
    path = directory / "connections.json"
    path.write_text(
        json.dumps(
            {
                "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                "connections": [
                    {
                        "connectorId": "freshdesk",
                        "connectionName": CONNECTION.name,
                        "modulePath": MODULE_PATH,
                        "provider": "freshdesk",
                        "configuration": {"domain": "acme", "pollInterval": 10_000_000_000},
                        "credentials": {"api_key": API_KEY},
                    }
                ],
                "triggerBindings": [
                    {
                        "connectorId": "freshdesk",
                        "connectionName": CONNECTION.name,
                        "triggerName": "ticketChanged",
                        "bindingName": binding_name,
                        "configuration": configuration,
                    }
                ],
            }
        )
    )
    path.chmod(0o600)
    return path


async def test_project_runner_loads_the_saved_kinds_and_commits_after_recording(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    """A saved created-only binding delivers the new ticket, skips the update, and drains its inbox."""
    fake = FreshdeskTicketFake(API_KEY)
    now = datetime.now(UTC).replace(microsecond=0)
    fake.create_at(1001, "Existing", now - timedelta(hours=1), now - timedelta(minutes=10))
    monkeypatch.setenv(
        "DEX_CONNECTOR_CONFIG_FILE", str(write_project(tmp_path, "newTickets", {"changeKinds": ["created"]}))
    )
    project = await load_from_environment()
    # The fake answers at the helpdesk's own https://acme.freshdesk.com/api/v2 base URL.
    connection = new_project_connection(
        project, CONNECTION.name, httpx.AsyncClient(transport=fake.transport())
    )
    target = QueueTarget()
    try:
        runner = new_project_ticket_trigger_runner(
            project,
            connection,
            ProjectTicketTriggerRunnerConfig([ProjectTicketChangedTriggerRoute("newTickets", target)]),
        )
        key = TriggerInboxKey("freshdesk", CONNECTION.name, "ticketChanged", "newTickets")
        cursor = project.poll_cursor_store(PollCursorKey.from_trigger_inbox_key(key))

        async def committed_token() -> str:
            state = (await cursor.read_poll_cursor()).state
            return state.token if state is not None else ""

        async def is_seeded() -> bool:
            return bool(await committed_token())

        context = TriggerContext()
        task = asyncio.create_task(runner.run(context))
        await eventually(is_seeded)
        baseline = await committed_token()
        fake.update(1001, "Existing, edited", 3)
        fake.create(1002, "Double charge")
        runner.wake_poller("newTickets")
        event = await asyncio.wait_for(target.events.get(), 10)
        assert event.payload.ticket.id == 1002
        assert event.payload.change_kind == TICKET_CHANGE_CREATED

        async def has_moved() -> bool:
            return await committed_token() != baseline

        async def is_drained() -> bool:
            return not await project.trigger_inbox(key).pending()

        await eventually(has_moved)
        await eventually(is_drained)
        assert target.events.empty(), "the skipped update is never delivered"
        context.cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await task
    finally:
        await connection.aclose()
        await project.close()


async def test_in_memory_trigger_baselines_and_delivers_the_next_change() -> None:
    """The Go generated-Trigger equivalent seeds on start and delivers the next change."""
    fake = FreshdeskTicketFake(API_KEY)
    now = datetime.now(UTC).replace(microsecond=0)
    fake.create_at(1001, "Existing", now - timedelta(hours=1), now - timedelta(minutes=10))
    connection = new_fake_connection(fake, poll_interval_seconds=10)
    target = QueueTarget()
    with pytest.raises(ValueError, match="connection_name"):
        new_ticket_changed_trigger(
            connection=connection, connection_name="other", binding_name="allTickets", target=target
        )
    trigger = new_ticket_changed_trigger(
        connection=connection, connection_name=CONNECTION.name, binding_name="allTickets", target=target
    )
    context = TriggerContext()
    task = asyncio.create_task(trigger.run(context))
    try:

        async def has_seeded() -> bool:
            return fake.list_request_count() > 0

        await eventually(has_seeded)
        fake.update(1001, "Existing, escalated", 3)
        # The trigger polls again after one 10-second interval with jitter.
        event = await asyncio.wait_for(target.events.get(), 30)
        assert event.payload.change_kind == TICKET_CHANGE_UPDATED
        assert event.payload.ticket.subject == "Existing, escalated"
    finally:
        context.cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await task
        await connection.aclose()
