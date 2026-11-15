# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The ordered Gmail message Trigger runner, its standalone factories, and its project inboxes."""

import asyncio
import time
from collections.abc import AsyncIterator, Callable
from pathlib import Path
from typing import Any, cast

import httpx
import pytest
import yaml
from dex_connectors.sdk import (
    TriggerCancellation,
    TriggerContext,
    TriggerDeliveryAbortedError,
    TriggerEvent,
)
from dex_connectors.sdk.projectconfig import (
    Configuration,
    InMemoryObjectStore,
    LoadedProject,
    Scope,
    TriggerInbox,
    TriggerInboxKey,
)

from dex_connectors.google_gmail import (
    MESSAGE_RECEIVED_TRIGGER_DEFINITION,
    REPLY_RECEIVED_TRIGGER_DEFINITION,
    MessageEvent,
    MessageMatcher,
    MessageReceivedTriggerConfiguration,
    MessageReceivedTriggerRoute,
    ProjectMessageReceivedTriggerRoute,
    ProjectReplyReceivedTriggerRoute,
    ReplyReceivedTriggerConfiguration,
    ReplyReceivedTriggerRoute,
    decode_message_event_payload,
    decode_message_received_trigger_configuration,
    decode_reply_received_trigger_configuration,
    define_message_received_trigger_binding,
    matches_message_event,
    new_connection_with_operations,
    new_message_received_trigger,
    new_message_trigger_runner,
    new_project_message_trigger_runner,
    new_reply_received_trigger,
)
from dex_connectors.google_gmail._trigger_source import (
    MessageListing,
    MessagePollingTriggerSource,
    MessageTriggerRouteSource,
    poll_message_trigger_routes,
)
from dex_connectors.google_gmail.gmailmock import Mock
from tests.gmail_inbox_fake import (
    CONNECTION_REFERENCE,
    FakeGmail,
    RecordingLogger,
    fake_connection,
    fake_http_client,
    fake_provider,
    reply_message,
    root_message,
)
from tests.test_message_triggers import RecordingTarget

MANIFEST = Path(__file__).resolve().parents[2] / "connector.yaml"
SCOPE = Scope("gmail-triggers", "live")


@pytest.fixture
async def gmail_http() -> AsyncIterator[tuple[FakeGmail, httpx.AsyncClient]]:
    gmail = FakeGmail()
    async with fake_http_client(gmail) as http_client:
        yield gmail, http_client


async def eventually(condition: Callable[[], bool], timeout: float = 5) -> None:
    """Poll condition until it holds."""
    deadline = time.monotonic() + timeout
    while not condition():
        assert time.monotonic() < deadline, "condition did not converge"
        await asyncio.sleep(0.01)


async def test_delivers_every_root_before_any_reply_including_a_root_that_arrives_during_the_poll(
    gmail_http: tuple[FakeGmail, httpx.AsyncClient],
) -> None:
    gmail, http_client = gmail_http
    provider = fake_provider(gmail, http_client, RecordingLogger())
    order: list[str] = []
    root_route = MessageTriggerRouteSource(
        MessagePollingTriggerSource(
            provider, CONNECTION_REFERENCE, "messageReceived", "start", "", None, False
        ),
        RecordingTarget(order),
    )
    reply_source = MessagePollingTriggerSource(
        provider, CONNECTION_REFERENCE, "replyReceived", "reply", "", None, True
    )
    reply_route = MessageTriggerRouteSource(reply_source, RecordingTarget(order))
    gmail.receive(root_message("root-1", "t1"))
    gmail.receive(reply_message("reply-1", "t1"))
    gmail.receive(root_message("root-2", "t2"))
    await poll_message_trigger_routes(TriggerContext(), [root_route], [reply_route])
    assert order == ["root-1", "root-2", "reply-1"]

    class InjectingReplySource:
        """Receives a reply right after the reply listing, so it waits for the next poll."""

        async def list(self, context: TriggerContext) -> MessageListing:
            listing = await reply_source.list(context)
            gmail.receive(reply_message("reply-3", "t3"))
            return listing

        async def deliver(self, context: TriggerContext, target: Any, listing: MessageListing) -> None:
            await reply_source.deliver(context, target, listing)

    gmail.receive(root_message("root-3", "t3"))
    listed_before = gmail.list_count
    injecting = MessageTriggerRouteSource(cast(Any, InjectingReplySource()), reply_route.target)
    await poll_message_trigger_routes(TriggerContext(), [root_route], [injecting])
    assert gmail.list_count - listed_before == 2
    assert order == ["root-1", "root-2", "reply-1", "root-3"]
    await poll_message_trigger_routes(TriggerContext(), [root_route], [reply_route])
    assert order == ["root-1", "root-2", "reply-1", "root-3", "reply-3"]


