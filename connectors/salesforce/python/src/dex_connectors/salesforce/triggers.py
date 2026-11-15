# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Receive each created or modified Salesforce record through the ``recordModifiedPolled`` poll Trigger.

The Trigger polls SOQL ordered by SystemModstamp and Id with the Connector SDK's durable poll kit, at
behavior parity with the Go connector; its cursor documents and event IDs are the Go connector's byte
for byte, so Go and Python replicas can share one binding's cursor and inbox.
`new_project_record_trigger_runner` is the durable path.
"""

import json
from collections.abc import Sequence
from dataclasses import dataclass
from datetime import timedelta
from typing import Final

import httpx
from dex import CodecRegistry, Value, WireKind
from dex_connectors.sdk import (
    ConnectionRef,
    TriggerBindingRef,
    TriggerConfig,
    TriggerContext,
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

from ._generated.definitions import CONNECTOR_ID, RECORD_MODIFIED_POLLED_TRIGGER_DEFINITION
from ._generated.triggers import (
    decode_record_modified_polled_trigger_configuration,
    define_record_modified_polled_trigger_binding,
)
from .client import Client
from .connection import Connection, new_project_connection
from .models import RecordModifiedEvent, RecordModifiedTriggerConfiguration
from .record_modified_fetcher import RecordPollError, RecordPollFetcher
from .record_modified_trigger import (
    MAX_TRIGGER_FIELD_NAMES,
    RECORD_CHANGE_CREATED,
    RECORD_CHANGE_UPDATED,
    validate_record_modified_trigger_configuration,
)

UI_UNIT_S_OBJECT_PICKER: Final = "sObjectPicker"
"""Salesforce object picker UI unit, which saves the object API name."""
UI_S_OBJECT_PICKER_PORT_S_OBJECT_TYPE: Final = "sObjectType"
"""Object picker output port."""
UI_UNIT_FIELD_NAMES_INPUT: Final = "fieldNamesInput"
"""Comma-separated field API name list UI unit."""
UI_FIELD_NAMES_INPUT_PORT_FIELD_NAMES: Final = "fieldNames"
"""Field name list output port."""

_TRIGGER: Final = RECORD_MODIFIED_POLLED_TRIGGER_DEFINITION.trigger


@dataclass(frozen=True, slots=True)
class RecordModifiedPolledTriggerRoute:
    """Binds one recordModifiedPolled configuration to its application target and cursor."""

    binding_name: str
    """The stable application binding name, unique within the runner."""
    configuration: RecordModifiedTriggerConfiguration
    """Selects the watched object and the extra fields read with each record."""
    target: TriggerTarget[RecordModifiedEvent]
    """Routes each record change into the application.

    A target that prepares deliveries, such as `new_durable_trigger_target`, records each change before
    the cursor commits past it; any other target consumes it first.
    """
    cursor: CursorStore | None = None
    """Stores the binding's (SystemModstamp, Id) position, such as `LoadedProject.poll_cursor_store`.

    None keeps it in memory, so every process start records a new baseline and changes made while
    stopped are lost.
    """


def new_record_trigger_runner(
    connection: Connection,
    routes: Sequence[RecordModifiedPolledTriggerRoute],
    logger: TriggerLogger | None = None,
) -> PollRunner:
    """Create one poll runner for the recordModifiedPolled bindings of a provider connection.

    `run` replays each durable target, records a baseline for a binding without a cursor, then polls
    every binding each pollInterval with jitter, reading at most pollPageSize records per request while
    a backlog remains. REQUEST_LIMIT_EXCEEDED or HTTP 429 pauses every binding of the connection for
    Retry-After, or backs off exponentially without it; other failures, such as an object or field the
    user cannot read, back off per binding. Delivery is at least once, and each record change keeps one
    Trigger event ID across refetches. logger, or else the client's logger, receives the records.
    Raises ValueError for a connection without a Salesforce client, no routes, or an invalid route.
    """
    client = _salesforce_client(connection)
    if not routes:
        raise ValueError("Salesforce recordModifiedPolled routes are required")
    pollers: list[Poller[object]] = [
        _new_record_modified_polled_poller(client, connection.reference, route) for route in routes
    ]
    return PollRunner(
        PollRunnerConfig(
            connection.reference,
            _poll_schedule(client),
            pollers,
            logger=logger if logger is not None else client.trigger_logger,
        )
    )


@dataclass(frozen=True, slots=True)
class ProjectRecordModifiedPolledTriggerRoute:
    """Selects one recordModifiedPolled binding saved in the project configuration."""

    binding_name: str
    """Names the saved binding."""
    target: TriggerTarget[RecordModifiedEvent]
    """Routes each record change into the application, usually `new_dex_flow_trigger_target`."""


class ProjectRecordTriggerRunner:
    """A durable recordModifiedPolled runner over a project connection it owns; close it with `aclose`."""

    def __init__(self, runner: PollRunner, connection: Connection) -> None:
        """Wrap the poll runner and the connection it polls through."""
        self._runner = runner
        self._connection = connection

    async def run(self, context: TriggerContext | None = None) -> None:
        """Replay each binding's inbox, then poll until the context is cancelled, as `PollRunner.run`."""
        await self._runner.run(context)

    def wake_poller(self, binding_name: str) -> None:
        """Make the binding due now; see `PollRunner.wake_poller`."""
        self._runner.wake_poller(binding_name)

    async def aclose(self) -> None:
        """Close the owned connection after `run` returns."""
        await self._connection.aclose()


