# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""ticketChanged runner wiring, saved configuration, and the in-memory Trigger."""

import asyncio
import json
import time
from collections.abc import Awaitable, Callable
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

from dex_connectors.zoho_desk import (
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
    new_project_ticket_trigger_runner,
    new_ticket_changed_trigger,
    new_ticket_trigger_runner,
    validate_ticket_changed_trigger_configuration,
)
from dex_connectors.zoho_desk.deskmock import Mock
from examples.ticket_changed.desk_search_fake import ACCESS_TOKEN, BASE_URL, ORGANIZATION_ID, DeskSearchFake

CONNECTION = ConnectionRef("zoho", "zoho-desk-trigger")
MODULE_PATH = "github.com/superdurable/dex-connectors-library/connectors/zoho/desk/go"
BILLING = "1892000000006907"
SUPPORT = "1892000000082069"


class QueueTarget:
    """Consume each event into a queue."""

    def __init__(self) -> None:
        self.events: asyncio.Queue[TriggerEvent[TicketChangedEvent]] = asyncio.Queue()

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[TicketChangedEvent]) -> None:
        await self.events.put(event)


def new_fake_connection(fake: DeskSearchFake, poll_interval_seconds: float = 60) -> Connection:
    """Connect a client to the Zoho Desk fake."""
    client = Client(
        Config(org_id=ORGANIZATION_ID, poll_interval_seconds=poll_interval_seconds),
        StaticCredentialProvider({CONNECTION: Credentials(access_token=SecretString(ACCESS_TOKEN))}),
        httpx.AsyncClient(transport=fake.transport()),
        api_base_url=BASE_URL,
    )
    return new_connection(client, CONNECTION)


async def eventually(condition: Callable[[], Awaitable[bool]], timeout: float = 10) -> None:
    """Poll condition until it holds."""
    deadline = time.monotonic() + timeout
    while not await condition():
        assert time.monotonic() < deadline, "condition did not converge"
        await asyncio.sleep(0.01)


def test_configuration_validation_and_strict_decoding() -> None:
    """Accept a blank or numeric department, reject another, and reject unknown saved fields, as Go does."""
    validate_ticket_changed_trigger_configuration(TicketChangedTriggerConfiguration())
    validate_ticket_changed_trigger_configuration(TicketChangedTriggerConfiguration(department_id=BILLING))
    with pytest.raises(ValueError, match="numeric Zoho Desk ID"):
        validate_ticket_changed_trigger_configuration(
            TicketChangedTriggerConfiguration(department_id="Billing")
        )
    decoded = decode_ticket_changed_trigger_configuration(
        {"departmentId": BILLING, "departmentName": "Billing"}
    )
    assert decoded == TicketChangedTriggerConfiguration(department_id=BILLING, department_name="Billing")
    with pytest.raises(ValueError):
        decode_ticket_changed_trigger_configuration({"department": BILLING})


def test_client_rejects_a_poll_interval_under_fifteen_seconds() -> None:
    """Keep polls from spending the organization's daily API credits, as Go's New does."""
    with pytest.raises(ValueError, match="pollInterval"):
        Client(Config(org_id=ORGANIZATION_ID, poll_interval_seconds=14), StaticCredentialProvider({}))


def test_runner_rejects_incomplete_routes() -> None:
    """Missing routes, names, targets, duplicates, bad departments, and mock connections fail at wiring."""
    connection = new_fake_connection(DeskSearchFake())
    target = QueueTarget()
    for routes in (
        [],
        [TicketChangedTriggerRoute("", target)],
        [TicketChangedTriggerRoute("ticketChanges", None)],  # type: ignore[arg-type]
        [
            TicketChangedTriggerRoute("ticketChanges", target),
            TicketChangedTriggerRoute("ticketChanges", target),
        ],
        [
            TicketChangedTriggerRoute(
                "ticketChanges", target, TicketChangedTriggerConfiguration(department_id="Billing")
            )
        ],
    ):
        with pytest.raises(ValueError):
            new_ticket_trigger_runner(TicketTriggerRunnerConfig(connection, routes))
    with pytest.raises(ValueError, match="mock"):
        new_ticket_trigger_runner(
            TicketTriggerRunnerConfig(
                Mock().connection(), [TicketChangedTriggerRoute("ticketChanges", target)]
            )
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
                        "connectorId": "zoho-desk",
                        "connectionName": CONNECTION.name,
                        "modulePath": MODULE_PATH,
                        "provider": "zoho",
                        "authMethodId": "zoho-us-oauth",
                        "configuration": {"orgId": ORGANIZATION_ID, "pollInterval": 15_000_000_000},
                        "credentials": {
                            "oauth_client_id": "inert",
                            "access_token": "inert",
                            "refresh_token": "inert",
                        },
                    }
                ],
                "triggerBindings": [
                    {
                        "connectorId": "zoho-desk",
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


async def test_project_runner_loads_the_saved_department_and_commits_after_recording(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    """A saved Billing binding searches only Billing, delivers its new ticket, and drains its inbox."""
    fake = DeskSearchFake()
    fake.create("Existing", BILLING)
    monkeypatch.setenv(
        "DEX_CONNECTOR_CONFIG_FILE",
        str(
            write_project(tmp_path, "billingTickets", {"departmentId": BILLING, "departmentName": "Billing"})
        ),
    )
    project = await load_from_environment()
    connection = new_fake_connection(fake)
    target = QueueTarget()
    try:
        runner = new_project_ticket_trigger_runner(
            project,
            connection,
            ProjectTicketTriggerRunnerConfig([ProjectTicketChangedTriggerRoute("billingTickets", target)]),
        )
        key = TriggerInboxKey("zoho-desk", CONNECTION.name, "ticketChanged", "billingTickets")
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
        assert f'"departmentId":"{BILLING}"' in baseline
        fake.create("Other department", SUPPORT)
        created = fake.create("Refund request", BILLING)
        runner.wake_poller("billingTickets")
        event = await asyncio.wait_for(target.events.get(), 10)
        assert event.payload.ticket.id == created
        assert event.payload.change_kind == TICKET_CHANGE_CREATED
        assert event.payload.department_id == BILLING

        async def has_moved() -> bool:
            return await committed_token() != baseline

        async def is_drained() -> bool:
            return not await project.trigger_inbox(key).pending()

        await eventually(has_moved)
        await eventually(is_drained)
        assert target.events.empty(), "the baseline and the other department are never delivered"
        assert {search.department_id for search in fake.searches} == {BILLING}
        context.cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await task
    finally:
        await connection.aclose()
        await project.close()


async def test_in_memory_trigger_baselines_and_delivers_the_next_change() -> None:
    """The Go generated-Trigger equivalent seeds on start and delivers the next change."""
    fake = DeskSearchFake()
    existing = fake.create("Existing", BILLING)
    connection = new_fake_connection(fake, poll_interval_seconds=15)
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
            return len(fake.searches) > 0

        await eventually(has_seeded)
        fake.update(existing, "On Hold")
        # The trigger polls again after one 15-second interval with jitter.
        event = await asyncio.wait_for(target.events.get(), 40)
        assert event.payload.change_kind == TICKET_CHANGE_UPDATED
        assert event.payload.ticket.status == "On Hold"
    finally:
        context.cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await task
        await connection.aclose()
