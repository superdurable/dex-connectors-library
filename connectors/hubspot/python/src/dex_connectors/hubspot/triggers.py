# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Receive each created or changed HubSpot CRM record through the ``crmObjectChanged`` poll Trigger.

The Trigger polls CRM search on the last-modified date with the Connector SDK's durable poll kit,
at behavior parity with the Go connector; its cursor documents and event IDs are the Go
connector's byte for byte, so Go and Python replicas can share one binding's cursor and inbox.
`new_project_crm_object_trigger_runner` is the durable path.
"""

import json
from collections.abc import Sequence
from dataclasses import dataclass, field, replace
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

from ._generated.definitions import CONNECTOR_ID, CRM_OBJECT_CHANGED_TRIGGER_DEFINITION
from ._generated.triggers import (
    decode_crm_object_changed_trigger_configuration,
    define_crm_object_changed_trigger_binding,
)
from .client import Client
from .connection import Connection, new_project_connection
from .crm_object_change_fetcher import (
    CRM_OBJECT_CHANGE_CREATED,
    CRM_OBJECT_CHANGE_UPDATED,
    MAXIMUM_TRIGGER_PROPERTIES,
    CRMObjectChangeFetcher,
    CRMObjectChangeSearchError,
    validate_crm_object_changed_trigger_configuration,
)
from .models import CRMObjectChangedEvent, CRMObjectChangedTriggerConfiguration

UI_UNIT_OBJECT_TYPE_PICKER: Final = "objectTypePicker"
"""HubSpot object type picker UI unit, which stores HubSpot's object name."""
UI_OBJECT_TYPE_PICKER_PORT_OBJECT_TYPE: Final = "objectType"
"""Object type picker output port."""

_TRIGGER: Final = CRM_OBJECT_CHANGED_TRIGGER_DEFINITION.trigger


@dataclass(frozen=True, slots=True)
class CRMObjectChangedTriggerRoute:
    """Binds one crmObjectChanged configuration to its application target and cursor."""

    binding_name: str
    """The stable application binding name, unique within the runner."""
    configuration: CRMObjectChangedTriggerConfiguration
    """Selects the watched object type and the returned properties."""
    target: TriggerTarget[CRMObjectChangedEvent]
    """Routes each change into the application.

    A target that prepares deliveries, such as `new_durable_trigger_target`, records each change before
    the cursor commits past it; any other target consumes it first.
    """
    cursor: CursorStore | None = None
    """Stores the binding's search position, such as `LoadedProject.poll_cursor_store`.

    None keeps it in memory, so every process start records a new baseline and changes made while
    stopped are lost.
    """


