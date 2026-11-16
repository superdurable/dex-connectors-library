# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The Re:amaze conversationCreated poll Trigger: one Flow per conversation created in a brand or channel.

The runners poll GET /conversations on the Python SDK's durable poll Trigger kit at parity with the Go
connector. Their cursor documents match the Go connector's byte for byte, and their Trigger inbox events
carry the same IDs, times, and payload values, so Go and Python replicas share one binding.
"""

import json
from collections.abc import Sequence
from dataclasses import dataclass, field
from datetime import timedelta

import dex
from dex_connectors.sdk import (
    TriggerBindingRef,
    TriggerConfig,
    TriggerLogger,
    TriggerRunner,
    TriggerTarget,
    new_trigger,
)
from dex_connectors.sdk.pollingtrigger import (
    BindingPollerConfig,
    CursorStore,
    PollRunner,
    PollRunnerConfig,
    PollSchedule,
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

from ._generated.definitions import CONNECTOR_ID, CONVERSATION_CREATED_TRIGGER_DEFINITION
from ._generated.triggers import (
    decode_conversation_created_trigger_configuration,
    define_conversation_created_trigger_binding,
)
from .client import Client
from .connection import Connection
from .conversation_created_fetcher import (
    CONVERSATION_CREATED_LATE_INDEX_WINDOW,
    MAXIMUM_CONVERSATION_CREATED_PAGES_PER_POLL,
    ConversationCreatedFetcher,
    conversation_created_event_id,
)
from .models import ConversationCreatedEvent, ConversationCreatedTriggerConfiguration
from .operations import _CHANNEL

_TRIGGER_NAME = CONVERSATION_CREATED_TRIGGER_DEFINITION.trigger.trigger_name

UI_UNIT_CHANNEL_SLUG_ENTRY = "channelSlugEntry"
"""The configuration UI unit that enters the watched channel slug, Go's UIUnitChannelSlugEntry."""
UI_CHANNEL_SLUG_ENTRY_PORT_CHANNEL = "channel"
"""The channelSlugEntry port that outputs the slug, Go's UIChannelSlugEntryPortChannel."""


def validate_conversation_created_trigger_configuration(
    configuration: ConversationCreatedTriggerConfiguration,
) -> None:
    """Reject a channel that is not a Re:amaze channel slug with ValueError; blank is valid."""
    if configuration.channel and not _CHANNEL.fullmatch(configuration.channel):
        raise ValueError(
            "reamaze conversationCreated configuration: channel must be a Re:amaze channel slug of "
            "letters, digits, hyphens, and underscores, such as support"
        )


def decode_conversation_created_event_payload(
    payload_json: str, payload_bytes: bytes
) -> ConversationCreatedEvent:
    """Decode a replayed Trigger inbox payload that the Go or Python connector recorded."""
    try:
        decoded = (
            dex.CodecRegistry()
            .resolve(ConversationCreatedEvent)
            .decode(dex.Value(dex.WireKind.JSON, payload_json))
        )
    except (TypeError, ValueError):
        raise ValueError("pending Re:amaze conversationCreated event is invalid") from None
    if not isinstance(decoded, ConversationCreatedEvent):
        raise ValueError("pending Re:amaze conversationCreated event is invalid")
    return decoded


@dataclass(frozen=True, slots=True)
class ConversationCreatedTriggerRoute:
    """Bind one conversationCreated configuration to its application target and cursor."""

    binding_name: str
    """The stable application binding name, unique within the runner."""
    target: TriggerTarget[ConversationCreatedEvent]
    """Routes each new conversation into the application.

    A durable target from `new_durable_trigger_target` records each one before the cursor commits past it;
    any other target consumes it first.
    """
    configuration: ConversationCreatedTriggerConfiguration = field(
        default_factory=ConversationCreatedTriggerConfiguration
    )
    """Selects the watched channel; the default watches every channel of the brand."""
    cursor: CursorStore | None = None
    """Stores the binding's creation-time cursor, such as `LoadedProject.poll_cursor_store`.

    None keeps it in memory, so every process start records a new baseline and conversations created while
    stopped are not delivered.
    """


