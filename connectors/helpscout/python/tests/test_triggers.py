# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""conversationEvent verification, decoding, filtering, Go parity, and durable project wiring."""

import asyncio
import base64
import hashlib
import hmac
import json
from collections.abc import AsyncIterator, Callable, Mapping
from contextlib import asynccontextmanager
from datetime import datetime, timedelta
from pathlib import Path

import pytest
from dex_connectors.sdk import (
    ConnectionRef,
    CredentialRefreshResult,
    CredentialRefreshState,
    SecretString,
    StaticCredentialProvider,
    TriggerCancellation,
    TriggerContext,
    TriggerDeliveryAbortedError,
    TriggerEvent,
    TriggerLogLevel,
    TriggerLogValue,
    TriggerRunner,
    TriggerTarget,
)
from dex_connectors.sdk.projectconfig import (
    Configuration,
    InMemoryObjectStore,
    Scope,
    TriggerInbox,
    TriggerInboxKey,
    encode_go_json_value,
    new_durable_trigger_target,
)

from dex_connectors.helpscout import (
    CONVERSATION_EVENT_TRIGGER_DEFINITION,
    WEBHOOK_EVENT_CONVERSATION_CREATED,
    WEBHOOK_EVENT_CUSTOMER_REPLY_CREATED,
    WEBHOOK_EVENT_NOTE_CREATED,
    Client,
    Config,
    ConversationEvent,
    ConversationEventTriggerConfiguration,
    Credentials,
    ProjectConversationEventTriggerRoute,
    conversation_event_webhook_endpoint,
    conversation_webhook_events,
    decode_conversation_event_trigger_configuration,
    define_conversation_event_trigger_binding,
    new_connection,
    new_conversation_event_trigger,
    validate_conversation_event_trigger_configuration,
)
from dex_connectors.helpscout.connection import Connection
from dex_connectors.helpscout.helpscoutmock import Mock
from dex_connectors.helpscout.triggers import (
    _decode_conversation_event_payload,
    _ExpiredRecordRefreshDriver,
    _new_conversation_event_endpoint_runner,
)

FIXTURE = json.loads((Path(__file__).parent / "fixtures" / "go-conversation-webhook-cases.json").read_text())
SECRET = FIXTURE["secret"]
RECEIVED_AT = datetime.fromisoformat(FIXTURE["receivedAt"].replace("Z", "+00:00"))
REFERENCE = ConnectionRef("helpscout", "support")
SCOPE = Scope("helpscout-tests", "live")
ConversationTriggerEvent = TriggerEvent[ConversationEvent]


def help_scout_signature(body: bytes, secret: str = SECRET) -> str:
    """Sign as Help Scout documents: base64 of the HMAC-SHA1 of the raw body."""
    return base64.b64encode(hmac.new(secret.encode(), body, hashlib.sha1).digest()).decode()


def conversation_body(conversation_id: int, mailbox_id: int, status: str = "active") -> bytes:
    """A minimal Help Scout conversation webhook body."""
    return json.dumps({"id": conversation_id, "status": status, "mailboxId": mailbox_id}).encode()


def help_scout_connection(
    webhook_secret: str = SECRET, config: Config | None = None, reference: ConnectionRef = REFERENCE
) -> Connection:
    """A connection with in-memory credentials and the fixture's receipt clock."""
    credentials = Credentials(
        "app-id", SecretString("app-secret"), SecretString("token"), SecretString(webhook_secret)
    )
    client = Client(
        config or Config(), StaticCredentialProvider({reference: credentials}), now=lambda: RECEIVED_AT
    )
    return new_connection(client, reference)


class RecordingTarget:
    """Collects delivered events."""

    def __init__(self) -> None:
        self.events: list[ConversationTriggerEvent] = []

    async def handle_trigger(self, context: TriggerContext, event: ConversationTriggerEvent) -> None:
        self.events.append(event)


async def eventually(predicate: Callable[[], bool]) -> None:
    """Poll a converging condition for up to five seconds."""
    for _ in range(500):
        if predicate():
            return
        await asyncio.sleep(0.01)
    raise AssertionError("condition did not converge")


@asynccontextmanager
async def running(*runners: TriggerRunner) -> AsyncIterator[None]:
    """Run Trigger runners until the block exits."""
    cancellation = TriggerCancellation()
    tasks = [asyncio.create_task(runner.run(TriggerContext(cancellation))) for runner in runners]
    try:
        yield
    finally:
        cancellation.cancel()
        for task in tasks:
            with pytest.raises(TriggerDeliveryAbortedError):
                await task


