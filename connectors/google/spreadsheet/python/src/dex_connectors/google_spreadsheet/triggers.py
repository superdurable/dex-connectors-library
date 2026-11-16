# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start Flows from rows appended to a Google Sheets tab through the polled ``rowAdded`` Trigger.

The Sheets API has no change notifications for cell values, so each binding polls its tab with the Python
Connector SDK's poll kit. `new_project_row_added_trigger_runner` runs every binding of one connection with
a durable cursor and Trigger inbox per binding; the cursor documents match the Go connector's byte for byte,
so Go and Python replicas share them.
"""

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
    RequestLimiter,
    new_binding_poller,
    new_binding_trigger_source,
    new_memory_cursor_store,
)
from dex_connectors.sdk.projectconfig import (
    LoadedProject,
    PollCursorKey,
    TriggerInboxKey,
    decode_trigger_configuration,
    new_durable_trigger_target,
)

from ._generated.definitions import CONNECTOR_ID, ROW_ADDED_TRIGGER_DEFINITION
from ._generated.triggers import decode_row_added_trigger_configuration, define_row_added_trigger_binding
from .client import Client
from .connection import Connection, new_project_connection
from .models import RowAddedEvent, RowAddedTriggerConfiguration
from .row_added_cursor import GO_SPACE
from .row_added_fetcher import RowAddedFetcher, validate_row_added_trigger_configuration

UI_UNIT_SPREADSHEET_PICKER: Final = "spreadsheetPicker"
"""The Studio unit that opens the Google Picker and stores the chosen spreadsheet ID."""
UI_SPREADSHEET_PICKER_PORT_SPREADSHEET_ID: Final = "spreadsheetId"
"""The spreadsheet picker port holding the spreadsheet ID; bind it to ``/spreadsheetId``."""
UI_UNIT_SHEET_TAB_PICKER: Final = "sheetTabPicker"
"""The Studio unit that lists one spreadsheet's tab titles and stores the exact chosen title."""
UI_SHEET_TAB_PICKER_PORT_SPREADSHEET_ID: Final = "spreadsheetId"
"""The tab picker input port naming the spreadsheet whose tabs it lists."""
UI_SHEET_TAB_PICKER_PORT_TAB: Final = "tab"
"""The tab picker port holding the exact tab title; bind it to ``/sheetName``."""

_TRIGGER_NAME: Final = ROW_ADDED_TRIGGER_DEFINITION.trigger.trigger_name
_MAXIMUM_BACKOFF: Final = timedelta(minutes=5)


def _connection_client(connection: Connection) -> Client:
    if connection is None:
        raise ValueError("Google Sheets rowAdded connection is required")
    client = connection._client
    if client is None:
        raise ValueError("Google Sheets rowAdded requires a connection built from a Client, not a mock")
    return client


def row_added_poll_schedule(client: Client) -> PollSchedule:
    """Return the schedule every rowAdded binding of the client's connection shares.

    It polls every ``pollInterval`` and backs off after failures up to five minutes, or up to the interval
    when that is longer.
    """
    interval = timedelta(seconds=client.config.poll_interval_seconds)
    return PollSchedule(interval, maximum_backoff=max(_MAXIMUM_BACKOFF, interval))


def decode_row_added_event_payload(payload_json: str, payload_bytes: bytes) -> RowAddedEvent:
    """Decode a pending inbox payload that Go or Python recorded; raise ValueError when invalid."""
    decoded = dex.CodecRegistry().resolve(RowAddedEvent).decode(dex.Value(dex.WireKind.JSON, payload_json))
    if not isinstance(decoded, RowAddedEvent):
        raise ValueError("pending Google Sheets rowAdded event is invalid")
    return decoded


def new_row_added_trigger(
    *,
    connection: Connection,
    binding_name: str,
    configuration: RowAddedTriggerConfiguration,
    target: TriggerTarget[RowAddedEvent],
    logger: TriggerLogger | None = None,
) -> TriggerRunner:
    """Create one ``rowAdded`` binding's runner whose cursor lives in process memory.

    A restart seeds again, so rows added while the process was stopped are not delivered; use it only in
    tests or applications without project storage, and `new_project_row_added_trigger_runner` otherwise.
    The first poll seeds at the tab's last row without delivering existing rows. Raises ValueError for a
    blank spreadsheet ID, tab title, or binding name, or a mock connection.
    """
    validate_row_added_trigger_configuration(configuration)
    client = _connection_client(connection)
    binding = TriggerBindingRef(connection.reference, ROW_ADDED_TRIGGER_DEFINITION.trigger, binding_name)
    poller_config = BindingPollerConfig[RowAddedEvent](
        ROW_ADDED_TRIGGER_DEFINITION,
        binding,
        new_memory_cursor_store(),
        RowAddedFetcher(client, binding, configuration, logger),
    )
    return new_trigger(
        TriggerConfig(
            definition=ROW_ADDED_TRIGGER_DEFINITION,
            binding=binding,
            source=new_binding_trigger_source(poller_config, row_added_poll_schedule(client)),
            target=target,
            logger=logger,
        )
    )