@dataclass(frozen=True, slots=True)
class ConversationTriggerRunnerConfig:
    """Configures every Re:amaze Trigger binding polled through one connection."""

    connection: Connection
    """A provider connection from `new_connection` or `new_project_connection`; a mock has no client."""
    conversation_created_routes: Sequence[ConversationCreatedTriggerRoute]
    """The conversationCreated bindings; at least one is required."""
    logger: TriggerLogger | None = None
    """Receives seeds, failed polls, rate-limit pauses, and skipped backlogs; None uses standard logging.

    Records carry the connection, Trigger, and binding, never conversation content or credentials.
    """


def _connection_client(connection: Connection) -> Client:
    if connection is None:
        raise ValueError("Re:amaze conversationCreated connection is required")
    client = connection._client
    if client is None:
        raise ValueError("Re:amaze conversationCreated Triggers require a provider connection, not a mock")
    return client


def _poll_schedule(client: Client) -> PollSchedule:
    # A zero maximum backoff uses the kit's cap, which grows with a long poll interval.
    return PollSchedule(timedelta(seconds=client.poll_interval_seconds))


def new_conversation_trigger_runner(config: ConversationTriggerRunnerConfig) -> PollRunner:
    """Create one poll runner for the configured conversationCreated bindings.

    `run` replays each durable target, records a baseline for a binding without a cursor, then polls every
    binding each `pollInterval` with jitter. A cursor for another brand or channel records a new baseline
    and logs a WARN record. A 429 pauses every binding of the connection for Retry-After, or for one minute,
    Re:amaze's documented limit window, without it; other failures back off per binding up to 15 minutes.
    Delivery is at least once, and each conversation keeps one Trigger event ID across refetches. Raises
    ValueError for a missing or mock connection, no routes, a blank binding name, a missing target, or an
    invalid configuration.
    """
    client = _connection_client(config.connection)
    if not config.conversation_created_routes:
        raise ValueError("Re:amaze conversationCreated routes are required")
    reference = config.connection.reference
    pollers = []
    for route in config.conversation_created_routes:
        if route is None or not (route.binding_name or "").strip() or route.target is None:
            raise ValueError("Re:amaze conversationCreated binding name and target are required")
        validate_conversation_created_trigger_configuration(route.configuration)
        pollers.append(
            new_binding_poller(
                BindingPollerConfig(
                    definition=CONVERSATION_CREATED_TRIGGER_DEFINITION,
                    binding=TriggerBindingRef(
                        reference, CONVERSATION_CREATED_TRIGGER_DEFINITION.trigger, route.binding_name
                    ),
                    cursor=route.cursor if route.cursor is not None else new_memory_cursor_store(),
                    fetcher=ConversationCreatedFetcher(
                        client, reference, route.binding_name, route.configuration, config.logger
                    ),
                    target=route.target,
                )
            )
        )
    return PollRunner(PollRunnerConfig(reference, _poll_schedule(client), pollers, logger=config.logger))


@dataclass(frozen=True, slots=True)
class ProjectConversationCreatedTriggerRoute:
    """Select one conversationCreated binding saved in the project configuration."""

    binding_name: str
    """The saved binding name."""
    target: TriggerTarget[ConversationCreatedEvent]
    """Routes each new conversation into the application, usually `new_dex_flow_trigger_target`."""


@dataclass(frozen=True, slots=True)
class ProjectConversationTriggerRunnerConfig:
    """Selects Re:amaze Trigger bindings saved in the project configuration."""

    conversation_created_routes: Sequence[ProjectConversationCreatedTriggerRoute]
    """The conversationCreated bindings; at least one is required."""
    logger: TriggerLogger | None = None
    """Receives the runner's and the durable inboxes' records; None uses standard logging."""


