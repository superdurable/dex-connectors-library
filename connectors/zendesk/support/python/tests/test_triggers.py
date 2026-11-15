# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""ticketChanged fetches against the stateful account fake, runner wiring, and the in-memory Trigger."""

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
from dex_connectors.sdk.pollingtrigger import Cursor, RateLimitedError, is_cursor_invalid
from dex_connectors.sdk.projectconfig import PollCursorKey, TriggerInboxKey, load_from_environment

from dex_connectors.zendesk_support import (
    TICKET_CHANGE_CREATED,
    TICKET_CHANGE_DELETED,
    TICKET_CHANGE_UPDATED,
    TICKET_STATUS_DELETED,
    Client,
    Config,
    Connection,
    Credentials,
    ProjectTicketChangedTriggerRoute,
    ProjectTicketTriggerRunnerConfig,
    TicketChangedEvent,
    TicketChangedTriggerConfiguration,
    TicketChangedTriggerRoute,
    TicketExportFetcher,
    TicketTriggerRunnerConfig,
    decode_ticket_changed_trigger_configuration,
    new_connection,
    new_project_connection,
    new_project_ticket_trigger_runner,
    new_ticket_changed_trigger,
    new_ticket_trigger_runner,
)
from dex_connectors.zendesk_support.supportmock import Mock
from examples.ticket_changed.export_fake import API_BASE_URL, API_TOKEN, EMAIL, ZendeskExportFake

CONNECTION = ConnectionRef("zendesk", "zendesk-ticket-watcher")
MODULE_PATH = "github.com/superdurable/dex-connectors-library/connectors/zendesk/support/go"


class QueueTarget:
    """Consume each event into a queue."""

    def __init__(self) -> None:
        self.events: asyncio.Queue[TriggerEvent[TicketChangedEvent]] = asyncio.Queue()

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[TicketChangedEvent]) -> None:
        await self.events.put(event)


def new_fake_client(
    fake: ZendeskExportFake,
    max_response_bytes: int = 4194304,
    poll_interval_seconds: float = 60,
    now: Callable[[], datetime] | None = None,
) -> Client:
    """Connect a client to the account fake with a fast export request rate."""
    return Client(
        Config(
            subdomain="acme",
            max_response_bytes=max_response_bytes,
            poll_interval_seconds=poll_interval_seconds,
        ),
        StaticCredentialProvider({CONNECTION: Credentials(EMAIL, SecretString(API_TOKEN))}),
        httpx.AsyncClient(transport=fake.transport()),
        api_base_url=API_BASE_URL,
        now=now,
        incremental_export_requests_per_minute=600,
    )


def new_fetcher(
    fake: ZendeskExportFake, include_deleted: bool = False, max_response_bytes: int = 4194304
) -> TicketExportFetcher:
    """Build a fetcher on the account fake."""
    configuration = TicketChangedTriggerConfiguration(include_deleted=include_deleted)
    return TicketExportFetcher(new_fake_client(fake, max_response_bytes), CONNECTION, configuration)


async def eventually(condition: Callable[[], Awaitable[bool]], timeout: float = 10) -> None:
    """Poll condition until it holds."""
    deadline = time.monotonic() + timeout
    while not await condition():
        assert time.monotonic() < deadline, "condition did not converge"
        await asyncio.sleep(0.01)


async def test_seed_drains_existing_changes_and_fetch_delivers_later_ones() -> None:
    """The baseline ends at the latest change; later changes arrive with derived kinds."""
    fake = ZendeskExportFake()
    fake.create(101, "Existing ticket")
    fetcher = new_fetcher(fake)
    context = TriggerContext()
    cursor = await fetcher.seed_cursor(context)
    assert fake.cursor() in cursor.token

    fake.create(102, "Double charge")
    fake.update(101, "pending")
    fake.create(103, "Spam")
    fake.delete(103)
    page = await fetcher.fetch_changes(context, cursor)
    assert not page.has_more_pages
    assert [change.payload.ticket.id for change in page.changes] == [102, 101], "a deletion is excluded"
    created, updated = page.changes
    assert created.payload.change_kind == TICKET_CHANGE_CREATED
    assert created.payload.ticket.agent_url == "https://acme.zendesk.com/agent/tickets/102"
    assert updated.payload.change_kind == TICKET_CHANGE_UPDATED
    assert updated.payload.ticket.status == "pending"
    assert updated.payload.generated_at > datetime(2026, 1, 1, tzinfo=UTC)
    requests = fake.requests()
    assert "start_time" in requests[0].query and "cursor" not in requests[0].query
    assert requests[-1].query["per_page"] == "100"
    assert requests[-1].query["exclude_deleted"] == "true"
    assert requests[-1].query["cursor"]

    again = await fetcher.fetch_changes(context, page.next_cursor)
    assert not again.changes
    assert again.next_cursor == page.next_cursor, "a caught-up poll leaves the cursor where it was"


