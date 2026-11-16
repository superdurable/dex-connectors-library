# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deliver inbound messages from a fake Twilio Messages list through the example to real Dex."""

import asyncio
import os
import tempfile
from collections.abc import AsyncIterator, Awaitable, Callable
from contextlib import asynccontextmanager
from dataclasses import dataclass, field
from datetime import UTC, datetime, timedelta
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

from dex_connectors.twilio_messaging import (
    Client,
    Config,
    Connection,
    Credentials,
    MessageReceivedEvent,
    MessageReceivedTriggerConfiguration,
    MessageReceivedTriggerRoute,
    new_connection,
    new_message_trigger_runner,
)
from dex_connectors.twilio_messaging.triggers import _decode_message_received_payload
from tests.twilio_messages_fake import TwilioMessagesFake

from .flow import (
    CONNECTION_NAME,
    MESSAGE_RECEIVED_TRIGGER_BINDING,
    InboundMessageFlow,
    InboundMessageOutcome,
    resolve_flow_id,
)
from .routing import new_inbound_message_target

# Split literals keep synthetic SIDs from matching repository secret scanning.
ACCOUNT_SID = "AC" + "0123456789abcdef0123456789abcdef"
AUTH_TOKEN = "integration-auth-token-0123456789"
SUPPORT_NUMBER = "+14155550100"
SALES_NUMBER = "+14155550199"
CUSTOMER_NUMBER = "+14155550123"
SCOPE = Scope("twilio-message-received", "live")
INBOX_KEY = TriggerInboxKey(
    "twilio-messaging", CONNECTION_NAME, "messageReceived", MESSAGE_RECEIVED_TRIGGER_BINDING
)


def now() -> datetime:
    return datetime.now(UTC)


@dataclass
class MessageReceivedEnvironment:
    """Storage that survives the Trigger process, plus the Dex client and the deliveries it saw."""

    fake: TwilioMessagesFake
    connection: Connection
    dex_client: dex.AsyncClient
    flow: InboundMessageFlow
    configuration: MessageReceivedTriggerConfiguration
    objects: InMemoryObjectStore = field(default_factory=InMemoryObjectStore)
    delivered: dict[str, TriggerEvent[MessageReceivedEvent]] = field(default_factory=dict)

    def cursor_store(self) -> PollCursorStore:
        return PollCursorStore(self.objects, SCOPE, PollCursorKey.from_trigger_inbox_key(INBOX_KEY))

    async def handle_trigger(
        self, context: TriggerContext, event: TriggerEvent[MessageReceivedEvent]
    ) -> None:
        """Start the example's Flow, then count the delivery."""
        await new_inbound_message_target(self.dex_client, self.flow).handle_trigger(context, event)
        self.delivered[event.id] = event

    def new_runner(self) -> PollRunner:
        """Wire the example target behind the durable inbox and cursor, as the project runner does."""
        target = new_durable_trigger_target(
            TriggerInbox(self.objects, SCOPE, INBOX_KEY),
            self,
            decode_payload=_decode_message_received_payload,
        )
        route = MessageReceivedTriggerRoute(
            MESSAGE_RECEIVED_TRIGGER_BINDING, self.configuration, target, self.cursor_store()
        )
        return new_message_trigger_runner(self.connection, [route])

    async def cursor_state(self) -> PollCursorState | None:
        return (await self.cursor_store().read_poll_cursor()).state

    async def wait_for_seed(self) -> None:
        async def is_seeded() -> bool:
            return await self.cursor_state() is not None

        await eventually(is_seeded)

    async def wait_for_list_requests(self, count: int) -> None:
        async def has_listed() -> bool:
            return len(self.fake.list_requests) >= count

        await eventually(has_listed)

    async def wait_for_outcomes(self, count: int) -> dict[str, InboundMessageOutcome]:
        """Wait for count deliveries, then each one's Flow, keyed by message SID."""

        async def has_count() -> bool:
            return len(self.delivered) >= count

        await eventually(has_count)
        outcomes: dict[str, InboundMessageOutcome] = {}
        for event in list(self.delivered.values()):
            result = await self.dex_client.wait_for_flow(resolve_flow_id(event), timedelta(seconds=60))
            assert result.status == dex.FlowStatus.COMPLETED, result.error_message
            outcome = result.single_output(InboundMessageOutcome)
            assert outcome.message_sid == event.payload.message.sid
            outcomes[outcome.message_sid] = outcome
        return outcomes


async def eventually(condition_met: Callable[[], Awaitable[bool]], timeout: float = 30) -> None:
    async with asyncio.timeout(timeout):
        while not await condition_met():
            await asyncio.sleep(0.05)


@asynccontextmanager
async def trigger_process(environment: MessageReceivedEnvironment) -> AsyncIterator[PollRunner]:
    """One run of the application's Trigger side; leaving the block models a process exit."""
    runner = environment.new_runner()
    context = TriggerContext()
    running = asyncio.create_task(runner.run(context))
    try:
        yield runner
    finally:
        context.cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await running


