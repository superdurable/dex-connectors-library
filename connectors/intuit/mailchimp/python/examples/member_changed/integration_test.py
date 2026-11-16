# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deliver Mailchimp contact changes through the example's Flow target to a real Dex Worker and Flow."""

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

from dex_connectors.intuit_mailchimp import (
    Client,
    Config,
    Credentials,
    MemberChangedEvent,
    ProjectMemberChangedTriggerRoute,
    new_connection,
    new_project_member_trigger_runner,
)
from tests.mailchimp_audience_fake import API_BASE_URL, API_KEY, Failure, MailchimpAudienceFake

from .flow import BINDING_NAME, CONNECTION_NAME, MemberChangeFlow, MemberChangeOutcome, resolve_flow_id
from .worker import new_member_change_flow_target

SCOPE = Scope("mailchimp-member-changed", "live")
INBOX_KEY = TriggerInboxKey("mailchimp", CONNECTION_NAME, "memberChanged", BINDING_NAME)


class DurableProject:
    """The project storage two Trigger processes share, with the binding's saved audience."""

    def __init__(self, audience_id: str) -> None:
        self.objects = InMemoryObjectStore()
        binding = {
            "connectorId": "mailchimp",
            "connectionName": CONNECTION_NAME,
            "triggerName": "memberChanged",
            "bindingName": BINDING_NAME,
            "configuration": {"audienceId": audience_id},
        }
        self.configuration = Configuration(SCOPE, 1, (), (binding,))

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
    """Wraps the example's Flow target and records each delivered event once, in order."""

    inner: Any
    delivered: dict[str, TriggerEvent[MemberChangedEvent]] = field(default_factory=dict)

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[MemberChangedEvent]) -> None:
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
    """Run one Trigger process until the block exits; leaving the block models a process exit."""
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
    provider: MailchimpAudienceFake
    project: DurableProject
    target: CountingTarget
    dex_client: dex.AsyncClient
    connection: Any

    def new_runner(self) -> PollRunner:
        routes = [ProjectMemberChangedTriggerRoute(BINDING_NAME, self.target)]
        return new_project_member_trigger_runner(cast(LoadedProject, self.project), self.connection, routes)

    async def wait_for_watermark(self) -> None:
        """Wait until the committed cursor has read the provider's newest change."""
        newest = self.provider.clock.strftime("%Y-%m-%dT%H:%M:%SZ")

        async def has_read_newest() -> bool:
            state = await self.project.cursor_state()
            return state is not None and f'"watermark":"{newest}"' in state.token

        await eventually(has_read_newest)

    async def wait_for_outcomes(self, count: int) -> dict[str, MemberChangeOutcome]:
        """Wait for count deliveries and return each Flow's outcome by delivered email address."""
        await eventually(lambda: len(self.target.delivered) >= count)
        outcomes: dict[str, MemberChangeOutcome] = {}
        for event in list(self.target.delivered.values()):
            result = await asyncio.wait_for(
                self.dex_client.wait_for_flow(resolve_flow_id(event), timedelta(seconds=30)), 40
            )
            assert result.status == dex.FlowStatus.COMPLETED, result.error_message
            outcome = result.single_output(MemberChangeOutcome)
            outcomes[outcome.email_address] = outcome
        return outcomes