async def deliver(
    connection: Connection, event: str | None, body: bytes, signature: str | None = None
) -> int:
    """Post one delivery to the connection's endpoint and return its status."""
    headers = [("Content-Type", "application/json")]
    if event is not None:
        headers.append(("X-HelpScout-Event", event))
    headers.append(
        ("X-HelpScout-Signature", signature if signature is not None else help_scout_signature(body))
    )
    response = await conversation_event_webhook_endpoint(connection).handle_request(
        "POST", "/webhooks/helpscout", headers, body
    )
    assert SECRET.encode() not in response.body
    return response.status


async def test_every_go_recorded_delivery_has_the_same_status_id_time_and_stored_payload() -> None:
    """Replays the deliveries recorded from the Go endpoint and compares the inbox bytes Go stores."""
    connection = help_scout_connection()
    target = RecordingTarget()
    trigger = new_conversation_event_trigger(connection=connection, binding_name="all", target=target)
    async with running(trigger):
        await eventually(lambda: conversation_event_webhook_endpoint(connection).running_source_count() == 1)
        for case in FIXTURE["cases"]:
            delivered = len(target.events)
            body = base64.b64decode(case["bodyBase64"]) if "bodyBase64" in case else case["body"].encode()
            status = await deliver(connection, case.get("event"), body, case["signature"])
            assert status == case["status"], case["name"]
            if "eventId" not in case:
                await asyncio.sleep(0)
                assert len(target.events) == delivered, case["name"]
                continue
            await eventually(lambda count=delivered: len(target.events) > count)  # type: ignore[misc]
            event = target.events[-1]
            assert event.id == case["eventId"], case["name"]
            assert event.occurred_at.isoformat().replace("+00:00", "Z") == case["occurredAt"]
            # The SDK stores object keys sorted, as Go encodes maps; Go decodes either order.
            assert json.loads(encode_go_json_value(event.payload)) == case["payload"], case["name"]
            go_stored = json.dumps(case["payload"], separators=(",", ":"), ensure_ascii=False)
            assert _decode_conversation_event_payload(go_stored, go_stored.encode()) == event.payload


async def test_the_documented_signature_verifies_and_a_customer_event_is_acknowledged() -> None:
    body = (
        b'{"ticket":{"id":"1","number":"2"},"customer":{"id":"1","fname":"Jackie","lname":"Chan",'
        b'"email":"jackie.chan@somewhere.com","emails":["jackie.chan@somewhere.com"]}}'
    )
    assert help_scout_signature(body, "your secret key") == "I1KlvGppYqvFTJgJ9jezdQMDiyI="
    connection = help_scout_connection("your secret key")
    target = RecordingTarget()
    async with running(
        new_conversation_event_trigger(connection=connection, binding_name="b", target=target)
    ):
        await eventually(lambda: conversation_event_webhook_endpoint(connection).running_source_count() == 1)
        assert await deliver(connection, "customer.created", body, "I1KlvGppYqvFTJgJ9jezdQMDiyI=") == 200
    assert target.events == []


async def test_a_missing_secret_answers_503_a_large_body_413_and_no_running_binding_503() -> None:
    body = conversation_body(501, 123)
    unsigned = help_scout_connection("")
    async with running(
        new_conversation_event_trigger(connection=unsigned, binding_name="b", target=RecordingTarget())
    ):
        await eventually(lambda: conversation_event_webhook_endpoint(unsigned).running_source_count() == 1)
        assert await deliver(unsigned, WEBHOOK_EVENT_CONVERSATION_CREATED, body) == 503
    small = help_scout_connection(config=Config(webhook_max_body_bytes=16))
    async with running(
        new_conversation_event_trigger(connection=small, binding_name="b", target=RecordingTarget())
    ):
        await eventually(lambda: conversation_event_webhook_endpoint(small).running_source_count() == 1)
        assert await deliver(small, WEBHOOK_EVENT_CONVERSATION_CREATED, body) == 413
    idle = help_scout_connection()
    assert await deliver(idle, WEBHOOK_EVENT_CONVERSATION_CREATED, body) == 503


