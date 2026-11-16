# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""messageReceived runner wiring, parity with the Go connector's message_trigger_runner_internal_test.go."""

import asyncio
import json
from collections.abc import AsyncIterator, Awaitable, Callable
from datetime import UTC, datetime, timedelta
from pathlib import Path

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
from dex_connectors.sdk.projectconfig import PollCursorKey, TriggerInboxKey, load_from_environment

from dex_connectors.twilio_messaging import (
    Client,
    Config,
    Connection,
    Credentials,
    MessageReceivedEvent,
    MessageReceivedTriggerConfiguration,
    MessageReceivedTriggerRoute,
    ProjectMessageReceivedTriggerRoute,
    new_connection,
    new_message_received_trigger,
    new_message_trigger_runner,
    new_project_message_trigger_runner,
)
from dex_connectors.twilio_messaging.messagingmock import Mock
from tests.twilio_messages_fake import TwilioMessagesFake

ACCOUNT_SID = "AC" + "00112233445566778899aabbccddeeff"
AUTH_TOKEN = "fetcher-auth-token-0123456789abcd"
NUMBER = "+14155550100"
OTHER_NUMBER = "+14155550199"
CUSTOMER = "+14155550123"
REFERENCE = ConnectionRef("twilio", "twilio-messaging")


class QueueTarget:
    """Consumes each event into a queue."""

    def __init__(self) -> None:
        self.events: asyncio.Queue[TriggerEvent[MessageReceivedEvent]] = asyncio.Queue()

    async def handle_trigger(
        self, context: TriggerContext, event: TriggerEvent[MessageReceivedEvent]
    ) -> None:
        await self.events.put(event)


@pytest.fixture
async def twilio_http() -> AsyncIterator[tuple[TwilioMessagesFake, Connection]]:
    fake = TwilioMessagesFake(ACCOUNT_SID, ACCOUNT_SID, AUTH_TOKEN)
    async with httpx.AsyncClient(transport=fake.transport()) as http_client:
        client = Client(
            Config(
                account_sid=ACCOUNT_SID, endpoint="http://127.0.0.1:24210/2010-04-01", poll_interval_seconds=5
            ),
            StaticCredentialProvider({REFERENCE: Credentials(auth_token=SecretString(AUTH_TOKEN))}),
            http_client,
        )
        yield fake, new_connection(client, REFERENCE)


async def eventually(condition_met: Callable[[], Awaitable[bool]], timeout: float = 10) -> None:
    async with asyncio.timeout(timeout):
        while not await condition_met():
            await asyncio.sleep(0.01)


def now() -> datetime:
    return datetime.now(UTC)


async def test_new_message_trigger_runner_rejects_incomplete_routes(
    twilio_http: tuple[TwilioMessagesFake, Connection],
) -> None:
    _, connection = twilio_http
    target = QueueTarget()
    blank = MessageReceivedTriggerConfiguration()
    for name, routes in {
        "none": [],
        "no binding": [MessageReceivedTriggerRoute("", blank, target)],
        "no target": [MessageReceivedTriggerRoute("inbound", blank, None)],  # type: ignore[arg-type]
        "duplicate": [
            MessageReceivedTriggerRoute("inbound", blank, target),
            MessageReceivedTriggerRoute("inbound", blank, target),
        ],
        "bad address": [
            MessageReceivedTriggerRoute(
                "inbound", MessageReceivedTriggerConfiguration(to="4155550100"), target
            )
        ],
    }.items():
        with pytest.raises(ValueError):
            new_message_trigger_runner(connection, routes)
        assert name
    with pytest.raises(ValueError, match="mock connection"):
        new_message_trigger_runner(
            Mock(REFERENCE.name).connection(), [MessageReceivedTriggerRoute("inbound", blank, target)]
        )