async def test_stops_a_poll_at_the_first_root_failure_so_no_reply_overtakes_it(
    gmail_http: tuple[FakeGmail, httpx.AsyncClient],
) -> None:
    gmail, http_client = gmail_http
    order: list[str] = []
    roots = RecordingTarget(order, failures={"root-1": 1})
    replies = RecordingTarget(order)
    runner = new_message_trigger_runner(
        fake_connection(gmail, http_client, RecordingLogger()),
        message_received_routes=[MessageReceivedTriggerRoute("start", roots)],
        reply_received_routes=[ReplyReceivedTriggerRoute("reply", replies)],
    )
    gmail.receive(root_message("root-1", "t1"))
    gmail.receive(reply_message("reply-1", "t1"))
    cancellation = TriggerCancellation()
    running = asyncio.create_task(runner.run(TriggerContext(cancellation)))
    await eventually(lambda: order == ["root-1", "reply-1"])
    cancellation.cancel()
    with pytest.raises(TriggerDeliveryAbortedError):
        await running
    assert roots.attempts == ["root-1", "root-1"]


async def test_runs_one_standalone_trigger_until_the_context_ends(
    gmail_http: tuple[FakeGmail, httpx.AsyncClient],
) -> None:
    gmail, http_client = gmail_http
    gmail.receive(root_message("m1", "t1"))
    gmail.receive(reply_message("r1", "t1"))
    connection = fake_connection(gmail, http_client, RecordingLogger())
    roots = RecordingTarget()
    replies = RecordingTarget()
    root_trigger = new_message_received_trigger(
        connection=connection,
        binding_name="start",
        target=roots,
        configuration=MessageReceivedTriggerConfiguration(
            message_matcher=MessageMatcher(message_contains="order")
        ),
    )
    reply_trigger = new_reply_received_trigger(connection=connection, binding_name="reply", target=replies)
    assert root_trigger.binding.name == "start"
    assert root_trigger.definition == MESSAGE_RECEIVED_TRIGGER_DEFINITION
    assert reply_trigger.definition == REPLY_RECEIVED_TRIGGER_DEFINITION
    cancellation = TriggerCancellation()
    running = [
        asyncio.create_task(trigger.run(TriggerContext(cancellation)))
        for trigger in (root_trigger, reply_trigger)
    ]
    await eventually(lambda: roots.ids() == ["m1"] and replies.ids() == ["r1"])
    cancellation.cancel()
    for task in running:
        with pytest.raises(TriggerDeliveryAbortedError):
            await task


