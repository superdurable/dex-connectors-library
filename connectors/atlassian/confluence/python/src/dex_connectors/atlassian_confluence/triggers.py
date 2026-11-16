# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start Flows when Confluence pages are created or updated, through the ``pageChanged`` poll Trigger.

Each binding polls Confluence CQL search, newest modification first, with the poll Trigger kit and a
keyed snapshot of recently seen page versions, at parity with the Go connector, and shares its cursor
documents byte for byte. The first poll records a baseline and delivers no existing page.
`new_project_page_trigger_runner` wires stored bindings to durable project inboxes and poll cursors, so a
restarted Worker neither misses nor repeats a change.
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

from ._generated.definitions import CONNECTOR_ID, PAGE_CHANGED_TRIGGER_DEFINITION
from ._generated.triggers import (
    decode_page_changed_trigger_configuration,
    define_page_changed_trigger_binding,
)
from ._page_change_cursor import page_change_event_id
from ._page_change_fetcher import (
    PAGE_CHANGE_CREATED,
    PAGE_CHANGE_SNAPSHOT_OPTIONS,
    PAGE_CHANGE_UPDATED,
    PageChangeFetcher,
    validate_page_changed_trigger_configuration,
)
from .client import Client
from .connection import Connection, new_project_connection
from .models import PageChangedEvent, PageChangedTriggerConfiguration

_TRIGGER_NAME: Final = PAGE_CHANGED_TRIGGER_DEFINITION.trigger.trigger_name


@dataclass(frozen=True, slots=True)
class PageChangedTriggerRoute:
    """Bind one ``pageChanged`` configuration to its application target and cursor."""

    binding_name: str
    """The stable application binding name, unique within the runner."""
    configuration: PageChangedTriggerConfiguration
    """Selects the watched space; blank watches every space the connection can view."""
    target: TriggerTarget[PageChangedEvent]
    """Routes each change into the application.

    A target from `new_durable_trigger_target` records each change before the cursor commits past it;
    any other target consumes it first.
    """
    cursor: CursorStore | None = None
    """Stores the binding's position, such as `LoadedProject.poll_cursor_store`.

    None keeps it in memory, so every process start records a new baseline and changes made while
    stopped are lost.
    """


@dataclass(frozen=True, slots=True)
class PageTriggerRunnerConfig:
    """Configure every Confluence Trigger binding polled through one connection."""

    connection: Connection
    """A connection from `new_connection` with a `Client`, or `new_project_connection`."""
    page_changed_routes: Sequence[PageChangedTriggerRoute]
    """The ``pageChanged`` bindings; at least one is required."""
    logger: TriggerLogger | None = None
    """Receives seed, reseed, failed-poll, and rate-limit records with binding identity, never payloads
    or credentials; None uses the standard logging adapter."""


def new_page_trigger_runner(config: PageTriggerRunnerConfig) -> PollRunner:
    """Create one poll runner for the configured ``pageChanged`` bindings of one connection.

    `PollRunner.run` replays each durable target, records a baseline for a binding without a cursor, then
    polls every binding each ``pollInterval`` with jitter. Each poll reads CQL search, newest
    modification first, back to ten minutes before the previous scan began, and delivers each page
    version not delivered before. A 429, or a 503 with Retry-After, pauses every binding of the connection
    for Retry-After, or backs off exponentially without it; other failures back off per binding up to 15
    minutes, and none of them moves the cursor. A cursor for another site or space records a new baseline
    and logs a WARN record. Delivery is at least once, and each page version keeps one Trigger event ID
    across refetches. Raises ValueError for empty routes, a blank binding name or missing target, an invalid
    configuration, or a connection without a Confluence `Client`.
    """
    client = _client_of(config.connection)
    if not config.page_changed_routes:
        raise ValueError("Confluence pageChanged routes are required")
    pollers = []
    for route in config.page_changed_routes:
        if not (route.binding_name or "").strip() or route.target is None:
            raise ValueError("Confluence pageChanged binding name and target are required")
        _validate_configuration(route.configuration)
        pollers.append(
            new_binding_poller(
                BindingPollerConfig(
                    definition=PAGE_CHANGED_TRIGGER_DEFINITION,
                    binding=TriggerBindingRef(
                        config.connection.reference,
                        PAGE_CHANGED_TRIGGER_DEFINITION.trigger,
                        route.binding_name,
                    ),
                    cursor=route.cursor if route.cursor is not None else new_memory_cursor_store(),
                    fetcher=PageChangeFetcher(client, config.connection.reference, route.configuration),
                    target=route.target,
                    snapshot=PAGE_CHANGE_SNAPSHOT_OPTIONS,
                )
            )
        )
    return PollRunner(
        PollRunnerConfig(config.connection.reference, _poll_schedule(client), pollers, logger=config.logger)
    )


def new_page_changed_trigger(
    *,
    connection: Connection,
    binding_name: str,
    configuration: PageChangedTriggerConfiguration,
    target: TriggerTarget[PageChangedEvent],
    logger: TriggerLogger | None = None,
) -> TriggerRunner:
    """Create one ``pageChanged`` binding as a `TriggerRunner`, polling on its own schedule.

    It has no project storage, so it keeps the cursor in memory and records a new baseline on every
    start; use `new_project_page_trigger_runner` for a durable cursor. Raises ValueError for an invalid
    configuration or a connection without a Confluence `Client`.
    """
    client = _client_of(connection)
    _validate_configuration(configuration)
    binding = TriggerBindingRef(connection.reference, PAGE_CHANGED_TRIGGER_DEFINITION.trigger, binding_name)
    source = new_binding_trigger_source(
        BindingPollerConfig(
            definition=PAGE_CHANGED_TRIGGER_DEFINITION,
            binding=binding,
            cursor=new_memory_cursor_store(),
            fetcher=PageChangeFetcher(client, connection.reference, configuration),
            snapshot=PAGE_CHANGE_SNAPSHOT_OPTIONS,
        ),
        _poll_schedule(client),
    )
    return new_trigger(
        TriggerConfig(
            definition=PAGE_CHANGED_TRIGGER_DEFINITION,
            binding=binding,
            source=source,
            target=target,
            logger=logger,
        )
    )


