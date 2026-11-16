# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Poll the Zoho Desk fake into a real Dex Worker: one Flow per ticket version, across restarts."""

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

from dex_connectors.zoho_desk import (
    TICKET_CHANGE_CREATED,
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

from .desk_search_fake import ACCESS_TOKEN, BASE_URL, ORGANIZATION_ID, DeskSearchFake
from .flow import (
    CONNECTION_NAME,
    TICKET_CHANGED_TRIGGER_BINDING,
    TicketChangedFlow,
    TicketChangeOutcome,
    accepts_every_ticket_change,
    map_to_ticket_change,
    resolve_ticket_change_flow_id,
)

DEPARTMENT_ID = "1892000000006907"
SCOPE = Scope("zoho-desk-ticket-changed", "live")
INBOX_KEY = TriggerInboxKey("zoho-desk", CONNECTION_NAME, "ticketChanged", TICKET_CHANGED_TRIGGER_BINDING)


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
    """A Dex Worker and Client, the Zoho Desk fake, and storage that outlives Trigger processes."""

    def __init__(
        self,
        dex_client: dex.AsyncClient,
        flow: TicketChangedFlow,
        connection: Connection,
        provider: DeskSearchFake,
    ) -> None:
        self.dex_client = dex_client
        self.connection = connection
        self.provider = provider
        self.objects = InMemoryObjectStore()
        self.target = CountingTarget(dex_client, flow)

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
            TICKET_CHANGED_TRIGGER_BINDING,
            target,
            TicketChangedTriggerConfiguration(department_id=DEPARTMENT_ID, department_name="Billing"),
            self.cursor_store(),
        )
        runner = new_ticket_trigger_runner(TicketTriggerRunnerConfig(self.connection, [route]))
        context = TriggerContext()
        return TriggerProcess(runner, context, asyncio.create_task(runner.run(context)))

    async def wait_for_committed_cursor(self) -> None:
        """Wait until the baseline cursor is committed."""

        async def is_committed() -> bool:
            return await self.cursor_state() is not None

        await eventually(is_committed)

    async def wait_for_outcomes(self, count: int) -> dict[str, TicketChangeOutcome]:
        """Wait for count deliveries, then each change's Flow; a later version replaces an earlier outcome."""

        async def has_delivered() -> bool:
            return len(self.target.delivered) >= count

        await eventually(has_delivered)
        outcomes: dict[str, TicketChangeOutcome] = {}
        for event in list(self.target.delivered.values()):
            result = await self.dex_client.wait_for_flow(
                resolve_ticket_change_flow_id(event), timedelta(seconds=30)
            )
            assert result.status == dex.FlowStatus.COMPLETED, result.error_message
            outcome = result.single_output(TicketChangeOutcome)
            assert outcome.ticket_id == event.payload.ticket.id
            outcomes[outcome.ticket_id] = outcome
        return outcomes

    async def wait_for_one_more_search_and_empty_inbox(self) -> None:
        """Wait for one more completed search and an empty inbox."""
        inbox = TriggerInbox(self.objects, SCOPE, INBOX_KEY)
        search_count = len(self.provider.searches)

        async def has_searched() -> bool:
            return len(self.provider.searches) > search_count

        async def is_drained() -> bool:
            return not await inbox.pending()

        await eventually(has_searched)
        await eventually(is_drained)


@asynccontextmanager
async def ticket_changed_environment(tmp_path: Path) -> AsyncIterator[TicketChangedEnvironment]:
    """Run a Worker for the example Flow against the Zoho Desk fake until the block ends."""
    server_address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
    provider = DeskSearchFake()
    reference = ConnectionRef("zoho", CONNECTION_NAME)
    client = Client(
        Config(org_id=ORGANIZATION_ID, poll_interval_seconds=15),
        StaticCredentialProvider({reference: Credentials(access_token=SecretString(ACCESS_TOKEN))}),
        httpx.AsyncClient(transport=provider.transport()),
        api_base_url=BASE_URL,
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
        await connection.aclose()


@pytest.mark.asyncio
async def test_ticket_changes_start_one_flow_each_across_a_restart(tmp_path: Path) -> None:
    """Recent tickets are a baseline, each later version starts one Flow, and a restart misses nothing."""
    async with ticket_changed_environment(tmp_path) as environment:
        provider = environment.provider
        existing = provider.create("Existing before the baseline", DEPARTMENT_ID)
        provider.create("Other department", "1892000000082069")
        first = environment.start_trigger_process()
        await environment.wait_for_committed_cursor()

        created = provider.create("Refund request", DEPARTMENT_ID)
        provider.update(existing, "On Hold")
        first.runner.wake_poller(TICKET_CHANGED_TRIGGER_BINDING)
        outcomes = await environment.wait_for_outcomes(2)
        assert outcomes[created].change_kind == TICKET_CHANGE_CREATED
        assert outcomes[created].current.ticket.subject == "Refund request"
        # The Flow reads the full ticket back; the search page leaves the description out.
        assert outcomes[created].current.ticket.description_html == "<div>Refund request</div>"
        assert outcomes[existing].change_kind == TICKET_CHANGE_UPDATED
        assert outcomes[existing].current.ticket.status == "On Hold"
        await first.stop()

        offline = provider.create("Opened while stopped", DEPARTMENT_ID)
        provider.update(created, "Closed")
        # The first poll after the restart is rate limited and retried after Retry-After.
        provider.rate_limit_next(1, "1")
        second = environment.start_trigger_process()
        second.runner.wake_poller(TICKET_CHANGED_TRIGGER_BINDING)
        outcomes = await environment.wait_for_outcomes(4)
        assert outcomes[offline].current.ticket.subject == "Opened while stopped"
        assert outcomes[offline].change_kind == TICKET_CHANGE_CREATED
        assert outcomes[created].current.ticket.status == "Closed"
        assert outcomes[created].change_kind == TICKET_CHANGE_UPDATED
        second.runner.wake_poller(TICKET_CHANGED_TRIGGER_BINDING)
        await environment.wait_for_one_more_search_and_empty_inbox()
        # No ticket version is delivered twice and the baseline is never delivered.
        assert len(environment.target.delivered) == 4
        assert {search.department_id for search in provider.searches} == {DEPARTMENT_ID}
        await second.stop()


@pytest.mark.asyncio
async def test_rejected_time_range_records_a_new_baseline(tmp_path: Path) -> None:
    """A 422 naming modifiedTimeRange reseeds the cursor, and later tickets still start Flows."""
    async with ticket_changed_environment(tmp_path) as environment:
        provider = environment.provider
        process = environment.start_trigger_process()
        await environment.wait_for_committed_cursor()

        provider.reject_next_ranges(1)
        process.runner.wake_poller(TICKET_CHANGED_TRIGGER_BINDING)

        async def has_reset() -> bool:
            state = await environment.cursor_state()
            return state is not None and state.reset_count == 1

        await eventually(has_reset)
        after_reset = provider.create("After the reset", DEPARTMENT_ID)
        process.runner.wake_poller(TICKET_CHANGED_TRIGGER_BINDING)
        outcomes = await environment.wait_for_outcomes(1)
        assert outcomes[after_reset].current.ticket.subject == "After the reset"
        await process.stop()
