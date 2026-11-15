# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deliver new Re:amaze conversations through the example's Flow target to a real Dex Worker and Flow."""

import asyncio
import os
import tempfile
import time
from collections.abc import AsyncIterator, Callable
from contextlib import asynccontextmanager
from dataclasses import dataclass, field
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any

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
    InMemoryObjectStore,
    PollCursorKey,
    PollCursorState,
    PollCursorStore,
    Scope,
    TriggerInbox,
    TriggerInboxKey,
    new_durable_trigger_target,
)

from dex_connectors.reamaze import (
    Client,
    Config,
    Connection,
    ConversationCreatedEvent,
    ConversationCreatedTriggerConfiguration,
    ConversationCreatedTriggerRoute,
    ConversationTriggerRunnerConfig,
    Credentials,
    decode_conversation_created_event_payload,
    new_connection,
    new_conversation_trigger_runner,
)
from tests.reamaze_conversations_fake import ReamazeConversationFake

from .flow import BINDING_NAME, CONNECTION_NAME, Intake, NewConversationFlow, resolve_flow_id
from .worker import new_conversation_flow_target

EMAIL = "agent@acme.example.com"
TOKEN = "reamazeIntegration" + "Token0123"  # Split so push protection does not read a fixture as a token.
SCOPE = Scope("reamaze-new-conversation", "live")
INBOX_KEY = TriggerInboxKey("reamaze", CONNECTION_NAME, "conversationCreated", BINDING_NAME)


@dataclass
class CountingTarget:
    """Wraps the example's Flow target and records each delivered event once, by slug."""

    inner: Any
    delivered: dict[str, TriggerEvent[ConversationCreatedEvent]] = field(default_factory=dict)

    async def handle_trigger(
        self, context: TriggerContext, event: TriggerEvent[ConversationCreatedEvent]
    ) -> None:
        await self.inner.handle_trigger(context, event)
        self.delivered.setdefault(event.payload.conversation.id, event)


async def eventually(condition: Callable[[], Any], timeout: float = 60) -> None:
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
    """Run one Trigger process until the block exits; leaving it models a process exit."""
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
    provider: ReamazeConversationFake
    target: CountingTarget
    dex_client: dex.AsyncClient
    connection: Connection
    # prefix keeps slugs, and so Flow IDs, unique per run against a shared Dex Server.
    prefix: str
    start: datetime
    objects: InMemoryObjectStore = field(default_factory=InMemoryObjectStore)
    configuration: ConversationCreatedTriggerConfiguration = field(
        default_factory=ConversationCreatedTriggerConfiguration
    )

    def insert(self, name: str, subject: str, status: int = 0, offset: float = 0, channel: str = "") -> None:
        self.provider.insert(
            f"{self.prefix}-{name}",
            subject,
            channel=channel,
            status=status,
            created_at=self.start + timedelta(seconds=offset),
        )

    def cursor_store(self) -> PollCursorStore:
        return PollCursorStore(self.objects, SCOPE, PollCursorKey.from_trigger_inbox_key(INBOX_KEY))

    async def cursor_state(self) -> PollCursorState | None:
        return (await self.cursor_store().read_poll_cursor()).state

    def new_runner(self) -> PollRunner:
        """Wire the target behind the durable inbox and project cursor over storage that survives."""
        durable = new_durable_trigger_target(
            TriggerInbox(self.objects, SCOPE, INBOX_KEY),
            self.target,
            decode_payload=decode_conversation_created_event_payload,
        )
        cursor = self.cursor_store()
        route = ConversationCreatedTriggerRoute(BINDING_NAME, durable, self.configuration, cursor)
        return new_conversation_trigger_runner(ConversationTriggerRunnerConfig(self.connection, [route]))

    async def wait_for_watermark(self, offset: float) -> None:
        moment = self.start + timedelta(seconds=offset)
        expected = f'"watermark":{(moment - datetime(1970, 1, 1, tzinfo=UTC)) // timedelta(milliseconds=1)},'

        async def has_watermark() -> bool:
            state = await self.cursor_state()
            return state is not None and expected in state.token

        await eventually(has_watermark)

    async def wait_for_intakes(self, count: int, names: list[str]) -> dict[str, Intake]:
        await eventually(lambda: len(self.target.delivered) >= count)
        intakes: dict[str, Intake] = {}
        for name in names:
            event = self.target.delivered.get(f"{self.prefix}-{name}")
            assert event is not None, f"{name} was not delivered"
            result = await asyncio.wait_for(
                self.dex_client.wait_for_flow(resolve_flow_id(event), timedelta(seconds=30)), 40
            )
            assert result.status == dex.FlowStatus.COMPLETED, result.error_message
            intake = result.single_output(Intake)
            assert intake.conversation_id == event.payload.conversation.id
            intakes[name] = intake
        return intakes


