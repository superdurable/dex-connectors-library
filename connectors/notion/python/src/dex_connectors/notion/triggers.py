# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The databaseRowChanged poll Trigger: each row created or edited in one Notion data source, read by
polling the data source query sorted by last edited time, on the Connector SDK's durable poll Trigger kit.

`define_database_row_changed_trigger_binding` declares a binding in a Flow's
`get_connector_trigger_bindings`. `new_project_database_row_trigger_runner` is the durable path: it records
each change in the binding's project Trigger inbox before the cursor commits in the binding's project poll
cursor. `new_database_row_trigger_runner` takes targets and cursor stores directly, and
`new_database_row_changed_trigger` keeps the cursor in memory.
"""

import json
from collections.abc import Sequence
from dataclasses import dataclass
from datetime import timedelta
from enum import StrEnum

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

from ._database_row_cursor import DATABASE_ROW_LOOKBACK_NANOSECONDS, MAXIMUM_SEEN_DATABASE_ROWS
from ._database_row_fetcher import DatabaseRowFetcher
from ._generated.definitions import CONNECTOR_ID, DATABASE_ROW_CHANGED_TRIGGER_DEFINITION
from ._generated.triggers import (
    decode_database_row_changed_trigger_configuration,
    define_database_row_changed_trigger_binding,
)
from .client import Client
from .connection import Connection
from .models import DatabaseRowChangedEvent, DatabaseRowChangedTriggerConfiguration
from .provider_models import parse_id

__all__ = [
    "DATABASE_ROW_CHANGED_TRIGGER_DEFINITION",
    "DATABASE_ROW_LOOKBACK",
    "MAXIMUM_SEEN_DATABASE_ROWS",
    "DatabaseRowChangeKind",
    "DatabaseRowChangedEvent",
    "DatabaseRowChangedTriggerConfiguration",
    "DatabaseRowChangedTriggerRoute",
    "DatabaseRowTriggerRunnerConfig",
    "ProjectDatabaseRowChangedTriggerRoute",
    "ProjectDatabaseRowTriggerRunnerConfig",
    "decode_database_row_changed_event_payload",
    "decode_database_row_changed_trigger_configuration",
    "define_database_row_changed_trigger_binding",
    "new_database_row_changed_trigger",
    "new_database_row_trigger_runner",
    "new_project_database_row_trigger_runner",
    "validate_database_row_changed_trigger_configuration",
]

DATABASE_ROW_LOOKBACK = timedelta(microseconds=DATABASE_ROW_LOOKBACK_NANOSECONDS // 1000)
"""How far before the newest last edited time each poll queries again, so a row Notion indexes late, or
stamps with a time rounded down, is still delivered once."""

_TRIGGER_NAME = DATABASE_ROW_CHANGED_TRIGGER_DEFINITION.trigger.trigger_name
_DATABASE_ROW_CHANGED_EVENT_CODEC = dex.CodecRegistry().resolve(DatabaseRowChangedEvent)


class DatabaseRowChangeKind(StrEnum):
    """The derived kind of one databaseRowChanged delivery: `DatabaseRowChangedEvent.change_kind`."""

    CREATED = "created"
    """A row whose last edited time equals its created time: Notion reports no change type, and a row
    edited within the same minute it was created still reads as created."""
    UPDATED = "updated"
    """Any other edit to a row's properties or content."""


def validate_database_row_changed_trigger_configuration(
    configuration: DatabaseRowChangedTriggerConfiguration,
) -> None:
    """Require `data_source_id` to be a Notion ID or a Notion URL ending in one.

    Raises ValueError naming the problem; a binding without a data source has nothing to watch.
    """
    if not (configuration.data_source_id or "").strip():
        raise ValueError("dataSourceId is required; pick the watched Notion data source in Dex Web")
    try:
        parse_id(configuration.data_source_id)
    except ValueError as error:
        raise ValueError(f"dataSourceId: {error}") from error


def decode_database_row_changed_event_payload(
    payload_json: str, payload_bytes: bytes
) -> DatabaseRowChangedEvent:
    """Decode a replayed databaseRowChanged payload from a project Trigger inbox, written by Go or Python.

    Pass it as `decode_payload` to `new_durable_trigger_target`. Raises ValueError for a payload that is
    not a DatabaseRowChangedEvent.
    """
    try:
        decoded = _DATABASE_ROW_CHANGED_EVENT_CODEC.decode(dex.Value(dex.WireKind.JSON, payload_json))
    except (TypeError, ValueError, dex.ValueMappingError) as error:
        raise ValueError("stored Notion databaseRowChanged payload is invalid") from error
    if not isinstance(decoded, DatabaseRowChangedEvent):
        raise ValueError("stored Notion databaseRowChanged payload is invalid")
    return decoded


