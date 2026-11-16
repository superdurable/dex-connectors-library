# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The ticketChanged poll Trigger: each Gorgias ticket event, such as ticket-created or ticket-closed, read
by polling the account's event log, GET /api/events, on the Connector SDK's durable poll Trigger kit.

`define_ticket_changed_trigger_binding` declares a binding in a Flow's `get_connector_trigger_bindings`.
`new_project_ticket_trigger_runner` is the durable path: it records each event in the binding's project
Trigger inbox before the cursor commits in the binding's project poll cursor. `new_ticket_trigger_runner`
takes targets and cursor stores directly, and `new_ticket_changed_trigger` keeps the cursor in memory.
"""

import json
from collections.abc import Sequence
from dataclasses import dataclass
from datetime import timedelta
from typing import Final

import dex
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

from ._generated.definitions import CONNECTOR_ID, TICKET_CHANGED_TRIGGER_DEFINITION
from ._generated.triggers import (
    decode_ticket_changed_trigger_configuration,
    define_ticket_changed_trigger_binding,
)
from ._ticket_event_cursor import MAXIMUM_SEEN_EVENT_KEYS, TICKET_EVENT_OVERLAP_NANOSECONDS
from ._ticket_event_fetcher import TICKET_EVENT_PAGE_SIZE, TicketEventFetcher
from ._ticket_event_types import (
    TICKET_EVENT_TYPES,
    TicketEventType,
    validate_ticket_changed_trigger_configuration,
)
from .client import Client
from .connection import Connection
from .models import TicketChangedEvent, TicketChangedTriggerConfiguration

__all__ = [
    "MAXIMUM_SEEN_EVENT_KEYS",
    "TICKET_CHANGED_TRIGGER_DEFINITION",
    "TICKET_EVENT_OVERLAP",
    "TICKET_EVENT_PAGE_SIZE",
    "TICKET_EVENT_TYPES",
    "ProjectTicketChangedTriggerRoute",
    "ProjectTicketTriggerRunnerConfig",
    "TicketChangedEvent",
    "TicketChangedTriggerConfiguration",
    "TicketChangedTriggerRoute",
    "TicketEventType",
    "TicketTriggerRunnerConfig",
    "decode_ticket_changed_event_payload",
    "decode_ticket_changed_trigger_configuration",
    "define_ticket_changed_trigger_binding",
    "new_project_ticket_trigger_runner",
    "new_ticket_changed_trigger",
    "new_ticket_trigger_runner",
    "validate_ticket_changed_trigger_configuration",
]

TICKET_EVENT_OVERLAP: Final = timedelta(microseconds=TICKET_EVENT_OVERLAP_NANOSECONDS // 1000)
"""How far before the committed high-water mark each poll starts reading, so an event whose write commits a
little after its created_datetime is still read; events already seen in that window are not delivered
again."""

# Leaves most of the account's 40 requests per 20 seconds to operations.
_MAXIMUM_PAGES_PER_POLL: Final = 5
_TRIGGER_NAME = TICKET_CHANGED_TRIGGER_DEFINITION.trigger.trigger_name
_TICKET_CHANGED_EVENT_CODEC = dex.CodecRegistry().resolve(TicketChangedEvent)


def decode_ticket_changed_event_payload(payload_json: str, payload_bytes: bytes) -> TicketChangedEvent:
    """Decode a replayed ticketChanged payload from a project Trigger inbox, written by Go or Python.

    Pass it as `decode_payload` to `new_durable_trigger_target`. Raises ValueError for a payload that is
    not a TicketChangedEvent.
    """
    try:
        decoded = _TICKET_CHANGED_EVENT_CODEC.decode(dex.Value(dex.WireKind.JSON, payload_json))
    except (TypeError, ValueError, dex.ValueMappingError) as error:
        raise ValueError("stored Gorgias ticketChanged payload is invalid") from error
    if not isinstance(decoded, TicketChangedEvent):
        raise ValueError("stored Gorgias ticketChanged payload is invalid")
    return decoded


@dataclass(frozen=True, slots=True)
class TicketChangedTriggerRoute:
    """Binds one ticketChanged configuration to its application target and cursor."""

    binding_name: str
    """The stable application binding name, unique within the runner."""
    configuration: TicketChangedTriggerConfiguration
    """Selects the delivered event types; the default delivers every ticket event type."""
    target: TriggerTarget[TicketChangedEvent]
    """Routes each event into the application.

    A target from `new_durable_trigger_target` records each event before the cursor commits past it; any
    other target consumes it first.
    """
    cursor: CursorStore | None = None
    """Stores the binding's event log position, such as `LoadedProject.poll_cursor_store`.

    None keeps it in memory, so every process start records a new baseline and events recorded while
    stopped are lost.
    """


@dataclass(frozen=True, slots=True)
class TicketTriggerRunnerConfig:
    """Configures every Gorgias ticketChanged binding polled through one connection."""

    connection: Connection
    """A connection built with `new_connection` or `new_project_connection`; a mock connection is refused."""
    ticket_changed_routes: Sequence[TicketChangedTriggerRoute]
    """The ticketChanged bindings; at least one is required."""
    logger: TriggerLogger | None = None
    """Receives seeds, reseeds, failed polls, rate-limit pauses, and delivery retries with binding identity,
    never event contents, cursors, or credentials. None uses the standard logging adapter."""


def new_ticket_trigger_runner(config: TicketTriggerRunnerConfig) -> PollRunner:
    """Create one poll runner for the configured ticketChanged bindings of one connection.

    `run` replays each durable target, records a baseline for a binding without a cursor, then polls every
    binding each `pollInterval` with jitter, reading at most five pages of 100 events per binding per poll
    and fewer when Gorgias's X-Gorgias-Account-Api-Call-Limit header shows three quarters of the window
    spent. Only an undecodable cursor records a new baseline and logs a WARN record; events recorded
    between the last poll and the new baseline are not delivered. A 429 pauses every binding of the
    connection for Retry-After, because Gorgias's limit is per account, or backs off exponentially without
    it; other failures back off per binding. Delivery is at least once, and each event keeps its Trigger
    event ID, gorgias-event-<id>, across refetches. The cursor documents match the Go connector's byte for
    byte, so Go and Python replicas can share one binding's cursor and inbox. Raises ValueError for a mock
    connection, no routes, a blank binding name, a missing target, or an invalid configuration.
    """
    client = _connection_client(config.connection)
    if not config.ticket_changed_routes:
        raise ValueError("Gorgias ticketChanged routes are required")
    pollers: list[Poller[TriggerEvent[TicketChangedEvent]]] = [
        _new_ticket_changed_poller(config.connection, client, route) for route in config.ticket_changed_routes
    ]
    return PollRunner(
        PollRunnerConfig(config.connection.reference, _poll_schedule(client), pollers, logger=config.logger)
    )


@dataclass(frozen=True, slots=True)
class ProjectTicketChangedTriggerRoute:
    """Selects one ticketChanged binding saved in the project configuration."""

    binding_name: str
    """The saved binding's name."""
    target: TriggerTarget[TicketChangedEvent]
    """Routes each event into the application, usually `new_dex_flow_trigger_target`."""


