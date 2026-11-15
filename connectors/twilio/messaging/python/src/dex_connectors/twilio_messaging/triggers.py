# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Receive inbound Twilio messages through the ``messageReceived`` poll Trigger.

Each binding polls the account's Messages list with the Connector SDK poll kit
(`dex_connectors.sdk.pollingtrigger`), at behavior parity with the Go connector:

- the first poll lists the last two minutes and records those inbound messages as delivered, so existing
  messages are a baseline and are never delivered;
- each later poll lists ``DateSent>=<watermark - 2m>`` (plus ``To`` for one receiving address), newest
  first in pages of 200 for at most ten pages, halving a busier window with ``DateSent<``; outbound
  messages and other numbers are never delivered, and at most 500 messages arrive per change page,
  oldest first;
- a message still in ``receiving`` (MMS media) holds the watermark and is delivered once ``received``, or
  as-is after ten minutes;
- an undecodable cursor, or one for another account or receiving address, records a new baseline and
  logs a WARN record; a 429 pauses every binding of the connection for Retry-After, or backs off
  exponentially without it;
- each message's Trigger event ID is ``twilio-message-<SID>``.

The cursor document is the Go connector's byte for byte, so Go and Python replicas share it.
`new_project_message_trigger_runner` records every message in the binding's project Trigger inbox before
the watermark commits in its project poll cursor.
"""

from collections.abc import Sequence
from dataclasses import dataclass
from datetime import timedelta
from typing import Any

from dex import CodecRegistry, Value, WireKind
from dex_connectors.sdk import (
    ConnectionRef,
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

from ._generated.definitions import CONNECTOR_ID, MESSAGE_RECEIVED_TRIGGER_DEFINITION
from ._generated.triggers import (
    decode_message_received_trigger_configuration,
    define_message_received_trigger_binding,
)
from ._inbound_message_fetcher import (
    INBOUND_MESSAGE_DIRECTION,
    INBOUND_SNAPSHOT_OPTIONS,
    MESSAGE_STATUS_RECEIVED,
    MESSAGE_STATUS_RECEIVING,
    InboundMessageFetcher,
)
from .client import Client, is_e164_phone_number, is_whatsapp
from .connection import Connection
from .models import MessageReceivedEvent, MessageReceivedTriggerConfiguration

_TRIGGER_NAME = MESSAGE_RECEIVED_TRIGGER_DEFINITION.trigger.trigger_name


def validate_message_received_trigger_configuration(
    configuration: MessageReceivedTriggerConfiguration,
) -> None:
    """Accept a blank ``to``, an E.164 number, or a ``whatsapp:+E.164`` address without surrounding spaces.

    Raises ValueError, as the Go configuration's ``Validate`` returns an error.
    """
    to = configuration.to
    if to == "":
        return
    if to != to.strip():
        raise ValueError("messageReceived to must not have surrounding whitespace")
    if is_e164_phone_number(to) or is_whatsapp(to):
        return
    raise ValueError("messageReceived to must be an E.164 phone number or a whatsapp:+E.164 address")


def _decode_message_received_payload(payload_json: str, payload_bytes: bytes) -> MessageReceivedEvent:
    """Decode a replayed message, including one a Go replica recorded."""
    decoded = CodecRegistry().resolve(MessageReceivedEvent).decode(Value(WireKind.JSON, payload_json))
    if not isinstance(decoded, MessageReceivedEvent) or not decoded.message.sid:
        raise ValueError("stored Twilio inbound message is invalid")
    return decoded


def _connection_client(connection: Connection) -> Client:
    connection.reference.validate()
    client = connection._client
    if client is None:
        raise ValueError("Twilio Messaging Triggers require a provider connection, not a mock connection")
    return client


def _poll_schedule(client: Client) -> PollSchedule:
    # A zero maximum backoff uses the kit's cap, which grows with a long poll interval.
    return PollSchedule(timedelta(seconds=client.poll_interval_seconds))


def _configuration_error(error: ValueError) -> ValueError:
    return ValueError(f"twilio-messaging messageReceived configuration: {error}")


@dataclass(frozen=True, slots=True)
class MessageReceivedTriggerRoute:
    """Bind one ``messageReceived`` configuration to its application target and cursor."""

    binding_name: str
    """The stable application binding name, unique within the runner."""
    configuration: MessageReceivedTriggerConfiguration
    """Selects the receiving address; blank receives every inbound message of the account."""
    target: TriggerTarget[MessageReceivedEvent]
    """Routes each inbound message into the application.

    A target from `new_durable_trigger_target` records each message before the cursor commits past it;
    any other target consumes it first.
    """
    cursor: CursorStore | None = None
    """Stores the binding's DateSent watermark and delivered-message snapshot, such as
    `LoadedProject.poll_cursor_store`.

    None keeps it in memory, so every process start records a new baseline and messages received while
    stopped are lost.
    """


def new_message_trigger_runner(
    connection: Connection,
    routes: Sequence[MessageReceivedTriggerRoute],
    logger: TriggerLogger | None = None,
) -> PollRunner:
    """Create one poll runner for the ``messageReceived`` bindings polled through one connection.

    `PollRunner.run` replays each durable target, records a baseline for a binding without a cursor,
    then lists each binding's Messages each ``pollInterval`` with jitter. A cursor for another account or
    receiving address records a new baseline and logs a WARN record; messages received between the last
    poll and the new baseline are not delivered. A 429 pauses every binding of the connection for
    Retry-After, or backs off exponentially without it; other failures back off per binding up to 15
    minutes. Delivery is at least once, and each message keeps one Trigger event ID, from its SID, across
    refetches. logger receives the runner's records, which carry binding identity and never message
    bodies, addresses, or credentials; None uses the standard ``dex_connectors.sdk.trigger`` logger.
    Raises ValueError for no routes, a blank or repeated binding name, a missing target, an invalid
    configuration, or a mock connection.
    """
    client = _connection_client(connection)
    if not routes:
        raise ValueError("Twilio messageReceived routes are required")
    pollers: list[Poller[Any]] = [
        _new_message_received_poller(client, connection.reference, route) for route in routes
    ]
    return PollRunner(PollRunnerConfig(connection.reference, _poll_schedule(client), pollers, logger=logger))


def _new_message_received_poller(
    client: Client, connection: ConnectionRef, route: MessageReceivedTriggerRoute
) -> Poller[TriggerEvent[MessageReceivedEvent]]:
    if not (route.binding_name or "").strip() or route.target is None:
        raise ValueError("Twilio messageReceived binding name and target are required")
    try:
        validate_message_received_trigger_configuration(route.configuration)
    except ValueError as error:
        raise _configuration_error(error) from error
    binding = TriggerBindingRef(connection, MESSAGE_RECEIVED_TRIGGER_DEFINITION.trigger, route.binding_name)
    fetcher = InboundMessageFetcher(client, connection, route.configuration.to)
    cursor = route.cursor if route.cursor is not None else new_memory_cursor_store()
    return new_binding_poller(
        BindingPollerConfig(
            MESSAGE_RECEIVED_TRIGGER_DEFINITION,
            binding,
            cursor,
            fetcher,
            route.target,
            snapshot=INBOUND_SNAPSHOT_OPTIONS,
        )
    )


@dataclass(frozen=True, slots=True)
class ProjectMessageReceivedTriggerRoute:
    """Select one ``messageReceived`` binding saved in the project configuration."""

    binding_name: str
    """The saved binding's name."""
    target: TriggerTarget[MessageReceivedEvent]
    """Routes each inbound message into the application, usually `new_dex_flow_trigger_target`."""