@asynccontextmanager
async def new_conversation_environment() -> AsyncIterator[Environment]:
    """Serve the example Flow on a real Dex Worker against a fake Re:amaze brand."""
    server_address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
    provider = ReamazeConversationFake(EMAIL, TOKEN)
    reference = ConnectionRef("reamaze", CONNECTION_NAME)
    async with httpx.AsyncClient(transport=provider.transport()) as http_client:
        client = Client(
            Config(brand="acme", poll_interval_seconds=10),
            StaticCredentialProvider({reference: Credentials(EMAIL, SecretString(TOKEN))}),
            http_client,
            api_base_url=provider.base_url,
        )
        connection = new_connection(client, reference)
        flow = NewConversationFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="reamaze-new-conversation-blobs-") as directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(directory) / "blobs"), 1 << 26))
            worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
            worker_task = asyncio.create_task(worker.start())
            try:
                await worker.wait_pull_registered(timeout=30)
                options = dex.ClientOptions(server_address=server_address)
                async with dex.AsyncClient(registry, cache, options) as dex_client:
                    now = datetime.now(UTC)
                    yield Environment(
                        provider,
                        CountingTarget(new_conversation_flow_target(dex_client, flow)),
                        dex_client,
                        connection,
                        f"run{time.time_ns():x}",
                        now.replace(microsecond=now.microsecond // 1000 * 1000),
                    )
            finally:
                await worker.stop()
                await worker_task
                cache.close()


@pytest.mark.asyncio
async def test_new_conversations_start_one_flow_each_across_a_restart_with_real_dex() -> None:
    """The baseline starts nothing; a 429 pauses; spam starts no Flow; offline conversations arrive later."""
    async with new_conversation_environment() as environment:
        environment.insert("existing-1", "Existing", offset=-7200)
        environment.insert("existing-2", "Existing", offset=-3600)
        async with trigger_process(environment.new_runner()) as first:
            await environment.wait_for_watermark(-3600)
            environment.insert("refund", "Refund request", 0, 0)
            environment.insert("spam", "Cheap watches", 3, 1)
            environment.insert("closed", "Closed on arrival", 4, 2)
            environment.provider.rate_limit_next_list("1")
            first.wake_poller(BINDING_NAME)
            intakes = await environment.wait_for_intakes(3, ["refund", "closed"])
        assert intakes["refund"].subject == "Refund request"
        assert intakes["refund"].message_count == 1, "the Flow reads the conversation back from Re:amaze"
        assert intakes["closed"].status == 4
        assert environment.provider.list_request_count() >= 3, "the rate-limited poll was retried"

        environment.insert("offline", "Opened while stopped", 0, 3)
        async with trigger_process(environment.new_runner()) as second:
            second.wake_poller(BINDING_NAME)
            intakes = await environment.wait_for_intakes(4, ["refund", "closed", "offline"])
            await environment.wait_for_watermark(3)
        assert intakes["offline"].subject == "Opened while stopped"
        assert len(environment.target.delivered) == 4, "spam is delivered once and starts no Flow"


@pytest.mark.asyncio
async def test_changed_channel_records_a_new_baseline_with_real_dex() -> None:
    """A cursor for another channel reseeds without delivering conversations before the new baseline."""
    async with new_conversation_environment() as environment:
        environment.insert("existing", "Existing", offset=-3600)
        async with trigger_process(environment.new_runner()):
            await environment.wait_for_watermark(-3600)

        environment.insert("gap", "Lost in the gap", channel="billing")
        environment.configuration = ConversationCreatedTriggerConfiguration(channel="billing")
        async with trigger_process(environment.new_runner()) as second:

            async def is_reset() -> bool:
                state = await environment.cursor_state()
                return state is not None and state.reset_count == 1

            await eventually(is_reset)
            environment.insert("after-reset", "After the reset", offset=1, channel="billing")
            second.wake_poller(BINDING_NAME)
            intakes = await environment.wait_for_intakes(1, ["after-reset"])
        assert intakes["after-reset"].channel == "billing"
        assert len(environment.target.delivered) == 1, "the conversation before the new baseline is skipped"
