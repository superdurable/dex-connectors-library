# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The Freshdesk ticketChanged poll Trigger: one Flow per ticket created or updated in the helpdesk.

The runners poll List All Tickets on the Python SDK's durable poll Trigger kit at parity with the Go
connector. Their cursor documents and Trigger inbox payloads match the Go connector's byte for byte, so Go
and Python replicas share one binding.
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

from ._generated.definitions import CONNECTOR_ID, TICKET_CHANGED_TRIGGER_DEFINITION
from ._generated.triggers import (
    decode_ticket_changed_trigger_configuration,
    define_ticket_changed_trigger_binding,
)
from .client import Client
from .connection import Connection
from .models import TicketChangedEvent, TicketChangedTriggerConfiguration
from .ticket_change_fetcher import (
    TICKET_CHANGE_CREATED,
    TICKET_CHANGE_OVERLAP,
    TICKET_CHANGE_UPDATED,
    TicketChangeFetcher,
    delivers_ticket_change_kind,
)

_TRIGGER_NAME = TICKET_CHANGED_TRIGGER_DEFINITION.trigger.trigger_name


def validate_ticket_changed_trigger_configuration(configuration: TicketChangedTriggerConfiguration) -> None:
    """Reject an unknown or repeated change kind with ValueError; an empty configuration is valid."""
    seen: set[str] = set()
    for kind in configuration.change_kinds:
        if kind not in (TICKET_CHANGE_CREATED, TICKET_CHANGE_UPDATED):
            raise ValueError(
                f"freshdesk ticketChanged configuration: ticketChanged changeKinds entry "
                f"{json.dumps(kind)} must be created or updated"
            )
        if kind in seen:
            raise ValueError(
                "freshdesk ticketChanged configuration: ticketChanged changeKinds lists "
                f"{json.dumps(kind)} twice"
            )
        seen.add(kind)


def decode_ticket_changed_event_payload(payload_json: str, payload_bytes: bytes) -> TicketChangedEvent:
    """Decode a replayed Trigger inbox payload that the Go or Python connector recorded."""
    decoded = (
        dex.CodecRegistry().resolve(TicketChangedEvent).decode(dex.Value(dex.WireKind.JSON, payload_json))
    )
    if not isinstance(decoded, TicketChangedEvent):
        raise ValueError("pending Freshdesk ticketChanged event is invalid")
    return decoded


@dataclass(frozen=True, slots=True)
class TicketChangedTriggerRoute:
    """Bind one ticketChanged configuration to its application target and cursor."""

    binding_name: str
    """The stable application binding name, unique within the runner."""
    target: TriggerTarget[TicketChangedEvent]
    """Routes each change into the application.

    A durable target from `new_durable_trigger_target` records each change before the cursor commits past
    it; any other target consumes it first.
    """
    configuration: TicketChangedTriggerConfiguration = field(
        default_factory=TicketChangedTriggerConfiguration
    )
    """Selects the delivered change kinds; the default delivers created and updated tickets."""
    cursor: CursorStore | None = None
    """Stores the binding's updated_at position, such as `LoadedProject.poll_cursor_store`.

    None keeps it in memory, so every process start records a new baseline and changes made while stopped
    are lost.
    """


@dataclass(frozen=True, slots=True)
class TicketTriggerRunnerConfig:
    """Configures every Freshdesk Trigger binding polled through one connection."""

    connection: Connection
    """A provider connection from `new_connection` or `new_project_connection`; a mock has no client."""
    ticket_changed_routes: Sequence[TicketChangedTriggerRoute]
    """The ticketChanged bindings; at least one is required."""
    logger: TriggerLogger | None = None
    """Receives seeds, reseeds, failed polls, and rate-limit pauses; None uses standard logging."""


def _connection_client(connection: Connection) -> Client:
    if connection is None:
        raise ValueError("Freshdesk ticketChanged connection is required")
    client = connection._client
    if client is None:
        raise ValueError("Freshdesk ticketChanged Triggers require a provider connection, not a mock")
    return client


def _poll_schedule(client: Client) -> PollSchedule:
    # A zero maximum backoff uses the kit's cap, which grows with a long poll interval.
    return PollSchedule(timedelta(seconds=client.config.poll_interval_seconds))


def new_ticket_trigger_runner(config: TicketTriggerRunnerConfig) -> PollRunner:
    """Create one poll runner for the configured ticketChanged bindings.

    `run` replays each durable target, records a baseline for a binding without a cursor, then polls
    every binding each `pollInterval` with jitter. A Freshdesk 400 for the cursor's query or an
    undecodable cursor records a new baseline and logs a WARN record; changes made between the last poll
    and the new baseline are not delivered. A 429 pauses every binding of the connection for Retry-After,
    because Freshdesk's limit is per account, or backs off exponentially without it; other failures back
    off per binding up to 15 minutes. Delivery is at least once, and each change keeps one Trigger event
    ID across refetches. Raises ValueError for a missing or mock connection, no routes, a blank or
    duplicated binding name, a missing target, or an invalid configuration.
    """
    client = _connection_client(config.connection)
    if not config.ticket_changed_routes:
        raise ValueError("Freshdesk ticketChanged routes are required")
    reference = config.connection.reference
    pollers = []
    for route in config.ticket_changed_routes:
        if route is None or not (route.binding_name or "").strip() or route.target is None:
            raise ValueError("Freshdesk ticketChanged binding name and target are required")
        validate_ticket_changed_trigger_configuration(route.configuration)
        pollers.append(
            new_binding_poller(
                BindingPollerConfig(
                    definition=TICKET_CHANGED_TRIGGER_DEFINITION,
                    binding=TriggerBindingRef(
                        reference, TICKET_CHANGED_TRIGGER_DEFINITION.trigger, route.binding_name
                    ),
                    cursor=route.cursor if route.cursor is not None else new_memory_cursor_store(),
                    fetcher=TicketChangeFetcher(client, reference, route.configuration),
                    target=route.target,
                )
            )
        )
    return PollRunner(PollRunnerConfig(reference, _poll_schedule(client), pollers, logger=config.logger))