def new_project_message_trigger_runner(
    project: LoadedProject,
    connection: Connection,
    routes: Sequence[ProjectMessageReceivedTriggerRoute],
    logger: TriggerLogger | None = None,
) -> PollRunner:
    """Create a durable runner for ``messageReceived`` bindings saved in the loaded project.

    connection is the project's connection, usually from `new_project_connection`; the caller keeps
    ownership and closes it after the runner stops. Each binding's saved receiving address is loaded (an
    unsaved binding receives every inbound message of the account), and each route records messages in
    its binding's project Trigger inbox before its watermark commits in the binding's project poll cursor,
    so a restarted or replaced replica neither misses nor repeats a message. logger also receives the
    inboxes' records. Raises ValueError for an invalid saved configuration or route, as
    `new_message_trigger_runner` does.
    """
    if project is None:
        raise ValueError("Twilio Trigger runner requires the loaded project configuration")
    _connection_client(connection)
    durable_routes = []
    for route in routes:
        if route.target is None:
            raise ValueError(f"Twilio messageReceived binding {route.binding_name!r} target is required")
        key = TriggerInboxKey(CONNECTOR_ID, connection.reference.name, _TRIGGER_NAME, route.binding_name)
        try:
            configuration = decode_trigger_configuration(
                project.configuration, key, decode_message_received_trigger_configuration
            )
        except ObjectNotFoundError:
            # An unsaved binding receives every inbound message, as a blank address does.
            configuration = MessageReceivedTriggerConfiguration()
        except ValueError as error:
            raise ValueError(
                f"twilio-messaging messageReceived binding {route.binding_name!r} configuration: {error}"
            ) from error
        target = new_durable_trigger_target(
            project.trigger_inbox(key),
            route.target,
            decode_payload=_decode_message_received_payload,
            logger=logger,
        )
        cursor = project.poll_cursor_store(PollCursorKey.from_trigger_inbox_key(key))
        durable_routes.append(MessageReceivedTriggerRoute(route.binding_name, configuration, target, cursor))
    return new_message_trigger_runner(connection, durable_routes, logger)


