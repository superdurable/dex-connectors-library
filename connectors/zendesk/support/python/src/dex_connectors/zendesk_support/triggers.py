# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The Zendesk Support ticketChanged poll Trigger: one Flow per ticket created, updated, or deleted.

The runners read Zendesk's cursor-based incremental ticket export on the Python SDK's durable poll Trigger
kit at parity with the Go connector. Their cursor documents and Trigger inbox payloads match the Go
connector's byte for byte, so Go and Python replicas share one binding.
"""

import json
from collections.abc import Sequence
from dataclasses import dataclass, field

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
from .ticket_export_fetcher import (
    TICKET_CHANGE_CREATED,
    TICKET_CHANGE_DELETED,
    TICKET_CHANGE_UPDATED,
    TICKET_STATUS_DELETED,
    TicketExportCursorInvalidError,
    TicketExportFetcher,
    ticket_change_event_id,
    ticket_change_kind,
)

_TRIGGER_NAME = TICKET_CHANGED_TRIGGER_DEFINITION.trigger.trigger_name


def decode_ticket_changed_event_payload(payload_json: str, payload_bytes: bytes) -> TicketChangedEvent:
    """Decode a replayed Trigger inbox payload that the Go or Python connector recorded."""
    decoded = (
        dex.CodecRegistry().resolve(TicketChangedEvent).decode(dex.Value(dex.WireKind.JSON, payload_json))
    )
    if not isinstance(decoded, TicketChangedEvent):
        raise ValueError("pending Zendesk ticketChanged event is invalid")
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
    """Selects whether deleted tickets are delivered; the default delivers created and updated tickets."""
    cursor: CursorStore | None = None
    """Stores the binding's export cursor, such as `LoadedProject.poll_cursor_store`.

    None keeps it in memory, so every process start records a new baseline and changes made while stopped
    are lost.
    """


@dataclass(frozen=True, slots=True)
class TicketTriggerRunnerConfig:
    """Configures every Zendesk Trigger binding polled through one connection."""

    connection: Connection
    """A provider connection from `new_connection` or `new_project_connection`; a mock has no client."""
    ticket_changed_routes: Sequence[TicketChangedTriggerRoute]
    """The ticketChanged bindings; at least one is required."""
    logger: TriggerLogger | None = None
    """Receives seeds, reseeds, failed polls, and rate-limit pauses; None uses standard logging.

    Records carry binding identity and never ticket payloads, cursors, or credentials.
    """


def _connection_client(connection: Connection) -> Client:
    if connection is None:
        raise ValueError("Zendesk ticketChanged connection is required")
    client = connection._client
    if client is None:
        raise ValueError("Zendesk ticketChanged Triggers require a provider connection, not a mock")
    return client


def _poll_schedule(client: Client) -> PollSchedule:
    # A zero maximum backoff uses the kit's cap, which grows with a long poll interval.
    return PollSchedule(client.poll_interval)


def new_ticket_trigger_runner(config: TicketTriggerRunnerConfig) -> PollRunner:
    """Create one poll runner for the configured ticketChanged bindings.

    `run` replays each durable target, records a baseline for a binding without a cursor, then polls every
    binding each `pollInterval` with jitter. Requests of every binding share the client's incremental
    export request rate (`Client(incremental_export_requests_per_minute=...)`). A cursor Zendesk rejects
    restarts the export from the cursor's last change time, so nothing is skipped; an undecodable cursor
    records a new baseline and logs a WARN record. A 429 pauses every binding of the connection for
    Retry-After, or backs off exponentially without it; other failures, such as a 403 for a connection
    that is not an administrator, back off per binding up to 15 minutes. Delivery is at least once, and
    each ticket version keeps one Trigger event ID across refetches. Raises ValueError for a missing or
    mock connection, no routes, or a blank binding name or missing target.
    """
    client = _connection_client(config.connection)
    if not config.ticket_changed_routes:
        raise ValueError("Zendesk ticketChanged routes are required")
    reference = config.connection.reference
    pollers = []
    for route in config.ticket_changed_routes:
        if route is None or not (route.binding_name or "").strip() or route.target is None:
            raise ValueError("Zendesk ticketChanged binding name and target are required")
        pollers.append(
            new_binding_poller(
                BindingPollerConfig(
                    definition=TICKET_CHANGED_TRIGGER_DEFINITION,
                    binding=TriggerBindingRef(
                        reference, TICKET_CHANGED_TRIGGER_DEFINITION.trigger, route.binding_name
                    ),
                    cursor=route.cursor if route.cursor is not None else new_memory_cursor_store(),
                    fetcher=TicketExportFetcher(client, reference, route.configuration),
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
    """Selects Zendesk Trigger bindings saved in the project configuration."""

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
    binding uses the zero configuration: created and updated tickets only), and each route records
    changes in its binding's project Trigger inbox before its export cursor commits in the binding's
    project poll cursor, so a restarted or replaced replica neither misses nor repeats a change. Raises
    ValueError as `new_ticket_trigger_runner` does and for an invalid saved configuration.
    """
    if project is None:
        raise ValueError("Zendesk ticketChanged Triggers require a loaded project")
    _connection_client(connection)
    connection_name = connection.reference.name
    routes: list[TicketChangedTriggerRoute] = []
    for route in config.ticket_changed_routes:
        if route is None or route.target is None:
            raise ValueError("Zendesk ticketChanged binding target is required")
        key = TriggerInboxKey(CONNECTOR_ID, connection_name, _TRIGGER_NAME, route.binding_name)
        try:
            configuration = decode_trigger_configuration(
                project.configuration, key, decode_ticket_changed_trigger_configuration
            )
        except ObjectNotFoundError:
            # An unsaved binding uses the zero configuration: created and updated tickets only.
            configuration = TicketChangedTriggerConfiguration()
        except ValueError as error:
            raise ValueError(
                f"zendesk-support ticketChanged binding {json.dumps(route.binding_name)} "
                f"configuration: {error}"
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
    connection_name differs from the connection's name or for a mock connection.
    """
    client = _connection_client(connection)
    if connection_name != connection.reference.name:
        raise ValueError(
            f"zendesk-support connector trigger connection_name {json.dumps(connection_name)} must equal "
            f"its connection's name {json.dumps(connection.reference.name)}"
        )
    configuration = configuration if configuration is not None else TicketChangedTriggerConfiguration()
    binding = TriggerBindingRef(connection.reference, TICKET_CHANGED_TRIGGER_DEFINITION.trigger, binding_name)
    source = new_binding_trigger_source(
        BindingPollerConfig(
            definition=TICKET_CHANGED_TRIGGER_DEFINITION,
            binding=binding,
            cursor=new_memory_cursor_store(),
            fetcher=TicketExportFetcher(client, connection.reference, configuration),
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
    "TICKET_CHANGE_DELETED",
    "TICKET_CHANGE_UPDATED",
    "TICKET_STATUS_DELETED",
    "ProjectTicketChangedTriggerRoute",
    "ProjectTicketTriggerRunnerConfig",
    "TicketChangedTriggerRoute",
    "TicketExportCursorInvalidError",
    "TicketExportFetcher",
    "TicketTriggerRunnerConfig",
    "decode_ticket_changed_event_payload",
    "decode_ticket_changed_trigger_configuration",
    "define_ticket_changed_trigger_binding",
    "new_project_ticket_trigger_runner",
    "new_ticket_changed_trigger",
    "new_ticket_trigger_runner",
    "ticket_change_event_id",
    "ticket_change_kind",
]
