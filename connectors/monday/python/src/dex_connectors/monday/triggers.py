# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Receive each active item created or changed on one monday.com board.

The ``itemChanged`` Trigger polls the board's items ordered by monday.com's last-updated time every
connection ``pollInterval``, at behavior parity with the Go connector, whose cursor documents it reads
and writes byte for byte. The first poll records a baseline and delivers no existing item.
`new_project_item_trigger_runner` records each change in the binding's project Trigger inbox before the
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

from ._generated.definitions import CONNECTOR_ID, ITEM_CHANGED_TRIGGER_DEFINITION
from ._generated.triggers import (
    decode_item_changed_trigger_configuration,
    define_item_changed_trigger_binding,
)
from ._item_changed_fetcher import ITEM_CHANGED_SNAPSHOT_OPTIONS, ItemChangeFetcher
from ._item_snapshot import ItemChangeKind
from .client import Client
from .connection import Connection
from .item import NUMERIC_ID
from .models import ItemChangedEvent, ItemChangedTriggerConfiguration

_TRIGGER_NAME: Final = ITEM_CHANGED_TRIGGER_DEFINITION.trigger.trigger_name


@dataclass(frozen=True, slots=True)
class ItemChangedTriggerRoute:
    """Binds one ``itemChanged`` configuration to its application target and cursor."""

    binding_name: str
    """The stable application binding name, unique within the runner."""
    configuration: ItemChangedTriggerConfiguration
    """Selects the watched board."""
    target: TriggerTarget[ItemChangedEvent]
    """Routes each change into the application.

    A target wrapped with `new_durable_trigger_target` records each change before the cursor commits past
    it; any other target consumes it first.
    """
    cursor: CursorStore | None = None
    """Stores the binding's position and snapshot, such as `LoadedProject.poll_cursor_store`.

    None keeps it in memory, so every process start records a new baseline and changes made while
    stopped are lost.
    """


@dataclass(frozen=True, slots=True)
class ProjectItemChangedTriggerRoute:
    """Selects one ``itemChanged`` binding saved in the project configuration."""

    binding_name: str
    """Names the saved binding."""
    target: TriggerTarget[ItemChangedEvent]
    """Routes each change into the application, usually `new_dex_flow_trigger_target`."""


def validate_item_changed_trigger_configuration(configuration: ItemChangedTriggerConfiguration) -> None:
    """Require a numeric monday.com board ID without surrounding whitespace; raises ValueError otherwise."""
    board_id = configuration.board_id if isinstance(configuration.board_id, str) else ""
    if not NUMERIC_ID.fullmatch(board_id.strip()):
        raise ValueError("boardId must be a numeric monday.com ID such as 1234567890")
    if board_id != board_id.strip():
        raise ValueError("boardId must not have surrounding whitespace")


def new_item_trigger_runner(
    connection: Connection,
    item_changed_routes: Sequence[ItemChangedTriggerRoute],
    *,
    logger: TriggerLogger | None = None,
) -> PollRunner:
    """Create one poll runner for the connection's ``itemChanged`` bindings.

    `PollRunner.run` replays each durable target, records a baseline for a binding without a cursor, then
    polls every binding each `Client.poll_interval` with jitter. A rate-limit error pauses every binding
    of the connection for monday.com's Retry-After or retry_in_seconds, and DAILY_LIMIT_EXCEEDED pauses
    it until midnight UTC; other failures back off per binding. More changes than one 10,000-item scan
    can read record a new baseline and log a WARN record. Delivery is at least once, and each item
    version keeps one Trigger event ID. logger receives poll records without payloads or credentials;
    None uses the standard logging adapter. Raises ValueError for a connection without a client, no
    routes, a blank binding name, a missing target, or an invalid configuration.
    """
    client = _connection_client(connection)
    if not item_changed_routes:
        raise ValueError("monday.com itemChanged routes are required")
    pollers: list[Poller[TriggerEvent[ItemChangedEvent]]] = [
        _new_item_changed_poller(connection, client, route) for route in item_changed_routes
    ]
    return PollRunner(PollRunnerConfig(connection.reference, _poll_schedule(client), pollers, logger=logger))


def new_project_item_trigger_runner(
    project: LoadedProject,
    connection: Connection,
    item_changed_routes: Sequence[ProjectItemChangedTriggerRoute],
    *,
    logger: TriggerLogger | None = None,
) -> PollRunner:
    """Create a durable runner for ``itemChanged`` bindings saved in the loaded project.

    connection is the one `new_project_connection` opened from project; the caller keeps owning it.
    Each route loads its binding's saved board and records changes in the binding's project Trigger
    inbox before its position commits in the binding's project poll cursor, so a restarted or replaced
    replica neither misses nor repeats a change. The inbox payload is the Go connector's JSON shape, so
    Go and Python replicas share it. Raises ValueError for a binding without a saved board, an invalid
    saved configuration, and as `new_item_trigger_runner` does.
    """
    if project is None:
        raise ValueError("monday.com itemChanged runner requires the loaded project configuration")
    routes: list[ItemChangedTriggerRoute] = []
    for route in item_changed_routes:
        name = json.dumps(route.binding_name)
        if route.target is None:
            raise ValueError(f"monday.com itemChanged binding {name} target is required")
        key = TriggerInboxKey(CONNECTOR_ID, connection.reference.name, _TRIGGER_NAME, route.binding_name)
        try:
            configuration = decode_trigger_configuration(
                project.configuration, key, decode_item_changed_trigger_configuration
            )
        except ObjectNotFoundError:
            raise ValueError(
                f"monday itemChanged binding {name} has no saved board; "
                "enter its board ID in Dex Web and restart"
            ) from None
        except ValueError as error:
            raise ValueError(f"monday itemChanged binding {name} configuration: {error}") from error
        target = new_durable_trigger_target(
            project.trigger_inbox(key),
            route.target,
            decode_payload=decode_item_changed_event_payload,
            logger=logger,
        )
        routes.append(
            ItemChangedTriggerRoute(
                route.binding_name,
                configuration,
                target,
                project.poll_cursor_store(PollCursorKey.from_trigger_inbox_key(key)),
            )
        )
    return new_item_trigger_runner(connection, routes, logger=logger)