async def test_a_binding_keeps_only_its_events_and_inbox() -> None:
    connection = help_scout_connection()
    target = RecordingTarget()
    configuration = ConversationEventTriggerConfiguration(
        events=(WEBHOOK_EVENT_CONVERSATION_CREATED, WEBHOOK_EVENT_CUSTOMER_REPLY_CREATED), mailbox_id=123
    )
    trigger = new_conversation_event_trigger(
        connection=connection, binding_name="b", target=target, configuration=configuration
    )
    async with running(trigger):
        await eventually(lambda: conversation_event_webhook_endpoint(connection).running_source_count() == 1)
        assert await deliver(connection, WEBHOOK_EVENT_NOTE_CREATED, conversation_body(501, 123)) == 200
        assert (
            await deliver(connection, WEBHOOK_EVENT_CONVERSATION_CREATED, conversation_body(502, 999)) == 200
        )
        assert (
            await deliver(connection, WEBHOOK_EVENT_CUSTOMER_REPLY_CREATED, conversation_body(503, 123))
            == 200
        )
        await eventually(lambda: len(target.events) == 1)
    assert target.events[0].payload.conversation.id == 503


def test_configuration_validation_and_strict_decoding_match_go() -> None:
    for invalid in (
        ConversationEventTriggerConfiguration(events=("customer.created",)),
        ConversationEventTriggerConfiguration(events=("convo.created", "convo.created")),
        ConversationEventTriggerConfiguration(mailbox_id=-1),
    ):
        with pytest.raises(ValueError):
            validate_conversation_event_trigger_configuration(invalid)
    validate_conversation_event_trigger_configuration(ConversationEventTriggerConfiguration())
    validate_conversation_event_trigger_configuration(
        ConversationEventTriggerConfiguration(events=conversation_webhook_events(), mailbox_id=123)
    )
    assert len(conversation_webhook_events()) == 10
    assert decode_conversation_event_trigger_configuration({"events": None, "mailboxId": None}) == (
        ConversationEventTriggerConfiguration()
    )
    for invalid_value in ({"mailboxId": "123"}, {"inbox": 1}, {"events": "convo.created"}):
        with pytest.raises(ValueError):
            decode_conversation_event_trigger_configuration(invalid_value)
    with pytest.raises(ValueError):
        new_conversation_event_trigger(
            connection=help_scout_connection(),
            binding_name="b",
            target=RecordingTarget(),
            configuration=ConversationEventTriggerConfiguration(mailbox_id=-1),
        )


def test_the_binding_factory_names_the_trigger_and_a_mock_connection_has_no_endpoint() -> None:
    binding = define_conversation_event_trigger_binding(connection_name="support", binding_name="new")
    assert binding.definition == CONVERSATION_EVENT_TRIGGER_DEFINITION
    assert CONVERSATION_EVENT_TRIGGER_DEFINITION.trigger.trigger_name == "conversationEvent"
    connection = help_scout_connection()
    endpoint = conversation_event_webhook_endpoint(connection)
    assert conversation_event_webhook_endpoint(connection) is endpoint, "one shared endpoint per connection"
    with pytest.raises(ValueError, match="Help Scout Client"):
        conversation_event_webhook_endpoint(Mock("support").connection())


def test_webhook_verification_renews_only_an_expired_record() -> None:
    class TokenDriver:
        def refresh_required(self, state: CredentialRefreshState[Credentials]) -> bool:
            return True

        async def refresh(
            self, state: CredentialRefreshState[Credentials]
        ) -> CredentialRefreshResult[Credentials]:
            raise AssertionError("not called")

    driver = _ExpiredRecordRefreshDriver(TokenDriver())
    credentials = Credentials("a", SecretString("b"), SecretString("c"), SecretString("d"))
    now = RECEIVED_AT
    assert not driver.refresh_required(CredentialRefreshState(credentials, None, now))
    assert not driver.refresh_required(CredentialRefreshState(credentials, now + timedelta(minutes=1), now))
    assert driver.refresh_required(CredentialRefreshState(credentials, now, now))


def project_configuration(**bindings: dict[str, object]) -> Configuration:
    """A project configuration that stores the named conversationEvent bindings."""
    entries = tuple(
        {
            "connectorId": "helpscout",
            "connectionName": REFERENCE.name,
            "triggerName": "conversationEvent",
            "bindingName": name,
            "configuration": configuration,
        }
        for name, configuration in bindings.items()
    )
    return Configuration(SCOPE, 1, (), entries)


class FailingTarget:
    """Fails every delivery, as an unreachable Dex Server would."""

    async def handle_trigger(self, context: TriggerContext, event: ConversationTriggerEvent) -> None:
        raise ConnectionError("Dex is unreachable")


