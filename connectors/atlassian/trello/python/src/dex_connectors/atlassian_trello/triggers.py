# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start Flows for each card action on a Trello board, through the ``cardChanged`` poll Trigger.

Each binding polls the board's action history with the poll Trigger kit, at parity with the Go
connector, and shares its cursor documents byte for byte. The first poll records a baseline and delivers
no earlier action. `new_project_card_trigger_runner` wires stored bindings to durable
project inboxes and poll cursors, so a restarted Worker neither misses nor repeats an action.
"""

import json
from collections.abc import Sequence
from dataclasses import dataclass
from datetime import timedelta
from typing import Final

import dex
import httpx
from dex_connectors.sdk import (
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

from ._generated.definitions import CARD_CHANGED_TRIGGER_DEFINITION, CONNECTOR_ID
from ._generated.triggers import (
    decode_card_changed_trigger_configuration,
    define_card_changed_trigger_binding,
)
from .card_action_fetcher import (
    CARD_ACTION_COMMENTED,
    CARD_ACTION_CONVERTED_FROM_CHECK_ITEM,
    CARD_ACTION_COPIED,
    CARD_ACTION_CREATED,
    CARD_ACTION_DELETED,
    CARD_ACTION_EMAILED,
    CARD_ACTION_MOVED_TO_BOARD,
    CARD_ACTION_UPDATED,
    WATCHED_CARD_ACTION_TYPES,
    CardActionFetcher,
    card_change_event_id,
    validate_card_changed_trigger_configuration,
)
from .client import Client
from .connection import Connection, new_project_connection
from .models import CardChangedEvent, CardChangedTriggerConfiguration

_TRIGGER_NAME: Final = CARD_CHANGED_TRIGGER_DEFINITION.trigger.trigger_name


@dataclass(frozen=True, slots=True)
class CardChangedTriggerRoute:
    """Bind one ``cardChanged`` configuration to its application target and cursor."""

    binding_name: str
    """The stable application binding name, unique within the runner."""
    configuration: CardChangedTriggerConfiguration
    """Selects the watched board, by its 24-character ID or 8-character short link."""
    target: TriggerTarget[CardChangedEvent]
    """Routes each card action into the application.

    A target from `new_durable_trigger_target` records each action before the cursor commits past it;
    any other target consumes it first.
    """
    cursor: CursorStore | None = None
    """Stores the binding's position, such as `LoadedProject.poll_cursor_store`.

    None keeps it in memory, so every process start records a new baseline and actions made while
    stopped are lost.
    """


@dataclass(frozen=True, slots=True)
class CardTriggerRunnerConfig:
    """Configure every Trello Trigger binding polled through one connection."""

    connection: Connection
    """A connection from `new_connection` with a `Client`, or `new_project_connection`."""
    card_changed_routes: Sequence[CardChangedTriggerRoute]
    """The ``cardChanged`` bindings; at least one is required."""
    logger: TriggerLogger | None = None
    """Receives seed, reseed, failed-poll, and rate-limit records with binding identity, never payloads
    or credentials; None uses the standard logging adapter."""


def new_card_trigger_runner(config: CardTriggerRunnerConfig) -> PollRunner:
    """Create one poll runner for the configured ``cardChanged`` bindings of one connection.

    `PollRunner.run` replays each durable target, records a baseline for a binding without a cursor, then
    polls every binding each ``pollInterval`` with jitter. One poll re-reads the last minute of the
    board's action history, skips the action IDs it already delivered, and delivers the rest oldest first,
    500 per page. A backlog over 10,000 actions, or a 400 that rejects since or before, records a new
    baseline; actions in that gap are not delivered. A 429 pauses every binding of the connection for
    Retry-After, or one 10-second Trello rate-limit window without it; other failures back off per
    binding up to 15 minutes. Delivery is at least once, and each action keeps one Trigger event ID
    across refetches. Raises ValueError for empty routes, a blank binding name or missing target, an invalid
    configuration, or a connection without a Trello `Client`.
    """
    client = _client_of(config.connection)
    if not config.card_changed_routes:
        raise ValueError("Trello cardChanged routes are required")
    pollers = []
    for route in config.card_changed_routes:
        if not (route.binding_name or "").strip() or route.target is None:
            raise ValueError("Trello cardChanged binding name and target are required")
        _validate_configuration(route.configuration)
        pollers.append(
            new_binding_poller(
                BindingPollerConfig(
                    definition=CARD_CHANGED_TRIGGER_DEFINITION,
                    binding=TriggerBindingRef(
                        config.connection.reference,
                        CARD_CHANGED_TRIGGER_DEFINITION.trigger,
                        route.binding_name,
                    ),
                    cursor=route.cursor if route.cursor is not None else new_memory_cursor_store(),
                    fetcher=CardActionFetcher(client, config.connection.reference, route.configuration),
                    target=route.target,
                )
            )
        )
    return PollRunner(
        PollRunnerConfig(config.connection.reference, _poll_schedule(client), pollers, logger=config.logger)
    )


def new_card_changed_trigger(
    *,
    connection: Connection,
    binding_name: str,
    configuration: CardChangedTriggerConfiguration,
    target: TriggerTarget[CardChangedEvent],
    logger: TriggerLogger | None = None,
) -> TriggerRunner:
    """Create one ``cardChanged`` binding as a `TriggerRunner`, polling on its own schedule.

    It has no project storage, so it keeps the cursor in memory and records a new baseline on every
    start; use `new_project_card_trigger_runner` for a durable cursor. Raises ValueError for an invalid
    configuration or a connection without a Trello `Client`.
    """
    client = _client_of(connection)
    _validate_configuration(configuration)
    binding = TriggerBindingRef(connection.reference, CARD_CHANGED_TRIGGER_DEFINITION.trigger, binding_name)
    source = new_binding_trigger_source(
        BindingPollerConfig(
            definition=CARD_CHANGED_TRIGGER_DEFINITION,
            binding=binding,
            cursor=new_memory_cursor_store(),
            fetcher=CardActionFetcher(client, connection.reference, configuration),
        ),
        _poll_schedule(client),
    )
    return new_trigger(
        TriggerConfig(
            definition=CARD_CHANGED_TRIGGER_DEFINITION,
            binding=binding,
            source=source,
            target=target,
            logger=logger,
        )
    )


@dataclass(frozen=True, slots=True)
class ProjectCardChangedTriggerRoute:
    """Select one ``cardChanged`` binding saved in the project configuration."""

    binding_name: str
    """The saved binding name."""
    target: TriggerTarget[CardChangedEvent]
    """Routes each card action into the application, usually `new_dex_flow_trigger_target`."""


class CardTriggerRunner:
    """Poll the project's ``cardChanged`` bindings through the connection it opened.

    Run it for the life of the Worker process and close it with `aclose` afterwards.
    """

    def __init__(self, poll_runner: PollRunner, connection: Connection) -> None:
        """Wrap a poll runner; applications use `new_project_card_trigger_runner`."""
        if poll_runner is None or connection is None:
            raise ValueError("Trello card Trigger runner and connection are required")
        self.poll_runner = poll_runner
        """The connection's poll runner."""
        self.connection = connection
        """The connection the runner opened and `aclose` closes."""

    async def run(self, context: TriggerContext | None = None) -> None:
        """Replay each binding's inbox, then poll until context is cancelled, as `PollRunner.run` does.

        Raises TriggerDeliveryAbortedError after cancellation.
        """
        await self.poll_runner.run(context)

    def wake_poller(self, binding_name: str) -> None:
        """Make the named binding poll now, without shortening a rate-limit pause."""
        self.poll_runner.wake_poller(binding_name)

    async def aclose(self) -> None:
        """Close the connection the runner opened, after `run` returns."""
        await self.connection.aclose()


