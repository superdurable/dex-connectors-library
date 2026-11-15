# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The taskChanged poll Trigger: each task added to, changed in, removed from, deleted from, or restored to
one Asana project, read through the Asana Events API on the Connector SDK's durable poll Trigger kit.

`define_task_changed_trigger_binding` declares a binding in a Flow's `get_connector_trigger_bindings`.
`new_project_task_trigger_runner` is the durable path: it records each event in the binding's project
Trigger inbox before the sync token commits in the binding's project poll cursor.
`new_task_trigger_runner` takes targets and cursor stores directly, and `new_task_changed_trigger`
keeps the cursor in memory.
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

from ._generated.definitions import CONNECTOR_ID, TASK_CHANGED_TRIGGER_DEFINITION
from ._generated.triggers import (
    decode_task_changed_trigger_configuration,
    define_task_changed_trigger_binding,
)
from ._task_event_fetcher import TaskEventFetcher, is_asana_gid
from .client import Client
from .connection import Connection
from .models import TaskChangedEvent, TaskChangedTriggerConfiguration

__all__ = [
    "TASK_CHANGED_TRIGGER_DEFINITION",
    "ProjectTaskChangedTriggerRoute",
    "ProjectTaskTriggerRunnerConfig",
    "TaskChangeAction",
    "TaskChangedEvent",
    "TaskChangedTriggerConfiguration",
    "TaskChangedTriggerRoute",
    "TaskTriggerRunnerConfig",
    "decode_task_changed_event_payload",
    "decode_task_changed_trigger_configuration",
    "define_task_changed_trigger_binding",
    "new_project_task_trigger_runner",
    "new_task_changed_trigger",
    "new_task_trigger_runner",
    "validate_task_changed_trigger_configuration",
]

_TRIGGER_NAME = TASK_CHANGED_TRIGGER_DEFINITION.trigger.trigger_name
_TASK_CHANGED_EVENT_CODEC = dex.CodecRegistry().resolve(TaskChangedEvent)


class TaskChangeAction(StrEnum):
    """The Asana Events API action of one taskChanged delivery, the value of `TaskChangedEvent.action`."""

    ADDED = "added"
    """A task added to the project, one of its sections, or a parent task; `parent` names the container."""
    CHANGED = "changed"
    """A field change, such as completed, assignee, due_on, or name; `changed_field` names the field."""
    REMOVED = "removed"
    """A task removed from the project, one of its sections, or a parent task; `parent` names it."""
    DELETED = "deleted"
    """A task moved to the Asana trash."""
    UNDELETED = "undeleted"
    """A task restored from the Asana trash."""


def validate_task_changed_trigger_configuration(configuration: TaskChangedTriggerConfiguration) -> None:
    """Require `project_id` to be an Asana gid and `workspace_id` to be blank or a gid.

    Raises ValueError naming the first invalid field.
    """
    if not is_asana_gid(configuration.project_id):
        raise ValueError("taskChanged projectId must be an Asana project gid, digits only")
    if configuration.workspace_id and not is_asana_gid(configuration.workspace_id):
        raise ValueError("taskChanged workspaceId must be blank or an Asana workspace gid")


def decode_task_changed_event_payload(payload_json: str, payload_bytes: bytes) -> TaskChangedEvent:
    """Decode a replayed taskChanged payload from a project Trigger inbox, written by Go or Python.

    Pass it as `decode_payload` to `new_durable_trigger_target`. Raises ValueError for a payload that is
    not a TaskChangedEvent.
    """
    try:
        decoded = _TASK_CHANGED_EVENT_CODEC.decode(dex.Value(dex.WireKind.JSON, payload_json))
    except (TypeError, ValueError, dex.ValueMappingError) as error:
        raise ValueError("stored Asana taskChanged payload is invalid") from error
    if not isinstance(decoded, TaskChangedEvent):
        raise ValueError("stored Asana taskChanged payload is invalid")
    return decoded