@asynccontextmanager
async def member_changed_environment() -> AsyncIterator[Environment]:
    """Serve the example Flow on a real Dex Worker against a fake audience with a unique ID."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    provider = MailchimpAudienceFake(f"it{time.time_ns()}")
    reference = ConnectionRef("mailchimp", CONNECTION_NAME)
    async with httpx.AsyncClient(transport=httpx.MockTransport(provider.handle)) as http_client:
        client = Client(
            Config(poll_interval_seconds=5),
            StaticCredentialProvider({reference: Credentials(SecretString(API_KEY))}),
            http_client,
            api_base_url=API_BASE_URL,
        )
        connection = new_connection(client, reference)
        flow = MemberChangeFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="mailchimp-member-changed-blobs-") as directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(directory) / "blobs"), 1 << 26))
            worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
            worker_task = asyncio.create_task(worker.start())
            try:
                await worker.wait_pull_registered(timeout=30)
                options = dex.ClientOptions(server_address=server_address)
                async with dex.AsyncClient(registry, cache, options) as dex_client:
                    yield Environment(
                        provider,
                        DurableProject(provider.audience_id),
                        CountingTarget(new_member_change_flow_target(dex_client, flow)),
                        dex_client,
                        connection,
                    )
            finally:
                await worker.stop()
                await worker_task
                cache.close()


@pytest.mark.asyncio
async def test_member_changes_start_one_flow_each_across_a_restart_with_real_dex() -> None:
    """The baseline starts nothing; each change starts one Flow; changes made while stopped arrive later."""
    async with member_changed_environment() as environment:
        provider = environment.provider
        provider.upsert("ben@example.com", "subscribed", "Ben")
        provider.upsert("cara@example.com", "subscribed", "Cara")
        async with trigger_process(environment.new_runner()) as first:
            await environment.wait_for_watermark()
            provider.upsert("dana@example.com", "pending", "Dana")
            provider.upsert("ben@example.com", "unsubscribed", "Ben")
            first.wake_poller(BINDING_NAME)
            outcomes = await environment.wait_for_outcomes(2)
        assert outcomes["dana@example.com"].current.status == "pending"
        assert outcomes["ben@example.com"].delivered_status == "unsubscribed"
        assert outcomes["ben@example.com"].current.merge_fields == {"FNAME": "Ben"}, "the Flow reads it back"

        provider.upsert("eli@example.com", "subscribed", "Eli")
        provider.upsert("dana@example.com", "subscribed", "Dana")
        async with trigger_process(environment.new_runner()) as second:
            second.wake_poller(BINDING_NAME)
            outcomes = await environment.wait_for_outcomes(4)
            await environment.wait_for_watermark()
            second.wake_poller(BINDING_NAME)
            requests = len(provider.requests)
            await eventually(lambda: len(provider.requests) > requests)
        assert outcomes["eli@example.com"].current.status == "subscribed"
        assert outcomes["dana@example.com"].delivered_status == "subscribed"
        assert len(environment.target.delivered) == 4, "no version twice and never the baseline"


@pytest.mark.asyncio
async def test_rejected_cursor_query_records_a_new_baseline_with_real_dex() -> None:
    """A 400 for the cursor's query reseeds without delivering the gap."""
    async with member_changed_environment() as environment:
        provider = environment.provider
        provider.upsert("ben@example.com", "subscribed", "Ben")
        async with trigger_process(environment.new_runner()):
            await environment.wait_for_watermark()

        provider.upsert("gap@example.com", "subscribed", "Gap")
        provider.fail_next_list(Failure(400))
        async with trigger_process(environment.new_runner()) as second:

            async def is_reset() -> bool:
                state = await environment.project.cursor_state()
                return state is not None and state.reset_count == 1

            await eventually(is_reset)
            provider.upsert("after@example.com", "subscribed", "After")
            second.wake_poller(BINDING_NAME)
            outcomes = await environment.wait_for_outcomes(1)
        assert list(outcomes) == ["after@example.com"], (
            "changes between the rejection and the reseed are lost"
        )


@pytest.mark.asyncio
async def test_rate_limited_poll_pauses_then_delivers_with_real_dex() -> None:
    """A 429 with Retry-After pauses polling, then the change arrives exactly once."""
    async with member_changed_environment() as environment:
        provider = environment.provider
        async with trigger_process(environment.new_runner()) as process:
            await eventually(lambda: len(provider.requests) >= 1)
            provider.fail_next_list(Failure(429, retry_after="1"))
            provider.upsert("ben@example.com", "subscribed", "Ben")
            process.wake_poller(BINDING_NAME)
            outcomes = await environment.wait_for_outcomes(1)
        assert outcomes["ben@example.com"].current.status == "subscribed"
        assert len(environment.target.delivered) == 1
