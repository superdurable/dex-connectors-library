# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the issueChanged example against a real Dex Worker and Client and a local Jira search stand-in."""

import asyncio
import contextlib
import os
import tempfile
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

from dex_connectors.atlassian_jira import (
    ISSUE_CHANGE_CREATED,
    Client,
    Config,
    Connection,
    Credentials,
    IssueChangedEvent,
    IssueChangedTriggerConfiguration,
    IssueChangedTriggerRoute,
    IssueTriggerRunnerConfig,
    decode_issue_changed_event_payload,
    new_connection,
    new_issue_trigger_runner,
)
from tests.jira_search_fake import CLOUD_ID, HUMAN_RESOURCES, OPERATIONS, JiraSearchSite

from .flow import (
    CONNECTION_NAME,
    ISSUE_CHANGED_TRIGGER_BINDING,
    IssueChangeFlow,
    IssueChangeOutcome,
    accepts_every_issue_change,
    map_to_flow_input,
    resolve_flow_id,
)

SCOPE = Scope("jira-issue-changed", "live")
OPS_PROJECT = IssueChangedTriggerConfiguration(OPERATIONS.id, "OPS", "Operations")
HR_PROJECT = IssueChangedTriggerConfiguration(project_key="HR")
INBOX_KEY = TriggerInboxKey("jira", CONNECTION_NAME, "issueChanged", ISSUE_CHANGED_TRIGGER_BINDING)


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

    site: JiraSearchSite
    flow: IssueChangeFlow
    connection: Connection
    client: dex.AsyncClient
    objects: InMemoryObjectStore = field(default_factory=InMemoryObjectStore)
    delivered: dict[str, TriggerEvent[IssueChangedEvent]] = field(default_factory=dict)
    processes: list[TriggerProcess] = field(default_factory=list)

    def cursor_store(self) -> PollCursorStore:
        return PollCursorStore(self.objects, SCOPE, PollCursorKey.from_trigger_inbox_key(INBOX_KEY))

    def start_trigger_process(self, configuration: IssueChangedTriggerConfiguration) -> TriggerProcess:
        """Wire the example's target behind the durable inbox and cursor, as the project runner does."""
        start = new_dex_flow_trigger_target(
            self.client,
            self.flow,
            accepts_event=accepts_every_issue_change,
            resolve_flow_id=resolve_flow_id,
            map_to_flow_input=map_to_flow_input,
        )
        environment = self

        class CountingTarget:
            async def handle_trigger(
                self, context: TriggerContext, event: TriggerEvent[IssueChangedEvent]
            ) -> None:
                await start.handle_trigger(context, event)
                environment.delivered[event.id] = event

        target = new_durable_trigger_target(
            TriggerInbox(self.objects, SCOPE, INBOX_KEY),
            CountingTarget(),
            decode_payload=decode_issue_changed_event_payload,
        )
        runner = new_issue_trigger_runner(
            IssueTriggerRunnerConfig(
                self.connection,
                [
                    IssueChangedTriggerRoute(
                        ISSUE_CHANGED_TRIGGER_BINDING, configuration, target, self.cursor_store()
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

    async def wait_for_outcomes(self, count: int) -> dict[str, IssueChangeOutcome]:
        """Wait for count delivered versions, then each version's Flow, keyed by issue key."""
        async with asyncio.timeout(30):
            while len(self.delivered) < count:
                await asyncio.sleep(0.05)
        outcomes: dict[str, IssueChangeOutcome] = {}
        for event in list(self.delivered.values()):
            result = await self.client.wait_for_flow(resolve_flow_id(event), timedelta(seconds=30))
            assert result.status == dex.FlowStatus.COMPLETED, result.error_message
            outcome = result.single_output(IssueChangeOutcome)
            assert outcome.issue_key == event.payload.issue.key
            outcomes[outcome.issue_key] = outcome
        return outcomes


@contextlib.asynccontextmanager
async def issue_changed_environment() -> AsyncIterator[Environment]:
    server_address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
    site = JiraSearchSite()
    reference = ConnectionRef("atlassian", CONNECTION_NAME)
    credentials = Credentials(
        "client-id", SecretString("client-secret"), SecretString("jira-token"), SecretString("refresh-token")
    )
    connection = new_connection(
        Client(
            Config(cloud_id=CLOUD_ID, endpoint="http://127.0.0.1:12345", poll_interval_seconds=5),
            StaticCredentialProvider({reference: credentials}),
            httpx.AsyncClient(transport=site.transport()),
        ),
        reference,
    )
    flow = IssueChangeFlow(connection)
    registry = dex.Registry([flow], allow_async_handlers=True)
    with tempfile.TemporaryDirectory(prefix="jira-python-issue-changed-") as directory:
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


async def test_issue_changes_start_one_flow_each_across_a_restart_with_real_dex() -> None:
    async with issue_changed_environment() as environment:
        site = environment.site
        standup = site.create(OPERATIONS, "Standup notes")
        site.create(OPERATIONS, "Old review")
        first = environment.start_trigger_process(OPS_PROJECT)
        await environment.wait_for_cursor(lambda state: state is not None)

        await asyncio.sleep(0.002)  # Keeps a new version's updated time distinct from the baseline's.
        kickoff = site.create(OPERATIONS, "Kickoff")
        site.update(standup, "Standup notes, edited")
        site.create(HUMAN_RESOURCES, "Other project")
        first.runner.wake_poller(ISSUE_CHANGED_TRIGGER_BINDING)
        outcomes = await environment.wait_for_outcomes(2)
        assert outcomes[kickoff].change_kind == ISSUE_CHANGE_CREATED
        assert outcomes[kickoff].current.summary == "Kickoff"
        assert outcomes[standup].current.summary == "Standup notes, edited", "the Flow reads the issue back"
        await first.stop()

        await asyncio.sleep(0.002)
        offline = site.create(OPERATIONS, "Filed while stopped")
        site.update(kickoff, "Kickoff, rescheduled")
        second = environment.start_trigger_process(OPS_PROJECT)
        second.runner.wake_poller(ISSUE_CHANGED_TRIGGER_BINDING)
        outcomes = await environment.wait_for_outcomes(4)
        assert outcomes[offline].current.summary == "Filed while stopped"
        assert outcomes[kickoff].current.summary == "Kickoff, rescheduled", (
            "the second version starts its Flow"
        )
        searches_before = len(site.requests())
        second.runner.wake_poller(ISSUE_CHANGED_TRIGGER_BINDING)
        async with asyncio.timeout(30):
            while len(site.requests()) <= searches_before:
                await asyncio.sleep(0.05)
        assert len(environment.delivered) == 4, "no version is delivered twice and the baseline never is"


async def test_rate_limit_pauses_then_delivers_with_real_dex() -> None:
    async with issue_changed_environment() as environment:
        process = environment.start_trigger_process(OPS_PROJECT)
        await environment.wait_for_cursor(lambda state: state is not None)
        environment.site.rate_limit_next_requests(1, "1")
        created = environment.site.create(OPERATIONS, "Filed during a rate limit")
        process.runner.wake_poller(ISSUE_CHANGED_TRIGGER_BINDING)
        outcomes = await environment.wait_for_outcomes(1)
        assert outcomes[created].current.summary == "Filed during a rate limit"
        assert 429 in [request.status for request in environment.site.requests()]


async def test_changed_project_records_a_new_baseline_with_real_dex() -> None:
    async with issue_changed_environment() as environment:
        first = environment.start_trigger_process(OPS_PROJECT)
        await environment.wait_for_cursor(lambda state: state is not None)
        await first.stop()
        gap = environment.site.create(HUMAN_RESOURCES, "Existing in the new project")
        second = environment.start_trigger_process(HR_PROJECT)
        await environment.wait_for_cursor(lambda state: state is not None and state.reset_count == 1)
        await asyncio.sleep(0.002)
        after = environment.site.create(HUMAN_RESOURCES, "After the reset")
        second.runner.wake_poller(ISSUE_CHANGED_TRIGGER_BINDING)
        outcomes = await environment.wait_for_outcomes(1)
        assert outcomes[after].current.summary == "After the reset"
        assert gap not in outcomes, "the new project's existing issues are its baseline"