@dataclass(frozen=True, slots=True)
class TaskChangedTriggerRoute:
    """Binds one taskChanged configuration to its application target and cursor."""

    binding_name: str
    """The stable application binding name, unique within the runner."""
    configuration: TaskChangedTriggerConfiguration
    """Selects the watched project; its `project_id` is required."""
    target: TriggerTarget[TaskChangedEvent]
    """Routes each change into the application.

    A target from `new_durable_trigger_target` records each change before the cursor commits past it; any
    other target consumes it first.
    """
    cursor: CursorStore | None = None
    """Stores the binding's sync token, such as `LoadedProject.poll_cursor_store`.

    None keeps it in memory, so every process start records a new baseline and changes made while stopped
    are lost.
    """


@dataclass(frozen=True, slots=True)
class TaskTriggerRunnerConfig:
    """Configures every Asana Trigger binding polled through one connection."""

    connection: Connection
    """A connection built with `new_connection` or `new_project_connection`; a mock connection is refused."""
    task_changed_routes: Sequence[TaskChangedTriggerRoute]
    """The taskChanged bindings; at least one is required."""
    logger: TriggerLogger | None = None
    """Receives seeds, reseeds after an expired sync token, failed polls, and rate-limit pauses, with binding
    identity and never payloads or credentials. None uses the standard logging adapter."""


def new_task_trigger_runner(config: TaskTriggerRunnerConfig) -> PollRunner:
    """Create one poll runner for the configured taskChanged bindings of one connection.

    `run` replays each durable target, records a baseline for a binding without a cursor, then polls every
    binding each `pollInterval` with jitter, following up to 10 pages of 100 events per poll. An expired or
    invalid sync token (HTTP 412) records a new baseline and logs a WARN record; changes made between the
    last poll and the new baseline are not delivered. A 429 pauses every binding of the connection for
    Retry-After, or backs off exponentially without it; other failures back off per binding up to 15
    minutes. Delivery is at least once from Asana's event stream, which Asana itself delivers at most once,
    and each event keeps one Trigger event ID across refetches of the same sync token, as in Go. Raises
    ValueError for a mock connection, no routes, a blank or repeated binding name, a missing target, or
    an invalid configuration.
    """
    client = _connection_client(config.connection)
    if not config.task_changed_routes:
        raise ValueError("Asana taskChanged routes are required")
    pollers: list[Poller[TriggerEvent[TaskChangedEvent]]] = [
        _new_task_changed_poller(config.connection, client, route) for route in config.task_changed_routes
    ]
    return PollRunner(
        PollRunnerConfig(config.connection.reference, _poll_schedule(client), pollers, logger=config.logger)
    )


@dataclass(frozen=True, slots=True)
class ProjectTaskChangedTriggerRoute:
    """Selects one taskChanged binding saved in the project configuration."""

    binding_name: str
    """The saved binding's name."""
    target: TriggerTarget[TaskChangedEvent]
    """Routes each change into the application, usually `new_dex_flow_trigger_target`."""


@dataclass(frozen=True, slots=True)
class ProjectTaskTriggerRunnerConfig:
    """Selects Asana Trigger bindings saved in the project configuration."""

    task_changed_routes: Sequence[ProjectTaskChangedTriggerRoute]
    """The taskChanged bindings; at least one is required."""
    logger: TriggerLogger | None = None
    """Receives the runner's and the durable inboxes' records; None uses the standard logging adapter."""