async def test_include_deleted_delivers_deletions() -> None:
    """With includeDeleted the export is asked for deletions, delivered as deleted."""
    fake = ZendeskExportFake()
    fetcher = new_fetcher(fake, include_deleted=True)
    cursor = await fetcher.seed_cursor(TriggerContext())
    fake.create(201, "Spam")
    fake.delete(201)
    page = await fetcher.fetch_changes(TriggerContext(), cursor)
    [change] = page.changes
    assert change.payload.change_kind == TICKET_CHANGE_DELETED
    assert change.payload.ticket.status == TICKET_STATUS_DELETED
    assert "exclude_deleted" not in fake.requests()[1].query


async def test_rejected_cursor_restarts_after_its_last_change_time() -> None:
    """A 422 cursor restarts by start_time and loses nothing; a repeated version keeps its event ID."""
    fake = ZendeskExportFake()
    fetcher = new_fetcher(fake)
    cursor = await fetcher.seed_cursor(TriggerContext())
    fake.create(301, "First")
    first = await fetcher.fetch_changes(TriggerContext(), cursor)
    assert len(first.changes) == 1
    fake.invalidate_cursors()
    fake.create(302, "In the gap")
    restarted = await fetcher.fetch_changes(TriggerContext(), first.next_cursor)
    assert restarted.changes[-1].payload.ticket.id == 302
    if len(restarted.changes) == 2:
        assert restarted.changes[0].id == first.changes[0].id
    requests = fake.requests()
    assert requests[-2].status == 422
    assert "start_time" in requests[-1].query


async def test_ticket_change_id_is_a_pure_function_of_the_ticket_version() -> None:
    """A refetch yields the same event ID and a new version a new one."""
    fake = ZendeskExportFake()
    fake.create(351, "Repeated read")
    old = Cursor('{"resumeAt":1}')
    first = await new_fetcher(fake).fetch_changes(TriggerContext(), old)
    second = await new_fetcher(fake).fetch_changes(TriggerContext(), old)
    assert len(first.changes) == 1 and first.changes[0].id == second.changes[0].id
    fake.update(351, "open")
    third = await new_fetcher(fake).fetch_changes(TriggerContext(), old)
    assert third.changes[0].id != first.changes[0].id


async def test_undecodable_cursor_is_invalid_without_a_request() -> None:
    """The kit records a new baseline for a cursor that is not a Go cursor document."""
    fake = ZendeskExportFake()
    with pytest.raises(Exception) as raised:
        await new_fetcher(fake).fetch_changes(TriggerContext(), Cursor("not-json"))
    assert is_cursor_invalid(raised.value)
    assert not fake.requests()


async def test_rate_limit_and_agent_rejection() -> None:
    """A 429 carries Retry-After; a 403 names the administrator requirement without the token."""
    fake = ZendeskExportFake()
    fetcher = new_fetcher(fake)
    fake.rate_limit_next_requests(1, "17")
    with pytest.raises(RateLimitedError) as rate_limited:
        await fetcher.seed_cursor(TriggerContext())
    assert rate_limited.value.retry_after == timedelta(seconds=17)
    fake.reject_as_agent(True)
    with pytest.raises(RuntimeError, match="only for administrators") as rejected:
        await fetcher.seed_cursor(TriggerContext())
    assert API_TOKEN not in str(rejected.value)
    assert not is_cursor_invalid(rejected.value)


async def test_pages_follow_after_cursor_until_end_of_stream() -> None:
    """A page that is not the end of the stream reports more pages."""
    fake = ZendeskExportFake()
    fetcher = new_fetcher(fake)
    cursor = await fetcher.seed_cursor(TriggerContext())
    fake.limit_per_page(2)
    for ticket_id in (401, 402, 403):
        fake.create(ticket_id, "Burst")
    first = await fetcher.fetch_changes(TriggerContext(), cursor)
    assert len(first.changes) == 2 and first.has_more_pages
    second = await fetcher.fetch_changes(TriggerContext(), first.next_cursor)
    assert [change.payload.ticket.id for change in second.changes] == [403]
    assert not second.has_more_pages