@dataclass(frozen=True, slots=True)
class ProjectTicketTriggerRunnerConfig:
    """Selects Gorgias ticketChanged bindings saved in the project configuration."""

    ticket_changed_routes: Sequence[ProjectTicketChangedTriggerRoute]
    """The ticketChanged bindings; at least one is required."""
    logger: TriggerLogger | None = None
    """Receives the runner's and the durable inboxes' records; None uses the standard logging adapter."""


def new_project_ticket_trigger_runner(
    project: LoadedProject, connection: Connection, config: ProjectTicketTriggerRunnerConfig
) -> PollRunner:
    """Create a durable runner for bindings saved in project, polled through connection.

    connection must be the project connection the bindings name, from `new_project_connection`; the caller
    keeps it and closes it after the runner stops. Each route loads its binding's saved configuration (an
    unsaved binding delivers every ticket event type) and records events in the binding's project Trigger
    inbox before its event log position commits in the binding's project poll cursor, so a restarted or
    replaced replica neither misses nor repeats an event. Raises ValueError for a missing target or an
    invalid saved configuration.
    """
    if project is None:
        raise ValueError("Gorgias Trigger runner requires the loaded project configuration")
    _connection_client(connection)
    routes: list[TicketChangedTriggerRoute] = []
    for route in config.ticket_changed_routes:
        if route.target is None:
            raise ValueError(
                f"Gorgias ticketChanged binding {json.dumps(route.binding_name)} target is required"
            )
        key = TriggerInboxKey(CONNECTOR_ID, connection.reference.name, _TRIGGER_NAME, route.binding_name)
        try:
            configuration = decode_trigger_configuration(
                project.configuration, key, decode_ticket_changed_trigger_configuration
            )
        except ObjectNotFoundError:
            # An unsaved binding delivers every ticket event type, as an empty configuration does.
            configuration = TicketChangedTriggerConfiguration()
        except ValueError as error:
            raise ValueError(
                f"gorgias ticketChanged binding {json.dumps(route.binding_name)} configuration: {error}"
            ) from error
        target = new_durable_trigger_target(
            project.trigger_inbox(key),
            route.target,
            decode_payload=decode_ticket_changed_event_payload,
            logger=config.logger,
        )
        cursor = project.poll_cursor_store(PollCursorKey.from_trigger_inbox_key(key))
        routes.append(TicketChangedTriggerRoute(route.binding_name, configuration, target, cursor))
    return new_ticket_trigger_runner(TicketTriggerRunnerConfig(connection, routes, config.logger))


