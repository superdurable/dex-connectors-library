# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Local stand-ins for Slack Socket Mode, the Slack Web API, and a Dex host relay and credential broker."""

from __future__ import annotations

import asyncio
import json
from collections.abc import Mapping
from datetime import UTC, datetime, timedelta
from typing import Any

import httpx
from dex_connectors.sdk import TriggerLogLevel, TriggerLogValue
from dex_connectors.sdk.projectconfig import (
    ConfigurationStore,
    ConnectionKey,
    ConnectionStore,
    CredentialMaterial,
    InMemoryObjectStore,
    LoadedProject,
    Scope,
    SnapshotRef,
    TriggerInboxKey,
)
from dex_connectors.sdk.projectconfig.broker import CredentialBrokerClient, SharedConnection
from dex_connectors.sdk.projectconfig.models import (
    ApplicationSecretRef,
    Configuration,
    ConnectionConfiguration,
    CredentialBrokerConfiguration,
)
from dex_connectors.sdk.relaysignature import (
    RELAY_SIGNATURE_HEADER,
    RELAY_TIMESTAMP_HEADER,
    sign_relayed_request,
)

# Split so push protection does not mistake test sentinels for Slack tokens.
BOT_TOKEN = "xoxb" + "-SENTINEL-BOT"
USER_TOKEN = "xoxp" + "-SENTINEL-USER"
APP_TOKEN = "xapp" + "-SENTINEL-APP"
SOCKET_URL = "wss://wss-primary.slack.com/link/?ticket=SENTINEL-TICKET"
RELAY_SCOPE_TOKEN = "relay-scope-token"
SNAPSHOT = SnapshotRef("snapshot", "1", "sha256:" + "0" * 64)


class FakeSocket:
    """A Socket Mode connection fed by the test; it records acknowledged envelope IDs."""

    def __init__(self) -> None:
        self._messages: asyncio.Queue[Any] = asyncio.Queue()
        self.acknowledged: list[str] = []
        self.is_closed = False

    def push(self, message: Any) -> None:
        self._messages.put_nowait(message)

    def fail(self) -> None:
        self._messages.put_nowait(ConnectionError("connection reset"))

    async def read_message(self, read_timeout: timedelta) -> Any:
        message = await self._messages.get()
        if isinstance(message, BaseException):
            raise message
        return message

    async def write_message(self, value: Any) -> None:
        self.acknowledged.append(value["envelope_id"])

    async def close(self) -> None:
        self.is_closed = True


class FakeDialer:
    """Hands out the given sockets in order, then sockets that never deliver."""

    def __init__(self, *sockets: FakeSocket) -> None:
        self._sockets = list(sockets)
        self.dialed: list[str] = []

    async def __call__(self, url: str) -> FakeSocket:
        self.dialed.append(url)
        return self._sockets.pop(0) if self._sockets else FakeSocket()


class RecordingLogger:
    """Collects every Trigger record at every level."""

    def __init__(self) -> None:
        self.records: list[tuple[TriggerLogLevel, str, dict[str, TriggerLogValue]]] = []

    def is_enabled_for(self, level: TriggerLogLevel) -> bool:
        return True

    def log(self, level: TriggerLogLevel, message: str, attributes: Mapping[str, TriggerLogValue]) -> None:
        self.records.append((level, message, dict(attributes)))

    def find(self, message: str, **attributes: TriggerLogValue) -> list[dict[str, TriggerLogValue]]:
        return [
            record
            for _, text, record in self.records
            if text == message and all(record.get(key) == value for key, value in attributes.items())
        ]


class SlackWebAPI:
    """A Slack Web API stand-in recording chat.postMessage payloads."""

    def __init__(self, channel_id: str) -> None:
        self.channel_id = channel_id
        self.posts: list[dict[str, Any]] = []
        self.paths: list[str] = []
        self.next_timestamp = 9

    def answer(self, request: httpx.Request) -> httpx.Response:
        path = request.url.path
        self.paths.append(path)
        if path.endswith("/apps.connections.open"):
            return httpx.Response(200, json={"ok": True, "url": SOCKET_URL})
        if path.endswith("/conversations.replies"):
            return httpx.Response(
                200,
                json={
                    "ok": True,
                    "messages": [{"ts": "1.0", "user": "U1", "text": "please request approval"}],
                },
            )
        if path.endswith("/chat.postMessage"):
            payload = json.loads(request.content)
            self.posts.append(payload)
            timestamp = f"1.{self.next_timestamp}"
            self.next_timestamp += 1
            message = {"ts": timestamp, "thread_ts": payload.get("thread_ts"), "text": payload["text"]}
            return httpx.Response(
                200, json={"ok": True, "channel": self.channel_id, "ts": timestamp, "message": message}
            )
        return httpx.Response(404, json={"ok": False, "error": "unknown_method"})


def events_payload(event_id: str, event: dict[str, Any], team_id: str = "T1") -> dict[str, Any]:
    return {"event_id": event_id, "event_time": 1_760_000_000, "team_id": team_id, "event": event}


def events_envelope(envelope_id: str, event_id: str, event: dict[str, Any]) -> dict[str, Any]:
    return {"envelope_id": envelope_id, "type": "events_api", "payload": events_payload(event_id, event)}


def root_message(
    ts: str, channel: str, user: str = "U1", text: str = "please request approval"
) -> dict[str, Any]:
    return {"type": "message", "channel": channel, "user": user, "text": text, "ts": ts}


def reply_message(
    ts: str, thread_ts: str, channel: str, user: str = "U2", text: str = "approved"
) -> dict[str, Any]:
    return {**root_message(ts, channel, user, text), "thread_ts": thread_ts}


