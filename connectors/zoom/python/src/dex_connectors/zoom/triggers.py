# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Receive each Zoom meeting the authorized user schedules, changes, or that leaves the scheduled list.

The ``meetingChanged`` Trigger polls Zoom's List meetings (``type=scheduled``) every connection
``pollInterval`` and diffs each complete listing with a durable keyed snapshot, at behavior parity with
the Go connector, whose cursor documents it reads and writes byte for byte. The first poll records a
baseline and delivers nothing. `new_project_meeting_trigger_runner` records each change in the binding's
project Trigger inbox before the snapshot commits, so a restarted replica neither misses nor repeats one.
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

from ._generated.definitions import CONNECTOR_ID, MEETING_CHANGED_TRIGGER_DEFINITION
from ._generated.triggers import (
    decode_meeting_changed_trigger_configuration,
    define_meeting_changed_trigger_binding,
)
from ._meeting_list_fetcher import MEETING_SNAPSHOT_OPTIONS, MeetingListFetcher
from ._meeting_snapshot import MeetingChangeKind, validate_meeting_changed_trigger_configuration
from .client import Client
from .connection import Connection
from .models import MeetingChangedEvent, MeetingChangedTriggerConfiguration

_TRIGGER_NAME: Final = MEETING_CHANGED_TRIGGER_DEFINITION.trigger.trigger_name


@dataclass(frozen=True, slots=True)
class MeetingChangedTriggerRoute:
    """Binds one ``meetingChanged`` configuration to its application target and cursor."""

    binding_name: str
    """The stable application binding name, unique within the runner."""
    target: TriggerTarget[MeetingChangedEvent]
    """Routes each change into the application.

    A target wrapped with `new_durable_trigger_target` records each change before the cursor commits past
    it; any other target consumes it first.
    """
    configuration: MeetingChangedTriggerConfiguration = field(
        default_factory=MeetingChangedTriggerConfiguration
    )
    """Narrows the delivered change kinds; the default delivers every kind."""
    cursor: CursorStore | None = None
    """Stores the binding's meeting snapshot, such as `LoadedProject.poll_cursor_store`.

    None keeps it in memory, so every process start records a new baseline and changes made while
    stopped are lost.
    """


@dataclass(frozen=True, slots=True)
class ProjectMeetingChangedTriggerRoute:
    """Selects one ``meetingChanged`` binding saved in the project configuration."""

    binding_name: str
    """Names the saved binding."""
    target: TriggerTarget[MeetingChangedEvent]
    """Routes each change into the application, usually `new_dex_flow_trigger_target`."""


def new_meeting_trigger_runner(
    connection: Connection,
    meeting_changed_routes: Sequence[MeetingChangedTriggerRoute],
    *,
    logger: TriggerLogger | None = None,
) -> PollRunner:
    """Create one poll runner for the connection's ``meetingChanged`` bindings.

    `PollRunner.run` replays each durable target, records the current meeting list as a baseline for a
    binding without a cursor, then lists every page of the authorized user's scheduled meetings each
    `Client.poll_interval` with jitter and delivers the differences from the stored snapshot. A stored
    cursor that is undecodable or holds another Zoom user's meetings records a new baseline and logs a
    WARN record; differences in that gap are not delivered. A 429, including Zoom's daily-limit 429,
    pauses every binding of the connection for Retry-After, or backs off exponentially without it; other
    failures back off per binding up to 15 minutes. Delivery is at least once, and each change keeps one
    Trigger event ID across refetches. logger receives poll records without payloads or credentials;
    None uses the standard logging adapter. Raises ValueError for a connection without a client, no
    routes, a blank binding name, a missing target, or an invalid configuration.
    """
    client = _connection_client(connection)
    if not meeting_changed_routes:
        raise ValueError("Zoom meetingChanged routes are required")
    pollers: list[Poller[TriggerEvent[MeetingChangedEvent]]] = [
        _new_meeting_changed_poller(connection, client, route) for route in meeting_changed_routes
    ]
    return PollRunner(PollRunnerConfig(connection.reference, _poll_schedule(client), pollers, logger=logger))


def new_project_meeting_trigger_runner(
    project: LoadedProject,
    connection: Connection,
    meeting_changed_routes: Sequence[ProjectMeetingChangedTriggerRoute],
    *,
    logger: TriggerLogger | None = None,
) -> PollRunner:
    """Create a durable runner for ``meetingChanged`` bindings saved in the loaded project.

    connection is the one `new_project_connection` opened from project; the caller keeps owning it.
    Each route loads its binding's saved configuration (an unsaved binding delivers every change kind)
    and records changes in the binding's project Trigger inbox before its meeting snapshot commits in
    the binding's project poll cursor, so a restarted or replaced replica neither misses nor repeats a
    change. The inbox payload is the Go connector's JSON, so Go and Python replicas share it. Raises
    ValueError for an invalid saved configuration and as `new_meeting_trigger_runner` does.
    """
    if project is None:
        raise ValueError("Zoom meetingChanged runner requires the loaded project configuration")
    routes: list[MeetingChangedTriggerRoute] = []
    for route in meeting_changed_routes:
        if route.target is None:
            raise ValueError(
                f"Zoom meetingChanged binding {json.dumps(route.binding_name)} target is required"
            )
        key = TriggerInboxKey(CONNECTOR_ID, connection.reference.name, _TRIGGER_NAME, route.binding_name)
        try:
            configuration = decode_trigger_configuration(
                project.configuration, key, decode_meeting_changed_trigger_configuration
            )
        except ObjectNotFoundError:
            # An unsaved binding delivers every change kind.
            configuration = MeetingChangedTriggerConfiguration()
        except ValueError as error:
            raise ValueError(
                f"zoom meetingChanged binding {json.dumps(route.binding_name)} configuration: {error}"
            ) from error
        target = new_durable_trigger_target(
            project.trigger_inbox(key),
            route.target,
            decode_payload=decode_meeting_changed_event_payload,
            logger=logger,
        )
        routes.append(
            MeetingChangedTriggerRoute(
                route.binding_name,
                target,
                configuration,
                project.poll_cursor_store(PollCursorKey.from_trigger_inbox_key(key)),
            )
        )
    return new_meeting_trigger_runner(connection, routes, logger=logger)