def write_project(directory: Path) -> Path:
    """Write the connections.json Dex Web saves, with a saved receiving number for support-line."""
    path = directory / "connections.json"
    connection = {
        "connectorId": "twilio-messaging",
        "connectionName": REFERENCE.name,
        "modulePath": "github.com/superdurable/dex-connectors-library/connectors/twilio/messaging/go",
        "provider": "twilio",
        "authMethodId": "auth-token",
        "configuration": {"accountSid": ACCOUNT_SID},
        "credentials": {"auth_token": "inert"},
    }
    binding = {
        "connectorId": "twilio-messaging",
        "connectionName": REFERENCE.name,
        "triggerName": "messageReceived",
        "bindingName": "support-line",
        "configuration": {"to": NUMBER},
    }
    document = {
        "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
        "connections": [connection],
        "triggerBindings": [binding],
    }
    path.write_text(json.dumps(document))
    path.chmod(0o600)
    return path


async def test_project_runner_loads_the_saved_address_and_commits_after_recording(
    twilio_http: tuple[TwilioMessagesFake, Connection], tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    fake, connection = twilio_http
    fake.receive(CUSTOMER, NUMBER, "existing", now() - timedelta(seconds=10))
    monkeypatch.setenv("DEX_CONNECTOR_CONFIG_FILE", str(write_project(tmp_path)))
    project = await load_from_environment()
    target = QueueTarget()
    runner = new_project_message_trigger_runner(
        project, connection, [ProjectMessageReceivedTriggerRoute("support-line", target)]
    )
    key = TriggerInboxKey("twilio-messaging", REFERENCE.name, "messageReceived", "support-line")
    cursor = project.poll_cursor_store(PollCursorKey.from_trigger_inbox_key(key))

    async def committed_state() -> object:
        return (await cursor.read_poll_cursor()).state

    context = TriggerContext()
    running = asyncio.create_task(runner.run(context))
    try:
        await eventually(lambda: _is_not_none(committed_state()))
        state = await committed_state()
        assert f'"to":"{NUMBER}"' in state.token, "the saved receiving address scopes the cursor"  # type: ignore[attr-defined]
        sid = fake.receive(CUSTOMER, NUMBER, "new", now())
        fake.receive(CUSTOMER, OTHER_NUMBER, "elsewhere", now())
        runner.wake_poller("support-line")
        event = await asyncio.wait_for(target.events.get(), 10)
        assert event.id == "twilio-message-" + sid, "the baseline message is never delivered"
        assert event.payload.body == "new"

        async def has_both_keys() -> bool:
            state = (await cursor.read_poll_cursor()).state
            return (
                state is not None
                and state.commit_count >= 2
                and state.snapshot is not None
                and len(state.snapshot.entries) == 2
            )

        await eventually(has_both_keys)
        inbox = project.trigger_inbox(key)
        await eventually(lambda: _pending_is_empty(inbox))
        assert target.events.empty(), "a message to another number is not delivered"
    finally:
        context.cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await running
        await project.close()


async def _is_not_none(value: Awaitable[object]) -> bool:
    return await value is not None


async def _pending_is_empty(inbox: object) -> bool:
    return len(await inbox.pending()) == 0  # type: ignore[attr-defined]


async def test_single_binding_trigger_baselines_and_delivers_the_next_message(
    twilio_http: tuple[TwilioMessagesFake, Connection],
) -> None:
    fake, connection = twilio_http
    fake.receive(CUSTOMER, NUMBER, "existing", now() - timedelta(seconds=10))
    target = QueueTarget()
    trigger = new_message_received_trigger(
        connection=connection,
        binding_name="every-inbound",
        configuration=MessageReceivedTriggerConfiguration(),
        target=target,
    )
    context = TriggerContext()
    running = asyncio.create_task(trigger.run(context))
    try:

        async def has_listed() -> bool:
            return len(fake.list_requests) > 0

        await eventually(has_listed)
        sid = fake.receive(CUSTOMER, OTHER_NUMBER, "hi", now())
        # The next poll comes one jittered 5s interval later.
        event = await asyncio.wait_for(target.events.get(), 20)
        assert event.id == "twilio-message-" + sid
        assert event.payload.message.to == OTHER_NUMBER, "a blank address receives every number"
    finally:
        context.cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await running
