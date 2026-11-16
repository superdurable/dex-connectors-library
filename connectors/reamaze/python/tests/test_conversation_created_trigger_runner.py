# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""conversationCreated runners, ported from the Go connector's conversation_created_trigger_runner tests."""

import asyncio
import json
from collections.abc import AsyncIterator, Awaitable, Callable
from contextlib import asynccontextmanager
from datetime import UTC, datetime, timedelta
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
    TriggerLogLevel,
)
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

from dex_connectors.reamaze import (
    Client,
    Config,
    Connection,
    ConversationCreatedEvent,
    ConversationCreatedTriggerConfiguration,
    ConversationCreatedTriggerRoute,
    ConversationTriggerRunnerConfig,
    Credentials,
    ProjectConversationCreatedTriggerRoute,
    ProjectConversationTriggerRunnerConfig,
    conversation_created_event_id,
    decode_conversation_created_event_payload,
    new_connection,
    new_conversation_created_trigger,
    new_conversation_trigger_runner,
    new_project_conversation_trigger_runner,
)
from tests.reamaze_conversations_fake import ReamazeConversationFake

EMAIL = "agent@acme.example.com"
TOKEN = "reamazeRunner" + "Token0123456789"  # Split so push protection does not read a fixture as a token.
REFERENCE = ConnectionRef("reamaze", "reamaze-brand")
SCOPE = Scope("reamaze-test", "preview")
KEY = TriggerInboxKey("reamaze", REFERENCE.name, "conversationCreated", "newConversations")
START = datetime(2026, 3, 2, 9, 0, tzinfo=UTC)


class QueueTarget:
    """Consumes each event into a queue."""

    def __init__(self) -> None:
        self.events: asyncio.Queue[TriggerEvent[ConversationCreatedEvent]] = asyncio.Queue()

    async def handle_trigger(
        self, context: TriggerContext, event: TriggerEvent[ConversationCreatedEvent]
    ) -> None:
        """Record the event."""
        await self.events.put(event)

    async def next_event(self) -> TriggerEvent[ConversationCreatedEvent]:
        """Wait up to ten seconds for the next event."""
        return await asyncio.wait_for(self.events.get(), 10)


class ProjectStub:
    """The parts of LoadedProject the durable runner reads, over one in-memory object store."""

    def __init__(self, trigger_bindings: tuple[dict[str, Any], ...] = ()) -> None:
        self.objects = InMemoryObjectStore()
        self.configuration = Configuration(SCOPE, 1, (), trigger_bindings)

    def trigger_inbox(self, key: TriggerInboxKey) -> TriggerInbox:
        return TriggerInbox(self.objects, SCOPE, key)

    def poll_cursor_store(self, key: PollCursorKey) -> PollCursorStore:
        return PollCursorStore(self.objects, SCOPE, key)


def saved_binding(configuration: dict[str, Any]) -> dict[str, Any]:
    """One saved conversationCreated binding of the test connection."""
    return {
        "connectorId": "reamaze",
        "connectionName": REFERENCE.name,
        "triggerName": "conversationCreated",
        "bindingName": "newConversations",
        "configuration": configuration,
    }


@asynccontextmanager
async def connection_for(fake: ReamazeConversationFake) -> AsyncIterator[Connection]:
    """A connection whose requests the fake answers."""
    async with httpx.AsyncClient(transport=fake.transport()) as http_client:
        client = Client(
            Config(brand="acme", poll_interval_seconds=10),
            StaticCredentialProvider({REFERENCE: Credentials(EMAIL, SecretString(TOKEN))}),
            http_client,
            api_base_url=fake.base_url,
        )
        yield new_connection(client, REFERENCE)


async def eventually(condition: Callable[[], Awaitable[bool]]) -> None:
    """Poll condition for up to ten seconds."""
    async with asyncio.timeout(10):
        while not await condition():
            await asyncio.sleep(0.01)


@asynccontextmanager
async def running(runner: Any) -> AsyncIterator[None]:
    """Run a runner until the block exits, then require it to stop through cancellation."""
    cancellation = TriggerCancellation()
    task = asyncio.create_task(runner.run(TriggerContext(cancellation)))
    try:
        yield
    finally:
        cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await task