async def test_oversized_pages_halve_per_page_before_failing() -> None:
    """per_page halves from 100 to 10, then the poll fails with guidance."""
    fake = ZendeskExportFake()
    for ticket_id in range(501, 521):
        fake.create(ticket_id, "Large backlog")
    with pytest.raises(ValueError, match="raise maxResponseBytes"):
        await new_fetcher(fake, max_response_bytes=512).seed_cursor(TriggerContext())
    assert [request.query["per_page"] for request in fake.requests()] == ["100", "50", "25", "12", "10"]


async def test_changes_tied_in_one_second_survive_a_cursor_reset_across_a_page_boundary() -> None:
    """The restart starts at the tied second, so the third ticket is not skipped."""
    fake = ZendeskExportFake()
    fetcher = new_fetcher(fake)
    cursor = await fetcher.seed_cursor(TriggerContext())
    fake.limit_per_page(2)
    fake.tie_changes(True)
    for ticket_id in (601, 602, 603):
        fake.create(ticket_id, "Same second")
    first = await fetcher.fetch_changes(TriggerContext(), cursor)
    assert first.has_more_pages and len(first.changes) == 2
    fake.invalidate_cursors()
    fake.limit_per_page(1000)
    restarted = await fetcher.fetch_changes(TriggerContext(), first.next_cursor)
    assert [change.payload.ticket.id for change in restarted.changes] == [601, 602, 603]
    assert restarted.changes[0].id == first.changes[0].id


async def test_seed_tolerates_a_worker_clock_ahead_of_zendesk() -> None:
    """A start_time five minutes before a clock three minutes ahead is still accepted."""
    fake = ZendeskExportFake()
    client = new_fake_client(fake, now=lambda: datetime.now(UTC) + timedelta(minutes=3))
    await TicketExportFetcher(client, CONNECTION, TicketChangedTriggerConfiguration()).seed_cursor(
        TriggerContext()
    )


async def test_rejected_start_time_restart_keeps_the_cursor() -> None:
    """A new baseline would lose the changes after resumeAt, so the poll fails instead."""
    fake = ZendeskExportFake()
    client = new_fake_client(fake, now=lambda: datetime.now(UTC) + timedelta(minutes=3))
    fetcher = TicketExportFetcher(client, CONNECTION, TicketChangedTriggerConfiguration())
    token = json.dumps({"resumeAt": int(time.time())})
    with pytest.raises(RuntimeError) as raised:
        await fetcher.fetch_changes(TriggerContext(), Cursor(token))
    assert not is_cursor_invalid(raised.value)
    assert fake.requests()[0].status == 400


async def test_export_requests_are_spaced_by_the_client_rate() -> None:
    """Two bindings of one client share its export request spacing."""
    fake = ZendeskExportFake()
    client = Client(
        Config(subdomain="acme"),
        StaticCredentialProvider({CONNECTION: Credentials(EMAIL, SecretString(API_TOKEN))}),
        httpx.AsyncClient(transport=fake.transport()),
        api_base_url=API_BASE_URL,
        incremental_export_requests_per_minute=120,
    )
    fetchers = [
        TicketExportFetcher(client, CONNECTION, TicketChangedTriggerConfiguration()) for _ in range(3)
    ]
    started = time.monotonic()
    await asyncio.gather(*(fetcher.seed_cursor(TriggerContext()) for fetcher in fetchers))
    # 120 per minute is one request each 0.5s, so the third waits about a second.
    assert time.monotonic() - started >= 0.9


def test_client_validates_poll_interval_and_export_rate() -> None:
    """Go's New rejects a pollInterval under 5s and a request rate outside 1 to 600."""
    credentials = StaticCredentialProvider[Credentials]({})
    with pytest.raises(ValueError, match="pollInterval"):
        Client(Config(subdomain="acme", poll_interval_seconds=4), credentials)
    for rate in (0, 601):
        with pytest.raises(ValueError, match="requests per minute"):
            Client(Config(subdomain="acme"), credentials, incremental_export_requests_per_minute=rate)
    assert Client(Config(subdomain="acme"), credentials).poll_interval == timedelta(minutes=1)