def new_crm_object_trigger_runner(
    connection: Connection,
    routes: Sequence[CRMObjectChangedTriggerRoute],
    logger: TriggerLogger | None = None,
) -> PollRunner:
    """Create one poll runner for the crmObjectChanged bindings of a provider connection.

    `run` replays each durable target, records a baseline for a binding without a cursor, then polls
    every binding each pollInterval with jitter, in route order, one search request at a time. A 429 or
    423 pauses every binding of the connection for Retry-After or
    X-HubSpot-RateLimit-Interval-Milliseconds, an exhausted daily limit backs off exponentially up to
    15 minutes, and other failures back off per binding. A paging offset HubSpot rejects records a new
    baseline with a WARN record. Delivery is at least once, and each record version keeps one Trigger
    event ID across refetches. logger, or else the client's logger, receives the records. Raises
    ValueError for a connection without a HubSpot client, no routes, or an invalid route.
    """
    client = _hubspot_client(connection)
    if not routes:
        raise ValueError("HubSpot crmObjectChanged routes are required")
    pollers: list[Poller[object]] = [
        _new_crm_object_changed_poller(client, connection.reference, route) for route in routes
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
class ProjectCRMObjectChangedTriggerRoute:
    """Selects one crmObjectChanged binding saved in the project configuration."""

    binding_name: str
    """Names the saved binding."""
    target: TriggerTarget[CRMObjectChangedEvent]
    """Routes each change into the application, usually `new_dex_flow_trigger_target`."""
    properties: tuple[str, ...] = field(default=())
    """Property names to return on each delivered record; applies when the saved binding names none."""


class ProjectCRMObjectTriggerRunner:
    """A durable crmObjectChanged runner over a project connection it owns; close it with `aclose`."""

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


async def new_project_crm_object_trigger_runner(
    project: LoadedProject,
    connection_name: str,
    routes: Sequence[ProjectCRMObjectChangedTriggerRoute],
    http_client: httpx.AsyncClient | None = None,
    logger: TriggerLogger | None = None,
) -> ProjectCRMObjectTriggerRunner:
    """Open connection_name and create a durable runner for each route's saved binding.

    Each route records changes in its binding's project Trigger inbox before its search position
    commits in the binding's project poll cursor, so a restarted or replaced replica neither misses nor
    repeats a change. logger also receives the inboxes' records. Raises ValueError for a binding
    without a saved object type or with an invalid configuration. An injected http_client stays owned
    by its caller.
    """
    connection = new_project_connection(project, connection_name, http_client)
    try:
        routes_with_storage = [_project_route(project, connection, route, logger) for route in routes]
        runner = new_crm_object_trigger_runner(connection, routes_with_storage, logger)
    except BaseException:
        await connection.aclose()
        raise
    return ProjectCRMObjectTriggerRunner(runner, connection)


def new_crm_object_changed_trigger(
    *,
    connection: Connection,
    connection_name: str,
    binding_name: str,
    configuration: CRMObjectChangedTriggerConfiguration,
    target: TriggerTarget[CRMObjectChangedEvent],
    logger: TriggerLogger | None = None,
) -> TriggerRunner:
    """Create one crmObjectChanged Trigger polled every pollInterval.

    It has no project storage, so it keeps the cursor in memory and records a new baseline on every
    start; use `new_project_crm_object_trigger_runner` for durable polling. Raises ValueError for an
    invalid configuration or a connection name that differs from the connection's.
    """
    client = _hubspot_client(connection)
    reference = connection.reference
    if connection_name != reference.name:
        raise ValueError(
            f"hubspot connector trigger connection_name {connection_name!r} must equal its connection's "
            f"name {reference.name!r}"
        )
    _validate(configuration)
    source = new_binding_trigger_source(
        BindingPollerConfig(
            CRM_OBJECT_CHANGED_TRIGGER_DEFINITION,
            TriggerBindingRef(reference, _TRIGGER, _TRIGGER.trigger_name),
            new_memory_cursor_store(),
            CRMObjectChangeFetcher(client, reference, configuration),
        ),
        _poll_schedule(client),
    )
    return new_trigger(
        TriggerConfig(
            CRM_OBJECT_CHANGED_TRIGGER_DEFINITION,
            TriggerBindingRef(reference, _TRIGGER, binding_name),
            source,
            target,
            logger if logger is not None else client.trigger_logger,
        )
    )


def decode_crm_object_changed_event_payload(payload_json: str, payload_bytes: bytes) -> CRMObjectChangedEvent:
    """Decode a replayed inbox payload that this package or the Go connector recorded."""
    decoded = CodecRegistry().resolve(CRMObjectChangedEvent).decode(Value(WireKind.JSON, payload_json))
    if not isinstance(decoded, CRMObjectChangedEvent):
        raise ValueError("crmObjectChanged payload did not decode")
    return decoded


def _project_route(
    project: LoadedProject,
    connection: Connection,
    route: ProjectCRMObjectChangedTriggerRoute,
    logger: TriggerLogger | None,
) -> CRMObjectChangedTriggerRoute:
    if route.target is None:
        raise ValueError(
            f"HubSpot crmObjectChanged binding {json.dumps(route.binding_name)} target is required"
        )
    key = TriggerInboxKey(CONNECTOR_ID, connection.reference.name, _TRIGGER.trigger_name, route.binding_name)
    try:
        configuration = decode_trigger_configuration(
            project.configuration, key, decode_crm_object_changed_trigger_configuration
        )
    except ObjectNotFoundError as error:
        raise ValueError(
            f"hubspot crmObjectChanged binding {json.dumps(route.binding_name)} has no saved object type; "
            "pick one in Dex Web"
        ) from error
    except ValueError as error:
        raise ValueError(
            f"hubspot crmObjectChanged binding {json.dumps(route.binding_name)} configuration: {error}"
        ) from error
    if not configuration.properties and route.properties:
        configuration = replace(configuration, properties=tuple(route.properties))
    target = new_durable_trigger_target(
        project.trigger_inbox(key),
        route.target,
        decode_payload=decode_crm_object_changed_event_payload,
        logger=logger if logger is not None else _hubspot_client(connection).trigger_logger,
    )
    cursor = project.poll_cursor_store(PollCursorKey.from_trigger_inbox_key(key))
    return CRMObjectChangedTriggerRoute(route.binding_name, configuration, target, cursor)


def _new_crm_object_changed_poller(
    client: Client, connection: ConnectionRef, route: CRMObjectChangedTriggerRoute
) -> Poller[object]:
    if not (route.binding_name or "").strip() or route.target is None:
        raise ValueError("HubSpot crmObjectChanged binding name and target are required")
    _validate(route.configuration)
    poller: Poller[object] = new_binding_poller(  # type: ignore[assignment]
        BindingPollerConfig(
            CRM_OBJECT_CHANGED_TRIGGER_DEFINITION,
            TriggerBindingRef(connection, _TRIGGER, route.binding_name),
            route.cursor if route.cursor is not None else new_memory_cursor_store(),
            CRMObjectChangeFetcher(client, connection, route.configuration),
            route.target,
        )
    )
    return poller


def _validate(configuration: CRMObjectChangedTriggerConfiguration) -> None:
    try:
        validate_crm_object_changed_trigger_configuration(configuration)
    except ValueError as error:
        raise ValueError(f"hubspot crmObjectChanged configuration: {error}") from error


def _poll_schedule(client: Client) -> PollSchedule:
    # A zero maximum backoff uses the kit's cap, which grows with a long poll interval.
    return PollSchedule(timedelta(seconds=client.config.poll_interval_seconds))


def _hubspot_client(connection: Connection) -> Client:
    client = getattr(connection, "_client", None)
    if not isinstance(client, Client):
        raise ValueError(
            "HubSpot Triggers require a connection from new_project_connection or new_connection"
        )
    return client


__all__ = [
    "CRM_OBJECT_CHANGED_TRIGGER_DEFINITION",
    "CRM_OBJECT_CHANGE_CREATED",
    "CRM_OBJECT_CHANGE_UPDATED",
    "MAXIMUM_TRIGGER_PROPERTIES",
    "UI_OBJECT_TYPE_PICKER_PORT_OBJECT_TYPE",
    "UI_UNIT_OBJECT_TYPE_PICKER",
    "CRMObjectChangeSearchError",
    "CRMObjectChangedEvent",
    "CRMObjectChangedTriggerConfiguration",
    "CRMObjectChangedTriggerRoute",
    "ProjectCRMObjectChangedTriggerRoute",
    "ProjectCRMObjectTriggerRunner",
    "decode_crm_object_changed_event_payload",
    "decode_crm_object_changed_trigger_configuration",
    "define_crm_object_changed_trigger_binding",
    "new_crm_object_changed_trigger",
    "new_crm_object_trigger_runner",
    "new_project_crm_object_trigger_runner",
    "validate_crm_object_changed_trigger_configuration",
]
