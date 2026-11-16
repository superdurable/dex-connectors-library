# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Poll the ticket fake into a real Dex Worker: one Flow per change, across restarts and failures."""

import asyncio
import os
import time
from collections.abc import AsyncIterator, Awaitable, Callable
from contextlib import asynccontextmanager
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
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

from dex_connectors.freshworks_freshdesk import (
    TICKET_CHANGE_CREATED,
    TICKET_CHANGE_UPDATED,
    Client,
    Config,
    Connection,
    Credentials,
    TicketChangedEvent,
    TicketChangedTriggerRoute,
    TicketTriggerRunnerConfig,
    decode_ticket_changed_event_payload,
    new_connection,
    new_ticket_trigger_runner,
)

from .flow import (
    CONNECTION_NAME,
    TICKET_CHANGED_TRIGGER_BINDING,
    TicketChangedFlow,
    TicketChangeOutcome,
    accepts_every_ticket_change,
    map_to_ticket_change,
    resolve_ticket_change_flow_id,
)
from .ticket_list_fake import FreshdeskTicketFake

API_KEY = "freshdeskIntegrationKey0123456789"
SCOPE = Scope("freshdesk-ticket-changed", "live")
INBOX_KEY = TriggerInboxKey("freshdesk", CONNECTION_NAME, "ticketChanged", TICKET_CHANGED_TRIGGER_BINDING)


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
    """A Dex Worker and Client for the example Flow, the ticket fake, and storage that outlives processes."""

    def __init__(
        self,
        dex_client: dex.AsyncClient,
        flow: TicketChangedFlow,
        connection: Connection,
        provider: FreshdeskTicketFake,
    ) -> None:
        self.dex_client = dex_client
        self.connection = connection
        self.provider = provider
        self.objects = InMemoryObjectStore()
        self.target = CountingTarget(dex_client, flow)
        # Unique ticket IDs keep Flow IDs unique across runs against one Dex server.
        self.ticket_id_base = time.time_ns() // 1000 % 1_000_000_000_000 * 10

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
        route = TicketChangedTriggerRoute(TICKET_CHANGED_TRIGGER_BINDING, target, cursor=self.cursor_store())
        runner = new_ticket_trigger_runner(TicketTriggerRunnerConfig(self.connection, [route]))
        context = TriggerContext()
        return TriggerProcess(runner, context, asyncio.create_task(runner.run(context)))

    async def wait_for_committed_cursor(self) -> None:
        """Wait until the baseline cursor is committed."""

        async def is_committed() -> bool:
            return await self.cursor_state() is not None

        await eventually(is_committed)

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

    async def wait_for_one_more_poll_and_empty_inbox(self, list_count: int) -> None:
        """Wait for one more completed list request and an empty inbox."""
        inbox = TriggerInbox(self.objects, SCOPE, INBOX_KEY)

        async def has_polled() -> bool:
            return self.provider.list_request_count() > list_count

        async def is_drained() -> bool:
            return not await inbox.pending()

        await eventually(has_polled)
        await eventually(is_drained)


