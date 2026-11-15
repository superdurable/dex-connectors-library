# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deliver Gorgias ticket events through the example's Flow target to a real Dex Worker and Flow."""

import asyncio
import os
import tempfile
import time
from collections.abc import AsyncIterator, Callable
from contextlib import asynccontextmanager
from dataclasses import dataclass, field
from datetime import timedelta
from pathlib import Path
from typing import Any, cast

import dex
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
from dex_connectors.sdk.pollingtrigger import PollRunner
from dex_connectors.sdk.projectconfig import (
    Configuration,
    InMemoryObjectStore,
    LoadedProject,
    PollCursorKey,
    PollCursorState,
    PollCursorStore,
    Scope,
    TriggerInbox,
    TriggerInboxKey,
)

from dex_connectors.gorgias import (
    Client,
    Config,
    Credentials,
    ProjectTicketChangedTriggerRoute,
    ProjectTicketTriggerRunnerConfig,
    TicketChangedEvent,
    new_connection,
    new_project_ticket_trigger_runner,
)
from tests.gorgias_events_fake import API_KEY, EMAIL, GorgiasEventsFake

from .flow import BINDING_NAME, CONNECTION_NAME, TicketChangeFlow, TicketChangeOutcome, resolve_flow_id
from .worker import new_ticket_change_flow_target

SCOPE = Scope("gorgias-ticket-changed", "live")
INBOX_KEY = TriggerInboxKey("gorgias", CONNECTION_NAME, "ticketChanged", BINDING_NAME)


class DurableProject:
    """The project storage two Trigger processes share: hosted storage keeps inboxes and cursors."""

    def __init__(self) -> None:
        self.objects = InMemoryObjectStore()
        self.configuration = Configuration(SCOPE, 1, ())

    def trigger_inbox(self, key: TriggerInboxKey) -> TriggerInbox:
        return TriggerInbox(self.objects, SCOPE, key)

    def poll_cursor_store(self, key: PollCursorKey) -> PollCursorStore:
        return PollCursorStore(self.objects, SCOPE, key)

    async def cursor_state(self) -> PollCursorState | None:
        return (
            await self.poll_cursor_store(PollCursorKey.from_trigger_inbox_key(INBOX_KEY)).read_poll_cursor()
        ).state


@dataclass
class CountingTarget:
    """Wraps the example's Flow target and records each delivered event once."""

    inner: Any
    delivered: dict[str, TriggerEvent[TicketChangedEvent]] = field(default_factory=dict)

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[TicketChangedEvent]) -> None:
        await self.inner.handle_trigger(context, event)
        self.delivered.setdefault(event.id, event)


async def eventually(condition: Callable[[], Any], timeout: float = 30) -> None:
    """Poll a sync or async condition until it holds."""
    deadline = time.monotonic() + timeout
    while True:
        result = condition()
        if asyncio.iscoroutine(result):
            result = await result
        if result:
            return
        assert time.monotonic() < deadline, "condition did not converge"
        await asyncio.sleep(0.05)


@asynccontextmanager
async def trigger_process(runner: PollRunner) -> AsyncIterator[PollRunner]:
    """Run one Trigger process until the block exits."""
    cancellation = TriggerCancellation()
    task = asyncio.create_task(runner.run(TriggerContext(cancellation)))
    try:
        yield runner
    finally:
        cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await task


@dataclass
class Environment:
    provider: GorgiasEventsFake
    project: DurableProject
    target: CountingTarget
    dex_client: dex.AsyncClient
    connection: Any
    id_base: int

    def ticket_id(self, offset: int) -> int:
        return self.id_base + offset

    def new_runner(self) -> PollRunner:
        routes = [ProjectTicketChangedTriggerRoute(BINDING_NAME, self.target)]
        return new_project_ticket_trigger_runner(
            cast(LoadedProject, self.project), self.connection, ProjectTicketTriggerRunnerConfig(routes)
        )

    async def wait_for_committed_cursor(self) -> None:
        async def has_cursor() -> bool:
            return await self.project.cursor_state() is not None

        await eventually(has_cursor)

    async def wait_for_outcomes(self, count: int) -> dict[int, TicketChangeOutcome]:
        """Wait for count deliveries and return each Flow's outcome by Gorgias event ID."""
        await eventually(lambda: len(self.target.delivered) >= count)
        outcomes: dict[int, TicketChangeOutcome] = {}
        for event in list(self.target.delivered.values()):
            result = await asyncio.wait_for(
                self.dex_client.wait_for_flow(resolve_flow_id(event), timedelta(seconds=30)), 40
            )
            assert result.status == dex.FlowStatus.COMPLETED, result.error_message
            outcome = result.single_output(TicketChangeOutcome)
            assert outcome.ticket_id == event.payload.ticket_id
            outcomes[event.payload.event_id] = outcome
        return outcomes


