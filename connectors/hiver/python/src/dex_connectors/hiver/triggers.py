# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start Flows when a conversation newly appears in one picked Hiver shared inbox.

Hiver documents no webhooks, so the ``conversationReceived`` Trigger polls the inbox's conversation list
at behavior parity with the Go connector, whose cursor documents it reads and writes byte for byte. A
binding's first polls walk the whole list, 10 pages of 100 at a time, and record every conversation ID
without delivering it. Afterwards each poll reads the list's first page plus up to pollPagesPerPoll-1
pages of a sweep that walks the rest of the list across polls, and delivers each untracked ID in list
order before the cursor commits. IDs that two complete sweeps in a row never list stop being tracked.
"""

import json
from collections.abc import Sequence
from dataclasses import dataclass
from typing import Final

from dex import CodecRegistry, Value, WireKind
from dex_connectors.sdk import (
    TriggerBindingRef,
    TriggerConfig,
    TriggerEvent,
    TriggerLogger,
    TriggerRunner,
    TriggerTarget,
    new_trigger,
)
from dex_connectors.sdk.pollingtrigger import (
    BindingPollerConfig,
    CursorStore,
    Poller,
    PollRunner,
    PollRunnerConfig,
    PollSchedule,
    RequestLimiter,
    new_binding_poller,
    new_binding_trigger_source,
    new_memory_cursor_store,
)
from dex_connectors.sdk.projectconfig import (
    LoadedProject,
    ObjectNotFoundError,
    PollCursorKey,
    TriggerInboxKey,
    decode_trigger_configuration,
    new_durable_trigger_target,
)

from ._conversation_received_fetcher import (
    CONVERSATION_RECEIVED_MAX_BACKOFF,
    CONVERSATION_SNAPSHOT_OPTIONS,
    MAX_TRACKED_CONVERSATIONS,
    ConversationReceivedFetcher,
    trim_go_space,
)
from ._generated.definitions import CONNECTOR_ID, CONVERSATION_RECEIVED_TRIGGER_DEFINITION
from ._generated.triggers import (
    decode_conversation_received_trigger_configuration,
    define_conversation_received_trigger_binding,
)
from .client import Client, validate_id
from .connection import Connection
from .models import ConversationReceivedEvent, ConversationReceivedTriggerConfiguration

_TRIGGER_NAME: Final = CONVERSATION_RECEIVED_TRIGGER_DEFINITION.trigger.trigger_name

UI_UNIT_INBOX_PICKER: Final = "inboxPicker"
"""The release-owned Studio unit that lists the account's shared inboxes and stores one inbox ID."""
UI_INBOX_PICKER_PORT_INBOX_ID: Final = "inboxId"
"""The inboxPicker output port holding the chosen Hiver inbox ID; bind it to ``/inboxId``."""


@dataclass(frozen=True, slots=True)
class ConversationReceivedTriggerRoute:
    """Binds one ``conversationReceived`` binding's configuration and cursor to its application target."""

    binding_name: str
    """Names the configured Trigger binding; it is unique within one runner."""
    configuration: ConversationReceivedTriggerConfiguration
    """Selects the polled shared inbox."""
    cursor: CursorStore
    """Persists the tracked conversation IDs and sweep position, normally `LoadedProject.poll_cursor_store`.

    Use `new_memory_cursor_store` only when conversations that arrive while the process is stopped may be
    skipped.
    """
    target: TriggerTarget[ConversationReceivedEvent]
    """Receives each new conversation.

    A target wrapped by `new_durable_trigger_target` records the conversation before the cursor moves past
    it; any other target consumes it first.
    """


@dataclass(frozen=True, slots=True)
class ProjectConversationReceivedTriggerRoute:
    """Selects one ``conversationReceived`` binding saved in the project configuration."""

    binding_name: str
    """Names the binding whose shared inbox Dex Web saved."""
    target: TriggerTarget[ConversationReceivedEvent]
    """Receives each new conversation after the binding's durable project inbox records it."""


def validate_conversation_received_trigger_configuration(
    configuration: ConversationReceivedTriggerConfiguration,
) -> None:
    """Check that the binding names one Hiver shared inbox ID; raises ValueError otherwise."""
    try:
        validate_id("inboxId", trim_go_space(configuration.inbox_id))
    except ValueError as error:
        raise ValueError(f"Hiver conversationReceived {error}") from None


