# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The recordChanged poll Trigger: each record of one picked table that is created or changed after the
last processed change, as reported by a picked Last modified time field, on the Connector SDK's durable
poll Trigger kit.

`define_record_changed_trigger_binding` declares a binding in a Flow's `get_connector_trigger_bindings`.
`new_project_record_changed_trigger_runner` is the durable path: it records each change in the binding's
project Trigger inbox before the cursor commits in the binding's project poll cursor.
`new_record_changed_trigger_runner` takes targets and cursor stores directly, and
`new_record_changed_trigger` keeps the cursor in memory. Cursor documents and event IDs match the Go
connector's byte for byte, so Go and Python replicas share them.
"""

import json
import re
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

from ._generated.definitions import CONNECTOR_ID, RECORD_CHANGED_TRIGGER_DEFINITION
from ._generated.triggers import (
    decode_record_changed_trigger_configuration,
    define_record_changed_trigger_binding,
)
from ._record_changed_fetcher import RecordChangedFetcher
from .client import Client
from .connection import Connection
from .models import RecordChangedEvent, RecordChangedTriggerConfiguration

__all__ = [
    "RECORD_CHANGED_TRIGGER_DEFINITION",
    "ProjectRecordChangedTriggerRoute",
    "RecordChangedEvent",
    "RecordChangedTriggerConfiguration",
    "RecordChangedTriggerRoute",
    "RecordChangedTriggerRunnerConfig",
    "decode_record_changed_event_payload",
    "decode_record_changed_trigger_configuration",
    "define_record_changed_trigger_binding",
    "new_project_record_changed_trigger_runner",
    "new_record_changed_trigger",
    "new_record_changed_trigger_runner",
    "validate_record_changed_trigger_configuration",
]

_TRIGGER_NAME: Final = RECORD_CHANGED_TRIGGER_DEFINITION.trigger.trigger_name
_RECORD_CHANGED_EVENT_CODEC = dex.CodecRegistry().resolve(RecordChangedEvent)
# Caps the wait after consecutive failed polls, or the poll interval when that is longer.
_RECORD_CHANGED_MAXIMUM_BACKOFF: Final = timedelta(minutes=5)
_BASE_ID: Final = re.compile(r"app[0-9A-Za-z]{14}", re.ASCII)
_TABLE_ID: Final = re.compile(r"tbl[0-9A-Za-z]{14}", re.ASCII)
_FIELD_ID: Final = re.compile(r"fld[0-9A-Za-z]{14}", re.ASCII)


def validate_record_changed_trigger_configuration(configuration: RecordChangedTriggerConfiguration) -> None:
    """Require the base, table, and Last modified time field IDs; the picked names are informational.

    Raises ValueError naming the first invalid ID.
    """
    if not _BASE_ID.fullmatch(configuration.base_id):
        raise ValueError("Airtable recordChanged baseId must be app followed by 14 letters and digits")
    if not _TABLE_ID.fullmatch(configuration.table_id):
        raise ValueError("Airtable recordChanged tableId must be tbl followed by 14 letters and digits")
    if not _FIELD_ID.fullmatch(configuration.field_id):
        raise ValueError("Airtable recordChanged fieldId must be fld followed by 14 letters and digits")


def decode_record_changed_event_payload(payload_json: str, payload_bytes: bytes) -> RecordChangedEvent:
    """Decode a replayed recordChanged payload from a project Trigger inbox, written by Go or Python.

    `new_project_record_changed_trigger_runner` passes it to `new_durable_trigger_target`; pass it as
    `decode_payload` when wiring a durable target yourself. Raises ValueError for a payload that is not a
    RecordChangedEvent.
    """
    try:
        decoded = _RECORD_CHANGED_EVENT_CODEC.decode(dex.Value(dex.WireKind.JSON, payload_json))
    except (TypeError, ValueError, dex.ValueMappingError) as error:
        raise ValueError("stored Airtable recordChanged payload is invalid") from error
    if not isinstance(decoded, RecordChangedEvent):
        raise ValueError("stored Airtable recordChanged payload is invalid")
    return decoded


@dataclass(frozen=True, slots=True)
class RecordChangedTriggerRoute:
    """Binds one recordChanged binding's configuration and cursor to its application target."""

    binding_name: str
    """Names the configured Trigger binding; it is unique within one runner."""
    configuration: RecordChangedTriggerConfiguration
    """Selects the polled table and its Last modified time field."""
    cursor: CursorStore
    """Persists the binding's last processed change, normally `LoadedProject.poll_cursor_store`.

    Use `new_memory_cursor_store` only when changes made while the process is stopped may be skipped.
    """
    target: TriggerTarget[RecordChangedEvent]
    """Receives each changed record.

    A target from `new_durable_trigger_target` records it before the cursor moves past it; any other target
    consumes it first.
    """


