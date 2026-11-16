# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the pageChanged example against a real Dex Worker and Client and a local Confluence stand-in."""

import asyncio
import contextlib
import os
import tempfile
import uuid
from collections.abc import AsyncIterator, Callable
from dataclasses import dataclass, field
from datetime import timedelta
from pathlib import Path

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

from dex_connectors.atlassian_confluence import (
    PAGE_CHANGE_CREATED,
    PAGE_CHANGE_UPDATED,
    Client,
    Config,
    Connection,
    Credentials,
    PageChangedEvent,
    PageChangedTriggerConfiguration,
    PageChangedTriggerRoute,
    PageTriggerRunnerConfig,
    decode_page_changed_event_payload,
    new_connection,
    new_page_trigger_runner,
)
from tests.confluence_page_fake import ACCESS_TOKEN, ConfluencePageSite

from .flow import (
    CONNECTION_NAME,
    PAGE_CHANGED_TRIGGER_BINDING,
    PageChangeFlow,
    PageChangeOutcome,
    accepts_every_page_change,
    map_to_flow_input,
    resolve_flow_id,
)

SCOPE = Scope("confluence-page-changed", "live")
OPS_SPACE = PageChangedTriggerConfiguration("98306", "OPS", "Operations")
HR_SPACE = PageChangedTriggerConfiguration(space_key="HR")
INBOX_KEY = TriggerInboxKey("confluence", CONNECTION_NAME, "pageChanged", PAGE_CHANGED_TRIGGER_BINDING)


@dataclass
class TriggerProcess:
    """One run of the application's Trigger side; stopping it models a process exit."""

    runner: PollRunner
    task: "asyncio.Task[None]"
    cancellation: TriggerCancellation

    async def stop(self) -> None:
        self.cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await self.task


@dataclass
class Environment:
    """The fake site, the Flow's real Dex Worker and Client, and storage that survives processes."""

    site: ConfluencePageSite
    flow: PageChangeFlow
    connection: Connection
    client: dex.AsyncClient
    objects: InMemoryObjectStore = field(default_factory=InMemoryObjectStore)
    delivered: dict[str, TriggerEvent[PageChangedEvent]] = field(default_factory=dict)
    processes: list[TriggerProcess] = field(default_factory=list)

    def cursor_store(self) -> PollCursorStore:
        return PollCursorStore(self.objects, SCOPE, PollCursorKey.from_trigger_inbox_key(INBOX_KEY))

    def start_trigger_process(self, configuration: PageChangedTriggerConfiguration) -> TriggerProcess:
        """Wire the example's target behind the durable inbox and cursor, as the project runner does."""
        start = new_dex_flow_trigger_target(
            self.client,
            self.flow,
            accepts_event=accepts_every_page_change,
            resolve_flow_id=resolve_flow_id,
            map_to_flow_input=map_to_flow_input,
        )
        environment = self

        class CountingTarget:
            async def handle_trigger(
                self, context: TriggerContext, event: TriggerEvent[PageChangedEvent]
            ) -> None:
                await start.handle_trigger(context, event)
                environment.delivered[event.id] = event

        target = new_durable_trigger_target(
            TriggerInbox(self.objects, SCOPE, INBOX_KEY),
            CountingTarget(),
            decode_payload=decode_page_changed_event_payload,
        )
        runner = new_page_trigger_runner(
            PageTriggerRunnerConfig(
                self.connection,
                [
                    PageChangedTriggerRoute(
                        PAGE_CHANGED_TRIGGER_BINDING, configuration, target, self.cursor_store()
                    )
                ],
            )
        )
        cancellation = TriggerCancellation()
        process = TriggerProcess(
            runner, asyncio.create_task(runner.run(TriggerContext(cancellation))), cancellation
        )
        self.processes.append(process)
        return process

    async def wait_for_cursor(self, is_ready: Callable[[PollCursorState | None], bool]) -> None:
        async with asyncio.timeout(30):
            while not is_ready((await self.cursor_store().read_poll_cursor()).state):
                await asyncio.sleep(0.05)

    async def wait_for_outcomes(self, count: int) -> dict[tuple[str, int], PageChangeOutcome]:
        """Wait for count delivered versions, then each version's Flow, keyed by page ID and version."""
        async with asyncio.timeout(30):
            while len(self.delivered) < count:
                await asyncio.sleep(0.05)
        outcomes: dict[tuple[str, int], PageChangeOutcome] = {}
        for event in list(self.delivered.values()):
            result = await self.client.wait_for_flow(resolve_flow_id(event), timedelta(seconds=30))
            assert result.status == dex.FlowStatus.COMPLETED, result.error_message
            outcome = result.single_output(PageChangeOutcome)
            assert outcome.page_id == event.payload.page.id
            outcomes[(outcome.page_id, outcome.delivered_version)] = outcome
        return outcomes