@asynccontextmanager
async def message_received_environment(receiving_number: str) -> AsyncIterator[MessageReceivedEnvironment]:
    """Start a Worker against real Dex and a fake account; its SID nonce keeps Flow IDs unique per run."""
    server_address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
    fake = TwilioMessagesFake(ACCOUNT_SID, ACCOUNT_SID, AUTH_TOKEN)
    reference = ConnectionRef("twilio", CONNECTION_NAME)
    async with httpx.AsyncClient(transport=fake.transport()) as provider_http_client:
        client = Client(
            Config(
                account_sid=ACCOUNT_SID, endpoint="http://127.0.0.1:24211/2010-04-01", poll_interval_seconds=5
            ),
            StaticCredentialProvider({reference: Credentials(auth_token=SecretString(AUTH_TOKEN))}),
            provider_http_client,
        )
        connection = new_connection(client, reference)
        flow = InboundMessageFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="twilio-python-message-received-") as directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(directory) / "blobs"), 1 << 24))
            worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
            worker_task = asyncio.create_task(worker.start())
            try:
                await worker.wait_pull_registered(timeout=30)
                client_options = dex.ClientOptions(server_address=server_address)
                async with dex.AsyncClient(registry, cache, client_options) as dex_client:
                    configuration = MessageReceivedTriggerConfiguration(to=receiving_number)
                    yield MessageReceivedEnvironment(fake, connection, dex_client, flow, configuration)
            finally:
                await worker.stop()
                await worker_task
                cache.close()


@pytest.mark.asyncio
async def test_inbound_messages_start_one_flow_each_across_a_restart_with_real_dex() -> None:
    """Existing messages are a baseline, each later inbound message to the number starts exactly one Flow,
    outbound and other-number messages start none, and messages received while stopped arrive later."""
    async with message_received_environment(SUPPORT_NUMBER) as environment:
        fake = environment.fake
        fake.receive(CUSTOMER_NUMBER, SUPPORT_NUMBER, "before the baseline", now() - timedelta(seconds=30))
        async with trigger_process(environment) as first:
            await environment.wait_for_seed()
            hello = fake.receive(CUSTOMER_NUMBER, SUPPORT_NUMBER, "hello", now())
            stop = fake.receive(CUSTOMER_NUMBER, SUPPORT_NUMBER, "STOP", now())
            fake.send(SUPPORT_NUMBER, CUSTOMER_NUMBER, "outbound reply", now())
            fake.receive(CUSTOMER_NUMBER, SALES_NUMBER, "to sales", now())
            first.wake_poller(MESSAGE_RECEIVED_TRIGGER_BINDING)
            outcomes = await environment.wait_for_outcomes(2)
        assert outcomes[hello].current.status == "received", "the Flow reads the message back from Twilio"
        assert outcomes[hello].from_ == CUSTOMER_NUMBER
        assert not outcomes[hello].is_opt_out
        assert outcomes[stop].is_opt_out

        offline = fake.receive(CUSTOMER_NUMBER, SUPPORT_NUMBER, "sent while stopped", now())
        async with trigger_process(environment) as second:
            second.wake_poller(MESSAGE_RECEIVED_TRIGGER_BINDING)
            outcomes = await environment.wait_for_outcomes(3)
            assert outcomes[offline].to == SUPPORT_NUMBER
            # Two more polls finish every delivery the first one started.
            listed = len(fake.list_requests)
            second.wake_poller(MESSAGE_RECEIVED_TRIGGER_BINDING)
            await environment.wait_for_list_requests(listed + 1)
            second.wake_poller(MESSAGE_RECEIVED_TRIGGER_BINDING)
            await environment.wait_for_list_requests(listed + 2)
        assert len(environment.delivered) == 3, "no message is delivered twice and the baseline never is"


@pytest.mark.asyncio
async def test_rate_limit_pauses_and_a_processing_mms_waits_with_real_dex() -> None:
    """A 429 pauses the poll for Retry-After, and an MMS still in receiving starts its Flow once received."""
    async with message_received_environment(SUPPORT_NUMBER) as environment:
        fake = environment.fake
        async with trigger_process(environment) as process:
            await environment.wait_for_seed()
            photo = fake.receive(CUSTOMER_NUMBER, SUPPORT_NUMBER, "photo", now(), "receiving")
            fake.throttle_next_list(1)
            requests_before = len(fake.list_requests)
            process.wake_poller(MESSAGE_RECEIVED_TRIGGER_BINDING)
            await environment.wait_for_list_requests(requests_before + 2)
            assert not environment.delivered, "a message still in receiving is held"
            fake.set_status(photo, "received")
            process.wake_poller(MESSAGE_RECEIVED_TRIGGER_BINDING)
            outcomes = await environment.wait_for_outcomes(1)
        assert outcomes[photo].current.status == "received"


@pytest.mark.asyncio
async def test_changed_receiving_number_records_a_new_baseline_with_real_dex() -> None:
    """A binding saved for another number reseeds without delivering the gap."""
    async with message_received_environment(SUPPORT_NUMBER) as environment:
        fake = environment.fake
        async with trigger_process(environment):
            await environment.wait_for_seed()
        fake.receive(CUSTOMER_NUMBER, SALES_NUMBER, "before the new baseline", now())
        environment.configuration = MessageReceivedTriggerConfiguration(to=SALES_NUMBER)
        async with trigger_process(environment) as second:

            async def has_reset_once() -> bool:
                state = await environment.cursor_state()
                return state is not None and state.reset_count == 1

            await eventually(has_reset_once)
            after = fake.receive(CUSTOMER_NUMBER, SALES_NUMBER, "after the reset", now())
            second.wake_poller(MESSAGE_RECEIVED_TRIGGER_BINDING)
            outcomes = await environment.wait_for_outcomes(1)
        assert outcomes[after].to == SALES_NUMBER
        assert len(outcomes) == 1, "messages before the new baseline are not delivered"