async def test_the_project_runner_stores_events_before_acknowledging_and_replays_them_after_restart() -> None:
    objects = InMemoryObjectStore()
    configuration = project_configuration(new={"events": ["convo.created"]})

    def durable(
        key: TriggerInboxKey, target: TriggerTarget[ConversationEvent]
    ) -> TriggerTarget[ConversationEvent]:
        return new_durable_trigger_target(
            TriggerInbox(objects, SCOPE, key), target, decode_payload=_decode_conversation_event_payload
        )

    first_connection = help_scout_connection()
    first = _new_conversation_event_endpoint_runner(
        first_connection,
        configuration,
        [ProjectConversationEventTriggerRoute("new", FailingTarget())],
        durable,
    )
    body = conversation_body(501, 123)
    async with running(first):  # type: ignore[arg-type]
        await eventually(lambda: first.running_source_count() == 1)
        response = await first.handle_request(
            "POST",
            "/webhooks/helpscout",
            [("X-HelpScout-Event", "convo.created"), ("X-HelpScout-Signature", help_scout_signature(body))],
            body,
        )
        assert response.status == 200
        await asyncio.sleep(0.05)
    key = TriggerInboxKey("helpscout", REFERENCE.name, "conversationEvent", "new")
    pending = await TriggerInbox(objects, SCOPE, key).pending()
    assert [event.id for event in pending] == [f"convo.created:501:{hashlib.sha256(body).hexdigest()[:32]}"]

    target = RecordingTarget()
    second_connection = help_scout_connection()
    second = _new_conversation_event_endpoint_runner(
        second_connection,
        configuration,
        [ProjectConversationEventTriggerRoute("new", target)],
        durable,
    )
    async with running(second):  # type: ignore[arg-type]
        await eventually(lambda: len(target.events) == 1)
    assert target.events[0].payload.conversation.id == 501
    assert target.events[0].occurred_at == RECEIVED_AT
    assert await TriggerInbox(objects, SCOPE, key).pending() == ()


class RecordingLogger:
    """Collects Trigger delivery records at every level."""

    def __init__(self) -> None:
        self.records: list[tuple[str, dict[str, object]]] = []

    def is_enabled_for(self, level: TriggerLogLevel) -> bool:
        return True

    def log(self, level: TriggerLogLevel, message: str, attributes: Mapping[str, TriggerLogValue]) -> None:
        self.records.append((message, dict(attributes)))


async def test_the_endpoint_sends_its_delivery_records_to_the_client_logger() -> None:
    logger = RecordingLogger()
    credentials = Credentials(
        "app-id", SecretString("app-secret"), SecretString("token"), SecretString(SECRET)
    )
    client = Client(
        Config(), StaticCredentialProvider({REFERENCE: credentials}), now=lambda: RECEIVED_AT, logger=logger
    )
    connection = new_connection(client, REFERENCE)
    runner = new_conversation_event_trigger(connection=connection, binding_name="new", target=FailingTarget())
    body = conversation_body(502, 123)
    async with running(runner):
        await eventually(lambda: conversation_event_webhook_endpoint(connection).running_source_count() == 1)
        assert await deliver(connection, "convo.created", body) == 200
        await eventually(lambda: bool(logger.records))
    rendered = repr(logger.records)
    assert SECRET not in rendered and "app-secret" not in rendered and body.decode() not in rendered


def test_the_project_runner_requires_each_route_binding_in_the_project_configuration() -> None:
    def durable(key: TriggerInboxKey, target: object) -> object:
        return target

    connection = help_scout_connection()
    configuration = project_configuration(new={}, bad={"mailboxId": -4}, odd={"inbox": 1})
    for routes, message in (
        ([], "routes are required"),
        ([ProjectConversationEventTriggerRoute(" ", RecordingTarget())], "binding name and target"),
        (
            [ProjectConversationEventTriggerRoute("missing", RecordingTarget())],
            'binding "missing" configuration',
        ),
        (
            [ProjectConversationEventTriggerRoute("bad", RecordingTarget())],
            'binding "bad": conversationEvent',
        ),
        ([ProjectConversationEventTriggerRoute("odd", RecordingTarget())], 'binding "odd" configuration'),
        (
            [
                ProjectConversationEventTriggerRoute("new", RecordingTarget()),
                ProjectConversationEventTriggerRoute(" new ", RecordingTarget()),
            ],
            "is duplicated",
        ),
    ):
        with pytest.raises(ValueError, match=message):
            _new_conversation_event_endpoint_runner(connection, configuration, routes, durable)  # type: ignore[arg-type]