@asynccontextmanager
async def ticket_changed_environment(tmp_path: Path) -> AsyncIterator[TicketChangedEnvironment]:
    """Run a Worker for the example Flow against the ticket fake until the block ends."""
    server_address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
    provider = FreshdeskTicketFake(API_KEY)
    reference = ConnectionRef("freshdesk", CONNECTION_NAME)
    client = Client(
        Config(domain="acme", poll_interval_seconds=10),
        StaticCredentialProvider({reference: Credentials(SecretString(API_KEY))}),
        httpx.AsyncClient(transport=provider.transport()),
        api_base_url=provider.base_url,
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
            yield TicketChangedEnvironment(dex_client, flow, connection, provider)
    finally:
        await worker.stop()
        await worker_task
        cache.close()


@pytest.mark.asyncio
async def test_ticket_changes_start_one_flow_each_across_a_restart(tmp_path: Path) -> None:
    """Existing tickets are a baseline, each later change starts one Flow, and a restart misses nothing."""
    async with ticket_changed_environment(tmp_path) as environment:
        provider = environment.provider
        existing, created, offline = (environment.ticket_id(offset) for offset in (1, 2, 3))
        now = datetime.now(UTC)
        provider.create_at(existing, "Existing", now - timedelta(hours=1), now - timedelta(minutes=10))
        first = environment.start_trigger_process()
        await environment.wait_for_committed_cursor()

        provider.create(created, "Double charge on order 88213")
        provider.update(existing, "Existing, escalated", 3)
        first.runner.wake_poller(TICKET_CHANGED_TRIGGER_BINDING)
        outcomes = await environment.wait_for_outcomes(2)
        assert outcomes[created].change_kind == TICKET_CHANGE_CREATED
        # The Flow reads the ticket back with the text and requester the list omits.
        current = outcomes[created].current
        assert current.ticket.description == "Customer message for Double charge on order 88213"
        assert current.requester is not None
        assert current.requester.email == "jane@acme.example.com"
        assert outcomes[existing].change_kind == TICKET_CHANGE_UPDATED
        assert outcomes[existing].current.ticket.status == 3
        await first.stop()

        provider.create(offline, "Opened while stopped")
        provider.update(created, "Double charge, refunded", 4)
        second = environment.start_trigger_process()
        second.runner.wake_poller(TICKET_CHANGED_TRIGGER_BINDING)
        outcomes = await environment.wait_for_outcomes(4)
        assert outcomes[offline].current.ticket.subject == "Opened while stopped"
        assert outcomes[offline].change_kind == TICKET_CHANGE_CREATED
        assert outcomes[created].change_kind == TICKET_CHANGE_UPDATED
        assert outcomes[created].current.ticket.status == 4
        list_count = provider.list_request_count()
        second.runner.wake_poller(TICKET_CHANGED_TRIGGER_BINDING)
        await environment.wait_for_one_more_poll_and_empty_inbox(list_count)
        # No change is delivered twice and the baseline is never delivered.
        assert len(environment.target.delivered) == 4
        await second.stop()


@pytest.mark.asyncio
async def test_rate_limit_and_rejected_cursor_recover(tmp_path: Path) -> None:
    """A 429 waits for Retry-After and then delivers; a 400 reseeds without delivering the gap."""
    async with ticket_changed_environment(tmp_path) as environment:
        provider = environment.provider
        throttled, gap, after_reset = (environment.ticket_id(offset) for offset in (1, 2, 3))
        process = environment.start_trigger_process()
        await environment.wait_for_committed_cursor()

        provider.fail_next_list(429, "1", '{"code":"too_many_requests"}')
        provider.create(throttled, "Created during a rate limit")
        process.runner.wake_poller(TICKET_CHANGED_TRIGGER_BINDING)
        outcomes = await environment.wait_for_outcomes(1)
        assert outcomes[throttled].change_kind == TICKET_CHANGE_CREATED
        await process.stop()

        provider.create(gap, "Lost in the gap")
        provider.fail_next_list(
            400,
            "",
            '{"description":"Validation failed","errors":[{"field":"updated_since","code":"invalid_value"}]}',
        )
        restarted = environment.start_trigger_process()
        restarted.runner.wake_poller(TICKET_CHANGED_TRIGGER_BINDING)

        async def has_reset() -> bool:
            state = await environment.cursor_state()
            return state is not None and state.reset_count == 1

        await eventually(has_reset)
        provider.create(after_reset, "After the reset")
        restarted.runner.wake_poller(TICKET_CHANGED_TRIGGER_BINDING)
        outcomes = await environment.wait_for_outcomes(2)
        assert outcomes[after_reset].current.ticket.subject == "After the reset"
        # Changes between the rejected cursor and the new baseline are not delivered.
        assert gap not in outcomes
        await restarted.stop()