@dataclass(frozen=True, slots=True)
class ProjectPageChangedTriggerRoute:
    """Select one ``pageChanged`` binding saved in the project configuration."""

    binding_name: str
    """The saved binding name."""
    target: TriggerTarget[PageChangedEvent]
    """Routes each change into the application, usually `new_dex_flow_trigger_target`."""


class PageTriggerRunner:
    """Poll the project's ``pageChanged`` bindings through the connection it opened.

    Run it for the life of the Worker process and close it with `aclose` afterwards.
    """

    def __init__(self, poll_runner: PollRunner, connection: Connection) -> None:
        """Wrap a poll runner; applications use `new_project_page_trigger_runner`."""
        if poll_runner is None or connection is None:
            raise ValueError("Confluence page Trigger runner and connection are required")
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


async def new_project_page_trigger_runner(
    project: LoadedProject,
    connection_name: str,
    routes: Sequence[ProjectPageChangedTriggerRoute],
    *,
    http_client: httpx.AsyncClient | None = None,
    logger: TriggerLogger | None = None,
) -> PageTriggerRunner:
    """Open the connection and its saved bindings and create one durable poll runner.

    Each route records changes in its binding's project Trigger inbox before its position commits in the
    binding's project poll cursor, so a restarted or replaced replica neither misses nor repeats a change.
    An unsaved binding watches every space the connection can view, as a blank picker does. Raises
    ValueError for empty routes, a blank or repeated binding name, or an invalid saved configuration.
    """
    if project is None:
        raise ValueError("Confluence page Trigger runner requires the loaded project configuration")
    if not routes:
        raise ValueError("Confluence pageChanged routes are required")
    connection = new_project_connection(project, connection_name, http_client)
    try:
        durable_routes = []
        binding_names: set[str] = set()
        for route in routes:
            binding_name = (route.binding_name or "").strip()
            if not binding_name or route.target is None:
                raise ValueError("Confluence pageChanged binding name and target are required")
            if binding_name in binding_names:
                raise ValueError(
                    f"Confluence pageChanged binding name {json.dumps(binding_name)} is duplicated"
                )
            binding_names.add(binding_name)
            key = TriggerInboxKey(CONNECTOR_ID, connection_name, _TRIGGER_NAME, binding_name)
            try:
                configuration = decode_trigger_configuration(
                    project.configuration, key, decode_page_changed_trigger_configuration
                )
            except ObjectNotFoundError:
                configuration = PageChangedTriggerConfiguration()
            except ValueError as error:
                raise ValueError(
                    f"confluence pageChanged binding {json.dumps(binding_name)} configuration: {error}"
                ) from error
            target = new_durable_trigger_target(
                project.trigger_inbox(key),
                route.target,
                decode_payload=decode_page_changed_event_payload,
                logger=logger,
            )
            durable_routes.append(
                PageChangedTriggerRoute(
                    binding_name,
                    configuration,
                    target,
                    project.poll_cursor_store(PollCursorKey.from_trigger_inbox_key(key)),
                )
            )
        poll_runner = new_page_trigger_runner(PageTriggerRunnerConfig(connection, durable_routes, logger))
    except BaseException:
        await connection.aclose()
        raise
    return PageTriggerRunner(poll_runner, connection)


def decode_page_changed_event_payload(payload_json: str, payload_bytes: bytes) -> PageChangedEvent:
    """Decode a replayed inbox payload written by this package or by the Go connector.

    Raises ValueError for a payload that is not an ``PageChangedEvent``.
    """
    try:
        decoded = (
            dex.CodecRegistry().resolve(PageChangedEvent).decode(dex.Value(dex.WireKind.JSON, payload_json))
        )
    except (TypeError, ValueError) as error:
        raise ValueError("pending Confluence page change is invalid") from error
    if not isinstance(decoded, PageChangedEvent):
        raise ValueError("pending Confluence page change is invalid")
    return decoded


def _validate_configuration(configuration: PageChangedTriggerConfiguration) -> None:
    try:
        validate_page_changed_trigger_configuration(configuration)
    except ValueError as error:
        raise ValueError(f"confluence pageChanged configuration: {error}") from error


def _client_of(connection: Connection) -> Client:
    client = getattr(connection, "_client", None)
    if not isinstance(client, Client):
        raise ValueError(
            "Confluence Triggers require a connection from new_project_connection or new_connection"
        )
    return client


def _poll_schedule(client: Client) -> PollSchedule:
    # Zero maximum backoff uses the kit's cap, which grows with a long poll interval.
    return PollSchedule(timedelta(seconds=client.poll_interval_seconds))


__all__ = [
    "PAGE_CHANGED_TRIGGER_DEFINITION",
    "PAGE_CHANGE_CREATED",
    "PAGE_CHANGE_UPDATED",
    "PageChangedEvent",
    "PageChangedTriggerConfiguration",
    "PageChangedTriggerRoute",
    "PageTriggerRunner",
    "PageTriggerRunnerConfig",
    "ProjectPageChangedTriggerRoute",
    "decode_page_changed_event_payload",
    "decode_page_changed_trigger_configuration",
    "define_page_changed_trigger_binding",
    "page_change_event_id",
    "new_page_changed_trigger",
    "new_page_trigger_runner",
    "new_project_page_trigger_runner",
    "validate_page_changed_trigger_configuration",
]