def new_message_received_trigger(
    *,
    connection: Connection,
    binding_name: str,
    configuration: MessageReceivedTriggerConfiguration,
    target: TriggerTarget[MessageReceivedEvent],
    logger: TriggerLogger | None = None,
) -> TriggerRunner:
    """Create one ``messageReceived`` binding's TriggerRunner, as Go's NewMessageReceivedTrigger does.

    It has no project storage, so it keeps the cursor in memory and records a new baseline on every
    start. Use `new_project_message_trigger_runner` for a durable cursor and inbox. Raises ValueError for
    an invalid configuration, a blank binding name, or a mock connection.
    """
    client = _connection_client(connection)
    try:
        validate_message_received_trigger_configuration(configuration)
    except ValueError as error:
        raise _configuration_error(error) from error
    binding = TriggerBindingRef(
        connection.reference, MESSAGE_RECEIVED_TRIGGER_DEFINITION.trigger, binding_name
    )
    fetcher = InboundMessageFetcher(client, connection.reference, configuration.to)
    source = new_binding_trigger_source(
        BindingPollerConfig(
            MESSAGE_RECEIVED_TRIGGER_DEFINITION,
            binding,
            new_memory_cursor_store(),
            fetcher,
            snapshot=INBOUND_SNAPSHOT_OPTIONS,
        ),
        _poll_schedule(client),
    )
    return new_trigger(TriggerConfig(MESSAGE_RECEIVED_TRIGGER_DEFINITION, binding, source, target, logger))


__all__ = [
    "INBOUND_MESSAGE_DIRECTION",
    "MESSAGE_RECEIVED_TRIGGER_DEFINITION",
    "MESSAGE_STATUS_RECEIVED",
    "MESSAGE_STATUS_RECEIVING",
    "MessageReceivedEvent",
    "MessageReceivedTriggerConfiguration",
    "MessageReceivedTriggerRoute",
    "ProjectMessageReceivedTriggerRoute",
    "decode_message_received_trigger_configuration",
    "define_message_received_trigger_binding",
    "new_message_received_trigger",
    "new_message_trigger_runner",
    "new_project_message_trigger_runner",
    "validate_message_received_trigger_configuration",
]
