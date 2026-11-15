# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deliver rows from a fake Sheets tab through the durable rowAdded runner to a real Dex Worker and Flow,
which reads each row back from the same tab."""

import asyncio
import io
import logging
import os
import tempfile
import time
from collections import Counter
from datetime import timedelta
from pathlib import Path

import dex
import httpx
import pytest
from dex_connectors.sdk import (
    ConnectionRef,
    SecretString,
    StandardLoggingTriggerLogger,
    StaticCredentialProvider,
    TriggerContext,
    TriggerDeliveryAbortedError,
    TriggerEvent,
)
from dex_connectors.sdk.projectconfig import (
    InMemoryObjectStore,
    PollCursorKey,
    PollCursorStore,
    Scope,
    TriggerInbox,
    TriggerInboxKey,
    new_durable_trigger_target,
)

from dex_connectors.google_spreadsheet import (
    Client,
    Config,
    Credentials,
    RowAddedEvent,
    RowAddedTriggerConfiguration,
    RowAddedTriggerRoute,
    RowAddedTriggerRunnerConfig,
    decode_row_added_event_payload,
    new_connection,
    new_row_added_trigger_runner,
)

from .flow import CONNECTION_NAME, FLOW_ID_PREFIX, NEW_ROW_TRIGGER_BINDING, NewRowIntakeFlow, Outcome
from .intake import new_added_row_target
from .sheets_fake import SHEETS_ENDPOINT, ScriptedResponse, SheetsFake

INBOX_KEY = TriggerInboxKey("google-sheets", CONNECTION_NAME, "rowAdded", NEW_ROW_TRIGGER_BINDING)


class CountingTarget:
    """Counts Flow-start deliveries per event ID after Dex accepted the start."""

    def __init__(self, flow_target: object) -> None:
        self._flow_target = flow_target
        self.deliveries: Counter[str] = Counter()
        self.order: list[str] = []

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[RowAddedEvent]) -> None:
        await self._flow_target.handle_trigger(context, event)  # type: ignore[attr-defined]
        if not self.deliveries[event.id]:
            self.order.append(event.id)
        self.deliveries[event.id] += 1


class IntakeHarness:
    """Builds the durable runner over one shared store each time, as a restarted replica finds it."""

    def __init__(
        self, sheet: SheetsFake, dex_client: dex.AsyncClient, flow: NewRowIntakeFlow, logs: io.StringIO
    ) -> None:
        self.sheet = sheet
        self.dex_client = dex_client
        self.objects, self.scope = InMemoryObjectStore(), Scope("sheets", "preview")
        self.inbox = TriggerInbox(self.objects, self.scope, INBOX_KEY)
        self.target = CountingTarget(new_added_row_target(dex_client, flow))
        handler = logging.StreamHandler(logs)
        standard = logging.getLogger("google-sheets-python-intake-test")
        standard.addHandler(handler)
        standard.setLevel(logging.DEBUG)
        self.logger = StandardLoggingTriggerLogger(standard)
        self.claimed: set[str] = set()

    def start(self) -> tuple[TriggerContext, asyncio.Task[None]]:
        reference = ConnectionRef("google", CONNECTION_NAME)
        credentials = Credentials(
            "client", SecretString(""), SecretString("SENTINEL-ACCESS-TOKEN"), SecretString("")
        )
        client = Client(
            Config(SHEETS_ENDPOINT, poll_interval_seconds=1),
            StaticCredentialProvider({reference: credentials}),
            httpx.AsyncClient(transport=self.sheet.transport()),
        )
        durable = new_durable_trigger_target(
            self.inbox, self.target, decode_payload=decode_row_added_event_payload, logger=self.logger
        )
        cursor = PollCursorStore(self.objects, self.scope, PollCursorKey.from_trigger_inbox_key(INBOX_KEY))
        route = RowAddedTriggerRoute(
            NEW_ROW_TRIGGER_BINDING, RowAddedTriggerConfiguration("sheet-1", "Leads"), cursor, durable
        )
        runner = new_row_added_trigger_runner(
            RowAddedTriggerRunnerConfig(new_connection(client, reference), [route], logger=self.logger)
        )
        context = TriggerContext()
        return context, asyncio.create_task(runner.run(context))

    async def stop(self, running: tuple[TriggerContext, asyncio.Task[None]]) -> None:
        context, task = running
        context.cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await asyncio.wait_for(task, 10)

    async def require_completed_row(self, row_number: int) -> Outcome:
        """Wait for the delivered event of row_number, then for its Flow to complete."""
        prefix = f"row-{row_number}-"
        async with asyncio.timeout(30):
            while True:
                found = [
                    event
                    for event in self.target.order
                    if event.startswith(prefix) and event not in self.claimed
                ]
                if found:
                    break
                await asyncio.sleep(0.05)
        event_id = found[0]
        self.claimed.add(event_id)
        result = await self.dex_client.wait_for_flow(FLOW_ID_PREFIX + event_id, timedelta(seconds=30))
        assert result.status == dex.FlowStatus.COMPLETED, result.error_message
        outcome = result.single_output(Outcome)
        assert outcome.row.event_id == event_id
        return outcome


async def wait_for_requests(sheet: SheetsFake, count: int) -> None:
    async with asyncio.timeout(30):
        while sheet.completed_requests() < count:
            await asyncio.sleep(0.02)


@pytest.mark.asyncio
async def test_new_row_intake_starts_one_flow_per_added_row_across_restarts() -> None:
    """Existing rows start nothing, each appended row starts one Flow, and a restart misses nothing."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    run_id = str(time.time_ns())
    sheet = SheetsFake("Leads", ["email", "name", "run"], ["existing@example.test", "Existing", run_id])
    logs = io.StringIO()
    with tempfile.TemporaryDirectory(prefix="sheets-python-intake-") as directory:
        # The Flow reads each row back through its own connection to the same fake tab.
        reference = ConnectionRef("google", CONNECTION_NAME)
        flow_credentials = Credentials(
            "client", SecretString(""), SecretString("SENTINEL-ACCESS-TOKEN"), SecretString("")
        )
        flow_connection = new_connection(
            Client(
                Config(SHEETS_ENDPOINT),
                StaticCredentialProvider({reference: flow_credentials}),
                httpx.AsyncClient(transport=sheet.transport()),
            ),
            reference,
        )
        flow = NewRowIntakeFlow(flow_connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(directory) / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=30)
            client_options = dex.ClientOptions(server_address=server_address)
            async with dex.AsyncClient(registry, cache, client_options) as dex_client:
                harness = IntakeHarness(sheet, dex_client, flow, logs)
                first_run = harness.start()
                await wait_for_requests(sheet, 2)  # The seed and one poll after it.
                sheet.append_rows(
                    ["ada@example.test", "Ada", run_id], [], ["grace@example.test", "Grace", run_id]
                )
                ada = await harness.require_completed_row(3)
                assert (ada.row.spreadsheet_id, ada.row.sheet_name, ada.row.row_number) == (
                    "sheet-1",
                    "Leads",
                    3,
                )
                assert ada.row.values == {"email": "ada@example.test", "name": "Ada", "run": run_id}
                assert tuple(ada.current_cells or ()) == ("ada@example.test", "Ada", run_id)
                await harness.require_completed_row(5)

                # Deleting a row above the cursor shifts every row up; only the new row is delivered.
                sheet.delete_row(2)
                sheet.append_rows(["linus@example.test", "Linus", run_id])
                linus = await harness.require_completed_row(5)
                assert linus.row.values["email"] == "linus@example.test"
                assert tuple(linus.current_cells or ()) == ("linus@example.test", "Linus", run_id)
                await harness.stop(first_run)

                sheet.append_rows(["while-stopped@example.test", "Stopped", run_id])
                sheet.script(ScriptedResponse(429, "1"))
                second_run = harness.start()
                stopped = await harness.require_completed_row(6)
                assert stopped.row.values["email"] == "while-stopped@example.test"
                await wait_for_requests(sheet, sheet.completed_requests() + 2)
                await harness.stop(second_run)

                assert len(harness.target.order) == 4, f"existing rows start no Flow: {harness.target.order}"
                assert all(count == 1 for count in harness.target.deliveries.values())
                assert not await harness.inbox.pending()
                assert {"'Leads'!3:3", "'Leads'!5:5"} <= set(sheet.read_ranges())
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await flow_connection.aclose()
    text = logs.getvalue()
    assert "poll cursor seeded" in text
    assert "sheet rows moved; cursor realigned" in text
    assert "poll rate limited; pausing connection" in text
    assert "SENTINEL" not in text
    assert "@example.test" not in text, "records never contain cell values"