def new_conversation_received_trigger_runner(
    connection: Connection,
    routes: Sequence[ConversationReceivedTriggerRoute],
    *,
    request_limiter: RequestLimiter | None = None,
    logger: TriggerLogger | None = None,
) -> PollRunner:
    """Validate every route and build one poll runner for the connection's ``conversationReceived`` bindings.

    It makes no provider request and reads no storage. `PollRunner.run` replays each durable target,
    starts missing baselines, and then polls every binding each `Client.poll_interval`. Hiver documents no
    order for the list, so a conversation is found on the next poll when Hiver lists it on the first page,
    and otherwise by the end of the current sweep. A rejected sweep or baseline page token (HTTP 400, 404,
    410, or 422) restarts the walk from the first page without a gap. A failed poll backs off up to 30
    minutes, or the poll interval when longer; HTTP 429 pauses every binding of the connection for at least
    its Retry-After. Delivery is at least once and each event ID, ``conversation-<inbox>-<conversation>``,
    is a pure function of the conversation. An inbox listing more than `MAX_TRACKED_CONVERSATIONS` pauses
    its binding. request_limiter optionally gates every poll request beside the connection's request
    spacing. logger receives poll records without payloads, page tokens, or credentials; None uses the
    standard logging adapter. Raises ValueError for a connection without a client, no routes, a blank
    binding name, a missing cursor or target, or an invalid configuration.
    """
    client = _connection_client(connection)
    if not routes:
        raise ValueError("Hiver conversationReceived Trigger routes are required")
    pollers: list[Poller[TriggerEvent[ConversationReceivedEvent]]] = []
    for route in routes:
        if not (route.binding_name or "").strip() or route.target is None or route.cursor is None:
            raise ValueError("Hiver conversationReceived binding name, cursor, and target are required")
        _validate_route_configuration(route.binding_name, route.configuration)
        binding = TriggerBindingRef(
            connection.reference, CONVERSATION_RECEIVED_TRIGGER_DEFINITION.trigger, route.binding_name
        )
        pollers.append(
            new_binding_poller(
                BindingPollerConfig(
                    CONVERSATION_RECEIVED_TRIGGER_DEFINITION,
                    binding,
                    route.cursor,
                    ConversationReceivedFetcher(client, binding, route.configuration, logger),
                    route.target,
                    CONVERSATION_SNAPSHOT_OPTIONS,
                )
            )
        )
    return PollRunner(
        PollRunnerConfig(connection.reference, _poll_schedule(client), pollers, request_limiter, logger)
    )


def new_project_conversation_received_trigger_runner(
    project: LoadedProject,
    connection: Connection,
    routes: Sequence[ProjectConversationReceivedTriggerRoute],
    *,
    request_limiter: RequestLimiter | None = None,
    logger: TriggerLogger | None = None,
) -> PollRunner:
    """Create a durable runner for ``conversationReceived`` bindings saved in the loaded project.

    connection is the one `new_project_connection` opened from project; the caller keeps owning it. Each
    route loads its binding's saved shared inbox, keeps its cursor in project storage beside its Trigger
    inbox, and records every conversation in that inbox before the cursor commits past it, so a restarted
    or replaced replica neither misses nor replays a conversation. Inbox payloads are the Go connector's
    JSON, so Go and Python replicas share them. Raises ValueError for an unsaved or invalid binding and as
    `new_conversation_received_trigger_runner` does.
    """
    if project is None:
        raise ValueError("Hiver conversationReceived runner requires the loaded project configuration")
    resolved: list[ConversationReceivedTriggerRoute] = []
    for route in routes:
        if route.target is None:
            raise ValueError(
                f"Hiver conversationReceived binding {json.dumps(route.binding_name)} target is required"
            )
        key = TriggerInboxKey(CONNECTOR_ID, connection.reference.name, _TRIGGER_NAME, route.binding_name)
        try:
            configuration = decode_trigger_configuration(
                project.configuration, key, decode_conversation_received_trigger_configuration
            )
        except (ObjectNotFoundError, ValueError) as error:
            raise ValueError(
                f"Hiver conversationReceived binding {json.dumps(route.binding_name)} configuration: {error}"
            ) from error
        target = new_durable_trigger_target(
            project.trigger_inbox(key),
            route.target,
            decode_payload=decode_conversation_received_event_payload,
            logger=logger,
        )
        cursor = project.poll_cursor_store(PollCursorKey.from_trigger_inbox_key(key))
        resolved.append(ConversationReceivedTriggerRoute(route.binding_name, configuration, cursor, target))
    return new_conversation_received_trigger_runner(
        connection, resolved, request_limiter=request_limiter, logger=logger
    )