def new_ticket_changed_trigger(
    *,
    connection: Connection,
    binding_name: str,
    configuration: TicketChangedTriggerConfiguration,
    target: TriggerTarget[TicketChangedEvent],
    logger: TriggerLogger | None = None,
) -> TriggerRunner:
    """Create one ticketChanged binding's TriggerRunner with its cursor in memory.

    It has no project storage, so every start records a new baseline and events recorded while the
    process is stopped are lost; use it for tests and demonstrations, and
    `new_project_ticket_trigger_runner` otherwise. Raises ValueError as `new_ticket_trigger_runner` does.
    """
    client = _connection_client(connection)
    _validate_route(binding_name, target, configuration)
    binding = TriggerBindingRef(connection.reference, TICKET_CHANGED_TRIGGER_DEFINITION.trigger, binding_name)
    source = new_binding_trigger_source(
        BindingPollerConfig(
            TICKET_CHANGED_TRIGGER_DEFINITION,
            binding,
            new_memory_cursor_store(),
            TicketEventFetcher(client, connection.reference, configuration),
        ),
        _poll_schedule(client),
    )
    return new_trigger(TriggerConfig(TICKET_CHANGED_TRIGGER_DEFINITION, binding, source, target, logger))


def _new_ticket_changed_poller(
    connection: Connection, client: Client, route: TicketChangedTriggerRoute
) -> Poller[TriggerEvent[TicketChangedEvent]]:
    _validate_route(route.binding_name, route.target, route.configuration)
    return new_binding_poller(
        BindingPollerConfig(
            TICKET_CHANGED_TRIGGER_DEFINITION,
            TriggerBindingRef(
                connection.reference, TICKET_CHANGED_TRIGGER_DEFINITION.trigger, route.binding_name
            ),
            route.cursor if route.cursor is not None else new_memory_cursor_store(),
            TicketEventFetcher(client, connection.reference, route.configuration),
            route.target,
        )
    )


def _validate_route(
    binding_name: str,
    target: TriggerTarget[TicketChangedEvent] | None,
    configuration: TicketChangedTriggerConfiguration,
) -> None:
    if not (binding_name or "").strip() or target is None:
        raise ValueError("Gorgias ticketChanged binding name and target are required")
    try:
        validate_ticket_changed_trigger_configuration(configuration)
    except ValueError as error:
        raise ValueError(f"gorgias ticketChanged configuration: {error}") from error


def _poll_schedule(client: Client) -> PollSchedule:
    # A zero maximum backoff uses the kit's cap, which grows with a long poll interval.
    return PollSchedule(
        timedelta(seconds=client.poll_interval_seconds), maximum_pages_per_poll=_MAXIMUM_PAGES_PER_POLL
    )


def _connection_client(connection: Connection) -> Client:
    # The generated Connection keeps its Client private; Triggers need its transport and settings.
    client = connection._client if connection is not None else None
    if client is None:
        raise ValueError(
            "Gorgias Triggers require a connection built with new_connection or new_project_connection"
        )
    return client