async def new_project_card_trigger_runner(
    project: LoadedProject,
    connection_name: str,
    routes: Sequence[ProjectCardChangedTriggerRoute],
    *,
    http_client: httpx.AsyncClient | None = None,
    logger: TriggerLogger | None = None,
) -> CardTriggerRunner:
    """Open the connection and its saved bindings and create one durable poll runner.

    Each route records card actions in its binding's project Trigger inbox before its position commits in
    the binding's project poll cursor, so a restarted or replaced replica neither misses nor repeats an
    action. Raises ValueError for empty routes, a blank or repeated binding name, a binding without a
    saved board, or an invalid saved configuration.
    """
    if project is None:
        raise ValueError("Trello card Trigger runner requires the loaded project configuration")
    if not routes:
        raise ValueError("Trello cardChanged routes are required")
    connection = new_project_connection(project, connection_name, http_client)
    try:
        durable_routes = []
        binding_names: set[str] = set()
        for route in routes:
            binding_name = (route.binding_name or "").strip()
            if not binding_name or route.target is None:
                raise ValueError("Trello cardChanged binding name and target are required")
            if binding_name in binding_names:
                raise ValueError(f"Trello cardChanged binding name {json.dumps(binding_name)} is duplicated")
            binding_names.add(binding_name)
            key = TriggerInboxKey(CONNECTOR_ID, connection_name, _TRIGGER_NAME, binding_name)
            try:
                configuration = decode_trigger_configuration(
                    project.configuration, key, decode_card_changed_trigger_configuration
                )
            except (ObjectNotFoundError, ValueError) as error:
                raise ValueError(
                    f"trello cardChanged binding {json.dumps(binding_name)} configuration "
                    f"(save the watched board in Dex Web): {error}"
                ) from error
            target = new_durable_trigger_target(
                project.trigger_inbox(key),
                route.target,
                decode_payload=decode_card_changed_event_payload,
                logger=logger,
            )
            durable_routes.append(
                CardChangedTriggerRoute(
                    binding_name,
                    configuration,
                    target,
                    project.poll_cursor_store(PollCursorKey.from_trigger_inbox_key(key)),
                )
            )
        poll_runner = new_card_trigger_runner(CardTriggerRunnerConfig(connection, durable_routes, logger))
    except BaseException:
        await connection.aclose()
        raise
    return CardTriggerRunner(poll_runner, connection)


