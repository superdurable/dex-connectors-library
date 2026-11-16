# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Receive each employee BambooHR reports as inserted, updated, or deleted after the last processed change.

The ``employeeChanged`` Trigger polls BambooHR's changed-employees list (``GET /employees/changed``)
every connection ``pollInterval`` with a durable cursor, at behavior parity with the Go connector, whose
cursor documents it reads and writes byte for byte. The first poll records BambooHR's current time and
delivers no earlier changes. `new_project_employee_changed_trigger_runner` records each change in the
binding's project Trigger inbox before the cursor commits, so a restarted replica neither misses nor
repeats one.
"""

import json
from collections.abc import Sequence
from dataclasses import dataclass, field
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

from ._employee_change_fetcher import (
    EMPLOYEE_CHANGE_ACTION_DELETED,
    EMPLOYEE_CHANGE_ACTION_INSERTED,
    EMPLOYEE_CHANGE_ACTION_UPDATED,
    EMPLOYEE_CHANGE_SETTLE_WINDOW,
    EMPLOYEE_CHANGE_TYPES,
    EmployeeChangeFetcher,
    employee_changed_event_id,
    validate_employee_changed_trigger_configuration,
)
from ._generated.definitions import CONNECTOR_ID, EMPLOYEE_CHANGED_TRIGGER_DEFINITION
from ._generated.triggers import (
    decode_employee_changed_trigger_configuration,
    define_employee_changed_trigger_binding,
)
from .client import Client
from .connection import Connection
from .models import EmployeeChangedEvent, EmployeeChangedTriggerConfiguration

UI_UNIT_CHANGE_TYPE_PICKER: Final = "changeTypePicker"
"""The connector UI unit that saves an employeeChanged binding's change type."""
UI_CHANGE_TYPE_PICKER_PORT_CHANGE_TYPE: Final = "changeType"
"""The changeTypePicker output port: inserted, updated, deleted, or empty for every change."""

_TRIGGER_NAME: Final = EMPLOYEE_CHANGED_TRIGGER_DEFINITION.trigger.trigger_name


@dataclass(frozen=True, slots=True)
class EmployeeChangedTriggerRoute:
    """Binds one ``employeeChanged`` configuration to its application target and cursor."""

    binding_name: str
    """The stable application binding name, unique within the runner."""
    target: TriggerTarget[EmployeeChangedEvent]
    """Routes each change into the application.

    A target wrapped with `new_durable_trigger_target` records each change before the cursor commits past
    it; any other target consumes it first.
    """
    configuration: EmployeeChangedTriggerConfiguration = field(
        default_factory=EmployeeChangedTriggerConfiguration
    )
    """Selects the change type the binding delivers; the default delivers every change."""
    cursor: CursorStore | None = None
    """Stores the binding's position, such as `LoadedProject.poll_cursor_store`.

    None keeps it in memory, so every process start begins at the current time and changes made while
    stopped are lost.
    """


@dataclass(frozen=True, slots=True)
class ProjectEmployeeChangedTriggerRoute:
    """Selects one ``employeeChanged`` binding saved in the project configuration."""

    binding_name: str
    """Names the saved binding."""
    target: TriggerTarget[EmployeeChangedEvent]
    """Routes each change into the application, usually `new_dex_flow_trigger_target`."""