async def new_project_record_trigger_runner(
    project: LoadedProject,
    connection_name: str,
    routes: Sequence[ProjectRecordModifiedPolledTriggerRoute],
    http_client: httpx.AsyncClient | None = None,
    logger: TriggerLogger | None = None,
) -> ProjectRecordTriggerRunner:
    """Open connection_name and create a durable runner for each route's saved binding.

    Each route records changes in its binding's project Trigger inbox before its cursor commits in the
    binding's project poll cursor, so a restarted or replaced replica neither misses nor repeats a
    change. logger also receives the inboxes' records. Raises ValueError for a binding without a saved
    object or with an invalid configuration. An injected http_client stays owned by its caller.
    """
    connection = new_project_connection(project, connection_name, http_client)
    try:
        routes_with_storage = [_project_route(project, connection, route, logger) for route in routes]
        runner = new_record_trigger_runner(connection, routes_with_storage, logger)
    except BaseException:
        await connection.aclose()
        raise
    return ProjectRecordTriggerRunner(runner, connection)


def new_record_modified_polled_trigger(
    *,
    connection: Connection,
    connection_name: str,
    binding_name: str,
    configuration: RecordModifiedTriggerConfiguration,
    target: TriggerTarget[RecordModifiedEvent],
    logger: TriggerLogger | None = None,
) -> TriggerRunner:
    """Create one recordModifiedPolled Trigger polled every pollInterval.

    It has no project storage, so it keeps the cursor in memory and records a new baseline on every
    start; use `new_project_record_trigger_runner` for durable polling. Raises ValueError for an invalid
    configuration or a connection name that differs from the connection's.
    """
    client = _salesforce_client(connection)
    reference = connection.reference
    if connection_name != reference.name:
        raise ValueError(
            f"salesforce connector trigger connection_name {connection_name!r} must equal its "
            f"connection's name {reference.name!r}"
        )
    _validate(configuration)
    source = new_binding_trigger_source(
        BindingPollerConfig(
            RECORD_MODIFIED_POLLED_TRIGGER_DEFINITION,
            TriggerBindingRef(reference, _TRIGGER, _TRIGGER.trigger_name),
            new_memory_cursor_store(),
            RecordPollFetcher(client, reference, configuration),
        ),
        _poll_schedule(client),
    )
    return new_trigger(
        TriggerConfig(
            RECORD_MODIFIED_POLLED_TRIGGER_DEFINITION,
            TriggerBindingRef(reference, _TRIGGER, binding_name),
            source,
            target,
            logger if logger is not None else client.trigger_logger,
        )
    )


def decode_record_modified_event_payload(payload_json: str, payload_bytes: bytes) -> RecordModifiedEvent:
    """Decode a replayed inbox payload that this package or the Go connector recorded."""
    decoded = CodecRegistry().resolve(RecordModifiedEvent).decode(Value(WireKind.JSON, payload_json))
    if not isinstance(decoded, RecordModifiedEvent):
        raise ValueError("recordModifiedPolled payload did not decode")
    return decoded


