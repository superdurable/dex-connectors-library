# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Receive each contact added to or changed in one Mailchimp audience.

The ``memberChanged`` Trigger polls GET /lists/{list_id}/members by last change time every connection
``pollInterval``, at behavior parity with the Go connector, whose cursor documents it reads and writes
byte for byte. The first poll records a baseline and delivers no existing contacts.
`new_project_member_trigger_runner` records each change in the binding's project Trigger inbox before its
cursor commits, so a restarted replica neither misses nor repeats one.
"""

import json
from collections.abc import Sequence
from dataclasses import dataclass
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

from ._generated.definitions import CONNECTOR_ID, MEMBER_CHANGED_TRIGGER_DEFINITION
from ._generated.triggers import (
    decode_member_changed_trigger_configuration,
    define_member_changed_trigger_binding,
)
from ._member_change_fetcher import MEMBER_SNAPSHOT_OPTIONS, MemberChangeFetcher
from ._member_snapshot import MEMBER_STATUSES, validate_member_changed_trigger_configuration
from .client import Client
from .connection import Connection
from .models import MemberChangedEvent, MemberChangedTriggerConfiguration

_TRIGGER_NAME: Final = MEMBER_CHANGED_TRIGGER_DEFINITION.trigger.trigger_name


@dataclass(frozen=True, slots=True)
class MemberChangedTriggerRoute:
    """Binds one ``memberChanged`` configuration to its application target and cursor."""

    binding_name: str
    """The stable application binding name, unique within the runner."""
    configuration: MemberChangedTriggerConfiguration
    """Selects the watched audience, which is required, and the delivered statuses."""
    target: TriggerTarget[MemberChangedEvent]
    """Routes each change into the application.

    A target wrapped with `new_durable_trigger_target` records each change before the cursor commits past
    it; any other target consumes it first.
    """
    cursor: CursorStore | None = None
    """Stores the binding's position, such as `LoadedProject.poll_cursor_store`.

    None keeps it in memory, so every process start records a new baseline and changes made while
    stopped are lost.
    """


@dataclass(frozen=True, slots=True)
class ProjectMemberChangedTriggerRoute:
    """Selects one ``memberChanged`` binding saved in the project configuration."""

    binding_name: str
    """Names the saved binding."""
    target: TriggerTarget[MemberChangedEvent]
    """Routes each change into the application, usually `new_dex_flow_trigger_target`."""


def member_statuses() -> tuple[str, ...]:
    """Return every status the memberStatusPicker offers, in its display order."""
    return MEMBER_STATUSES


def new_member_trigger_runner(
    connection: Connection,
    member_changed_routes: Sequence[MemberChangedTriggerRoute],
    *,
    logger: TriggerLogger | None = None,
) -> PollRunner:
    """Create one poll runner for the connection's ``memberChanged`` bindings.

    `PollRunner.run` replays each durable target, records a baseline for a binding without a cursor, then
    polls every binding each `Client.poll_interval` with jitter, one request at a time, so the runner holds
    at most one of the 10 simultaneous requests Mailchimp allows each user. A 429, or a 403 without an
    error document, pauses every binding of the connection for Retry-After, or backs off exponentially
    without it; a 400 for the cursor's query records a new baseline and logs a WARN record; other failures
    back off per binding up to 15 minutes. Delivery is at least once, and each contact version keeps one
    Trigger event ID across refetches. logger receives poll records without contact data or credentials;
    None uses the standard logging adapter. Raises ValueError for a connection without a client, no
    routes, a blank or repeated binding name, a missing target, or an invalid configuration.
    """
    client = _connection_client(connection)
    if not member_changed_routes:
        raise ValueError("Mailchimp memberChanged routes are required")
    pollers: list[Poller[TriggerEvent[MemberChangedEvent]]] = [
        _new_member_changed_poller(connection, client, route) for route in member_changed_routes
    ]
    return PollRunner(PollRunnerConfig(connection.reference, _poll_schedule(client), pollers, logger=logger))


def new_project_member_trigger_runner(
    project: LoadedProject,
    connection: Connection,
    member_changed_routes: Sequence[ProjectMemberChangedTriggerRoute],
    *,
    logger: TriggerLogger | None = None,
) -> PollRunner:
    """Create a durable runner for ``memberChanged`` bindings saved in the loaded project.

    connection is the one `new_project_connection` opened from project; the caller keeps owning it. Each
    route loads its binding's saved audience and statuses and records changes in the binding's project
    Trigger inbox before its position commits in the binding's project poll cursor, so a restarted or
    replaced replica neither misses nor repeats a change. The inbox payload is the Go connector's JSON,
    so Go and Python replicas share it. Raises ValueError for a binding without a saved audience, an
    invalid saved configuration, and as `new_member_trigger_runner` does.
    """
    if project is None:
        raise ValueError("Mailchimp memberChanged runner requires the loaded project configuration")
    routes: list[MemberChangedTriggerRoute] = []
    for route in member_changed_routes:
        if route.target is None:
            raise ValueError(
                f"Mailchimp memberChanged binding {json.dumps(route.binding_name)} target is required"
            )
        key = TriggerInboxKey(CONNECTOR_ID, connection.reference.name, _TRIGGER_NAME, route.binding_name)
        try:
            configuration = decode_trigger_configuration(
                project.configuration, key, decode_member_changed_trigger_configuration
            )
        except ObjectNotFoundError:
            raise ValueError(
                f"mailchimp memberChanged binding {json.dumps(route.binding_name)} has no saved audience; "
                "enter its audience ID in Dex Web Connections"
            ) from None
        except ValueError as error:
            raise ValueError(
                f"mailchimp memberChanged binding {json.dumps(route.binding_name)} configuration: {error}"
            ) from error
        target = new_durable_trigger_target(
            project.trigger_inbox(key),
            route.target,
            decode_payload=decode_member_changed_event_payload,
            logger=logger,
        )
        routes.append(
            MemberChangedTriggerRoute(
                route.binding_name,
                configuration,
                target,
                project.poll_cursor_store(PollCursorKey.from_trigger_inbox_key(key)),
            )
        )
    return new_member_trigger_runner(connection, routes, logger=logger)


def new_member_changed_trigger(
    *,
    connection: Connection,
    binding_name: str,
    configuration: MemberChangedTriggerConfiguration,
    target: TriggerTarget[MemberChangedEvent],
    logger: TriggerLogger | None = None,
) -> TriggerRunner:
    """Create a single ``memberChanged`` Trigger runner, as Go's generated NewMemberChangedTrigger does.

    It has no project storage, so it keeps the cursor in memory and records a new baseline on every start;
    changes made while stopped are lost. Use `new_project_member_trigger_runner` for durable delivery.
    Raises ValueError as `new_member_trigger_runner` does.
    """
    client = _connection_client(connection)
    if not (binding_name or "").strip() or target is None:
        raise ValueError("Mailchimp memberChanged binding name and target are required")
    _validate_configuration(configuration)
    binding = TriggerBindingRef(connection.reference, MEMBER_CHANGED_TRIGGER_DEFINITION.trigger, binding_name)
    source = new_binding_trigger_source(
        BindingPollerConfig(
            MEMBER_CHANGED_TRIGGER_DEFINITION,
            binding,
            new_memory_cursor_store(),
            MemberChangeFetcher(client, connection.reference, binding.trigger, configuration),
            snapshot=MEMBER_SNAPSHOT_OPTIONS,
        ),
        _poll_schedule(client),
    )
    return new_trigger(TriggerConfig(MEMBER_CHANGED_TRIGGER_DEFINITION, binding, source, target, logger))


def decode_member_changed_event_payload(payload_json: str, payload_bytes: bytes) -> MemberChangedEvent:
    """Decode a replayed inbox payload, written by a Go or Python replica, as a MemberChangedEvent.

    Raises ValueError for a payload that is not a memberChanged event.
    """
    try:
        decoded = CodecRegistry().resolve(MemberChangedEvent).decode(Value(WireKind.JSON, payload_json))
    except (TypeError, ValueError) as error:
        raise ValueError("stored Mailchimp memberChanged payload is invalid") from error
    if not isinstance(decoded, MemberChangedEvent) or not decoded.audience_id:
        raise ValueError("stored Mailchimp memberChanged payload is invalid")
    return decoded


def _new_member_changed_poller(
    connection: Connection, client: Client, route: MemberChangedTriggerRoute
) -> Poller[TriggerEvent[MemberChangedEvent]]:
    if not (route.binding_name or "").strip() or route.target is None:
        raise ValueError("Mailchimp memberChanged binding name and target are required")
    _validate_configuration(route.configuration)
    binding = TriggerBindingRef(
        connection.reference, MEMBER_CHANGED_TRIGGER_DEFINITION.trigger, route.binding_name
    )
    return new_binding_poller(
        BindingPollerConfig(
            MEMBER_CHANGED_TRIGGER_DEFINITION,
            binding,
            route.cursor if route.cursor is not None else new_memory_cursor_store(),
            MemberChangeFetcher(client, connection.reference, binding.trigger, route.configuration),
            route.target,
            MEMBER_SNAPSHOT_OPTIONS,
        )
    )


def _validate_configuration(configuration: MemberChangedTriggerConfiguration | None) -> None:
    if configuration is None:
        raise ValueError("mailchimp memberChanged configuration: audienceId is required")
    try:
        validate_member_changed_trigger_configuration(configuration)
    except ValueError as error:
        raise ValueError(f"mailchimp memberChanged configuration: {error}") from error


def _connection_client(connection: Connection) -> Client:
    client = getattr(connection, "_client", None) if connection is not None else None
    if not isinstance(client, Client):
        raise ValueError(
            "Mailchimp memberChanged Trigger requires a connection opened with a Mailchimp client"
        )
    return client


def _poll_schedule(client: Client) -> PollSchedule:
    # A zero maximum backoff uses the kit's cap, which grows with a long poll interval.
    return PollSchedule(client.poll_interval)


__all__ = [
    "MEMBER_CHANGED_TRIGGER_DEFINITION",
    "MemberChangedTriggerRoute",
    "ProjectMemberChangedTriggerRoute",
    "decode_member_changed_event_payload",
    "decode_member_changed_trigger_configuration",
    "define_member_changed_trigger_binding",
    "member_statuses",
    "new_member_changed_trigger",
    "new_member_trigger_runner",
    "new_project_member_trigger_runner",
    "validate_member_changed_trigger_configuration",
]
