# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Poll the Xero invoice fake into a real Dex Worker: one Flow per change, across restarts."""

import asyncio
import json
import os
import time
import uuid
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

from dex_connectors.xero import (
    Client,
    Config,
    Connection,
    Credentials,
    InvoiceChangedEvent,
    InvoiceChangedTriggerConfiguration,
    InvoiceChangedTriggerRoute,
    InvoiceTriggerRunnerConfig,
    decode_invoice_changed_event_payload,
    new_connection,
    new_invoice_trigger_runner,
)
from dex_connectors.xero.invoice_change_fetcher import unix_milliseconds

from .flow import (
    CONNECTION_NAME,
    INVOICE_CHANGED_TRIGGER_BINDING,
    InvoiceChangedFlow,
    InvoiceChangeOutcome,
    accepts_every_invoice_change,
    map_to_invoice_change,
    resolve_invoice_change_flow_id,
)
from .invoice_fake import XeroInvoiceFake

SCOPE = Scope("xero-invoice-changed", "live")
INBOX_KEY = TriggerInboxKey("xero", CONNECTION_NAME, "invoiceChanged", INVOICE_CHANGED_TRIGGER_BINDING)


async def eventually(condition: Callable[[], Awaitable[bool]], timeout: float = 45) -> None:
    """Poll condition until it holds; convergence polling, never a guessed sleep."""
    deadline = time.monotonic() + timeout
    while not await condition():
        if time.monotonic() > deadline:
            raise AssertionError("condition did not converge")
        await asyncio.sleep(0.05)


def new_invoice_id() -> str:
    """Return a random invoice UUID, so Flow IDs stay unique across runs against one Dex server."""
    return str(uuid.uuid4())


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
        provider: XeroInvoiceFake,
    ) -> None:
        self.dex_client = dex_client
        self.connection = connection
        self.provider = provider
        self.objects = InMemoryObjectStore()
        self.target = CountingTarget(dex_client, flow)
        self.configuration = InvoiceChangedTriggerConfiguration()

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
            INVOICE_CHANGED_TRIGGER_BINDING, target, self.configuration, self.cursor_store()
        )
        runner = new_invoice_trigger_runner(InvoiceTriggerRunnerConfig(self.connection, [route]))
        context = TriggerContext()
        return TriggerProcess(runner, context, asyncio.create_task(runner.run(context)))

    async def wait_for_committed_cursor(self) -> None:
        """Wait until the baseline cursor is committed."""

        async def is_committed() -> bool:
            return await self.cursor_state() is not None

        await eventually(is_committed)

    async def wait_for_committed_since(self, since: int) -> None:
        """Wait until the committed cursor's high-water mark reaches since, in Unix milliseconds."""

        async def has_reached() -> bool:
            state = await self.cursor_state()
            return state is not None and json.loads(state.token)["since"] == since

        await eventually(has_reached)

    async def wait_for_outcomes(self, count: int) -> dict[str, InvoiceChangeOutcome]:
        """Wait for count deliveries, then each change's Flow; a later change replaces an earlier outcome."""

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