@dataclass(frozen=True, slots=True)
class RecordChangedTriggerRunnerConfig:
    """Configures every recordChanged binding of one Airtable connection."""

    connection: Connection
    """A connection from `new_connection` or `new_project_connection`, whose pollInterval and pollPageSize
    apply; a mock connection is refused. The caller keeps it and closes it after the runner stops."""
    routes: Sequence[RecordChangedTriggerRoute]
    """Polled in this order whenever several are due; at least one is required."""
    request_limiter: RequestLimiter | None = None
    """Optionally paces every poll request, for example to share Airtable's 50 requests per second per
    token."""
    logger: TriggerLogger | None = None
    """Receives seeded and committed cursors, failed polls, cursor resets, and delivery outcomes, with the
    binding identity and never record fields, field names, or the token. None uses the standard logging
    adapter."""


def new_record_changed_trigger_runner(config: RecordChangedTriggerRunnerConfig) -> PollRunner:
    """Validate every route and build one poll runner for the connection; it sends and reads nothing.

    `run` replays each durable target, then each binding first reads the base schema to find the Last
    modified time field's current name and seeds its cursor at the table's most recent change, so existing
    records never start Flows. Later polls list up to pollPageSize records modified since the cursor,
    oldest change first, and deliver each before the cursor commits. The cursor stores the latest delivered
    modification time and the records delivered in the five seconds before it, so a change that becomes
    visible slightly late is still delivered and a record is delivered once per change; a long scan
    resumes from its stored Airtable offset. A failed poll backs off up to five minutes, and HTTP 429
    pauses every binding of the connection for at least Airtable's 30 seconds. Delivery is at least once,
    and each event ID is a pure function of the record ID and its modification time. Raises ValueError for
    a mock connection, no routes, a blank or repeated binding name, a missing cursor or target, or an
    invalid configuration.
    """
    client = _connection_client(config.connection)
    if not config.routes:
        raise ValueError("Airtable recordChanged Trigger routes are required")
    pollers: list[Poller[TriggerEvent[RecordChangedEvent]]] = []
    for route in config.routes:
        if not (route.binding_name or "").strip() or route.target is None or route.cursor is None:
            raise ValueError("Airtable recordChanged binding name, cursor, and target are required")
        _validate_route_configuration(route.binding_name, route.configuration)
        binding = TriggerBindingRef(
            config.connection.reference, RECORD_CHANGED_TRIGGER_DEFINITION.trigger, route.binding_name
        )
        pollers.append(
            new_binding_poller(
                BindingPollerConfig(
                    RECORD_CHANGED_TRIGGER_DEFINITION,
                    binding,
                    route.cursor,
                    RecordChangedFetcher(client, binding, route.configuration),
                    route.target,
                )
            )
        )
    return PollRunner(
        PollRunnerConfig(
            config.connection.reference,
            _poll_schedule(client),
            pollers,
            request_limiter=config.request_limiter,
            logger=config.logger,
        )
    )


@dataclass(frozen=True, slots=True)
class ProjectRecordChangedTriggerRoute:
    """Binds one recordChanged binding saved in the project configuration to its target."""

    binding_name: str
    """Names the binding whose base, table, and field Dex Web saved."""
    target: TriggerTarget[RecordChangedEvent]
    """Receives each changed record after the binding's durable project inbox records it, usually
    `new_dex_flow_trigger_target`."""