def test_client_rejects_a_poll_interval_under_ten_seconds() -> None:
    credentials = StaticCredentialProvider({REFERENCE: Credentials(EMAIL, SecretString(TOKEN))})
    with pytest.raises(ValueError, match="at least 10s"):
        Client(Config(brand="acme", poll_interval_seconds=9), credentials)


def test_client_uses_the_one_minute_default_for_a_zero_poll_interval() -> None:
    credentials = StaticCredentialProvider({REFERENCE: Credentials(EMAIL, SecretString(TOKEN))})
    assert Client(Config(brand="acme", poll_interval_seconds=0), credentials).poll_interval_seconds == 60


async def test_runner_rejects_incomplete_routes() -> None:
    fake = ReamazeConversationFake(EMAIL, TOKEN)
    target = QueueTarget()
    async with connection_for(fake) as connection:
        for routes in {
            "none": [],
            "no binding": [ConversationCreatedTriggerRoute(" ", target)],
            "no target": [ConversationCreatedTriggerRoute("newConversations", cast(Any, None))],
            "bad channel": [
                ConversationCreatedTriggerRoute(
                    "newConversations", target, ConversationCreatedTriggerConfiguration(channel="a/b")
                )
            ],
            "duplicate": [
                ConversationCreatedTriggerRoute("a", target),
                ConversationCreatedTriggerRoute("a", target),
            ],
        }.values():
            with pytest.raises(ValueError):
                new_conversation_trigger_runner(ConversationTriggerRunnerConfig(connection, routes))
    mock = cast(Connection, None)
    with pytest.raises(ValueError, match="connection is required"):
        new_conversation_trigger_runner(
            ConversationTriggerRunnerConfig(mock, [ConversationCreatedTriggerRoute("a", target)])
        )


async def test_durable_runner_loads_the_saved_channel_and_commits_after_recording() -> None:
    fake = ReamazeConversationFake(EMAIL, TOKEN)
    fake.insert("existing", "Existing", channel="support", created_at=START)
    project = ProjectStub((saved_binding({"channel": "support"}),))
    target = QueueTarget()
    cursor = project.poll_cursor_store(PollCursorKey.from_trigger_inbox_key(KEY))
    async with connection_for(fake) as connection:
        runner = new_project_conversation_trigger_runner(
            cast(LoadedProject, project),
            connection,
            ProjectConversationTriggerRunnerConfig(
                [ProjectConversationCreatedTriggerRoute("newConversations", target)]
            ),
        )

        async def commit_count() -> int:
            state = (await cursor.read_poll_cursor()).state
            return state.commit_count if state is not None else 0

        async with running(runner):
            await eventually(lambda: _at_least(commit_count, 1))
            state = (await cursor.read_poll_cursor()).state
            assert state is not None
            assert json.loads(state.token)["channel"] == "support"
            fake.insert("elsewhere", "Sales", channel="sales", created_at=START + timedelta(minutes=1))
            fake.insert("refund", "Refund <now>", channel="support", created_at=START + timedelta(minutes=2))
            runner.wake_poller("newConversations")
            event = await target.next_event()
            assert event.id == conversation_created_event_id("acme", "refund")
            assert event.payload.channel == "support"
            assert event.payload.conversation.first_message == "Opening message of Refund <now>"
            await eventually(lambda: _at_least(commit_count, 2))
            assert target.events.empty(), "the other channel is not delivered"
            assert all(query.get("category") == "support" for query in fake.listed_queries())
            assert all(query.get("filter") == "all" for query in fake.listed_queries())
            inbox = project.trigger_inbox(KEY)

            async def is_drained() -> bool:
                return not await inbox.pending()

            await eventually(is_drained)


async def test_durable_runner_watches_every_channel_for_an_unsaved_binding() -> None:
    fake = ReamazeConversationFake(EMAIL, TOKEN)
    project = ProjectStub()
    target = QueueTarget()
    async with connection_for(fake) as connection:
        runner = new_project_conversation_trigger_runner(
            cast(LoadedProject, project),
            connection,
            ProjectConversationTriggerRunnerConfig(
                [ProjectConversationCreatedTriggerRoute("newConversations", target)]
            ),
        )
        async with running(runner):
            await eventually(lambda: _listed_at_least(fake, 1))
            fake.insert("sales-1", "Sales", channel="sales", created_at=START)
            runner.wake_poller("newConversations")
            event = await target.next_event()
            assert event.payload.channel == "" and event.payload.conversation.channel.slug == "sales"
        assert "category" not in fake.listed_queries()[0]