def new_project_conversation_trigger_runner(
    project: LoadedProject, connection: Connection, config: ProjectConversationTriggerRunnerConfig
) -> PollRunner:
    """Create a durable runner for bindings saved in the loaded project.

    connection is the project connection from `new_project_connection(project, name)`; the caller owns it
    and closes it after the runner stops. Each binding's saved channel is loaded (an unsaved binding
    watches every channel), and each route records new conversations in its binding's project Trigger
    inbox before its cursor commits in the binding's project poll cursor, so a restarted or replaced
    replica neither misses nor repeats a conversation created within
    `CONVERSATION_CREATED_LATE_INDEX_WINDOW` of the newest one seen. Raises ValueError as
    `new_conversation_trigger_runner` does and for an invalid saved configuration.
    """
    if project is None:
        raise ValueError("Re:amaze conversationCreated Triggers require a loaded project")
    _connection_client(connection)
    connection_name = connection.reference.name
    routes: list[ConversationCreatedTriggerRoute] = []
    for route in config.conversation_created_routes:
        if route is None or route.target is None:
            raise ValueError("Re:amaze conversationCreated binding target is required")
        key = TriggerInboxKey(CONNECTOR_ID, connection_name, _TRIGGER_NAME, route.binding_name)
        try:
            configuration = decode_trigger_configuration(
                project.configuration, key, decode_conversation_created_trigger_configuration
            )
        except ObjectNotFoundError:
            # An unsaved binding watches every channel, as a blank entry does.
            configuration = ConversationCreatedTriggerConfiguration()
        except ValueError as error:
            raise ValueError(
                f"reamaze conversationCreated binding {json.dumps(route.binding_name)} configuration: {error}"
            ) from error
        target = new_durable_trigger_target(
            project.trigger_inbox(key),
            route.target,
            decode_payload=decode_conversation_created_event_payload,
            logger=config.logger,
        )
        cursor = project.poll_cursor_store(PollCursorKey.from_trigger_inbox_key(key))
        routes.append(ConversationCreatedTriggerRoute(route.binding_name, target, configuration, cursor))
    return new_conversation_trigger_runner(ConversationTriggerRunnerConfig(connection, routes, config.logger))


def new_conversation_created_trigger(
    *,
    connection: Connection,
    connection_name: str,
    target: TriggerTarget[ConversationCreatedEvent],
    configuration: ConversationCreatedTriggerConfiguration | None = None,
    logger: TriggerLogger | None = None,
) -> TriggerRunner:
    """Create one conversationCreated Trigger with an in-memory cursor, as Go's generated factory does.

    It matches Go's `NewConversationCreatedTrigger`, and its binding is named after the Trigger. It has no
    project storage, so it records a new baseline on every start and conversations created while it is
    stopped are not delivered; use `new_project_conversation_trigger_runner` for durable delivery. Raises
    ValueError when connection_name differs from the connection's name, for a mock connection, or for an
    invalid configuration.
    """
    client = _connection_client(connection)
    if connection_name != connection.reference.name:
        raise ValueError(
            f"reamaze connector trigger connection_name {json.dumps(connection_name)} must equal its "
            f"connection's name {json.dumps(connection.reference.name)}"
        )
    configuration = configuration if configuration is not None else ConversationCreatedTriggerConfiguration()
    validate_conversation_created_trigger_configuration(configuration)
    binding = TriggerBindingRef(
        connection.reference, CONVERSATION_CREATED_TRIGGER_DEFINITION.trigger, _TRIGGER_NAME
    )
    source = new_binding_trigger_source(
        BindingPollerConfig(
            definition=CONVERSATION_CREATED_TRIGGER_DEFINITION,
            binding=binding,
            cursor=new_memory_cursor_store(),
            fetcher=ConversationCreatedFetcher(
                client, connection.reference, _TRIGGER_NAME, configuration, logger
            ),
        ),
        _poll_schedule(client),
    )
    return new_trigger(
        TriggerConfig(
            definition=CONVERSATION_CREATED_TRIGGER_DEFINITION,
            binding=binding,
            source=source,
            target=target,
            logger=logger,
        )
    )


__all__ = [
    "CONVERSATION_CREATED_LATE_INDEX_WINDOW",
    "CONVERSATION_CREATED_TRIGGER_DEFINITION",
    "MAXIMUM_CONVERSATION_CREATED_PAGES_PER_POLL",
    "UI_CHANNEL_SLUG_ENTRY_PORT_CHANNEL",
    "UI_UNIT_CHANNEL_SLUG_ENTRY",
    "ConversationCreatedFetcher",
    "ConversationCreatedTriggerRoute",
    "ConversationTriggerRunnerConfig",
    "ProjectConversationCreatedTriggerRoute",
    "ProjectConversationTriggerRunnerConfig",
    "conversation_created_event_id",
    "decode_conversation_created_event_payload",
    "decode_conversation_created_trigger_configuration",
    "define_conversation_created_trigger_binding",
    "new_conversation_created_trigger",
    "new_conversation_trigger_runner",
    "new_project_conversation_trigger_runner",
    "validate_conversation_created_trigger_configuration",
]
