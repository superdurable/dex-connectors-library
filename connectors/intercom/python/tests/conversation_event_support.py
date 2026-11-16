# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Signed Intercom notifications, an in-memory project, and a queueing Trigger target for tests."""

import asyncio
import hashlib
import hmac
import json
from typing import Any

from dex_connectors.sdk import (
    ConnectionRef,
    SecretString,
    StaticCredentialProvider,
    TriggerContext,
    TriggerEvent,
)
from dex_connectors.sdk.projectconfig import (
    Configuration,
    ConnectionConfiguration,
    ConnectionKey,
    InMemoryObjectStore,
    Scope,
    TriggerInbox,
    TriggerInboxKey,
)

from dex_connectors.intercom import Client, Config, ConversationEvent, Credentials

CONNECTION_NAME = "intercom-support-inbox"
REFERENCE = ConnectionRef("intercom", CONNECTION_NAME)
ACCESS_TOKEN = "intercom-test-" + "access-token"  # Split so secret scanners do not flag a fixture.
CLIENT_SECRET = "intercom-test-" + "client-secret"
SCOPE = Scope("intercom-conversation-event-test", "preview")
NOTIFIED_AT = 1767229300


def conversation_item(conversation_id: str = "215468", state: str = "open") -> dict[str, Any]:
    """Return a conversation as a notification carries it, with a body the event must drop."""
    return {
        "type": "conversation",
        "id": conversation_id,
        "title": "Refund question",
        "state": state,
        "open": state == "open",
        "created_at": NOTIFIED_AT - 100,
        "updated_at": NOTIFIED_AT - 10,
        "source": {
            "type": "conversation",
            "body": "<p>I need help with my refund</p>",
            "author": {"type": "user", "id": "5310d8e7", "name": "Jane", "email": "jane@acme.example.com"},
        },
        "contacts": {"type": "contact.list", "contacts": [{"type": "contact", "id": "5310d8e7"}]},
    }


def notification_body(notification_id: str, topic: str, item: object) -> bytes:
    """Encode an Intercom webhook notification envelope."""
    return json.dumps(
        {
            "type": "notification_event",
            "app_id": "ecahpwf5",
            "id": notification_id,
            "topic": topic,
            "created_at": NOTIFIED_AT,
            "data": {"type": "notification_event_data", "item": item},
        }
    ).encode()


def sign(body: bytes, secret: str = CLIENT_SECRET) -> str:
    """Return the X-Hub-Signature value Intercom sends for body."""
    return "sha1=" + hmac.new(secret.encode(), body, hashlib.sha1).hexdigest()


def signed_headers(body: bytes, secret: str = CLIENT_SECRET) -> list[tuple[str, str]]:
    """Return request headers with a valid signature."""
    return [("Content-Type", "application/json"), ("X-Hub-Signature", sign(body, secret))]


def new_client(client_secret: str = CLIENT_SECRET, config: Config | None = None) -> Client:
    """Return a client whose credentials carry client_secret."""
    credentials = Credentials(SecretString(ACCESS_TOKEN), SecretString(client_secret))
    return Client(config or Config(region="eu"), StaticCredentialProvider({REFERENCE: credentials}))


class QueueTarget:
    """Consumes each event into a queue and records each prepared event."""

    def __init__(self) -> None:
        self.events: asyncio.Queue[TriggerEvent[ConversationEvent]] = asyncio.Queue()

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[ConversationEvent]) -> None:
        """Record the event."""
        await self.events.put(event)

    async def next_event(self) -> TriggerEvent[ConversationEvent]:
        """Wait up to ten seconds for the next event."""
        return await asyncio.wait_for(self.events.get(), 10)


def trigger_binding(binding_name: str, configuration: object) -> dict[str, Any]:
    """Return one stored conversationEvent binding."""
    return {
        "connectorId": "intercom",
        "connectionName": CONNECTION_NAME,
        "triggerName": "conversationEvent",
        "bindingName": binding_name,
        "configuration": configuration,
    }


class ProjectStub:
    """The parts of LoadedProject the endpoint runner reads, over one in-memory object store."""

    def __init__(self, *trigger_bindings: dict[str, Any], client_secret: str = CLIENT_SECRET) -> None:
        self.objects = InMemoryObjectStore()
        connection = ConnectionConfiguration(
            "intercom",
            CONNECTION_NAME,
            "github.com/superdurable/dex-connectors-library/connectors/intercom/go",
            "intercom",
            {"region": "eu"},
        )
        self.configuration = Configuration(SCOPE, 1, (connection,), tuple(trigger_bindings))
        self.client_secret = client_secret

    def trigger_inbox(self, key: TriggerInboxKey) -> TriggerInbox:
        return TriggerInbox(self.objects, SCOPE, key)

    async def resolve_credential(self, key: ConnectionKey, call_id: str) -> dict[str, str]:
        return {"access_token": ACCESS_TOKEN, "client_secret": self.client_secret}