def new_item_changed_trigger(
    *,
    connection: Connection,
    binding_name: str,
    configuration: ItemChangedTriggerConfiguration,
    target: TriggerTarget[ItemChangedEvent],
    logger: TriggerLogger | None = None,
) -> TriggerRunner:
    """Create a single ``itemChanged`` Trigger runner, as Go's generated NewItemChangedTrigger does.

    It has no project storage, so it keeps the cursor in memory and records a new baseline on every
    start; changes made while stopped are lost. Use `new_project_item_trigger_runner` for durable
    delivery. Raises ValueError as `new_item_trigger_runner` does.
    """
    client = _connection_client(connection)
    if not (binding_name or "").strip() or target is None:
        raise ValueError("monday.com itemChanged binding name and target are required")
    _validate_configuration(configuration)
    binding = TriggerBindingRef(connection.reference, ITEM_CHANGED_TRIGGER_DEFINITION.trigger, binding_name)
    source = new_binding_trigger_source(
        BindingPollerConfig(
            ITEM_CHANGED_TRIGGER_DEFINITION,
            binding,
            new_memory_cursor_store(),
            ItemChangeFetcher(client, connection.reference, binding.trigger, configuration),
            snapshot=ITEM_CHANGED_SNAPSHOT_OPTIONS,
        ),
        _poll_schedule(client),
    )
    return new_trigger(TriggerConfig(ITEM_CHANGED_TRIGGER_DEFINITION, binding, source, target, logger))


def decode_item_changed_event_payload(payload_json: str, payload_bytes: bytes) -> ItemChangedEvent:
    """Decode a replayed inbox payload, written by a Go or Python replica, as an ItemChangedEvent.

    Raises ValueError for a payload that is not an itemChanged event.
    """
    try:
        decoded = CodecRegistry().resolve(ItemChangedEvent).decode(Value(WireKind.JSON, payload_json))
    except (TypeError, ValueError) as error:
        raise ValueError("stored monday.com itemChanged payload is invalid") from error
    if not isinstance(decoded, ItemChangedEvent) or decoded.change_kind not in tuple(ItemChangeKind):
        raise ValueError("stored monday.com itemChanged payload is invalid")
    return decoded


def _new_item_changed_poller(
    connection: Connection, client: Client, route: ItemChangedTriggerRoute
) -> Poller[TriggerEvent[ItemChangedEvent]]:
    if not (route.binding_name or "").strip() or route.target is None:
        raise ValueError("monday.com itemChanged binding name and target are required")
    _validate_configuration(route.configuration)
    binding = TriggerBindingRef(
        connection.reference, ITEM_CHANGED_TRIGGER_DEFINITION.trigger, route.binding_name
    )
    return new_binding_poller(
        BindingPollerConfig(
            ITEM_CHANGED_TRIGGER_DEFINITION,
            binding,
            route.cursor if route.cursor is not None else new_memory_cursor_store(),
            ItemChangeFetcher(client, connection.reference, binding.trigger, route.configuration),
            route.target,
            ITEM_CHANGED_SNAPSHOT_OPTIONS,
        )
    )


def _validate_configuration(configuration: ItemChangedTriggerConfiguration) -> None:
    if configuration is None:
        raise ValueError("monday itemChanged configuration is required")
    try:
        validate_item_changed_trigger_configuration(configuration)
    except ValueError as error:
        raise ValueError(f"monday itemChanged configuration: {error}") from error


def _connection_client(connection: Connection) -> Client:
    client = getattr(connection, "_client", None) if connection is not None else None
    if not isinstance(client, Client):
        raise ValueError(
            "monday.com itemChanged Trigger requires a connection opened with a monday.com client"
        )
    return client


def _poll_schedule(client: Client) -> PollSchedule:
    # A zero maximum backoff uses the kit's cap, which grows with a long poll interval.
    return PollSchedule(client.poll_interval)


__all__ = [
    "ITEM_CHANGED_TRIGGER_DEFINITION",
    "ItemChangeKind",
    "ItemChangedTriggerRoute",
    "ProjectItemChangedTriggerRoute",
    "decode_item_changed_event_payload",
    "decode_item_changed_trigger_configuration",
    "define_item_changed_trigger_binding",
    "new_item_changed_trigger",
    "new_item_trigger_runner",
    "new_project_item_trigger_runner",
    "validate_item_changed_trigger_configuration",
]
