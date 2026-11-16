# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Poll the QuickBooks invoice fake into a real Dex Worker: one Flow per version, across restarts."""

import asyncio
import os
import random
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

from dex_connectors.intuit_quickbooks import (
    INVOICE_CHANGE_CREATED,
    INVOICE_CHANGE_UPDATED,
    Client,
    Config,
    Connection,
    Credentials,
    InvoiceChangedEvent,
    InvoiceChangedTriggerRoute,
    InvoiceTriggerRunnerConfig,
    decode_invoice_changed_event_payload,
    new_connection,
    new_invoice_trigger_runner,
)

from .flow import (
    CONNECTION_NAME,
    INVOICE_CHANGED_TRIGGER_BINDING,
    InvoiceChangedFlow,
    InvoiceChangeOutcome,
    accepts_every_invoice_change,
    map_to_invoice_change,
    resolve_invoice_change_flow_id,
)
from .invoice_fake import QuickBooksInvoiceFake

SCOPE = Scope("quickbooks-invoice-changed", "live")
INBOX_KEY = TriggerInboxKey("quickbooks", CONNECTION_NAME, "invoiceChanged", INVOICE_CHANGED_TRIGGER_BINDING)


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

    def __init__(self, dex_client: dex.AsyncClient, flow: InvoiceChangedFlow) -> None:
        self._start = new_dex_flow_trigger_target(
            dex_client,
            flow,
            accepts_event=accepts_every_invoice_change,
            resolve_flow_id=resolve_invoice_change_flow_id,
            map_to_flow_input=map_to_invoice_change,
        )
        self.delivered: dict[str, TriggerEvent[InvoiceChangedEvent]] = {}

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[InvoiceChangedEvent]) -> None:
        await self._start.handle_trigger(context, event)
        self.delivered.setdefault(event.id, event)


class InvoiceChangedEnvironment:
    """A Dex Worker and Client for the example Flow, the invoice fake, and storage that outlives processes."""

    def __init__(
        self,
        dex_client: dex.AsyncClient,
        flow: InvoiceChangedFlow,
        connection: Connection,
        provider: QuickBooksInvoiceFake,
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
            decode_payload=decode_invoice_changed_event_payload,
        )
        route = InvoiceChangedTriggerRoute(
            INVOICE_CHANGED_TRIGGER_BINDING, target, cursor=self.cursor_store()
        )
        runner = new_invoice_trigger_runner(InvoiceTriggerRunnerConfig(self.connection, [route]))
        context = TriggerContext()
        return TriggerProcess(runner, context, asyncio.create_task(runner.run(context)))

    async def wait_for_committed_cursor(self) -> None:
        """Wait until the baseline cursor is committed."""

        async def is_committed() -> bool:
            return await self.cursor_state() is not None

        await eventually(is_committed)

    async def wait_for_outcomes(self, count: int) -> dict[str, InvoiceChangeOutcome]:
        """Wait for count deliveries, then each change's Flow; a later version replaces an earlier outcome."""

        async def has_delivered() -> bool:
            return len(self.target.delivered) >= count

        await eventually(has_delivered)
        outcomes: dict[str, InvoiceChangeOutcome] = {}
        for event in list(self.target.delivered.values()):
            result = await self.dex_client.wait_for_flow(
                resolve_invoice_change_flow_id(event), timedelta(seconds=30)
            )
            assert result.status == dex.FlowStatus.COMPLETED, result.error_message
            outcome = result.single_output(InvoiceChangeOutcome)
            assert outcome.invoice_id == event.payload.invoice.invoice_id
            outcomes[outcome.invoice_id] = outcome
        return outcomes

    async def wait_for_one_more_poll_and_empty_inbox(self, query_count: int) -> None:
        """Wait for one more answered query and an empty inbox."""
        inbox = TriggerInbox(self.objects, SCOPE, INBOX_KEY)

        async def has_polled() -> bool:
            return len(self.provider.queries()) > query_count

        async def is_drained() -> bool:
            return not await inbox.pending()

        await eventually(has_polled)
        await eventually(is_drained)