def _project_route(
    project: LoadedProject,
    connection: Connection,
    route: ProjectRecordModifiedPolledTriggerRoute,
    logger: TriggerLogger | None,
) -> RecordModifiedPolledTriggerRoute:
    if route.target is None:
        raise ValueError(
            f"Salesforce recordModifiedPolled binding {json.dumps(route.binding_name)} target is required"
        )
    key = TriggerInboxKey(CONNECTOR_ID, connection.reference.name, _TRIGGER.trigger_name, route.binding_name)
    try:
        configuration = decode_trigger_configuration(
            project.configuration, key, decode_record_modified_polled_trigger_configuration
        )
    except ObjectNotFoundError as error:
        raise ValueError(
            f"salesforce recordModifiedPolled binding {json.dumps(route.binding_name)} has no saved object; "
            "choose one in Dex Web Connections"
        ) from error
    except ValueError as error:
        raise ValueError(
            f"salesforce recordModifiedPolled binding {json.dumps(route.binding_name)} configuration: {error}"
        ) from error
    target = new_durable_trigger_target(
        project.trigger_inbox(key),
        route.target,
        decode_payload=decode_record_modified_event_payload,
        logger=logger if logger is not None else _salesforce_client(connection).trigger_logger,
    )
    cursor = project.poll_cursor_store(PollCursorKey.from_trigger_inbox_key(key))
    return RecordModifiedPolledTriggerRoute(route.binding_name, configuration, target, cursor)


def _new_record_modified_polled_poller(
    client: Client, connection: ConnectionRef, route: RecordModifiedPolledTriggerRoute
) -> Poller[object]:
    if not (route.binding_name or "").strip() or route.target is None:
        raise ValueError("Salesforce recordModifiedPolled binding name and target are required")
    _validate(route.configuration)
    poller: Poller[object] = new_binding_poller(  # type: ignore[assignment]
        BindingPollerConfig(
            RECORD_MODIFIED_POLLED_TRIGGER_DEFINITION,
            TriggerBindingRef(connection, _TRIGGER, route.binding_name),
            route.cursor if route.cursor is not None else new_memory_cursor_store(),
            RecordPollFetcher(client, connection, route.configuration),
            route.target,
        )
    )
    return poller


def _validate(configuration: RecordModifiedTriggerConfiguration) -> None:
    try:
        validate_record_modified_trigger_configuration(configuration)
    except ValueError as error:
        raise ValueError(f"salesforce recordModifiedPolled configuration: {error}") from error


def _poll_schedule(client: Client) -> PollSchedule:
    # A zero maximum backoff uses the kit's cap, which grows with a long poll interval.
    return PollSchedule(timedelta(seconds=client.poll_interval_seconds))


def _salesforce_client(connection: Connection) -> Client:
    client = getattr(connection, "_client", None)
    if not isinstance(client, Client):
        raise ValueError(
            "Salesforce Triggers require a connection from new_project_connection or new_connection"
        )
    return client


__all__ = [
    "MAX_TRIGGER_FIELD_NAMES",
    "RECORD_CHANGE_CREATED",
    "RECORD_CHANGE_UPDATED",
    "RECORD_MODIFIED_POLLED_TRIGGER_DEFINITION",
    "UI_FIELD_NAMES_INPUT_PORT_FIELD_NAMES",
    "UI_S_OBJECT_PICKER_PORT_S_OBJECT_TYPE",
    "UI_UNIT_FIELD_NAMES_INPUT",
    "UI_UNIT_S_OBJECT_PICKER",
    "ProjectRecordModifiedPolledTriggerRoute",
    "ProjectRecordTriggerRunner",
    "RecordModifiedEvent",
    "RecordModifiedPolledTriggerRoute",
    "RecordModifiedTriggerConfiguration",
    "RecordPollError",
    "decode_record_modified_event_payload",
    "decode_record_modified_polled_trigger_configuration",
    "define_record_modified_polled_trigger_binding",
    "new_project_record_trigger_runner",
    "new_record_modified_polled_trigger",
    "new_record_trigger_runner",
    "validate_record_modified_trigger_configuration",
]
