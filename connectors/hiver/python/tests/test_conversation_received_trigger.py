# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""conversationReceived runners, configuration, and client limits, as the Go runner tests check them."""

import asyncio
from collections.abc import AsyncIterator, Awaitable, Callable
from contextlib import asynccontextmanager
from datetime import timedelta
from typing import Any, cast

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
from dex_connectors.sdk.pollingtrigger import PollSchedule, new_memory_cursor_store
from dex_connectors.sdk.projectconfig import (
    Configuration,
    InMemoryObjectStore,
    LoadedProject,
    PollCursorKey,
    PollCursorStore,
    Scope,
    TriggerInbox,
    TriggerInboxKey,
)

from dex_connectors.hiver import (
    Client,
    Config,
    Connection,
    ConversationReceivedEvent,
    ConversationReceivedTriggerConfiguration,
    ConversationReceivedTriggerRoute,
    Credentials,
    ProjectConversationReceivedTriggerRoute,
    decode_conversation_received_event_payload,
    new_connection,
    new_conversation_received_trigger,
    new_conversation_received_trigger_runner,
    new_project_conversation_received_trigger_runner,
    validate_conversation_received_trigger_configuration,
)
from dex_connectors.hiver.triggers import _poll_schedule
from tests.hiver_conversations_fake import API_BASE_URL, API_KEY, HiverConversationsFake

INBOX_ID = "105902"
REFERENCE = ConnectionRef("hiver", "hiver-support")
SCOPE = Scope("hiver-test", "preview")
KEY = TriggerInboxKey("hiver", REFERENCE.name, "conversationReceived", "new-conversations")


class QueueTarget:
    """Consumes each event into a queue."""

    def __init__(self) -> None:
        self.events: asyncio.Queue[TriggerEvent[ConversationReceivedEvent]] = asyncio.Queue()

    async def handle_trigger(
        self, context: TriggerContext, event: TriggerEvent[ConversationReceivedEvent]
    ) -> None:
        await self.events.put(event)

    async def next_event(self) -> TriggerEvent[ConversationReceivedEvent]:
        return await asyncio.wait_for(self.events.get(), 10)


class ProjectStub:
    """The parts of LoadedProject the durable runner reads, over one in-memory object store."""

    def __init__(self, trigger_bindings: tuple[dict[str, Any], ...]) -> None:
        self.objects = InMemoryObjectStore()
        self.configuration = Configuration(SCOPE, 1, (), trigger_bindings)

    def trigger_inbox(self, key: TriggerInboxKey) -> TriggerInbox:
        return TriggerInbox(self.objects, SCOPE, key)

    def poll_cursor_store(self, key: PollCursorKey) -> PollCursorStore:
        return PollCursorStore(self.objects, SCOPE, key)


def new_client(provider: HiverConversationsFake, http_client: httpx.AsyncClient, **config: Any) -> Client:
    return Client(
        Config(request_interval_milliseconds=1, poll_interval_seconds=1, **config),
        StaticCredentialProvider({REFERENCE: Credentials(SecretString(API_KEY))}),
        http_client,
        API_BASE_URL,
    )


@asynccontextmanager
async def connection_for(provider: HiverConversationsFake) -> AsyncIterator[Connection]:
    async with httpx.AsyncClient(transport=provider.transport) as http_client:
        yield new_connection(new_client(provider, http_client), REFERENCE)


async def eventually(condition: Callable[[], Awaitable[bool]]) -> None:
    async with asyncio.timeout(10):
        while not await condition():
            await asyncio.sleep(0.01)


@asynccontextmanager
async def running(runner: Any) -> AsyncIterator[None]:
    cancellation = TriggerCancellation()
    task = asyncio.create_task(runner.run(TriggerContext(cancellation)))
    try:
        yield
    finally:
        cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await task


def test_configuration_and_client_limits() -> None:
    validate_conversation_received_trigger_configuration(ConversationReceivedTriggerConfiguration(INBOX_ID))
    with pytest.raises(ValueError, match="inboxId"):
        validate_conversation_received_trigger_configuration(ConversationReceivedTriggerConfiguration())
    with pytest.raises(ValueError):
        validate_conversation_received_trigger_configuration(
            ConversationReceivedTriggerConfiguration("../inboxes")
        )
    credentials = StaticCredentialProvider[Credentials]({})
    for pages in (1, 6):
        with pytest.raises(ValueError, match="pollPagesPerPoll must be from 2 to 5"):
            Client(Config(poll_pages_per_poll=pages), credentials)
    with pytest.raises(ValueError, match="pollInterval"):
        Client(Config(poll_interval_seconds=0.001), credentials)
    client = Client(Config(), credentials)
    assert client.poll_interval == timedelta(minutes=5) and client.poll_pages_per_poll == 3
    assert _poll_schedule(client) == PollSchedule(timedelta(minutes=5), maximum_backoff=timedelta(minutes=30))
    long_interval = Client(Config(poll_interval_seconds=3600), credentials)
    assert _poll_schedule(long_interval).maximum_backoff == timedelta(hours=1)