def new_meeting_changed_trigger(
    *,
    connection: Connection,
    binding_name: str,
    target: TriggerTarget[MeetingChangedEvent],
    configuration: MeetingChangedTriggerConfiguration | None = None,
    logger: TriggerLogger | None = None,
) -> TriggerRunner:
    """Create a single ``meetingChanged`` Trigger runner, as Go's generated NewMeetingChangedTrigger does.

    It has no project storage, so it keeps the cursor in memory and records a new baseline on every
    start; changes made while stopped are lost. Use `new_project_meeting_trigger_runner` for durable
    delivery. configuration defaults to every change kind. Raises ValueError as
    `new_meeting_trigger_runner` does.
    """
    client = _connection_client(connection)
    if not (binding_name or "").strip() or target is None:
        raise ValueError("Zoom meetingChanged binding name and target are required")
    configuration = configuration or MeetingChangedTriggerConfiguration()
    _validate_configuration(configuration)
    binding = TriggerBindingRef(
        connection.reference, MEETING_CHANGED_TRIGGER_DEFINITION.trigger, binding_name
    )
    source = new_binding_trigger_source(
        BindingPollerConfig(
            MEETING_CHANGED_TRIGGER_DEFINITION,
            binding,
            new_memory_cursor_store(),
            MeetingListFetcher(client, connection.reference, binding.trigger, configuration),
            snapshot=MEETING_SNAPSHOT_OPTIONS,
        ),
        _poll_schedule(client),
    )
    return new_trigger(TriggerConfig(MEETING_CHANGED_TRIGGER_DEFINITION, binding, source, target, logger))


def decode_meeting_changed_event_payload(payload_json: str, payload_bytes: bytes) -> MeetingChangedEvent:
    """Decode a replayed inbox payload, written by a Go or Python replica, as a MeetingChangedEvent.

    Raises ValueError for a payload that is not a meetingChanged event.
    """
    try:
        decoded = CodecRegistry().resolve(MeetingChangedEvent).decode(Value(WireKind.JSON, payload_json))
    except (TypeError, ValueError) as error:
        raise ValueError("stored Zoom meetingChanged payload is invalid") from error
    if not isinstance(decoded, MeetingChangedEvent) or decoded.change_kind not in tuple(MeetingChangeKind):
        raise ValueError("stored Zoom meetingChanged payload is invalid")
    return decoded


def _new_meeting_changed_poller(
    connection: Connection, client: Client, route: MeetingChangedTriggerRoute
) -> Poller[TriggerEvent[MeetingChangedEvent]]:
    if not (route.binding_name or "").strip() or route.target is None:
        raise ValueError("Zoom meetingChanged binding name and target are required")
    _validate_configuration(route.configuration)
    binding = TriggerBindingRef(
        connection.reference, MEETING_CHANGED_TRIGGER_DEFINITION.trigger, route.binding_name
    )
    return new_binding_poller(
        BindingPollerConfig(
            MEETING_CHANGED_TRIGGER_DEFINITION,
            binding,
            route.cursor if route.cursor is not None else new_memory_cursor_store(),
            MeetingListFetcher(client, connection.reference, binding.trigger, route.configuration),
            route.target,
            MEETING_SNAPSHOT_OPTIONS,
        )
    )


def _validate_configuration(configuration: MeetingChangedTriggerConfiguration) -> None:
    try:
        validate_meeting_changed_trigger_configuration(configuration)
    except ValueError as error:
        raise ValueError(f"zoom meetingChanged configuration: {error}") from error


def _connection_client(connection: Connection) -> Client:
    client = getattr(connection, "_client", None) if connection is not None else None
    if not isinstance(client, Client):
        raise ValueError("Zoom meetingChanged Trigger requires a connection opened with a Zoom client")
    return client


def _poll_schedule(client: Client) -> PollSchedule:
    # A zero maximum backoff uses the kit's cap, which grows with a long poll interval.
    return PollSchedule(client.poll_interval)


__all__ = [
    "MEETING_CHANGED_TRIGGER_DEFINITION",
    "MeetingChangeKind",
    "MeetingChangedTriggerRoute",
    "ProjectMeetingChangedTriggerRoute",
    "decode_meeting_changed_event_payload",
    "decode_meeting_changed_trigger_configuration",
    "define_meeting_changed_trigger_binding",
    "new_meeting_changed_trigger",
    "new_meeting_trigger_runner",
    "new_project_meeting_trigger_runner",
    "validate_meeting_changed_trigger_configuration",
]