def new_conversation_received_trigger(
    *,
    connection: Connection,
    binding_name: str,
    configuration: ConversationReceivedTriggerConfiguration,
    target: TriggerTarget[ConversationReceivedEvent],
    logger: TriggerLogger | None = None,
) -> TriggerRunner:
    """Create one ``conversationReceived`` Trigger runner, as Go's generated NewConversationReceivedTrigger.

    It has no project storage, so it keeps the cursor in process memory and records a new baseline on
    every start; conversations that arrive while stopped are never delivered. Use
    `new_project_conversation_received_trigger_runner` for durable delivery. Raises ValueError for a
    connection without a client, a blank binding name, a missing target, or an invalid configuration.
    """
    client = _connection_client(connection)
    if not (binding_name or "").strip() or target is None:
        raise ValueError("Hiver conversationReceived binding name and target are required")
    _validate_route_configuration(binding_name, configuration)
    binding = TriggerBindingRef(
        connection.reference, CONVERSATION_RECEIVED_TRIGGER_DEFINITION.trigger, binding_name
    )
    source = new_binding_trigger_source(
        BindingPollerConfig(
            CONVERSATION_RECEIVED_TRIGGER_DEFINITION,
            binding,
            new_memory_cursor_store(),
            ConversationReceivedFetcher(client, binding, configuration, logger),
            snapshot=CONVERSATION_SNAPSHOT_OPTIONS,
        ),
        _poll_schedule(client),
    )
    return new_trigger(
        TriggerConfig(CONVERSATION_RECEIVED_TRIGGER_DEFINITION, binding, source, target, logger)
    )


def decode_conversation_received_event_payload(
    payload_json: str, payload_bytes: bytes
) -> ConversationReceivedEvent:
    """Decode a replayed inbox payload, written by a Go or Python replica, as a ConversationReceivedEvent.

    Raises ValueError for a payload that is not a conversationReceived event.
    """
    try:
        decoded = (
            CodecRegistry().resolve(ConversationReceivedEvent).decode(Value(WireKind.JSON, payload_json))
        )
    except (TypeError, ValueError) as error:
        raise ValueError("stored Hiver conversationReceived payload is invalid") from error
    if not isinstance(decoded, ConversationReceivedEvent) or not decoded.conversation.id:
        raise ValueError("stored Hiver conversationReceived payload is invalid")
    return decoded


def _validate_route_configuration(
    binding_name: str, configuration: ConversationReceivedTriggerConfiguration | None
) -> None:
    if configuration is None:
        raise ValueError(
            f"Hiver conversationReceived binding {json.dumps(binding_name)} configuration is required"
        )
    try:
        validate_conversation_received_trigger_configuration(configuration)
    except ValueError as error:
        raise ValueError(f"Hiver conversationReceived binding {json.dumps(binding_name)}: {error}") from None


def _connection_client(connection: Connection) -> Client:
    client = getattr(connection, "_client", None) if connection is not None else None
    if not isinstance(client, Client):
        raise ValueError(
            "Hiver conversationReceived Trigger requires a connection opened with a Hiver client"
        )
    return client


def _poll_schedule(client: Client) -> PollSchedule:
    return PollSchedule(
        client.poll_interval, maximum_backoff=max(CONVERSATION_RECEIVED_MAX_BACKOFF, client.poll_interval)
    )


__all__ = [
    "CONVERSATION_RECEIVED_TRIGGER_DEFINITION",
    "MAX_TRACKED_CONVERSATIONS",
    "ConversationReceivedTriggerRoute",
    "ProjectConversationReceivedTriggerRoute",
    "UI_INBOX_PICKER_PORT_INBOX_ID",
    "UI_UNIT_INBOX_PICKER",
    "decode_conversation_received_event_payload",
    "decode_conversation_received_trigger_configuration",
    "define_conversation_received_trigger_binding",
    "new_conversation_received_trigger",
    "new_conversation_received_trigger_runner",
    "new_project_conversation_received_trigger_runner",
    "validate_conversation_received_trigger_configuration",
]