def new_employee_changed_trigger_runner(
    connection: Connection,
    routes: Sequence[EmployeeChangedTriggerRoute],
    *,
    request_limiter: RequestLimiter | None = None,
    logger: TriggerLogger | None = None,
) -> PollRunner:
    """Create one poll runner for the connection's ``employeeChanged`` bindings.

    `PollRunner.run` replays each durable target, starts a binding without a cursor at BambooHR's current
    time, then polls every binding each `Client.poll_interval` with jitter. Each poll makes one
    ``GET /employees/changed`` request and delivers the changes after the cursor that are at least 30
    seconds old by BambooHR's Date header, oldest first, at most 500 per page; further pages are read at
    once, up to the kit's page cap per poll. A 429 or 503 pauses every binding of the connection for
    Retry-After, or backs off exponentially without it. A 400 rejecting the cursor starts the binding
    again at the current time and logs a WARN record; changes in between are not delivered. Other
    failures back off per binding. Delivery is at least once, and each change keeps one Trigger event ID
    across refetches. request_limiter optionally paces every poll request; logger receives poll records
    without employee data, API keys, or cursor tokens. Raises ValueError for a connection without a
    client, no routes, a blank binding name, a missing target, or an invalid configuration.
    """
    client = _connection_client(connection)
    if not routes:
        raise ValueError("BambooHR employeeChanged routes are required")
    pollers: list[Poller[TriggerEvent[EmployeeChangedEvent]]] = [
        _new_employee_changed_poller(connection, client, route) for route in routes
    ]
    return PollRunner(
        PollRunnerConfig(
            connection.reference,
            _poll_schedule(client),
            pollers,
            request_limiter=request_limiter,
            logger=logger,
        )
    )


def new_project_employee_changed_trigger_runner(
    project: LoadedProject,
    connection: Connection,
    routes: Sequence[ProjectEmployeeChangedTriggerRoute],
    *,
    logger: TriggerLogger | None = None,
) -> PollRunner:
    """Create a durable runner for ``employeeChanged`` bindings saved in the loaded project.

    connection is the one `new_project_connection` opened from project; the caller keeps owning it. Each
    route loads its binding's saved change type (an unsaved binding delivers every change) and records
    changes in the binding's project Trigger inbox before its cursor commits in the binding's project poll
    cursor, so a restarted or replaced replica neither misses nor repeats a change. The inbox payload and
    cursor are the Go connector's JSON, so Go and Python replicas share them. Raises ValueError for an
    invalid saved configuration and as `new_employee_changed_trigger_runner` does.
    """
    if project is None:
        raise ValueError("BambooHR employeeChanged runner requires the loaded project configuration")
    durable_routes: list[EmployeeChangedTriggerRoute] = []
    for route in routes:
        if route.target is None:
            raise ValueError(
                f"BambooHR employeeChanged binding {json.dumps(route.binding_name)} target is required"
            )
        key = TriggerInboxKey(CONNECTOR_ID, connection.reference.name, _TRIGGER_NAME, route.binding_name)
        try:
            configuration = decode_trigger_configuration(
                project.configuration, key, decode_employee_changed_trigger_configuration
            )
        except ObjectNotFoundError:
            # An unsaved binding delivers every change, as an empty changeTypePicker does.
            configuration = EmployeeChangedTriggerConfiguration()
        except ValueError as error:
            raise ValueError(
                f"bamboohr employeeChanged binding {json.dumps(route.binding_name)} configuration: {error}"
            ) from error
        target = new_durable_trigger_target(
            project.trigger_inbox(key),
            route.target,
            decode_payload=decode_employee_changed_event_payload,
            logger=logger,
        )
        durable_routes.append(
            EmployeeChangedTriggerRoute(
                route.binding_name,
                target,
                configuration,
                project.poll_cursor_store(PollCursorKey.from_trigger_inbox_key(key)),
            )
        )
    return new_employee_changed_trigger_runner(connection, durable_routes, logger=logger)


def new_employee_changed_trigger(
    *,
    connection: Connection,
    target: TriggerTarget[EmployeeChangedEvent],
    configuration: EmployeeChangedTriggerConfiguration | None = None,
    logger: TriggerLogger | None = None,
) -> TriggerRunner:
    """Create a single ``employeeChanged`` Trigger runner, as Go's generated NewEmployeeChangedTrigger does.

    It has no project storage, so it keeps the cursor in memory and starts at the current time on every
    start; changes made while stopped are lost. Use `new_project_employee_changed_trigger_runner` for
    durable delivery. As in Go, the binding is named after the Trigger. configuration defaults to every
    change. Raises ValueError as `new_employee_changed_trigger_runner` does.
    """
    client = _connection_client(connection)
    if target is None:
        raise ValueError("BambooHR employeeChanged target is required")
    configuration = configuration or EmployeeChangedTriggerConfiguration()
    _validate_configuration(configuration)
    binding = TriggerBindingRef(
        connection.reference, EMPLOYEE_CHANGED_TRIGGER_DEFINITION.trigger, _TRIGGER_NAME
    )
    source = new_binding_trigger_source(
        BindingPollerConfig(
            EMPLOYEE_CHANGED_TRIGGER_DEFINITION,
            binding,
            new_memory_cursor_store(),
            EmployeeChangeFetcher(client, connection.reference, configuration),
        ),
        _poll_schedule(client),
    )
    return new_trigger(TriggerConfig(EMPLOYEE_CHANGED_TRIGGER_DEFINITION, binding, source, target, logger))