def new_project_record_changed_trigger_runner(
    project: LoadedProject,
    connection: Connection,
    routes: Sequence[ProjectRecordChangedTriggerRoute],
    *,
    logger: TriggerLogger | None = None,
) -> PollRunner:
    """Build a durable runner for bindings saved in project, polled through connection.

    connection must be the project connection the bindings name, from `new_project_connection`; the caller
    keeps it and closes it after the runner stops. Each route loads its binding's saved configuration, and
    each binding's cursor lives in project storage beside its Trigger inbox: every change is recorded in
    that inbox before the cursor commits past it, so a restarted or replaced replica neither misses nor
    replays a change. logger also receives the inboxes' records. Raises ValueError for a missing target, a
    binding without a saved configuration, or an invalid configuration.
    """
    if project is None:
        raise ValueError("Airtable recordChanged runner requires the loaded project configuration")
    _connection_client(connection)
    durable_routes: list[RecordChangedTriggerRoute] = []
    for route in routes:
        name = json.dumps(route.binding_name)
        if route.target is None:
            raise ValueError(f"Airtable recordChanged binding {name} target is required")
        key = TriggerInboxKey(CONNECTOR_ID, connection.reference.name, _TRIGGER_NAME, route.binding_name)
        try:
            configuration = decode_trigger_configuration(
                project.configuration, key, decode_record_changed_trigger_configuration
            )
        except ObjectNotFoundError as error:
            raise ValueError(
                f"Airtable recordChanged binding {name} has no saved configuration; "
                "pick the base, table, and field in Dex Web and restart"
            ) from error
        except ValueError as error:
            raise ValueError(f"Airtable recordChanged binding {name} configuration: {error}") from error
        target = new_durable_trigger_target(
            project.trigger_inbox(key),
            route.target,
            decode_payload=decode_record_changed_event_payload,
            logger=logger,
        )
        cursor = project.poll_cursor_store(PollCursorKey.from_trigger_inbox_key(key))
        durable_routes.append(RecordChangedTriggerRoute(route.binding_name, configuration, cursor, target))
    return new_record_changed_trigger_runner(
        RecordChangedTriggerRunnerConfig(connection, durable_routes, logger=logger)
    )


def new_record_changed_trigger(
    *,
    connection: Connection,
    binding_name: str,
    configuration: RecordChangedTriggerConfiguration,
    target: TriggerTarget[RecordChangedEvent],
    logger: TriggerLogger | None = None,
) -> TriggerRunner:
    """Create one recordChanged binding's TriggerRunner with its cursor in process memory.

    A restart seeds again, so changes made while the process is stopped are not delivered; use it for
    tests and demonstrations, and `new_project_record_changed_trigger_runner` otherwise. Raises ValueError
    as `new_record_changed_trigger_runner` does.
    """
    client = _connection_client(connection)
    if not (binding_name or "").strip() or target is None:
        raise ValueError("Airtable recordChanged binding name, cursor, and target are required")
    _validate_route_configuration(binding_name, configuration)
    binding = TriggerBindingRef(connection.reference, RECORD_CHANGED_TRIGGER_DEFINITION.trigger, binding_name)
    source = new_binding_trigger_source(
        BindingPollerConfig(
            RECORD_CHANGED_TRIGGER_DEFINITION,
            binding,
            new_memory_cursor_store(),
            RecordChangedFetcher(client, binding, configuration),
        ),
        _poll_schedule(client),
    )
    return new_trigger(TriggerConfig(RECORD_CHANGED_TRIGGER_DEFINITION, binding, source, target, logger))


def _validate_route_configuration(
    binding_name: str, configuration: RecordChangedTriggerConfiguration
) -> None:
    try:
        validate_record_changed_trigger_configuration(configuration)
    except ValueError as error:
        raise ValueError(f"Airtable recordChanged binding {json.dumps(binding_name)}: {error}") from error


def _poll_schedule(client: Client) -> PollSchedule:
    interval = timedelta(seconds=client.poll_interval_seconds)
    return PollSchedule(interval, maximum_backoff=max(_RECORD_CHANGED_MAXIMUM_BACKOFF, interval))


def _connection_client(connection: Connection) -> Client:
    # The generated Connection keeps its Client private; the Trigger needs its transport and settings.
    client = connection._client if connection is not None else None
    if client is None:
        raise ValueError(
            "Airtable recordChanged requires a connection built with new_connection or new_project_connection"
        )
    return client