@dataclass(frozen=True, slots=True)
class DatabaseRowChangedTriggerRoute:
    """Binds one databaseRowChanged configuration to its application target and cursor."""

    binding_name: str
    """The stable application binding name, unique within the runner."""
    configuration: DatabaseRowChangedTriggerConfiguration
    """Selects the watched data source; its `data_source_id` is required."""
    target: TriggerTarget[DatabaseRowChangedEvent]
    """Routes each change into the application.

    A target from `new_durable_trigger_target` records each change before the cursor commits past it; any
    other target consumes it first.
    """
    cursor: CursorStore | None = None
    """Stores the binding's last-edited-time cursor, such as `LoadedProject.poll_cursor_store`.

    None keeps it in memory, so every process start records a new baseline and changes made while stopped
    are lost.
    """


@dataclass(frozen=True, slots=True)
class DatabaseRowTriggerRunnerConfig:
    """Configures every Notion databaseRowChanged binding polled through one connection."""

    connection: Connection
    """A connection built with `new_connection` or `new_project_connection`; a mock connection is refused."""
    database_row_changed_routes: Sequence[DatabaseRowChangedTriggerRoute]
    """The databaseRowChanged bindings; at least one is required."""
    logger: TriggerLogger | None = None
    """Receives seeds, reseeds, failed polls, rate-limit pauses, and delivery retries with binding identity,
    never row contents or the API token. None uses the standard logging adapter."""


def new_database_row_trigger_runner(config: DatabaseRowTriggerRunnerConfig) -> PollRunner:
    """Create one poll runner for the configured databaseRowChanged bindings of one connection.

    `run` replays each durable target, records a baseline for a binding without a cursor, then queries
    every binding's data source each `pollInterval` with jitter, at most `pollPageSize` rows per request
    and ten requests per poll. A 429 rate_limited or 529 service_overload pauses every binding of the
    connection for Notion's Retry-After seconds, or backs off exponentially without it; a stale start
    cursor restarts the query, a cursor for another data source records a new baseline, and other
    failures, such as a data source that is no longer shared, back off per binding up to 15 minutes.
    Delivery is at least once, and each row version keeps one Trigger event ID across refetches. The
    cursor documents match the Go connector's byte for byte, so Go and Python replicas can share one
    binding's cursor and inbox. Raises ValueError for a mock connection, no routes, a blank binding name,
    a missing target, or an invalid configuration.
    """
    client = _connection_client(config.connection)
    if not config.database_row_changed_routes:
        raise ValueError("Notion databaseRowChanged routes are required")
    pollers: list[Poller[TriggerEvent[DatabaseRowChangedEvent]]] = [
        _new_database_row_changed_poller(config.connection, client, route)
        for route in config.database_row_changed_routes
    ]
    return PollRunner(
        PollRunnerConfig(config.connection.reference, _poll_schedule(client), pollers, logger=config.logger)
    )


@dataclass(frozen=True, slots=True)
class ProjectDatabaseRowChangedTriggerRoute:
    """Selects one databaseRowChanged binding saved in the project configuration."""

    binding_name: str
    """The saved binding's name."""
    target: TriggerTarget[DatabaseRowChangedEvent]
    """Routes each change into the application, usually `new_dex_flow_trigger_target`."""


@dataclass(frozen=True, slots=True)
class ProjectDatabaseRowTriggerRunnerConfig:
    """Selects Notion databaseRowChanged bindings saved in the project configuration."""

    database_row_changed_routes: Sequence[ProjectDatabaseRowChangedTriggerRoute]
    """The databaseRowChanged bindings; at least one is required."""
    logger: TriggerLogger | None = None
    """Receives the runner's and the durable inboxes' records; None uses the standard logging adapter."""