def test_saved_configuration_decodes_strictly() -> None:
    """includeDeleted must be a boolean and unknown fields are rejected, as Go strict decoding does."""
    assert decode_ticket_changed_trigger_configuration({"includeDeleted": True}).include_deleted
    assert not decode_ticket_changed_trigger_configuration({}).include_deleted
    for invalid in ({"includeDeleted": "true"}, {"includeDeletions": True}):
        with pytest.raises(ValueError):
            decode_ticket_changed_trigger_configuration(invalid)


def test_runner_rejects_incomplete_routes() -> None:
    """Missing routes, names, or targets, duplicates, and mock connections fail at wiring."""
    connection = new_connection(new_fake_client(ZendeskExportFake()), CONNECTION)
    target = QueueTarget()
    for routes in (
        [],
        [TicketChangedTriggerRoute("", target)],
        [TicketChangedTriggerRoute("ticketChanges", None)],  # type: ignore[arg-type]
        [
            TicketChangedTriggerRoute("ticketChanges", target),
            TicketChangedTriggerRoute("ticketChanges", target),
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
    connection = {
        "connectorId": "zendesk-support",
        "connectionName": CONNECTION.name,
        "modulePath": MODULE_PATH,
        "provider": "zendesk",
        "configuration": {"subdomain": "acme", "pollInterval": 5_000_000_000},
        "credentials": {"email": EMAIL, "api_token": API_TOKEN},
    }
    binding = {
        "connectorId": "zendesk-support",
        "connectionName": CONNECTION.name,
        "triggerName": "ticketChanged",
        "bindingName": binding_name,
        "configuration": configuration,
    }
    document = {
        "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
        "connections": [connection],
        "triggerBindings": [binding],
    }
    path.write_text(json.dumps(document))
    path.chmod(0o600)
    return path


async def test_project_runner_loads_the_saved_configuration_and_commits_after_recording(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    """A saved includeDeleted binding delivers a deletion through its inbox, then drains it."""
    fake = ZendeskExportFake()
    fake.create(1001, "Existing")
    monkeypatch.setenv(
        "DEX_CONNECTOR_CONFIG_FILE", str(write_project(tmp_path, "allChanges", {"includeDeleted": True}))
    )
    project = await load_from_environment()
    # The fake answers at the account's own https://acme.zendesk.com/api/v2 base URL.
    connection: Connection = new_project_connection(
        project, CONNECTION.name, httpx.AsyncClient(transport=fake.transport())
    )
    target = QueueTarget()
    try:
        runner = new_project_ticket_trigger_runner(
            project,
            connection,
            ProjectTicketTriggerRunnerConfig([ProjectTicketChangedTriggerRoute("allChanges", target)]),
        )
        key = TriggerInboxKey("zendesk-support", CONNECTION.name, "ticketChanged", "allChanges")
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
        assert '"includeDeleted":true' in baseline
        fake.delete(1001)
        runner.wake_poller("allChanges")
        event = await asyncio.wait_for(target.events.get(), 20)
        assert event.payload.ticket.id == 1001
        assert event.payload.change_kind == TICKET_CHANGE_DELETED

        async def has_moved() -> bool:
            return await committed_token() != baseline

        async def is_drained() -> bool:
            return not await project.trigger_inbox(key).pending()

        await eventually(has_moved, 20)
        await eventually(is_drained)
        context.cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await task
    finally:
        await connection.aclose()
        await project.close()


async def test_in_memory_trigger_baselines_and_delivers_the_next_change() -> None:
    """The Go generated-Trigger equivalent seeds on start and delivers the next change."""
    fake = ZendeskExportFake()
    fake.create(1001, "Existing")
    connection = new_connection(new_fake_client(fake, poll_interval_seconds=5), CONNECTION)
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
            return len(fake.requests()) > 0

        await eventually(has_seeded)
        fake.update(1001, "solved")
        # The trigger polls again after one 5-second interval with jitter.
        event = await asyncio.wait_for(target.events.get(), 30)
        assert event.payload.change_kind == TICKET_CHANGE_UPDATED
        assert event.payload.ticket.status == "solved"
    finally:
        context.cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await task
        await connection.aclose()