def click_envelope(
    envelope_id: str,
    channel: str,
    message_ts: str,
    action_id: str,
    value: str,
    user: str = "U2",
    action_ts: str = "1760000001.000100",
    thread_ts: str = "1.0",
) -> dict[str, Any]:
    return {
        "envelope_id": envelope_id,
        "type": "interactive",
        "payload": {
            "type": "block_actions",
            "team": {"id": "T1"},
            "user": {"id": user},
            "channel": {"id": channel},
            "container": {
                "type": "message",
                "message_ts": message_ts,
                "channel_id": channel,
                "thread_ts": thread_ts,
            },
            "message": {"ts": message_ts, "thread_ts": thread_ts},
            "actions": [{"type": "button", "action_id": action_id, "value": value, "action_ts": action_ts}],
        },
    }


def binding(
    trigger_name: str, connection_name: str, binding_name: str, configuration: dict[str, Any]
) -> dict[str, Any]:
    return {
        "connectorId": "slack",
        "connectionName": connection_name,
        "triggerName": trigger_name,
        "bindingName": binding_name,
        "configuration": configuration,
    }


async def socket_mode_project(
    connection_name: str, bindings: list[dict[str, Any]], endpoint: str = "http://127.0.0.1:9/api"
) -> LoadedProject:
    """A project whose private connection credential has an app_token."""
    objects = InMemoryObjectStore()
    scope = Scope("socket", "live")
    connections = ConnectionStore(objects, scope)
    await connections.replace_credential(
        ConnectionKey("slack", connection_name),
        0,
        CredentialMaterial(
            {"bot_token": BOT_TOKEN, "user_token": USER_TOKEN, "app_token": APP_TOKEN}, None, "oauth2"
        ),
    )
    configuration = Configuration(
        scope,
        1,
        (
            ConnectionConfiguration(
                "slack", connection_name, "slack", "slack", {"endpoint": endpoint}, "oauth2"
            ),
        ),
        tuple(bindings),
    )
    return LoadedProject(configuration, SNAPSHOT, connections, ConfigurationStore(objects, scope))


class RelayHost:
    """A Dex host that shares a connection without an app_token, relays its events, and runs a broker."""

    def __init__(
        self, connection_name: str, bindings: list[dict[str, Any]], endpoint: str = "http://127.0.0.1:9/api"
    ) -> None:
        self.connection_name = connection_name
        self.credential: dict[str, str] = {"bot_token": BOT_TOKEN, "user_token": USER_TOKEN}
        self.credential_reads = 0
        self.registrations: list[dict[str, Any]] = []
        self.registration_answers: list[tuple[int, dict[str, Any]]] = []
        self.objects = InMemoryObjectStore()
        self.scope = Scope("relay", "live")
        broker_endpoint = "https://broker.example.test/internal/connection-credentials/read"
        token_reference = ApplicationSecretRef(
            f"projects/relay/live/app-secrets/DEX_CREDENTIAL_BROKER_TOKEN/{'a' * 32}",
            "v1",
            "sha256:" + "b" * 64,
        )
        broker_configuration = CredentialBrokerConfiguration(broker_endpoint, token_reference)

        async def read_token(reference: ApplicationSecretRef) -> str:
            return RELAY_SCOPE_TOKEN

        async def current_reference() -> ApplicationSecretRef:
            return token_reference

        self.broker = CredentialBrokerClient(
            broker_configuration,
            read_token,
            current_reference,
            httpx.AsyncClient(transport=httpx.MockTransport(self._answer)),
        )
        key = ConnectionKey("slack", connection_name)
        shared = {key: SharedConnection("conn-slack", self.broker)}
        configuration = Configuration(
            self.scope,
            1,
            (
                ConnectionConfiguration(
                    "slack", connection_name, "slack", "slack", {"endpoint": endpoint}, "oauth2", "conn-slack"
                ),
            ),
            tuple(bindings),
            credential_broker=broker_configuration,
        )
        self.project = LoadedProject(
            configuration,
            SNAPSHOT,
            ConnectionStore(self.objects, self.scope, shared),
            ConfigurationStore(self.objects, self.scope),
            broker=self.broker,
            shared_connections=shared,
        )

    def _answer(self, request: httpx.Request) -> httpx.Response:
        if request.url.path.endswith("/internal/connection-credentials/read"):
            self.credential_reads += 1
            return httpx.Response(200, json={"status": "READY", "credential": self.credential, "revision": 1})
        self.registrations.append(
            {"authorization": request.headers.get("authorization", ""), "body": json.loads(request.content)}
        )
        if self.registration_answers:
            status, body = self.registration_answers.pop(0)
            return httpx.Response(status, json=body)
        return httpx.Response(200, json={"status": "REGISTERED"})

    async def pending_event_ids(self, *keys: tuple[str, str]) -> list[str]:
        identifiers: list[str] = []
        for trigger_name, binding_name in keys:
            inbox = self.project.trigger_inbox(
                TriggerInboxKey("slack", self.connection_name, trigger_name, binding_name)
            )
            identifiers.extend(event.id for event in await inbox.pending())
        return identifiers


def relayed_request(body: Any, token: str = RELAY_SCOPE_TOKEN) -> tuple[list[tuple[str, str]], bytes]:
    """Return the headers and body of a callback the host signed with token now."""
    raw = body if isinstance(body, bytes) else json.dumps(body).encode()
    signed_at = datetime.now(UTC)
    headers = [
        (RELAY_TIMESTAMP_HEADER, str(int(signed_at.timestamp()))),
        (RELAY_SIGNATURE_HEADER, sign_relayed_request(token, signed_at, raw)),
    ]
    return headers, raw