def decode_employee_changed_event_payload(payload_json: str, payload_bytes: bytes) -> EmployeeChangedEvent:
    """Decode a replayed inbox payload, written by a Go or Python replica, as an EmployeeChangedEvent.

    Raises ValueError for a payload that is not an employeeChanged event.
    """
    try:
        decoded = CodecRegistry().resolve(EmployeeChangedEvent).decode(Value(WireKind.JSON, payload_json))
    except (TypeError, ValueError) as error:
        raise ValueError("stored BambooHR employeeChanged payload is invalid") from error
    if not isinstance(decoded, EmployeeChangedEvent):
        raise ValueError("stored BambooHR employeeChanged payload is invalid")
    return decoded


def _new_employee_changed_poller(
    connection: Connection, client: Client, route: EmployeeChangedTriggerRoute
) -> Poller[TriggerEvent[EmployeeChangedEvent]]:
    if not (route.binding_name or "").strip() or route.target is None:
        raise ValueError("BambooHR employeeChanged binding name and target are required")
    _validate_configuration(route.configuration)
    binding = TriggerBindingRef(
        connection.reference, EMPLOYEE_CHANGED_TRIGGER_DEFINITION.trigger, route.binding_name
    )
    return new_binding_poller(
        BindingPollerConfig(
            EMPLOYEE_CHANGED_TRIGGER_DEFINITION,
            binding,
            route.cursor if route.cursor is not None else new_memory_cursor_store(),
            EmployeeChangeFetcher(client, connection.reference, route.configuration),
            route.target,
        )
    )


def _validate_configuration(configuration: EmployeeChangedTriggerConfiguration) -> None:
    if configuration is None:
        raise ValueError("bamboohr employeeChanged configuration is required")
    validate_employee_changed_trigger_configuration(configuration)


def _connection_client(connection: Connection) -> Client:
    client = getattr(connection, "_client", None) if connection is not None else None
    if not isinstance(client, Client):
        raise ValueError(
            "BambooHR employeeChanged Trigger requires a connection opened with a BambooHR client"
        )
    return client


def _poll_schedule(client: Client) -> PollSchedule:
    # A zero maximum backoff uses the kit's cap, which grows with a long poll interval.
    return PollSchedule(client.poll_interval)


__all__ = [
    "EMPLOYEE_CHANGED_TRIGGER_DEFINITION",
    "EMPLOYEE_CHANGE_ACTION_DELETED",
    "EMPLOYEE_CHANGE_ACTION_INSERTED",
    "EMPLOYEE_CHANGE_ACTION_UPDATED",
    "EMPLOYEE_CHANGE_SETTLE_WINDOW",
    "EMPLOYEE_CHANGE_TYPES",
    "EmployeeChangedTriggerRoute",
    "ProjectEmployeeChangedTriggerRoute",
    "UI_CHANGE_TYPE_PICKER_PORT_CHANGE_TYPE",
    "UI_UNIT_CHANGE_TYPE_PICKER",
    "decode_employee_changed_event_payload",
    "decode_employee_changed_trigger_configuration",
    "define_employee_changed_trigger_binding",
    "employee_changed_event_id",
    "new_employee_changed_trigger",
    "new_employee_changed_trigger_runner",
    "new_project_employee_changed_trigger_runner",
    "validate_employee_changed_trigger_configuration",
]