def new_project_database_row_trigger_runner(
    project: LoadedProject, connection: Connection, config: ProjectDatabaseRowTriggerRunnerConfig
) -> PollRunner:
    """Create a durable runner for bindings saved in project, polled through connection.

    connection must be the project connection the bindings name, from `new_project_connection`; the caller
    keeps it and closes it after the runner stops. Each route loads its binding's saved data source (an
    unsaved binding is a configuration error) and records changes in the binding's project Trigger inbox
    before its cursor commits in the binding's project poll cursor, so a restarted or replaced replica
    neither misses nor repeats a change. Under Dex Web (`dexcli dev`) the poll cursor stays in process
    memory; hosted project storage keeps it. Raises ValueError for a missing target, a binding without a
    saved data source, or an invalid configuration.
    """
    if project is None:
        raise ValueError("Notion Trigger runner requires the loaded project configuration")
    _connection_client(connection)
    routes: list[DatabaseRowChangedTriggerRoute] = []
    for route in config.database_row_changed_routes:
        if route.target is None:
            raise ValueError(
                f"Notion databaseRowChanged binding {json.dumps(route.binding_name)} target is required"
            )
        key = TriggerInboxKey(CONNECTOR_ID, connection.reference.name, _TRIGGER_NAME, route.binding_name)
        try:
            configuration = decode_trigger_configuration(
                project.configuration, key, decode_database_row_changed_trigger_configuration
            )
        except ObjectNotFoundError as error:
            raise ValueError(
                f"notion databaseRowChanged binding {json.dumps(route.binding_name)} has no saved data "
                "source; pick one in Dex Web and restart"
            ) from error
        except ValueError as error:
            raise ValueError(
                f"notion databaseRowChanged binding {json.dumps(route.binding_name)} configuration: {error}"
            ) from error
        target = new_durable_trigger_target(
            project.trigger_inbox(key),
            route.target,
            decode_payload=decode_database_row_changed_event_payload,
            logger=config.logger,
        )
        cursor = project.poll_cursor_store(PollCursorKey.from_trigger_inbox_key(key))
        routes.append(DatabaseRowChangedTriggerRoute(route.binding_name, configuration, target, cursor))
    return new_database_row_trigger_runner(DatabaseRowTriggerRunnerConfig(connection, routes, config.logger))


def new_database_row_changed_trigger(
    *,
    connection: Connection,
    binding_name: str,
    configuration: DatabaseRowChangedTriggerConfiguration,
    target: TriggerTarget[DatabaseRowChangedEvent],
    logger: TriggerLogger | None = None,
) -> TriggerRunner:
    """Create one databaseRowChanged binding's TriggerRunner with its cursor in memory.

    It has no project storage, so every start records a new baseline and changes made while the process
    is stopped are lost; use it for tests and demonstrations, and `new_project_database_row_trigger_runner`
    otherwise. Raises ValueError as `new_database_row_trigger_runner` does.
    """
    client = _connection_client(connection)
    _validate_route_configuration(binding_name, configuration)
    if target is None:
        raise ValueError("Notion databaseRowChanged binding name and target are required")
    binding = TriggerBindingRef(
        connection.reference, DATABASE_ROW_CHANGED_TRIGGER_DEFINITION.trigger, binding_name
    )
    source = new_binding_trigger_source(
        BindingPollerConfig(
            DATABASE_ROW_CHANGED_TRIGGER_DEFINITION,
            binding,
            new_memory_cursor_store(),
            DatabaseRowFetcher(client, connection.reference, parse_id(configuration.data_source_id)),
        ),
        _poll_schedule(client),
    )
    return new_trigger(
        TriggerConfig(DATABASE_ROW_CHANGED_TRIGGER_DEFINITION, binding, source, target, logger)
    )


def _new_database_row_changed_poller(
    connection: Connection, client: Client, route: DatabaseRowChangedTriggerRoute
) -> Poller[TriggerEvent[DatabaseRowChangedEvent]]:
    if route.target is None:
        raise ValueError("Notion databaseRowChanged binding name and target are required")
    _validate_route_configuration(route.binding_name, route.configuration)
    return new_binding_poller(
        BindingPollerConfig(
            DATABASE_ROW_CHANGED_TRIGGER_DEFINITION,
            TriggerBindingRef(
                connection.reference, DATABASE_ROW_CHANGED_TRIGGER_DEFINITION.trigger, route.binding_name
            ),
            route.cursor if route.cursor is not None else new_memory_cursor_store(),
            DatabaseRowFetcher(client, connection.reference, parse_id(route.configuration.data_source_id)),
            route.target,
        )
    )


def _validate_route_configuration(
    binding_name: str, configuration: DatabaseRowChangedTriggerConfiguration
) -> None:
    if not (binding_name or "").strip():
        raise ValueError("Notion databaseRowChanged binding name and target are required")
    try:
        validate_database_row_changed_trigger_configuration(configuration)
    except ValueError as error:
        raise ValueError(f"notion databaseRowChanged configuration: {error}") from error


def _poll_schedule(client: Client) -> PollSchedule:
    # A zero maximum backoff uses the kit's cap, which grows with a long poll interval.
    return PollSchedule(timedelta(seconds=client.poll_interval_seconds))


def _connection_client(connection: Connection) -> Client:
    # The generated Connection keeps its Client private; Triggers need its transport and settings.
    client = connection._client if connection is not None else None
    if client is None:
        raise ValueError(
            "Notion Triggers require a connection built with new_connection or new_project_connection"
        )
    return client