@dataclass(frozen=True, slots=True)
class ProjectTicketChangedTriggerRoute:
    """Select one ticketChanged binding saved in the project configuration."""

    binding_name: str
    """The saved binding name."""
    target: TriggerTarget[TicketChangedEvent]
    """Routes each change into the application, usually `new_dex_flow_trigger_target`."""


@dataclass(frozen=True, slots=True)
class ProjectTicketTriggerRunnerConfig:
    """Selects Freshdesk Trigger bindings saved in the project configuration."""

    ticket_changed_routes: Sequence[ProjectTicketChangedTriggerRoute]
    """The ticketChanged bindings; at least one is required."""
    logger: TriggerLogger | None = None
    """Receives the runner's and the durable inboxes' records; None uses standard logging."""


def new_project_ticket_trigger_runner(
    project: LoadedProject, connection: Connection, config: ProjectTicketTriggerRunnerConfig
) -> PollRunner:
    """Create a durable runner for bindings saved in the loaded project.

    connection is the project connection from `new_project_connection(project, name)`; the caller owns
    it and closes it after the runner stops. Each binding's saved configuration is loaded (an unsaved
    binding delivers every change kind), and each route records changes in its binding's project Trigger
    inbox before its updated_at position commits in the binding's project poll cursor, so a restarted or
    replaced replica neither misses nor repeats a change. Raises ValueError as
    `new_ticket_trigger_runner` does and for an invalid saved configuration.
    """
    if project is None:
        raise ValueError("Freshdesk ticketChanged Triggers require a loaded project")
    _connection_client(connection)
    connection_name = connection.reference.name
    routes: list[TicketChangedTriggerRoute] = []
    for route in config.ticket_changed_routes:
        if route is None or route.target is None:
            raise ValueError("Freshdesk ticketChanged binding target is required")
        key = TriggerInboxKey(CONNECTOR_ID, connection_name, _TRIGGER_NAME, route.binding_name)
        try:
            configuration = decode_trigger_configuration(
                project.configuration, key, decode_ticket_changed_trigger_configuration
            )
        except ObjectNotFoundError:
            # An unsaved binding delivers every change kind, as an empty configuration does.
            configuration = TicketChangedTriggerConfiguration()
        except ValueError as error:
            raise ValueError(
                f"freshdesk ticketChanged binding {json.dumps(route.binding_name)} configuration: {error}"
            ) from error
        target = new_durable_trigger_target(
            project.trigger_inbox(key),
            route.target,
            decode_payload=decode_ticket_changed_event_payload,
            logger=config.logger,
        )
        cursor = project.poll_cursor_store(PollCursorKey.from_trigger_inbox_key(key))
        routes.append(TicketChangedTriggerRoute(route.binding_name, target, configuration, cursor))
    return new_ticket_trigger_runner(TicketTriggerRunnerConfig(connection, routes, config.logger))


def new_ticket_changed_trigger(
    *,
    connection: Connection,
    connection_name: str,
    binding_name: str,
    target: TriggerTarget[TicketChangedEvent],
    configuration: TicketChangedTriggerConfiguration | None = None,
    logger: TriggerLogger | None = None,
) -> TriggerRunner:
    """Create one ticketChanged Trigger with an in-memory cursor, as Go's `NewTicketChangedTrigger`.

    It has no project storage, so it records a new baseline on every start and changes made while it is
    stopped are lost; use `new_project_ticket_trigger_runner` for durable delivery. Raises ValueError when
    connection_name differs from the connection's name, for a mock connection, or for an invalid
    configuration.
    """
    client = _connection_client(connection)
    if connection_name != connection.reference.name:
        raise ValueError(
            f"freshdesk connector trigger connection_name {json.dumps(connection_name)} must equal its "
            f"connection's name {json.dumps(connection.reference.name)}"
        )
    configuration = configuration if configuration is not None else TicketChangedTriggerConfiguration()
    validate_ticket_changed_trigger_configuration(configuration)
    binding = TriggerBindingRef(connection.reference, TICKET_CHANGED_TRIGGER_DEFINITION.trigger, binding_name)
    source = new_binding_trigger_source(
        BindingPollerConfig(
            definition=TICKET_CHANGED_TRIGGER_DEFINITION,
            binding=binding,
            cursor=new_memory_cursor_store(),
            fetcher=TicketChangeFetcher(client, connection.reference, configuration),
        ),
        _poll_schedule(client),
    )
    return new_trigger(
        TriggerConfig(
            definition=TICKET_CHANGED_TRIGGER_DEFINITION,
            binding=binding,
            source=source,
            target=target,
            logger=logger,
        )
    )


__all__ = [
    "TICKET_CHANGED_TRIGGER_DEFINITION",
    "TICKET_CHANGE_CREATED",
    "TICKET_CHANGE_OVERLAP",
    "TICKET_CHANGE_UPDATED",
    "ProjectTicketChangedTriggerRoute",
    "ProjectTicketTriggerRunnerConfig",
    "TicketChangeFetcher",
    "TicketChangedTriggerRoute",
    "TicketTriggerRunnerConfig",
    "decode_ticket_changed_event_payload",
    "decode_ticket_changed_trigger_configuration",
    "define_ticket_changed_trigger_binding",
    "delivers_ticket_change_kind",
    "new_project_ticket_trigger_runner",
    "new_ticket_changed_trigger",
    "new_ticket_trigger_runner",
    "validate_ticket_changed_trigger_configuration",
]