async def test_durable_runner_rejects_an_invalid_saved_configuration() -> None:
    fake = ReamazeConversationFake(EMAIL, TOKEN)
    async with connection_for(fake) as connection:
        for configuration in ({"channel": "a/b"}, {"channel": "support", "unknown": True}, {"channel": 1}):
            project = ProjectStub((saved_binding(configuration),))
            with pytest.raises(ValueError, match="conversationCreated"):
                new_project_conversation_trigger_runner(
                    cast(LoadedProject, project),
                    connection,
                    ProjectConversationTriggerRunnerConfig(
                        [ProjectConversationCreatedTriggerRoute("newConversations", QueueTarget())]
                    ),
                )


async def test_single_trigger_baselines_and_delivers_the_next_conversation() -> None:
    fake = ReamazeConversationFake(EMAIL, TOKEN)
    fake.insert("existing", "Existing", created_at=START)
    target = QueueTarget()
    async with connection_for(fake) as connection:
        with pytest.raises(ValueError, match="connection_name"):
            new_conversation_created_trigger(connection=connection, connection_name="other", target=target)
        with pytest.raises(ValueError, match="channel"):
            new_conversation_created_trigger(
                connection=connection,
                connection_name=REFERENCE.name,
                target=target,
                configuration=ConversationCreatedTriggerConfiguration(channel="a b"),
            )
        # A short interval keeps the test fast; Client rejects one under the 10s minimum.
        assert connection._client is not None
        connection._client.poll_interval_seconds = 1
        trigger = new_conversation_created_trigger(
            connection=connection, connection_name=REFERENCE.name, target=target
        )
        async with running(trigger):
            await eventually(lambda: _listed_at_least(fake, 1))
            fake.insert("new", "New", created_at=START + timedelta(seconds=1))
            event = await target.next_event()
            assert event.payload.conversation.id == "new"
            assert event.occurred_at == START + timedelta(seconds=1)


class RecordingLogger:
    """Keeps every Trigger log record as text."""

    def __init__(self) -> None:
        self.records: list[str] = []

    def is_enabled_for(self, level: TriggerLogLevel) -> bool:
        return True

    def log(self, level: TriggerLogLevel, message: str, attributes: Any = None) -> None:
        self.records.append(f"{level} {message} {attributes!r}")


async def test_rejected_credentials_never_reach_the_trigger_log() -> None:
    fake = ReamazeConversationFake(EMAIL, "reamazeOther" + "Token0123456789")
    logger = RecordingLogger()
    async with connection_for(fake) as connection:
        runner = new_conversation_trigger_runner(
            ConversationTriggerRunnerConfig(
                connection, [ConversationCreatedTriggerRoute("newConversations", QueueTarget())], logger
            )
        )
        async with running(runner):
            await eventually(lambda: _has_records(logger))
    assert logger.records
    assert not any(TOKEN in record or EMAIL in record for record in logger.records)


async def _has_records(logger: RecordingLogger) -> bool:
    return any("warn" in record or "error" in record for record in logger.records)


async def _at_least(read: Callable[[], Awaitable[int]], expected: int) -> bool:
    return await read() >= expected


async def _listed_at_least(fake: ReamazeConversationFake, count: int) -> bool:
    return fake.list_request_count() >= count


def test_inbox_payload_decodes_a_go_written_event() -> None:
    go_payload = (
        '{"brand":"acme","channel":"support","conversation":{"id":"refund",'
        '"subject":"Refund \\u003cnow\\u003e",'
        '"status":4,"tags":[],"channel":{"slug":"support","name":"Support","type":1},'
        '"requester":{"name":"Jane Smith","email":"jane@acme.example.com"},'
        '"firstMessage":"Opening message","createdAt":"2026-03-02T09:01:00.123Z"}}'
    )
    event = decode_conversation_created_event_payload(go_payload, go_payload.encode())
    assert event.conversation.subject == "Refund <now>"
    assert event.conversation.status == 4
    assert event.conversation.created_at == START.replace(minute=1, microsecond=123000)
    assert event.conversation.requester is not None
    with pytest.raises(ValueError):
        decode_conversation_created_event_payload('{"brand":"acme","conversation":[]}', b"")