async def test_rejects_invalid_wiring_and_sender_emails(
    gmail_http: tuple[FakeGmail, httpx.AsyncClient],
) -> None:
    gmail, http_client = gmail_http
    connection = fake_connection(gmail, http_client)
    target = RecordingTarget()
    with pytest.raises(ValueError, match="Gmail message Trigger routes are required"):
        new_message_trigger_runner(connection)
    with pytest.raises(ValueError, match='"a" is duplicated'):
        new_message_trigger_runner(
            connection,
            message_received_routes=[
                MessageReceivedTriggerRoute("a", target),
                MessageReceivedTriggerRoute(" a ", target),
            ],
        )
    with pytest.raises(ValueError, match="binding name and target are required"):
        new_message_trigger_runner(
            connection, message_received_routes=[MessageReceivedTriggerRoute(" ", target)]
        )
    assert new_message_trigger_runner(
        connection,
        message_received_routes=[MessageReceivedTriggerRoute("a", target)],
        reply_received_routes=[ReplyReceivedTriggerRoute("a", target)],
    )
    invalid = MessageReceivedTriggerConfiguration(
        message_matcher=MessageMatcher(sender_emails=("not an address",))
    )
    with pytest.raises(ValueError, match="Gmail sender email is invalid"):
        new_message_trigger_runner(
            connection, message_received_routes=[MessageReceivedTriggerRoute("a", target, invalid)]
        )
    repeated = ReplyReceivedTriggerConfiguration(
        reply_matcher=MessageMatcher(sender_emails=("Ada <ada@example.com>", "ADA@example.com"))
    )
    with pytest.raises(ValueError, match="Gmail sender emails must be unique"):
        new_reply_received_trigger(
            connection=connection, binding_name="a", target=target, configuration=repeated
        )
    mock = Mock("gmail-inbox").connection
    mock_connection = new_connection_with_operations(
        mock.get_message(), mock.send_message(), mock.reply_to_message(), CONNECTION_REFERENCE
    )
    with pytest.raises(ValueError, match="opened with a Gmail provider"):
        new_message_trigger_runner(
            mock_connection, message_received_routes=[MessageReceivedTriggerRoute("a", target)]
        )


def test_decodes_saved_configuration_strictly_like_go() -> None:
    decoded = decode_reply_received_trigger_configuration(
        {"searchQuery": "in:inbox", "replyMatcher": {"senderEmails": None, "messageContains": "ok"}}
    )
    assert decoded == ReplyReceivedTriggerConfiguration("in:inbox", MessageMatcher("ok"))
    assert decode_message_received_trigger_configuration({}) == MessageReceivedTriggerConfiguration()
    with pytest.raises(ValueError, match="unknown field"):
        decode_message_received_trigger_configuration({"replyMatcher": {}})
    with pytest.raises(ValueError, match="must be a string"):
        decode_message_received_trigger_configuration({"searchQuery": 1})


def test_matches_like_the_go_matcher() -> None:
    event = MessageEvent(subject="Order 1042", snippet="Has it SHIPPED?", from_='"Ada L." <Ada@Example.com>')
    assert matches_message_event(MessageMatcher("shipped", ("ada@example.com",)), event)
    assert matches_message_event(MessageMatcher("1042\nhas"), event)
    assert matches_message_event(None, event)
    assert not matches_message_event(MessageMatcher(sender_emails=("eve@example.com",)), event)
    not_a_mailbox = MessageEvent(from_="not a mailbox")
    assert not matches_message_event(MessageMatcher(sender_emails=("ada@example.com",)), not_a_mailbox)


def test_declares_the_manifest_triggers_and_binding_factories() -> None:
    manifest = yaml.safe_load(MANIFEST.read_text())
    assert manifest["spec"]["languages"]["python"]["triggers"] == ["messageReceived", "replyReceived"]
    definitions = [MESSAGE_RECEIVED_TRIGGER_DEFINITION, REPLY_RECEIVED_TRIGGER_DEFINITION]
    assert [(definition.trigger.trigger_name, definition.description) for definition in definitions] == [
        (trigger["name"], trigger["description"]) for trigger in manifest["spec"]["triggers"]
    ]
    binding = define_message_received_trigger_binding(connection_name="gmail-inbox", binding_name="start")
    assert binding.definition == MESSAGE_RECEIVED_TRIGGER_DEFINITION
    with pytest.raises(ValueError):
        define_message_received_trigger_binding(connection_name="", binding_name="start")


def test_decodes_a_go_inbox_payload() -> None:
    go_payload = (
        '{"primaryEmail":"owner@example.com","messageId":"m1","threadId":"t1",'
        '"from":"Ada \\u003cada@example.com\\u003e",'
        '"subject":"Order","receivedAt":"2026-01-02T00:00:00.12Z","isReply":true}'
    )
    decoded = decode_message_event_payload(go_payload, go_payload.encode())
    assert decoded.from_ == "Ada <ada@example.com>" and decoded.is_reply and decoded.snippet == ""
    assert decoded.received_at.microsecond == 120000
    with pytest.raises(ValueError, match="payload is invalid"):
        decode_message_event_payload('"text"', b'"text"')