@contextlib.asynccontextmanager
async def page_changed_environment() -> AsyncIterator[Environment]:
    server_address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
    # A fresh site ID per run gives every page version a new event ID, so Flow IDs never collide.
    site = ConfluencePageSite(str(uuid.uuid4()))
    reference = ConnectionRef("atlassian", CONNECTION_NAME)
    credentials = Credentials(
        "client-id", SecretString("client-secret"), SecretString(ACCESS_TOKEN), SecretString("refresh-token")
    )
    connection = new_connection(
        Client(
            Config(cloud_id="", endpoint="http://127.0.0.1:12345", poll_interval_seconds=10),
            StaticCredentialProvider({reference: credentials}),
            httpx.AsyncClient(transport=site.transport()),
        ),
        reference,
    )
    flow = PageChangeFlow(connection)
    registry = dex.Registry([flow], allow_async_handlers=True)
    with tempfile.TemporaryDirectory(prefix="confluence-python-page-changed-") as directory:
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(directory) / "blobs"), 1 << 26))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=30)
            async with dex.AsyncClient(
                registry, cache, dex.ClientOptions(server_address=server_address)
            ) as client:
                environment = Environment(site, flow, connection, client)
                try:
                    yield environment
                finally:
                    for process in environment.processes:
                        process.cancellation.cancel()
                        with contextlib.suppress(TriggerDeliveryAbortedError):
                            await process.task
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()


async def test_page_versions_start_one_flow_each_across_a_restart_with_real_dex() -> None:
    async with page_changed_environment() as environment:
        site = environment.site
        existing = site.publish("OPS", "Existing policy", "Before the Trigger started")
        first = environment.start_trigger_process(OPS_SPACE)
        await environment.wait_for_cursor(lambda state: state is not None)

        await asyncio.sleep(0.002)  # Keeps a new version's time distinct from the baseline's.
        created = site.publish("OPS", "Remote work policy", "Two days a week")
        site.edit(existing, "Existing policy", "Revised")
        site.publish("HR", "Other space", "Ignored")
        first.runner.wake_poller(PAGE_CHANGED_TRIGGER_BINDING)
        outcomes = await environment.wait_for_outcomes(2)
        assert outcomes[(created, 1)].change_kind == PAGE_CHANGE_CREATED
        assert outcomes[(created, 1)].body == "Two days a week", "the Flow reads the page back as Markdown"
        assert outcomes[(existing, 2)].change_kind == PAGE_CHANGE_UPDATED
        assert outcomes[(existing, 2)].title == "Existing policy"
        await first.stop()

        await asyncio.sleep(0.002)
        offline = site.publish("OPS", "Saved while stopped", "Offline")
        site.edit(created, "Remote work policy", "Three days a week")
        site.throttle_searches(1, 1)
        second = environment.start_trigger_process(OPS_SPACE)
        outcomes = await environment.wait_for_outcomes(4)
        assert outcomes[(offline, 1)].title == "Saved while stopped"
        assert outcomes[(created, 2)].body == "Three days a week", "the second version starts its own Flow"
        assert 429 in site.search_statuses, "the restart's first search was rate limited"
        searches_before = site.search_requests()
        second.runner.wake_poller(PAGE_CHANGED_TRIGGER_BINDING)
        async with asyncio.timeout(30):
            while site.search_requests() <= searches_before:
                await asyncio.sleep(0.05)
        assert len(environment.delivered) == 4, "no version is delivered twice and the baseline never is"


async def test_changed_space_records_a_new_baseline_with_real_dex() -> None:
    async with page_changed_environment() as environment:
        first = environment.start_trigger_process(OPS_SPACE)
        await environment.wait_for_cursor(lambda state: state is not None)
        await first.stop()
        existing = environment.site.publish("HR", "Existing in the new space", "Baseline")
        second = environment.start_trigger_process(HR_SPACE)
        await environment.wait_for_cursor(lambda state: state is not None and state.reset_count == 1)
        await asyncio.sleep(0.002)
        after = environment.site.publish("HR", "After the reset", "New")
        second.runner.wake_poller(PAGE_CHANGED_TRIGGER_BINDING)
        outcomes = await environment.wait_for_outcomes(1)
        assert outcomes[(after, 1)].title == "After the reset"
        assert (existing, 1) not in outcomes, "the new space's existing pages are its baseline"