def decode_card_changed_event_payload(payload_json: str, payload_bytes: bytes) -> CardChangedEvent:
    """Decode a replayed inbox payload written by this package or by the Go connector.

    Raises ValueError for a payload that is not an ``CardChangedEvent``.
    """
    try:
        decoded = (
            dex.CodecRegistry().resolve(CardChangedEvent).decode(dex.Value(dex.WireKind.JSON, payload_json))
        )
    except (TypeError, ValueError) as error:
        raise ValueError("pending Trello card action is invalid") from error
    if not isinstance(decoded, CardChangedEvent):
        raise ValueError("pending Trello card action is invalid")
    return decoded


def _validate_configuration(configuration: CardChangedTriggerConfiguration) -> None:
    try:
        validate_card_changed_trigger_configuration(configuration)
    except ValueError as error:
        raise ValueError(f"trello cardChanged configuration: {error}") from error


def _client_of(connection: Connection) -> Client:
    client = getattr(connection, "_client", None)
    if not isinstance(client, Client):
        raise ValueError("Trello Triggers require a connection from new_project_connection or new_connection")
    return client


def _poll_schedule(client: Client) -> PollSchedule:
    # Zero maximum backoff uses the kit's cap, which grows with a long poll interval.
    return PollSchedule(timedelta(seconds=client.poll_interval_seconds))


__all__ = [
    "CARD_CHANGED_TRIGGER_DEFINITION",
    "CARD_ACTION_COMMENTED",
    "CARD_ACTION_CONVERTED_FROM_CHECK_ITEM",
    "CARD_ACTION_COPIED",
    "CARD_ACTION_CREATED",
    "CARD_ACTION_DELETED",
    "CARD_ACTION_EMAILED",
    "CARD_ACTION_MOVED_TO_BOARD",
    "CARD_ACTION_UPDATED",
    "CardChangedEvent",
    "CardChangedTriggerConfiguration",
    "CardChangedTriggerRoute",
    "CardTriggerRunner",
    "CardTriggerRunnerConfig",
    "ProjectCardChangedTriggerRoute",
    "decode_card_changed_event_payload",
    "decode_card_changed_trigger_configuration",
    "define_card_changed_trigger_binding",
    "card_change_event_id",
    "new_card_changed_trigger",
    "new_card_trigger_runner",
    "new_project_card_trigger_runner",
    "validate_card_changed_trigger_configuration",
    "WATCHED_CARD_ACTION_TYPES",
]