async def test_runner_rejects_incomplete_routes() -> None:
    provider = HiverConversationsFake(INBOX_ID, "support@acme.example.com")
    target = QueueTarget()
    configuration = ConversationReceivedTriggerConfiguration(INBOX_ID)
    cursor = new_memory_cursor_store()
    async with connection_for(provider) as connection:
        for routes in {
            "none": [],
            "no binding": [ConversationReceivedTriggerRoute("", configuration, cursor, target)],
            "no cursor": [ConversationReceivedTriggerRoute("a", configuration, cast(Any, None), target)],
            "no target": [ConversationReceivedTriggerRoute("a", configuration, cursor, cast(Any, None))],
            "bad inbox": [
                ConversationReceivedTriggerRoute(
                    "a", ConversationReceivedTriggerConfiguration(" "), cursor, target
                )
            ],
        }.values():
            with pytest.raises(ValueError):
                new_conversation_received_trigger_runner(connection, routes)
    with pytest.raises(ValueError, match="Hiver client"):
        new_conversation_received_trigger_runner(
            cast(Connection, object()), [ConversationReceivedTriggerRoute("a", configuration, cursor, target)]
        )


def binding_entry(configuration: dict[str, Any]) -> dict[str, Any]:
    return {
        "connectorId": "hiver",
        "connectionName": REFERENCE.name,
        "triggerName": "conversationReceived",
        "bindingName": "new-conversations",
        "configuration": configuration,
    }


async def test_durable_runner_loads_the_saved_inbox_and_records_before_committing() -> None:
    provider = HiverConversationsFake(INBOX_ID, "support@acme.example.com")
    provider.add_conversations(INBOX_ID, "573741001", "573741002")
    project = ProjectStub((binding_entry({"inboxId": INBOX_ID}),))
    target = QueueTarget()
    cursor = project.poll_cursor_store(PollCursorKey.from_trigger_inbox_key(KEY))
    async with connection_for(provider) as connection:
        runner = new_project_conversation_received_trigger_runner(
            cast(LoadedProject, project),
            connection,
            [ProjectConversationReceivedTriggerRoute("new-conversations", target)],
        )

        async def is_baselined() -> bool:
            state = (await cursor.read_poll_cursor()).state
            return state is not None and '"baselineComplete":true' in state.token

        async with running(runner):
            await eventually(is_baselined)
            provider.add_conversations(INBOX_ID, "573741003")
            runner.wake_poller("new-conversations")
            event = await target.next_event()
            assert event.id == "conversation-105902-573741003"
            assert event.payload.conversation.inbox_id == INBOX_ID
            inbox = project.trigger_inbox(KEY)

            async def is_drained() -> bool:
                return not await inbox.pending()

            await eventually(is_drained)
        state = (await cursor.read_poll_cursor()).state
        assert state is not None and state.snapshot is not None
        assert [entry.key for entry in state.snapshot.entries] == ["573741001", "573741002", "573741003"]
        assert target.events.empty(), "the baseline delivers nothing"


async def test_durable_runner_rejects_a_missing_or_invalid_saved_configuration() -> None:
    provider = HiverConversationsFake(INBOX_ID, "support@acme.example.com")
    async with connection_for(provider) as connection:
        for bindings in ((), (binding_entry({"inboxId": INBOX_ID, "unknown": True}),), (binding_entry({}),)):
            with pytest.raises(ValueError, match="new-conversations"):
                new_project_conversation_received_trigger_runner(
                    cast(LoadedProject, ProjectStub(bindings)),
                    connection,
                    [ProjectConversationReceivedTriggerRoute("new-conversations", QueueTarget())],
                )


async def test_single_trigger_baselines_and_delivers_the_next_conversation() -> None:
    provider = HiverConversationsFake(INBOX_ID, "support@acme.example.com")
    provider.add_conversations(INBOX_ID, "573741001")
    target = QueueTarget()
    async with connection_for(provider) as connection:
        trigger = new_conversation_received_trigger(
            connection=connection,
            binding_name="new-conversations",
            configuration=ConversationReceivedTriggerConfiguration(INBOX_ID),
            target=target,
        )
        async with running(trigger):
            await eventually(lambda: _requested_at_least(provider, 1))
            provider.add_conversations(INBOX_ID, "573741002")
            event = await target.next_event()
            assert event.payload.conversation.id == "573741002"


async def _requested_at_least(provider: HiverConversationsFake, count: int) -> bool:
    return provider.conversation_list_requests >= count


def test_inbox_payload_decodes_a_go_written_event() -> None:
    go_payload = (
        '{"inboxId":"105902","conversation":{"id":"573741003","inboxId":"105902","status":"open",'
        '"assignee":{"type":"user","id":"4401"},"tagIds":["77"],"gmailThreadId":"19cf573741003",'
        '"privatePermalink":"https://app.hiverhq.com/conversation/573741003"}}'
    )
    event = decode_conversation_received_event_payload(go_payload, go_payload.encode())
    assert event.inbox_id == INBOX_ID and event.conversation.id == "573741003"
    assert event.conversation.tag_ids == ("77",)
    with pytest.raises(ValueError):
        decode_conversation_received_event_payload('{"inboxId":"105902"}', b"")