@asynccontextmanager
async def invoice_changed_environment(tmp_path: Path) -> AsyncIterator[InvoiceChangedEnvironment]:
    """Run a Worker for the example Flow against a fresh fake organisation until the block ends."""
    server_address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
    provider = XeroInvoiceFake()
    reference = ConnectionRef("xero", CONNECTION_NAME)
    credentials = Credentials(
        auth_method_id="custom-connection", access_token=SecretString("inert-xero-access-token")
    )
    http_client = httpx.AsyncClient(transport=provider.transport())
    client = Client(
        Config(poll_interval_seconds=30),
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
async def test_invoice_changes_start_one_flow_each_across_a_restart(tmp_path: Path) -> None:
    """Existing invoices are a baseline, each later change starts one Flow, and a restart misses nothing."""
    async with invoice_changed_environment(tmp_path) as environment:
        provider = environment.provider
        existing, draft = new_invoice_id(), new_invoice_id()
        provider.upsert(existing, "INV-0001", "ACCREC", "PAID", "49.95")
        provider.upsert(draft, "INV-0002", "ACCREC", "DRAFT", "20.00")
        provider.advance_clock(60_000)
        first = environment.start_trigger_process()
        await environment.wait_for_committed_cursor()

        bill = new_invoice_id()
        provider.upsert(bill, "BILL-77", "ACCPAY", "AUTHORISED", "120.00")
        provider.set_status(draft, "AUTHORISED")
        first.runner.wake_poller(INVOICE_CHANGED_TRIGGER_BINDING)
        outcomes = await environment.wait_for_outcomes(2)
        assert outcomes[bill].current.type == "ACCPAY"
        assert outcomes[draft].delivered_status == "AUTHORISED"
        # The Flow reads the invoice back from Xero.
        assert outcomes[draft].current.status == "AUTHORISED"
        await first.stop()

        offline = new_invoice_id()
        provider.upsert(offline, "INV-0003", "ACCREC", "DRAFT", "5.00")
        last_update = provider.set_status(draft, "PAID")
        second = environment.start_trigger_process()
        second.runner.wake_poller(INVOICE_CHANGED_TRIGGER_BINDING)
        outcomes = await environment.wait_for_outcomes(4)
        assert outcomes[offline].current.invoice_number == "INV-0003"
        assert outcomes[draft].current.status == "PAID"
        # The baseline is never delivered, and no change is delivered twice.
        assert existing not in outcomes
        await environment.wait_for_committed_since(last_update)
        assert len(environment.target.delivered) == 4
        last = max(environment.target.delivered.values(), key=lambda event: event.occurred_at)
        assert unix_milliseconds(last.occurred_at) == last_update
        await second.stop()


@pytest.mark.asyncio
async def test_changed_filter_records_a_new_baseline(tmp_path: Path) -> None:
    """A saved filter change reseeds without delivering the gap; later matching changes still arrive."""
    async with invoice_changed_environment(tmp_path) as environment:
        provider = environment.provider
        first = environment.start_trigger_process()
        await environment.wait_for_committed_cursor()
        await first.stop()

        gap = new_invoice_id()
        provider.upsert(gap, "INV-GAP", "ACCREC", "PAID", "9.00")
        provider.advance_clock(60_000)
        environment.configuration = InvoiceChangedTriggerConfiguration(statuses=("PAID",))
        second = environment.start_trigger_process()

        async def has_reset() -> bool:
            state = await environment.cursor_state()
            return state is not None and state.reset_count == 1

        await eventually(has_reset)
        paid = new_invoice_id()
        provider.upsert(paid, "INV-PAID", "ACCREC", "PAID", "12.00")
        provider.upsert(new_invoice_id(), "INV-OPEN", "ACCREC", "AUTHORISED", "3.00")
        second.runner.wake_poller(INVOICE_CHANGED_TRIGGER_BINDING)
        outcomes = await environment.wait_for_outcomes(1)
        assert outcomes[paid].current.status == "PAID"
        # Changes between the old cursor and the new baseline are not delivered.
        assert gap not in outcomes and len(outcomes) == 1
        await second.stop()


@pytest.mark.asyncio
async def test_minute_rate_limit_pauses_then_delivers(tmp_path: Path) -> None:
    """A 429 with Retry-After pauses the poll, then the change arrives with one Flow."""
    async with invoice_changed_environment(tmp_path) as environment:
        provider = environment.provider
        process = environment.start_trigger_process()
        await environment.wait_for_committed_cursor()
        invoice_id = new_invoice_id()
        provider.upsert(invoice_id, "INV-0429", "ACCREC", "AUTHORISED", "7.00")
        provider.rate_limit_next(1, "minute", "1")
        process.runner.wake_poller(INVOICE_CHANGED_TRIGGER_BINDING)
        outcomes = await environment.wait_for_outcomes(1)
        assert outcomes[invoice_id].current.invoice_number == "INV-0429"
        assert sum(1 for request in provider.requests() if request.status == 429) == 1
        await process.stop()
