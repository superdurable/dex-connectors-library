# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""conversationEvent endpoint and binding behavior, ported from the Go conversation_event tests."""

import asyncio
from collections.abc import AsyncIterator
from contextlib import asynccontextmanager
from datetime import UTC, datetime

import httpx
import pytest
from dex_connectors.sdk import TriggerCancellation, TriggerContext, TriggerDeliveryAbortedError

from dex_connectors.intercom import (
    TOPIC_CONVERSATION_ADMIN_CLOSED,
    TOPIC_CONVERSATION_USER_CREATED,
    TOPIC_CONVERSATION_USER_REPLIED,
    Client,
    Config,
    ConversationEventTriggerConfiguration,
    conversation_event_topics,
    decode_conversation_event_trigger_configuration,
    new_connection,
    new_conversation_event_trigger,
    new_conversation_event_webhook_handler,
    validate_conversation_event_trigger_configuration,
)
from dex_connectors.intercom.intercommock import Mock
from tests.conversation_event_support import (
    CONNECTION_NAME,
    NOTIFIED_AT,
    REFERENCE,
    QueueTarget,
    conversation_item,
    new_client,
    notification_body,
    sign,
    signed_headers,
)

WEBHOOK_URL = "https://webhook.test/webhooks/intercom"


@asynccontextmanager
async def running_binding(
    client: Client, configuration: ConversationEventTriggerConfiguration
) -> AsyncIterator[tuple[httpx.AsyncClient, QueueTarget]]:
    """Run one binding and yield an HTTP client for its connection's webhook handler."""
    connection = new_connection(client, REFERENCE)
    target = QueueTarget()
    runner = new_conversation_event_trigger(
        connection=connection,
        connection_name=CONNECTION_NAME,
        binding_name="inbound",
        configuration=configuration,
        target=target,
    )
    handler = new_conversation_event_webhook_handler(connection)
    cancellation = TriggerCancellation()
    task = asyncio.create_task(runner.run(TriggerContext(cancellation)))
    endpoint = client.conversation_event_webhook_endpoint(REFERENCE)
    async with asyncio.timeout(10):
        while endpoint.running_source_count() == 0:
            await asyncio.sleep(0.01)
    try:
        async with httpx.AsyncClient(transport=httpx.ASGITransport(app=handler)) as http:
            yield http, target
    finally:
        cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await task


async def post(http: httpx.AsyncClient, body: bytes, headers: list[tuple[str, str]] | None = None) -> int:
    """POST body to the webhook and return the status."""
    response = await http.post(WEBHOOK_URL, content=body, headers=headers or [])
    return response.status_code


async def test_endpoint_verifies_decodes_and_filters_notifications() -> None:
    """A signed new conversation is delivered; refusals answer 400 and other topics 200 unrecorded."""
    configuration = ConversationEventTriggerConfiguration(
        topics=(TOPIC_CONVERSATION_USER_CREATED, TOPIC_CONVERSATION_USER_REPLIED)
    )
    async with running_binding(new_client(), configuration) as (http, target):
        notification_id = "notif_ccd8a4d0-f965-11e3-a367-c779cae3e1b3"
        created = notification_body(notification_id, TOPIC_CONVERSATION_USER_CREATED, conversation_item())
        assert (await http.head(WEBHOOK_URL)).status_code == 200, "Intercom validates the URL with HEAD"
        assert (await http.get(WEBHOOK_URL)).status_code == 405
        upper_hex = "sha1=" + sign(created).removeprefix("sha1=").upper()
        assert await post(http, created, [("X-Hub-Signature", upper_hex)]) == 200
        event = await target.next_event()
        assert event.id == notification_id
        assert event.occurred_at == datetime.fromtimestamp(NOTIFIED_AT, UTC)
        assert event.payload.notification_id == notification_id
        assert event.payload.topic == TOPIC_CONVERSATION_USER_CREATED
        assert event.payload.workspace_id == "ecahpwf5"
        assert event.payload.notified_at == event.occurred_at
        assert event.payload.conversation.id == "215468"
        assert event.payload.conversation.source.author.email == "jane@acme.example.com"
        assert event.payload.conversation.source.body == "", "the event carries no message text"

        tampered = created.replace(b"jane@", b"eve@")
        malformed = b'{"type":'
        not_notification = b'{"type":"conversation","id":"1"}'
        refused = {
            "wrong secret": (created, sign(created, "another-secret")),
            "missing signature": (created, None),
            "sha256 signature": (created, "sha256=" + "a" * 64),
            "short signature": (created, sign(created)[:-2]),
            "tampered body": (tampered, sign(created)),
            "malformed JSON": (malformed, sign(malformed)),
            "not a notification": (not_notification, sign(not_notification)),
        }
        for name, (body, signature) in refused.items():
            headers = [("X-Hub-Signature", signature)] if signature else []
            assert await post(http, body, headers) == 400, name
        ignored = {
            "ping": notification_body("notif_ping", "ping", {"type": "ping", "message": "something"}),
            "contact topic": notification_body("notif_contact", "contact.user.created", {"type": "contact"}),
            "topic the binding skips": notification_body(
                "notif_closed", TOPIC_CONVERSATION_ADMIN_CLOSED, conversation_item(state="closed")
            ),
        }
        for name, body in ignored.items():
            assert await post(http, body, signed_headers(body)) == 200, name
        assert target.events.empty()


