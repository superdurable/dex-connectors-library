# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Poll the account fake into a real Dex Worker: one Flow per change, across restarts and failures."""

import asyncio
import os
import time
from collections.abc import AsyncIterator, Awaitable, Callable
from contextlib import asynccontextmanager
from dataclasses import dataclass
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

from dex_connectors.zendesk_support import (
    TICKET_CHANGE_CREATED,
    TICKET_CHANGE_DELETED,
    TICKET_CHANGE_UPDATED,
    Client,
    Config,
    Connection,
    Credentials,
    TicketChangedEvent,
    TicketChangedTriggerConfiguration,
    TicketChangedTriggerRoute,
    TicketTriggerRunnerConfig,
    decode_ticket_changed_event_payload,
    new_connection,
    new_ticket_trigger_runner,
)

from .export_fake import API_BASE_URL, API_TOKEN, EMAIL, ZendeskExportFake
from .flow import (
    CONNECTION_NAME,
    TICKET_CHANGED_TRIGGER_BINDING,
    TicketChangedFlow,
    TicketChangeOutcome,
    accepts_every_ticket_change,
    map_to_ticket_change,
    resolve_ticket_change_flow_id,
)

SCOPE = Scope("zendesk-ticket-changed", "live")
INBOX_KEY = TriggerInboxKey(
    "zendesk-support", CONNECTION_NAME, "ticketChanged", TICKET_CHANGED_TRIGGER_BINDING
)


async def eventually(condition: Callable[[], Awaitable[bool]], timeout: float = 45) -> None:
    """Poll condition until it holds; convergence polling, never a guessed sleep."""
    deadline = time.monotonic() + timeout
    while not await condition():
        if time.monotonic() > deadline:
            raise AssertionError("condition did not converge")
        await asyncio.sleep(0.05)


@dataclass
class TriggerProcess:
    """One run of the application's Trigger side; stopping it models a process exit."""

    runner: PollRunner
    context: TriggerContext
    task: asyncio.Task[None]

    async def stop(self) -> None:
        """Cancel the runner and confirm it stopped as a cancelled run does."""
        self.context.cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await self.task


class CountingTarget:
    """Start the example's Flow, then remember each delivered event once, in delivery order."""

    def __init__(self, dex_client: dex.AsyncClient, flow: TicketChangedFlow) -> None:
        self._start = new_dex_flow_trigger_target(
            dex_client,
            flow,
            accepts_event=accepts_every_ticket_change,
            resolve_flow_id=resolve_ticket_change_flow_id,
            map_to_flow_input=map_to_ticket_change,
        )
        self.delivered: dict[str, TriggerEvent[TicketChangedEvent]] = {}

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[TicketChangedEvent]) -> None:
        await self._start.handle_trigger(context, event)
        self.delivered.setdefault(event.id, event)


class TicketChangedEnvironment:
    """A Dex Worker and Client for the example Flow, the account fake, and storage that outlives processes."""

    def __init__(
        self,
        dex_client: dex.AsyncClient,
        flow: TicketChangedFlow,
        connection: Connection,
        provider: ZendeskExportFake,
        configuration: TicketChangedTriggerConfiguration,
    ) -> None:
        self.dex_client = dex_client
        self.connection = connection
        self.provider = provider
        self.configuration = configuration
        self.objects = InMemoryObjectStore()
        self.target = CountingTarget(dex_client, flow)
        # Unique ticket IDs keep Flow IDs unique across runs against one Dex server.
        self.ticket_id_base = time.time_ns() // 1000 % 1_000_000_000 * 10

    def ticket_id(self, offset: int) -> int:
        """Return this run's ticket ID for offset."""
        return self.ticket_id_base + offset

    def cursor_store(self) -> PollCursorStore:
        """Bind the binding's cursor in the shared storage."""
        return PollCursorStore(self.objects, SCOPE, PollCursorKey.from_trigger_inbox_key(INBOX_KEY))

    async def cursor_state(self) -> PollCursorState | None:
        """Read the committed cursor, or None before the baseline."""
        return (await self.cursor_store().read_poll_cursor()).state

    def start_trigger_process(self) -> TriggerProcess:
        """Wire the counting target behind the durable inbox and cursor, as the project runner does."""
        target = new_durable_trigger_target(
            TriggerInbox(self.objects, SCOPE, INBOX_KEY),
            self.target,
            decode_payload=decode_ticket_changed_event_payload,
        )
        route = TicketChangedTriggerRoute(
            TICKET_CHANGED_TRIGGER_BINDING, target, self.configuration, self.cursor_store()
        )
        runner = new_ticket_trigger_runner(TicketTriggerRunnerConfig(self.connection, [route]))
        context = TriggerContext()
        return TriggerProcess(runner, context, asyncio.create_task(runner.run(context)))

    async def wait_for_caught_up_cursor(self) -> None:
        """Wait until the committed cursor ends at the fake's latest change."""

        async def is_caught_up() -> bool:
            state = await self.cursor_state()
            return state is not None and f'"afterCursor":"{self.provider.cursor()}"' in state.token

        await eventually(is_caught_up)

    def is_delivered(self, ticket_id: int) -> bool:
        """Report whether any change of ticket_id was delivered."""
        return any(event.payload.ticket.id == ticket_id for event in self.target.delivered.values())

    def request_statuses(self) -> list[int]:
        """Return the statuses the fake answered, in order."""
        return [request.status for request in self.provider.requests()]

    async def wait_for_outcomes(self, count: int) -> dict[int, TicketChangeOutcome]:
        """Wait for count deliveries, then each change's Flow; a later change replaces an earlier outcome."""

        async def has_delivered() -> bool:
            return len(self.target.delivered) >= count

        await eventually(has_delivered)
        outcomes: dict[int, TicketChangeOutcome] = {}
        for event in list(self.target.delivered.values()):
            flow_id = resolve_ticket_change_flow_id(event)
            result = await self.dex_client.wait_for_flow(flow_id, timedelta(seconds=30))
            assert result.status == dex.FlowStatus.COMPLETED, result.error_message
            outcome = result.single_output(TicketChangeOutcome)
            assert outcome.ticket_id == event.payload.ticket.id
            outcomes[outcome.ticket_id] = outcome
        return outcomes