@dataclass(frozen=True, slots=True)
class RowAddedTriggerRoute:
    """Bind one ``rowAdded`` binding's configuration and cursor to its application target."""

    binding_name: str
    """The configured Trigger binding name, unique within one runner."""
    configuration: RowAddedTriggerConfiguration
    """Selects the polled spreadsheet tab."""
    cursor: CursorStore
    """Persists the last processed row, normally `LoadedProject.poll_cursor_store`.

    Use `new_memory_cursor_store` only when rows added while the process is stopped may be skipped.
    """
    target: TriggerTarget[RowAddedEvent]
    """Receives each added row. A target wrapped by `new_durable_trigger_target` records the row before
    the cursor moves past it; any other target consumes it first."""


@dataclass(frozen=True, slots=True)
class RowAddedTriggerRunnerConfig:
    """Configure every ``rowAdded`` binding of one Google Sheets connection."""

    connection: Connection
    """The connection whose ``pollInterval`` and ``pollPageSize`` settings apply."""
    routes: Sequence[RowAddedTriggerRoute]
    """Polled in this order whenever several are due; at least one is required."""
    request_limiter: RequestLimiter | None = None
    """Optionally paces every poll request, for example to share a per-user Sheets quota."""
    logger: TriggerLogger | None = None
    """Receives poll, realignment, and delivery records with the binding identity and row numbers, never
    cell values, tab titles, or credentials. None uses the `dex_connectors.sdk.trigger` logger."""


class RowAddedTriggerRunner:
    """Polls each ``rowAdded`` binding's tab of one connection from one task.

    Each binding seeds its cursor at the tab's last row on its first poll, so existing rows never start
    Flows. Later polls read the header and up to ``pollPageSize`` rows after the last processed row with
    one values:batchGet request and deliver each non-empty row, in row order, before the cursor commits.
    The cursor stores the last processed row number and a SHA-256 of its cells: when rows above it are
    deleted or inserted, the runner finds that row again and continues after it; when it was edited in
    place, the runner keeps its position; when the tab now ends above it, the runner reseeds and logs the
    skipped gap. Delivery is at least once, and each event ID is a pure function of the tab, row number,
    and cells. Build it with `new_row_added_trigger_runner` or `new_project_row_added_trigger_runner`.
    """

    def __init__(self, poll_runner: PollRunner, owned_connection: Connection | None = None) -> None:
        """Wrap a poll runner; close owned_connection in `aclose`."""
        if poll_runner is None:
            raise ValueError("Google Sheets rowAdded poll runner is required")
        self._poll_runner = poll_runner
        self._owned_connection = owned_connection

    async def run(self, context: TriggerContext | None = None) -> None:
        """Replay pending rows, seed missing cursors, and poll every binding until context is cancelled.

        A failed poll backs off exponentially up to five minutes, and HTTP 429 pauses every binding of the
        connection for at least its Retry-After. Raises TriggerDeliveryAbortedError after cancellation and
        PollCursorDocumentInvalidError for a stored cursor document that can never be decoded.
        """
        await self._poll_runner.run(context)

    def wake_binding(self, binding_name: str) -> None:
        """Make one binding due now, for example after the application knows a row was added."""
        self._poll_runner.wake_poller(binding_name)

    async def aclose(self) -> None:
        """Close the connection `new_project_row_added_trigger_runner` opened, after `run` returns."""
        if self._owned_connection is not None:
            await self._owned_connection.aclose()


def new_row_added_trigger_runner(config: RowAddedTriggerRunnerConfig) -> RowAddedTriggerRunner:
    """Validate every route and build one poll runner for the connection; it makes no request or read.

    Raises ValueError for no routes, a blank or repeated binding name, a missing cursor or target, an
    invalid configuration, or a mock connection.
    """
    return RowAddedTriggerRunner(_new_row_added_poll_runner(config))