def new_project_task_trigger_runner(
    project: LoadedProject, connection: Connection, config: ProjectTaskTriggerRunnerConfig
) -> PollRunner:
    """Create a durable runner for bindings saved in project, polled through connection.

    connection must be the project connection the bindings name, from `new_project_connection`; the caller
    keeps it and closes it after the runner stops. Each route loads its binding's saved project, records
    changes in the binding's project Trigger inbox before its sync token commits in the binding's project
    poll cursor, so a restarted or replaced replica neither misses nor repeats a change. Under Dex Web
    (`dexcli dev`) the poll cursor stays in process memory; hosted project storage keeps it. Raises
    ValueError for a missing target, a binding without a saved project, or an invalid configuration.
    """
    if project is None:
        raise ValueError("Asana Trigger runner requires the loaded project configuration")
    _connection_client(connection)
    routes: list[TaskChangedTriggerRoute] = []
    for route in config.task_changed_routes:
        if route.target is None:
            raise ValueError(f"Asana taskChanged binding {json.dumps(route.binding_name)} target is required")
        key = TriggerInboxKey(CONNECTOR_ID, connection.reference.name, _TRIGGER_NAME, route.binding_name)
        try:
            configuration = decode_trigger_configuration(
                project.configuration, key, decode_task_changed_trigger_configuration
            )
        except ObjectNotFoundError as error:
            raise ValueError(
                f"asana taskChanged binding {json.dumps(route.binding_name)} has no saved project; "
                "pick one in Dex Web and restart"
            ) from error
        except ValueError as error:
            raise ValueError(
                f"asana taskChanged binding {json.dumps(route.binding_name)} configuration: {error}"
            ) from error
        target = new_durable_trigger_target(
            project.trigger_inbox(key),
            route.target,
            decode_payload=decode_task_changed_event_payload,
            logger=config.logger,
        )
        cursor = project.poll_cursor_store(PollCursorKey.from_trigger_inbox_key(key))
        routes.append(TaskChangedTriggerRoute(route.binding_name, configuration, target, cursor))
    return new_task_trigger_runner(TaskTriggerRunnerConfig(connection, routes, config.logger))


def new_task_changed_trigger(
    *,
    connection: Connection,
    binding_name: str,
    configuration: TaskChangedTriggerConfiguration,
    target: TriggerTarget[TaskChangedEvent],
    logger: TriggerLogger | None = None,
) -> TriggerRunner:
    """Create one taskChanged binding's TriggerRunner with its cursor in memory.

    It has no project storage, so every start records a new baseline and changes made while the process
    is stopped are lost; use it for tests and demonstrations, and `new_project_task_trigger_runner`
    otherwise. Raises ValueError as `new_task_trigger_runner` does.
    """
    client = _connection_client(connection)
    _validate_route_configuration(binding_name, configuration)
    if target is None:
        raise ValueError("Asana taskChanged binding name and target are required")
    binding = TriggerBindingRef(connection.reference, TASK_CHANGED_TRIGGER_DEFINITION.trigger, binding_name)
    source = new_binding_trigger_source(
        BindingPollerConfig(
            TASK_CHANGED_TRIGGER_DEFINITION,
            binding,
            new_memory_cursor_store(),
            TaskEventFetcher(client, connection.reference, configuration.project_id),
        ),
        _poll_schedule(client),
    )
    return new_trigger(TriggerConfig(TASK_CHANGED_TRIGGER_DEFINITION, binding, source, target, logger))


def _new_task_changed_poller(
    connection: Connection, client: Client, route: TaskChangedTriggerRoute
) -> Poller[TriggerEvent[TaskChangedEvent]]:
    if route.target is None:
        raise ValueError("Asana taskChanged binding name and target are required")
    _validate_route_configuration(route.binding_name, route.configuration)
    return new_binding_poller(
        BindingPollerConfig(
            TASK_CHANGED_TRIGGER_DEFINITION,
            TriggerBindingRef(
                connection.reference, TASK_CHANGED_TRIGGER_DEFINITION.trigger, route.binding_name
            ),
            route.cursor if route.cursor is not None else new_memory_cursor_store(),
            TaskEventFetcher(client, connection.reference, route.configuration.project_id),
            route.target,
        )
    )


def _validate_route_configuration(binding_name: str, configuration: TaskChangedTriggerConfiguration) -> None:
    if not (binding_name or "").strip():
        raise ValueError("Asana taskChanged binding name and target are required")
    try:
        validate_task_changed_trigger_configuration(configuration)
    except ValueError as error:
        raise ValueError(f"asana taskChanged configuration: {error}") from error


def _poll_schedule(client: Client) -> PollSchedule:
    # A zero maximum backoff uses the kit's cap, which grows with a long poll interval.
    return PollSchedule(timedelta(seconds=client.poll_interval_seconds))


def _connection_client(connection: Connection) -> Client:
    # The generated Connection keeps its Client private; Triggers need its transport and settings.
    client = connection._client if connection is not None else None
    if client is None:
        raise ValueError(
            "Asana Triggers require a connection built with new_connection or new_project_connection"
        )
    return client