@asynccontextmanager
async def ticket_changed_environment(
    tmp_path: Path, configuration: TicketChangedTriggerConfiguration
) -> AsyncIterator[TicketChangedEnvironment]:
    """Run a Worker for the example Flow against the account fake until the block ends."""
    server_address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
    provider = ZendeskExportFake()
    reference = ConnectionRef("zendesk", CONNECTION_NAME)
    client = Client(
        Config(subdomain="acme", poll_interval_seconds=5),
        StaticCredentialProvider({reference: Credentials(EMAIL, SecretString(API_TOKEN))}),
        httpx.AsyncClient(transport=provider.transport()),
        api_base_url=API_BASE_URL,
        incremental_export_requests_per_minute=600,
    )
    connection = new_connection(client, reference)
    flow = TicketChangedFlow(connection)
    registry = dex.Registry([flow], allow_async_handlers=True)
    cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 64 << 20))
    worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
    worker_task = asyncio.create_task(worker.start())
    try:
        await worker.wait_pull_registered(timeout=30)
        options = dex.ClientOptions(server_address=server_address)
        async with dex.AsyncClient(registry, cache, options) as dex_client:
            yield TicketChangedEnvironment(dex_client, flow, connection, provider, configuration)
    finally:
        await worker.stop()
        await worker_task
        await connection.aclose()
        cache.close()


@pytest.mark.asyncio
async def test_ticket_changes_start_one_flow_each_across_a_restart(tmp_path: Path) -> None:
    """Existing tickets are a baseline, each change starts one Flow, and a 429 restart misses none."""
    configuration = TicketChangedTriggerConfiguration(include_deleted=True)
    async with ticket_changed_environment(tmp_path, configuration) as environment:
        provider = environment.provider
        existing, created, spam, offline = (environment.ticket_id(offset) for offset in (1, 2, 3, 4))
        provider.create(existing, "Existing ticket")
        provider.create(spam, "Spam")
        first = environment.start_trigger_process()
        await environment.wait_for_caught_up_cursor()

        provider.create(created, "Double charge on order 88213")
        provider.update(existing, "pending")
        provider.delete(spam)
        first.runner.wake_poller(TICKET_CHANGED_TRIGGER_BINDING)
        outcomes = await environment.wait_for_outcomes(3)
        assert outcomes[created].change_kind == TICKET_CHANGE_CREATED
        created_current = outcomes[created].current
        assert created_current is not None
        assert created_current.latest_comments[0].body == "Opened: Double charge on order 88213"
        assert outcomes[existing].change_kind == TICKET_CHANGE_UPDATED
        existing_current = outcomes[existing].current
        # The Flow reads the ticket back from Zendesk.
        assert existing_current is not None and existing_current.ticket.status == "pending"
        assert outcomes[spam].change_kind == TICKET_CHANGE_DELETED
        assert outcomes[spam].current is None, "a deleted ticket is not read back"
        await first.stop()

        provider.create(offline, "Opened while stopped")
        provider.update(created, "solved")
        provider.rate_limit_next_requests(1, "1")
        second = environment.start_trigger_process()
        second.runner.wake_poller(TICKET_CHANGED_TRIGGER_BINDING)
        outcomes = await environment.wait_for_outcomes(5)
        offline_current, solved_current = outcomes[offline].current, outcomes[created].current
        assert offline_current is not None and offline_current.ticket.subject == "Opened while stopped"
        assert solved_current is not None and solved_current.ticket.status == "solved"
        await environment.wait_for_caught_up_cursor()
        # No change is delivered twice and the baseline is never delivered.
        assert len(environment.target.delivered) == 5
        assert 429 in environment.request_statuses()
        await second.stop()


@pytest.mark.asyncio
async def test_rejected_cursor_restarts_without_a_gap(tmp_path: Path) -> None:
    """A 422 cursor restarts the export from its last change time, so a change in between still starts."""
    async with ticket_changed_environment(tmp_path, TicketChangedTriggerConfiguration()) as environment:
        provider = environment.provider
        before, gap = environment.ticket_id(1), environment.ticket_id(2)
        provider.create(before, "Before the restart")
        first = environment.start_trigger_process()
        await environment.wait_for_caught_up_cursor()
        await first.stop()

        provider.invalidate_cursors()
        provider.create(gap, "Created after the cursor expired")
        second = environment.start_trigger_process()
        second.runner.wake_poller(TICKET_CHANGED_TRIGGER_BINDING)

        async def is_gap_delivered() -> bool:
            return environment.is_delivered(gap)

        await eventually(is_gap_delivered)
        # The restart may repeat up to the last minute, so the baseline ticket is not asserted either way.
        outcomes = await environment.wait_for_outcomes(1)
        gap_current = outcomes[gap].current
        assert gap_current is not None and gap_current.ticket.subject == "Created after the cursor expired"
        await environment.wait_for_caught_up_cursor()
        state = await environment.cursor_state()
        assert state is not None and state.reset_count == 0, (
            "a rejected cursor restarts by time, not a reseed"
        )
        assert 422 in environment.request_statuses()
        await second.stop()