def _new_row_added_poll_runner(config: RowAddedTriggerRunnerConfig) -> PollRunner:
    if config is None:
        raise ValueError("Google Sheets rowAdded runner configuration is required")
    client = _connection_client(config.connection)
    if not config.routes:
        raise ValueError("Google Sheets rowAdded Trigger routes are required")
    pollers = []
    for route in config.routes:
        if not (route.binding_name or "").strip(GO_SPACE) or route.target is None or route.cursor is None:
            raise ValueError("Google Sheets rowAdded binding name, cursor, and target are required")
        binding = TriggerBindingRef(
            config.connection.reference, ROW_ADDED_TRIGGER_DEFINITION.trigger, route.binding_name
        )
        try:
            fetcher = RowAddedFetcher(client, binding, route.configuration, config.logger)
            pollers.append(
                new_binding_poller(
                    BindingPollerConfig(
                        ROW_ADDED_TRIGGER_DEFINITION, binding, route.cursor, fetcher, route.target
                    )
                )
            )
        except ValueError as error:
            raise ValueError(f"Google Sheets rowAdded binding {route.binding_name!r}: {error}") from error
    return PollRunner(
        PollRunnerConfig(
            config.connection.reference,
            row_added_poll_schedule(client),
            pollers,
            config.request_limiter,
            config.logger,
        )
    )


@dataclass(frozen=True, slots=True)
class ProjectRowAddedTriggerRoute:
    """Bind one ``rowAdded`` binding saved in the project configuration to its application target."""

    binding_name: str
    """The binding whose spreadsheet and tab Dex Web saved."""
    target: TriggerTarget[RowAddedEvent]
    """Receives each added row after the binding's durable project inbox records it."""


async def new_project_row_added_trigger_runner(
    project: LoadedProject,
    connection_name: str,
    routes: Sequence[ProjectRowAddedTriggerRoute],
    http_client: httpx.AsyncClient | None = None,
    logger: TriggerLogger | None = None,
) -> RowAddedTriggerRunner:
    """Open connection_name from the project and build a durable runner for its saved bindings.

    Each binding's cursor lives in project storage beside its Trigger inbox, and every row is recorded in
    that inbox before the cursor commits past it, so a restarted or replaced replica neither misses nor
    replays a row. Dex Web projects keep cursors in process memory, as Go does, so a local restart
    reseeds. Raises ValueError for an invalid route or stored configuration and ObjectNotFoundError for a
    binding the project does not store. Close the runner with `aclose` after `run` returns.
    """
    if project is None:
        raise ValueError("Google Sheets rowAdded runner requires the loaded project configuration")
    connection = new_project_connection(project, connection_name, http_client)
    try:
        runner_routes = []
        for route in routes:
            if route.target is None:
                raise ValueError(f"Google Sheets rowAdded binding {route.binding_name!r} target is required")
            key = TriggerInboxKey(CONNECTOR_ID, connection_name, _TRIGGER_NAME, route.binding_name)
            try:
                configuration = decode_trigger_configuration(
                    project.configuration, key, decode_row_added_trigger_configuration
                )
            except ValueError as error:
                raise ValueError(
                    f"Google Sheets rowAdded binding {route.binding_name!r} configuration: {error}"
                ) from error
            target = new_durable_trigger_target(
                project.trigger_inbox(key),
                route.target,
                decode_payload=decode_row_added_event_payload,
                logger=logger,
            )
            cursor = project.poll_cursor_store(PollCursorKey.from_trigger_inbox_key(key))
            runner_routes.append(RowAddedTriggerRoute(route.binding_name, configuration, cursor, target))
        poll_runner = _new_row_added_poll_runner(
            RowAddedTriggerRunnerConfig(connection, runner_routes, logger=logger)
        )
    except BaseException:
        await connection.aclose()
        raise
    return RowAddedTriggerRunner(poll_runner, connection)


__all__ = [
    "ROW_ADDED_TRIGGER_DEFINITION",
    "UI_SHEET_TAB_PICKER_PORT_SPREADSHEET_ID",
    "UI_SHEET_TAB_PICKER_PORT_TAB",
    "UI_SPREADSHEET_PICKER_PORT_SPREADSHEET_ID",
    "UI_UNIT_SHEET_TAB_PICKER",
    "UI_UNIT_SPREADSHEET_PICKER",
    "ProjectRowAddedTriggerRoute",
    "RowAddedEvent",
    "RowAddedTriggerConfiguration",
    "RowAddedTriggerRoute",
    "RowAddedTriggerRunner",
    "RowAddedTriggerRunnerConfig",
    "decode_row_added_event_payload",
    "decode_row_added_trigger_configuration",
    "define_row_added_trigger_binding",
    "new_project_row_added_trigger_runner",
    "new_row_added_trigger",
    "new_row_added_trigger_runner",
    "row_added_poll_schedule",
    "validate_row_added_trigger_configuration",
]
