# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deliver new Hiver conversations through the example's Flow target to a real Dex Worker and Flow."""

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

from dex_connectors.hiver import Client, Config, ConversationReceivedEvent, Credentials, new_connection
from tests.hiver_conversations_fake import API_BASE_URL, API_KEY, HiverConversationsFake

from .flow import (
    CONNECTION_NAME,
    CONVERSATION_RECEIVED_TRIGGER_BINDING,
    IntakeOutcome,
    NewConversationIntakeFlow,
    resolve_flow_id,
)
from .intake import new_conversation_target, open_conversation_intake_runner

SCOPE = Scope("hiver-new-conversation-intake", "live")
INBOX_KEY = TriggerInboxKey(
    "hiver", CONNECTION_NAME, "conversationReceived", CONVERSATION_RECEIVED_TRIGGER_BINDING
)


class DurableProject:
    """The project storage two Trigger processes share: hosted storage keeps inboxes and cursors."""

    def __init__(self, inbox_id: str) -> None:
        self.objects = InMemoryObjectStore()
        self.configuration = Configuration(SCOPE, 1, (), (self.binding(inbox_id),))

    @staticmethod
    def binding(inbox_id: str) -> dict[str, Any]:
        return {
            "connectorId": "hiver",
            "connectionName": CONNECTION_NAME,
            "triggerName": "conversationReceived",
            "bindingName": CONVERSATION_RECEIVED_TRIGGER_BINDING,
            "configuration": {"inboxId": inbox_id},
        }

    def repoint(self, inbox_id: str) -> None:
        self.configuration = Configuration(SCOPE, 2, (), (self.binding(inbox_id),))

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
    delivered: dict[str, TriggerEvent[ConversationReceivedEvent]] = field(default_factory=dict)

    async def handle_trigger(
        self, context: TriggerContext, event: TriggerEvent[ConversationReceivedEvent]
    ) -> None:
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
    provider: HiverConversationsFake
    project: DurableProject
    target: CountingTarget
    dex_client: dex.AsyncClient
    connection: Any
    inbox_id: str

    def new_runner(self) -> PollRunner:
        return open_conversation_intake_runner(
            cast(LoadedProject, self.project), self.connection, self.target
        )

    async def wait_for_baseline(self, inbox_id: str, reset_count: int = 0) -> None:
        async def is_baselined() -> bool:
            state = await self.project.cursor_state()
            return (
                state is not None
                and state.reset_count == reset_count
                and f'"inboxId":"{inbox_id}","baselineComplete":true' in state.token
            )

        await eventually(is_baselined)

    async def wait_for_outcomes(self, count: int) -> dict[str, IntakeOutcome]:
        await eventually(lambda: len(self.target.delivered) >= count)
        outcomes: dict[str, IntakeOutcome] = {}
        for event in list(self.target.delivered.values()):
            result = await asyncio.wait_for(
                self.dex_client.wait_for_flow(resolve_flow_id(event), timedelta(seconds=30)), 40
            )
            assert result.status == dex.FlowStatus.COMPLETED, result.error_message
            outcome = result.single_output(IntakeOutcome)
            assert outcome.conversation_id == event.payload.conversation.id
            outcomes[outcome.conversation_id] = outcome
        return outcomes


@asynccontextmanager
async def intake_environment() -> AsyncIterator[Environment]:
    """Serve the example Flow on a real Dex Worker against a fake Hiver shared inbox."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    # A unique inbox per run keeps Flow IDs unique on a shared Dex Server.
    inbox_id = str(100000 + time.time_ns() % 900_000_000)
    provider = HiverConversationsFake(inbox_id, "support@acme.example.com")
    reference = ConnectionRef("hiver", CONNECTION_NAME)
    async with httpx.AsyncClient(transport=provider.transport) as http_client:
        client = Client(
            Config(request_interval_milliseconds=1, poll_interval_seconds=1),
            StaticCredentialProvider({reference: Credentials(SecretString(API_KEY))}),
            http_client,
            API_BASE_URL,
        )
        connection = new_connection(client, reference)
        flow = NewConversationIntakeFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="hiver-new-conversation-blobs-") as directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(directory) / "blobs"), 1 << 26))
            worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
            worker_task = asyncio.create_task(worker.start())
            try:
                await worker.wait_pull_registered(timeout=30)
                options = dex.ClientOptions(server_address=server_address)
                async with dex.AsyncClient(registry, cache, options) as dex_client:
                    yield Environment(
                        provider,
                        DurableProject(inbox_id),
                        CountingTarget(new_conversation_target(dex_client, flow)),
                        dex_client,
                        connection,
                        inbox_id,
                    )
            finally:
                await worker.stop()
                await worker_task
                cache.close()


@pytest.mark.asyncio
async def test_new_conversations_start_one_flow_each_across_a_restart_with_real_dex() -> None:
    """No backlog; one Flow per conversation; conversations added while stopped arrive after a 429."""
    async with intake_environment() as environment:
        provider, inbox_id = environment.provider, environment.inbox_id
        provider.add_conversations(inbox_id, "573741001", "573741002")
        async with trigger_process(environment.new_runner()) as first:
            await environment.wait_for_baseline(inbox_id)
            provider.add_conversations(inbox_id, "573741003", "573741004")
            first.wake_poller(CONVERSATION_RECEIVED_TRIGGER_BINDING)
            outcomes = await environment.wait_for_outcomes(2)
        assert sorted(outcomes) == ["573741003", "573741004"], "the baseline starts no Flow"
        outcome = outcomes["573741003"]
        assert outcome.inbox_id == inbox_id and outcome.status == "open"
        assert [message.hiver_message_id for message in outcome.messages] == ["8573741003"]
        assert outcome.private_permalink == "https://app.hiverhq.com/conversation/573741003"

        provider.add_conversations(inbox_id, "573741005")
        provider.rate_limit_next(1)
        async with trigger_process(environment.new_runner()) as second:
            second.wake_poller(CONVERSATION_RECEIVED_TRIGGER_BINDING)
            outcomes = await environment.wait_for_outcomes(3)
        assert sorted(outcomes) == ["573741003", "573741004", "573741005"]
        assert len(environment.target.delivered) == 3, "no conversation twice and never the baseline"


@pytest.mark.asyncio
async def test_repointing_the_binding_records_a_new_baseline_with_real_dex() -> None:
    """A cursor of another inbox reseeds; conversations already in the new inbox start no Flow."""
    async with intake_environment() as environment:
        provider, first_inbox = environment.provider, environment.inbox_id
        provider.add_conversations(first_inbox, "573741001")
        async with trigger_process(environment.new_runner()):
            await environment.wait_for_baseline(first_inbox)

        second_inbox = str(int(first_inbox) + 1)
        provider.add_inbox(second_inbox, "billing@acme.example.com")
        provider.add_conversations(second_inbox, "673741001")
        environment.project.repoint(second_inbox)
        async with trigger_process(environment.new_runner()) as second:
            await environment.wait_for_baseline(second_inbox, reset_count=1)
            provider.add_conversations(second_inbox, "673741002")
            second.wake_poller(CONVERSATION_RECEIVED_TRIGGER_BINDING)
            outcomes = await environment.wait_for_outcomes(1)
        assert list(outcomes) == ["673741002"], "the new inbox's existing conversations are its baseline"
        assert outcomes["673741002"].inbox_id == second_inbox