class SharedProject:
    """The project storage two Trigger processes share: hosted storage keeps the inboxes."""

    def __init__(self) -> None:
        self.objects = InMemoryObjectStore()
        self.configuration = Configuration(
            SCOPE,
            1,
            (),
            (
                {
                    "connectorId": "gmail",
                    "connectionName": "gmail-inbox",
                    "triggerName": "messageReceived",
                    "bindingName": "start",
                    "configuration": {"messageMatcher": {"messageContains": "order"}},
                },
                {
                    "connectorId": "gmail",
                    "connectionName": "gmail-inbox",
                    "triggerName": "replyReceived",
                    "bindingName": "reply",
                    "configuration": {"replyMatcher": {"senderEmails": ["ada@example.com"]}},
                },
            ),
        )

    def trigger_inbox(self, key: TriggerInboxKey) -> TriggerInbox:
        return TriggerInbox(self.objects, SCOPE, key)


async def test_keeps_a_failed_event_in_the_binding_inbox_and_replays_it_before_polling(
    gmail_http: tuple[FakeGmail, httpx.AsyncClient],
) -> None:
    gmail, http_client = gmail_http
    project = SharedProject()
    loaded = cast(LoadedProject, project)
    logger = RecordingLogger()
    connection = fake_connection(gmail, http_client, logger)
    gmail.receive(root_message("root-1", "t1"))
    failing = RecordingTarget(failures={"root-1": 1 << 30})
    first = new_project_message_trigger_runner(
        loaded, connection, message_received_routes=[ProjectMessageReceivedTriggerRoute("start", failing)]
    )
    stop_first = TriggerCancellation()
    first_run = asyncio.create_task(first.run(TriggerContext(stop_first)))
    await eventually(lambda: len(failing.attempts) > 0)
    stop_first.cancel()
    with pytest.raises(TriggerDeliveryAbortedError):
        await first_run
    inbox = project.trigger_inbox(TriggerInboxKey("gmail", "gmail-inbox", "messageReceived", "start"))
    assert [event.id for event in await inbox.pending()] == ["root-1"]

    gmail.inbox = []
    order: list[str] = []
    roots = RecordingTarget(order)
    replies = RecordingTarget(order)
    lists_at_replay: list[int] = []
    handle_root = roots.handle_trigger

    async def record_lists_then_handle(context: TriggerContext, event: TriggerEvent[MessageEvent]) -> None:
        lists_at_replay.append(gmail.list_count)
        await handle_root(context, event)

    roots.handle_trigger = record_lists_then_handle  # type: ignore[method-assign]
    second = new_project_message_trigger_runner(
        loaded,
        connection,
        message_received_routes=[ProjectMessageReceivedTriggerRoute("start", roots)],
        reply_received_routes=[ProjectReplyReceivedTriggerRoute("reply", replies)],
    )
    lists_before = gmail.list_count
    stop_second = TriggerCancellation()
    second_run = asyncio.create_task(second.run(TriggerContext(stop_second)))
    await eventually(lambda: roots.ids() == ["root-1"])
    assert lists_at_replay[0] == lists_before, "the inbox replays before the first poll"
    payload = roots.delivered[0].payload
    assert (payload.message_id, payload.thread_id, payload.primary_email, payload.is_reply) == (
        "root-1",
        "t1",
        "owner@example.com",
        False,
    )
    gmail.receive(reply_message("reply-1", "t1"))
    await eventually(lambda: order == ["root-1", "reply-1"])
    stop_second.cancel()
    with pytest.raises(TriggerDeliveryAbortedError):
        await second_run
    assert await inbox.pending() == ()
    assert "SENTINEL" not in repr([record.attributes for record in logger.records])


async def test_names_a_binding_missing_from_the_project_configuration(
    gmail_http: tuple[FakeGmail, httpx.AsyncClient],
) -> None:
    gmail, http_client = gmail_http
    with pytest.raises(ValueError, match='gmail replyReceived binding "absent" configuration'):
        new_project_message_trigger_runner(
            cast(LoadedProject, SharedProject()),
            fake_connection(gmail, http_client),
            reply_received_routes=[ProjectReplyReceivedTriggerRoute("absent", RecordingTarget())],
        )