@asynccontextmanager
async def invoice_changed_environment(tmp_path: Path) -> AsyncIterator[InvoiceChangedEnvironment]:
    """Run a Worker for the example Flow against a fresh fake company until the block ends."""
    server_address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
    # A fresh company keeps Trigger event IDs, and so Flow IDs, unique across runs against one Dex server.
    realm_id = str(random.randrange(10**15, 10**16))
    provider = QuickBooksInvoiceFake(realm_id)
    reference = ConnectionRef("quickbooks", CONNECTION_NAME)
    credentials = Credentials(
        "test-client",
        SecretString("test-secret"),
        SecretString("integration-access-token"),
        SecretString("integration-refresh-token"),
        SecretString(""),
    )
    http_client = httpx.AsyncClient(transport=provider.transport())
    client = Client(
        Config(realm_id=realm_id, poll_interval_seconds=5),
        StaticCredentialProvider({reference: credentials}),
        http_client,
        local_provider_url="http://127.0.0.1:1",
    )
    connection = new_connection(client, reference)
    flow = InvoiceChangedFlow(connection)
    registry = dex.Registry([flow], allow_async_handlers=True)
    cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 64 << 20))
    worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
    worker_task = asyncio.create_task(worker.start())
    try:
        await worker.wait_pull_registered(timeout=30)
        options = dex.ClientOptions(server_address=server_address)
        async with dex.AsyncClient(registry, cache, options) as dex_client:
            yield InvoiceChangedEnvironment(dex_client, flow, connection, provider)
    finally:
        await worker.stop()
        await worker_task
        await http_client.aclose()
        cache.close()


@pytest.mark.asyncio
async def test_invoice_changes_start_one_flow_each_across_a_throttled_restart(tmp_path: Path) -> None:
    """Existing invoices are a baseline, each later version starts one Flow, and a restart misses nothing."""
    async with invoice_changed_environment(tmp_path) as environment:
        provider = environment.provider
        existing = provider.create("ORDER-1", "58", "100.00")
        first = environment.start_trigger_process()
        await environment.wait_for_committed_cursor()

        created = provider.create("ORDER-2", "58", "25.50")
        provider.pay(existing)
        first.runner.wake_poller(INVOICE_CHANGED_TRIGGER_BINDING)
        outcomes = await environment.wait_for_outcomes(2)
        assert outcomes[created].change_kind == INVOICE_CHANGE_CREATED
        assert not outcomes[created].is_paid and outcomes[created].current.balance == "25.50"
        # The Flow reads the invoice back, so it records the payment QuickBooks applied.
        assert outcomes[existing].change_kind == INVOICE_CHANGE_UPDATED and outcomes[existing].is_paid
        assert outcomes[existing].realm_id == provider.realm_id
        await first.stop()

        offline = provider.create("ORDER-3", "59", "40.00")
        provider.pay(created)
        # The restarted process's first poll is throttled; it waits for Retry-After and then delivers.
        provider.throttle(1, "1")
        second = environment.start_trigger_process()
        second.runner.wake_poller(INVOICE_CHANGED_TRIGGER_BINDING)
        outcomes = await environment.wait_for_outcomes(4)
        assert outcomes[offline].change_kind == INVOICE_CHANGE_CREATED
        assert outcomes[offline].current.doc_number == "ORDER-3"
        assert outcomes[created].change_kind == INVOICE_CHANGE_UPDATED and outcomes[created].is_paid
        query_count = len(provider.queries())
        second.runner.wake_poller(INVOICE_CHANGED_TRIGGER_BINDING)
        await environment.wait_for_one_more_poll_and_empty_inbox(query_count)
        # No version is delivered twice and the baseline is never delivered.
        assert len(environment.target.delivered) == 4
        await second.stop()


@pytest.mark.asyncio
async def test_rejected_watermark_reseeds_without_delivering_the_gap(tmp_path: Path) -> None:
    """A validation Fault for the watermark query records a new baseline; later changes still arrive."""
    async with invoice_changed_environment(tmp_path) as environment:
        provider = environment.provider
        provider.create("ORDER-1", "58", "10.00")
        process = environment.start_trigger_process()
        await environment.wait_for_committed_cursor()
        await process.stop()

        gap = provider.create("LOST-IN-GAP", "58", "11.00")
        provider.reject_watermark_queries(True)
        restarted = environment.start_trigger_process()
        restarted.runner.wake_poller(INVOICE_CHANGED_TRIGGER_BINDING)

        async def has_reset() -> bool:
            state = await environment.cursor_state()
            return state is not None and state.reset_count == 1

        await eventually(has_reset)
        provider.reject_watermark_queries(False)
        after_reset = provider.create("AFTER-RESET", "58", "12.00")
        restarted.runner.wake_poller(INVOICE_CHANGED_TRIGGER_BINDING)
        outcomes = await environment.wait_for_outcomes(1)
        assert outcomes[after_reset].current.doc_number == "AFTER-RESET"
        # Changes between the rejected cursor and the new baseline are not delivered.
        assert gap not in outcomes
        await restarted.stop()