async def test_signature_reads_only_the_first_header_value_trimmed() -> None:
    """Go's Header.Get reads the first X-Hub-Signature value and TrimSpace removes surrounding spaces."""
    async with running_binding(new_client(), ConversationEventTriggerConfiguration()) as (http, target):
        first = notification_body("notif_first", TOPIC_CONVERSATION_USER_CREATED, conversation_item("1"))
        headers = [("X-Hub-Signature", " " + sign(first) + "\t"), ("X-Hub-Signature", "sha1=" + "0" * 40)]
        assert await post(http, first, headers) == 200
        assert (await target.next_event()).id == "notif_first"
        second = notification_body("notif_second", TOPIC_CONVERSATION_USER_CREATED, conversation_item("2"))
        headers = [("X-Hub-Signature", "sha1=" + "0" * 40), ("X-Hub-Signature", sign(second))]
        assert await post(http, second, headers) == 400
        joined = [("X-Hub-Signature", sign(second) + ", sha1=" + "0" * 40)]
        assert await post(http, second, joined) == 400, "one comma-joined value is not a SHA-1 signature"
        assert target.events.empty()


async def test_endpoint_asks_intercom_to_retry_until_it_can_verify_and_record() -> None:
    """A blank client secret or no running binding answers 503, so Intercom redelivers."""
    created = notification_body("notif_retry", TOPIC_CONVERSATION_USER_CREATED, conversation_item())
    async with running_binding(new_client(client_secret=""), ConversationEventTriggerConfiguration()) as (
        http,
        target,
    ):
        assert await post(http, created, signed_headers(created)) == 503
        assert target.events.empty()

    client = new_client()
    handler = new_conversation_event_webhook_handler(new_connection(client, REFERENCE))
    async with httpx.AsyncClient(transport=httpx.ASGITransport(app=handler)) as http:
        assert await post(http, created, signed_headers(created)) == 503, "no binding runs yet"
        assert (await http.head(WEBHOOK_URL)).status_code == 200


async def test_endpoint_rejects_an_oversized_body_and_zero_takes_the_default_limit() -> None:
    """A body over webhookMaxBodyBytes answers 413 before verification; zero selects the default."""
    async with running_binding(
        new_client(config=Config(region="eu", webhook_max_body_bytes=64)),
        ConversationEventTriggerConfiguration(),
    ) as (http, target):
        body = notification_body("notif_big", TOPIC_CONVERSATION_USER_CREATED, conversation_item())
        assert await post(http, body, signed_headers(body)) == 413
        assert target.events.empty()
    assert new_client(config=Config(webhook_max_body_bytes=0)).webhook_max_body_bytes == 2097152
    with pytest.raises(ValueError, match="must be positive"):
        new_client(config=Config(webhook_max_body_bytes=-1))


def test_configuration_accepts_only_supported_topics_once() -> None:
    """Validation names an unsupported or repeated topic; decoding applies Go's strict JSON rules."""
    validate_conversation_event_trigger_configuration(ConversationEventTriggerConfiguration())
    validate_conversation_event_trigger_configuration(
        ConversationEventTriggerConfiguration(topics=tuple(conversation_event_topics()))
    )
    with pytest.raises(ValueError, match='"ping" is not a supported conversation topic'):
        validate_conversation_event_trigger_configuration(
            ConversationEventTriggerConfiguration(topics=("ping",))
        )
    with pytest.raises(ValueError, match="is listed twice"):
        validate_conversation_event_trigger_configuration(
            ConversationEventTriggerConfiguration(
                topics=(TOPIC_CONVERSATION_USER_CREATED, TOPIC_CONVERSATION_USER_CREATED)
            )
        )
    assert decode_conversation_event_trigger_configuration({"topics": None}).topics == ()
    assert decode_conversation_event_trigger_configuration({}).topics == ()
    for invalid in ({"topics": "conversation.user.created"}, {"topic": []}, {"topics": [1]}):
        with pytest.raises(ValueError, match="conversationEvent"):
            decode_conversation_event_trigger_configuration(invalid)


def test_topics_are_listed_in_the_go_connectors_order() -> None:
    """The topic picker lists the fourteen supported topics in Go's order, as a new list each call."""
    topics = conversation_event_topics()
    assert topics[:3] == [
        TOPIC_CONVERSATION_USER_CREATED,
        TOPIC_CONVERSATION_USER_REPLIED,
        "conversation.admin.replied",
    ]
    assert topics[-1] == "conversation.rating.added" and len(topics) == 14
    topics.clear()
    assert len(conversation_event_topics()) == 14


def test_trigger_rejects_a_mismatched_connection_or_a_mock_connection() -> None:
    """The binding's connection name must be its connection's, and the connection needs a Client."""
    connection = new_connection(new_client(), REFERENCE)
    with pytest.raises(ValueError, match="must equal its connection's name"):
        new_conversation_event_trigger(
            connection=connection,
            connection_name="another",
            binding_name="inbound",
            configuration=ConversationEventTriggerConfiguration(),
            target=QueueTarget(),
        )
    with pytest.raises(ValueError, match="not a supported conversation topic"):
        new_conversation_event_trigger(
            connection=connection,
            connection_name=CONNECTION_NAME,
            binding_name="inbound",
            configuration=ConversationEventTriggerConfiguration(topics=("contact.user.created",)),
            target=QueueTarget(),
        )
    mock = Mock(CONNECTION_NAME).connection()
    with pytest.raises(ValueError, match="bound to an Intercom Client"):
        new_conversation_event_webhook_handler(mock)