@asynccontextmanager
async def ticket_changed_environment() -> AsyncIterator[Environment]:
    """Serve the example Flow on a real Dex Worker against a fake Gorgias helpdesk."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    provider = GorgiasEventsFake()
    # Unique ticket and event IDs keep Flow IDs unique across runs against one Dex server.
    id_base = (time.time_ns() // 1000) % 1_000_000_000_000 * 10
    provider.next_event_id = id_base
    reference = ConnectionRef("gorgias", CONNECTION_NAME)
    credentials = StaticCredentialProvider({reference: Credentials(EMAIL, SecretString(API_KEY))})
    async with httpx.AsyncClient(transport=httpx.MockTransport(provider.handle)) as http_client:
        client = Client(Config(domain="acme", poll_interval_seconds=10), credentials, http_client)
        connection = new_connection(client, reference)
        flow = TicketChangeFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="gorgias-ticket-changed-blobs-") as directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(directory) / "blobs"), 1 << 26))
            worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
            worker_task = asyncio.create_task(worker.start())
            try:
                await worker.wait_pull_registered(timeout=30)
                options = dex.ClientOptions(server_address=server_address)
                async with dex.AsyncClient(registry, cache, options) as dex_client:
                    yield Environment(
                        provider,
                        DurableProject(),
                        CountingTarget(new_ticket_change_flow_target(dex_client, flow)),
                        dex_client,
                        connection,
                        id_base,
                    )
            finally:
                await worker.stop()
                await worker_task
                cache.close()


@pytest.mark.asyncio
async def test_ticket_events_start_one_flow_each_across_a_restart_with_real_dex() -> None:
    """Earlier events are a baseline; each later event, also one made while stopped, starts one Flow."""
    async with ticket_changed_environment() as environment:
        provider = environment.provider
        existing, created, offline = (environment.ticket_id(offset) for offset in (1, 2, 3))
        provider.set_ticket(existing, "Existing", "open")
        provider.record_ticket_event("ticket-created", existing)
        async with trigger_process(environment.new_runner()) as first:
            await environment.wait_for_committed_cursor()
            provider.set_ticket(created, "Double charge on order 88213", "open", "billing")
            created_event = provider.record_ticket_event("ticket-created", created)
            provider.set_ticket(existing, "Existing", "closed")
            closed_event = provider.record_ticket_event("ticket-closed", existing, 12)
            first.wake_poller(BINDING_NAME)
            outcomes = await environment.wait_for_outcomes(2)
        current = outcomes[created_event].current
        assert outcomes[created_event].event_type == "ticket-created"
        assert current.latest_messages[0].body == "Customer message for Double charge on order 88213"
        assert current.ticket.tags == ("billing",)
        assert current.requester is not None and current.requester.email == "ada@example.com"
        assert outcomes[closed_event].event_type == "ticket-closed"
        assert outcomes[closed_event].current.ticket.status == "closed"

        provider.set_ticket(offline, "Opened while stopped", "open")
        offline_event = provider.record_ticket_event("ticket-created", offline)
        reopened_event = provider.record_ticket_event("ticket-reopened", existing, 12)
        async with trigger_process(environment.new_runner()) as second:
            second.wake_poller(BINDING_NAME)
            outcomes = await environment.wait_for_outcomes(4)
            requests_before = provider.event_request_count
            second.wake_poller(BINDING_NAME)
            await eventually(lambda: provider.event_request_count > requests_before)
        assert outcomes[offline_event].current.ticket.subject == "Opened while stopped"
        assert outcomes[reopened_event].event_type == "ticket-reopened"
        assert len(environment.target.delivered) == 4, "no event twice and never the baseline"


@pytest.mark.asyncio
async def test_rate_limit_and_rejected_request_recover_with_real_dex() -> None:
    """A 429 waits for Retry-After, and a 400 backs off and retries from the same cursor without a reseed."""
    async with ticket_changed_environment() as environment:
        provider = environment.provider
        throttled, rejected = environment.ticket_id(1), environment.ticket_id(2)
        async with trigger_process(environment.new_runner()) as process:
            await environment.wait_for_committed_cursor()
            provider.fail_next_events(429, "1", '{"error":{"msg":"too_many_requests"}}')
            provider.set_ticket(throttled, "Created during a rate limit", "open")
            throttled_event = provider.record_ticket_event("ticket-created", throttled)
            process.wake_poller(BINDING_NAME)
            outcomes = await environment.wait_for_outcomes(1)
        assert outcomes[throttled_event].ticket_id == throttled

        provider.set_ticket(rejected, "Recorded while Gorgias rejected a poll", "open")
        rejected_event = provider.record_ticket_event("ticket-created", rejected)
        bad_query = '{"error":{"msg":"invalid_query","data":{"created_datetime":["bad"]}}}'
        provider.fail_next_events(400, "", bad_query)
        async with trigger_process(environment.new_runner()) as restarted:
            restarted.wake_poller(BINDING_NAME)
            outcomes = await environment.wait_for_outcomes(2)
        assert outcomes[rejected_event].current.ticket.subject == "Recorded while Gorgias rejected a poll"
        state = await environment.project.cursor_state()
        assert state is not None and state.reset_count == 0, "a 400 never records a new baseline"
