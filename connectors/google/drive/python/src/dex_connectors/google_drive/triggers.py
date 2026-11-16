# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start Flows when Drive files are created, updated, or trashed: the fileCreated, fileUpdated, and
fileTrashed Triggers.

All file Trigger bindings of one connection share one durable changes.list poller.
`new_project_file_trigger_runner` seeds the cursor from changes.getStartPageToken on first start
without delivering existing files, and records every event in its binding's project Trigger inbox
before the shared ``poll-feeds/google-drive/<connection>/fileChanges/head`` cursor commits, so a
restarted or replaced replica, Go or Python, neither misses nor replays a change.
"""

from collections.abc import Sequence
from dataclasses import dataclass, field

import httpx
from dex_connectors.sdk import (
    TriggerBindingRef,
    TriggerConfig,
    TriggerContext,
    TriggerDefinition,
    TriggerLogger,
    TriggerRunner,
    TriggerTarget,
    new_trigger,
)
from dex_connectors.sdk.pollingtrigger import (
    BindingPollerConfig,
    CursorStore,
    FeedMember,
    FeedPollerConfig,
    PollRunner,
    PollRunnerConfig,
    PollSchedule,
    new_binding_trigger_source,
    new_feed_member,
    new_feed_poller,
    new_memory_cursor_store,
)
from dex_connectors.sdk.projectconfig import (
    LoadedProject,
    PollFeedKey,
    TriggerInboxKey,
    decode_trigger_configuration,
    new_durable_trigger_target,
)

from ._generated.definitions import (
    CONNECTOR_ID,
    FILE_CREATED_TRIGGER_DEFINITION,
    FILE_TRASHED_TRIGGER_DEFINITION,
    FILE_UPDATED_TRIGGER_DEFINITION,
)
from ._generated.triggers import (
    decode_file_created_trigger_configuration,
    decode_file_trashed_trigger_configuration,
    decode_file_updated_trigger_configuration,
    define_file_created_trigger_binding,
    define_file_trashed_trigger_binding,
    define_file_updated_trigger_binding,
)
from .client import Client
from .connection import Connection, new_project_connection
from .file_changes import (
    FILE_CHANGES_FEED_NAME,
    FILE_CREATED_TRIGGER_NAME,
    FILE_TRASHED_TRIGGER_NAME,
    FILE_UPDATED_TRIGGER_NAME,
    BindingFileEventFetcher,
    FileChange,
    FileChangeFetcher,
    decode_file_event_payload,
    file_change_selector,
    validate_file_trigger_configuration,
)
from .models import FileEvent, FileTriggerConfiguration

# Identifies a per-Trigger factory's poller, which never learns its binding name; logs and memory only.
_STANDALONE_BINDING_NAME = "standalone"

_TRIGGER_DEFINITIONS = {
    FILE_CREATED_TRIGGER_NAME: FILE_CREATED_TRIGGER_DEFINITION,
    FILE_UPDATED_TRIGGER_NAME: FILE_UPDATED_TRIGGER_DEFINITION,
    FILE_TRASHED_TRIGGER_NAME: FILE_TRASHED_TRIGGER_DEFINITION,
}
_CONFIGURATION_DECODERS = {
    FILE_CREATED_TRIGGER_NAME: decode_file_created_trigger_configuration,
    FILE_UPDATED_TRIGGER_NAME: decode_file_updated_trigger_configuration,
    FILE_TRASHED_TRIGGER_NAME: decode_file_trashed_trigger_configuration,
}


@dataclass(frozen=True, slots=True)
class FileTriggerRoute:
    """Binds one fileCreated, fileUpdated, or fileTrashed binding to its application target."""

    binding_name: str
    """Names this configured Trigger binding; it is unique per Trigger."""
    target: TriggerTarget[FileEvent]
    """Routes accepted events into the application, such as `new_dex_flow_trigger_target`."""
    configuration: FileTriggerConfiguration = field(default_factory=FileTriggerConfiguration)
    """Filters the files this binding receives; the default matches every file."""


@dataclass(frozen=True, slots=True)
class FileTriggerRunnerConfig:
    """Configures every file Trigger binding of one connection on one shared poller."""

    connection: Connection
    """The Drive connection from `new_project_connection` or `new_connection`; a mock cannot poll."""
    cursor: CursorStore
    """Stores the shared changes.list position.

    Use `new_memory_cursor_store` in tests; `new_project_file_trigger_runner` uses the project's
    durable feed cursor.
    """
    file_created_routes: Sequence[FileTriggerRoute] = ()
    """Receive files created after the cursor was first seeded."""
    file_updated_routes: Sequence[FileTriggerRoute] = ()
    """Receive existing files whose modified time advanced."""
    file_trashed_routes: Sequence[FileTriggerRoute] = ()
    """Receive files moved to the trash."""
    logger: TriggerLogger | None = None
    """Receives failed and rate-limited polls, cursor seeds and resets, and delivery retries.

    None uses the ``dex_connectors.sdk.trigger`` standard logger. Records never carry file names, page
    tokens, or credentials.
    """


class FileTriggerRunner:
    """Reads the connection's Drive change log once per poll for every file Trigger binding.

    It delivers each binding's matching events in change order. Build it with `new_file_trigger_runner`
    or `new_project_file_trigger_runner`.
    """

    def __init__(self, runner: PollRunner, owned_connection: Connection | None = None) -> None:
        """Wrap a poll runner; close owned_connection in `aclose`."""
        self._runner = runner
        self._owned_connection = owned_connection

    async def run(self, context: TriggerContext | None = None) -> None:
        """Replay every durable target, seed the cursor on first start, and poll until cancelled.

        The first seed stores Drive's current position without delivering existing files. Delivery is at
        least once with stable event IDs, so a Dex Flow target starts one Flow per event. Raises
        TriggerDeliveryAbortedError after the context is cancelled, and an error for a stored cursor
        document that can never be decoded.
        """
        await self._runner.run(context)

    async def aclose(self) -> None:
        """Close the connection `new_project_file_trigger_runner` opened, after `run` returns."""
        if self._owned_connection is not None:
            await self._owned_connection.aclose()


def _connection_client(connection: Connection) -> Client:
    client = getattr(connection, "_client", None) if connection is not None else None
    if not isinstance(client, Client):
        raise ValueError("Google Drive file Triggers require a connection built from a Client")
    return client


def new_file_trigger_runner(config: FileTriggerRunnerConfig) -> FileTriggerRunner:
    """Validate config and build one runner with one shared changes.list feed; it does no I/O until `run`.

    Raises ValueError for a mock connection, a missing cursor or target, no routes, a duplicated binding
    name within one Trigger, or an invalid configuration. One binding name may serve several Triggers.
    The caller keeps ownership of the connection.
    """
    return FileTriggerRunner(_new_file_poll_runner(config))


def _new_file_poll_runner(config: FileTriggerRunnerConfig) -> PollRunner:
    client = _connection_client(config.connection)
    if config.cursor is None:
        raise ValueError("Google Drive file Trigger cursor store is required")
    reference = config.connection.reference
    members: list[FeedMember[FileChange]] = []
    for trigger_name, routes in (
        (FILE_CREATED_TRIGGER_NAME, config.file_created_routes),
        (FILE_UPDATED_TRIGGER_NAME, config.file_updated_routes),
        (FILE_TRASHED_TRIGGER_NAME, config.file_trashed_routes),
    ):
        for route in routes:
            members.append(_new_file_feed_member(config.connection, trigger_name, route))
    if not members:
        raise ValueError("Google Drive file Trigger routes are required")
    poller = new_feed_poller(
        FeedPollerConfig(
            FILE_CHANGES_FEED_NAME,
            config.cursor,
            FileChangeFetcher(client, reference, FILE_CHANGES_FEED_NAME),
            members,
        )
    )
    return PollRunner(
        PollRunnerConfig(reference, PollSchedule(client.poll_interval), [poller], logger=config.logger)
    )


def _new_file_feed_member(
    connection: Connection, trigger_name: str, route: FileTriggerRoute
) -> FeedMember[FileChange]:
    if route is None or not (route.binding_name or "").strip() or route.target is None:
        raise ValueError("Google Drive file Trigger binding name and target are required")
    try:
        validate_file_trigger_configuration(route.configuration)
    except ValueError as error:
        raise ValueError(f"google-drive {trigger_name} binding {route.binding_name!r}: {error}") from error
    definition = _TRIGGER_DEFINITIONS[trigger_name]
    binding = TriggerBindingRef(connection.reference, definition.trigger, route.binding_name)
    return new_feed_member(
        definition, binding, file_change_selector(trigger_name, route.configuration), route.target
    )


@dataclass(frozen=True, slots=True)
class ProjectFileTriggerRoute:
    """Binds one file Trigger binding saved in the project configuration to its target."""

    binding_name: str
    """Names the saved Trigger binding."""
    target: TriggerTarget[FileEvent]
    """Routes accepted events into the application."""


@dataclass(frozen=True, slots=True)
class ProjectFileTriggerRunnerConfig:
    """Selects file Trigger bindings saved in the project configuration."""

    file_created_routes: Sequence[ProjectFileTriggerRoute] = ()
    """Select saved fileCreated bindings."""
    file_updated_routes: Sequence[ProjectFileTriggerRoute] = ()
    """Select saved fileUpdated bindings."""
    file_trashed_routes: Sequence[ProjectFileTriggerRoute] = ()
    """Select saved fileTrashed bindings."""


async def new_project_file_trigger_runner(
    project: LoadedProject,
    connection_name: str,
    config: ProjectFileTriggerRunnerConfig,
    *,
    http_client: httpx.AsyncClient | None = None,
    logger: TriggerLogger | None = None,
) -> FileTriggerRunner:
    """Open connection_name from the loaded project and build one durable runner of its saved bindings.

    Each route records events in its binding's project Trigger inbox before the shared feed cursor,
    stored beside the inboxes, commits past them, so a restarted or replaced replica neither misses nor
    replays a change. The cursor and inbox documents are the Go connector's, so Go and Python replicas
    can share them. logger also receives the inboxes' records. Raises ValueError for a binding without
    saved configuration or an invalid one, as `new_file_trigger_runner` does otherwise. Close the runner
    with `aclose` after `run` returns; it closes the connection it opened.
    """
    if project is None or config is None:
        raise ValueError("Google Drive file Trigger runner requires the loaded project and routes")
    connection = new_project_connection(project, connection_name, http_client)
    try:
        cursor = project.poll_feed_cursor_store(
            PollFeedKey(CONNECTOR_ID, connection_name, FILE_CHANGES_FEED_NAME)
        )
        runner = _new_file_poll_runner(
            FileTriggerRunnerConfig(
                connection,
                cursor,
                _resolve_project_routes(
                    project, connection_name, FILE_CREATED_TRIGGER_NAME, config.file_created_routes, logger
                ),
                _resolve_project_routes(
                    project, connection_name, FILE_UPDATED_TRIGGER_NAME, config.file_updated_routes, logger
                ),
                _resolve_project_routes(
                    project, connection_name, FILE_TRASHED_TRIGGER_NAME, config.file_trashed_routes, logger
                ),
                logger,
            )
        )
    except BaseException:
        await connection.aclose()
        raise
    return FileTriggerRunner(runner, connection)


def _resolve_project_routes(
    project: LoadedProject,
    connection_name: str,
    trigger_name: str,
    routes: Sequence[ProjectFileTriggerRoute],
    logger: TriggerLogger | None,
) -> list[FileTriggerRoute]:
    """Decode each saved binding and wrap its target in the binding's durable inbox."""
    resolved: list[FileTriggerRoute] = []
    for route in routes:
        if route is None or route.target is None:
            raise ValueError("Google Drive file Trigger binding name and target are required")
        key = TriggerInboxKey(CONNECTOR_ID, connection_name, trigger_name, route.binding_name)
        try:
            saved = decode_trigger_configuration(
                project.configuration, key, _CONFIGURATION_DECODERS[trigger_name]
            )
        except ValueError as error:
            raise ValueError(
                f"google-drive {trigger_name} binding {route.binding_name!r} configuration: {error}"
            ) from error
        target = new_durable_trigger_target(
            project.trigger_inbox(key), route.target, decode_payload=decode_file_event_payload, logger=logger
        )
        resolved.append(FileTriggerRoute(route.binding_name, target, saved))
    return resolved


def _new_standalone_file_trigger(
    trigger_name: str,
    connection: Connection,
    connection_name: str,
    binding_name: str,
    configuration: FileTriggerConfiguration,
    target: TriggerTarget[FileEvent],
) -> TriggerRunner:
    client = _connection_client(connection)
    if connection_name != connection.reference.name:
        raise ValueError(
            f"google-drive connector trigger connection_name {connection_name!r} must equal "
            f"its connection's name {connection.reference.name!r}"
        )
    validate_file_trigger_configuration(configuration)
    definition: TriggerDefinition = _TRIGGER_DEFINITIONS[trigger_name]
    fetcher = BindingFileEventFetcher(
        FileChangeFetcher(client, connection.reference, trigger_name),
        file_change_selector(trigger_name, configuration),
    )
    standalone = TriggerBindingRef(connection.reference, definition.trigger, _STANDALONE_BINDING_NAME)
    source = new_binding_trigger_source(
        BindingPollerConfig(definition, standalone, new_memory_cursor_store(), fetcher),
        PollSchedule(client.poll_interval),
    )
    binding = TriggerBindingRef(connection.reference, definition.trigger, binding_name)
    return new_trigger(TriggerConfig(definition, binding, source, target))


def new_file_created_trigger(
    *,
    connection: Connection,
    connection_name: str,
    binding_name: str,
    configuration: FileTriggerConfiguration,
    target: TriggerTarget[FileEvent],
) -> TriggerRunner:
    """Poll changes.list for one fileCreated binding with an in-memory cursor.

    A restart reseeds and misses files created while stopped; `new_project_file_trigger_runner` is
    durable and shares one poll among bindings. Raises ValueError for a mock connection, a
    connection_name that differs from the connection's, or an invalid configuration.
    """
    return _new_standalone_file_trigger(
        FILE_CREATED_TRIGGER_NAME, connection, connection_name, binding_name, configuration, target
    )


def new_file_updated_trigger(
    *,
    connection: Connection,
    connection_name: str,
    binding_name: str,
    configuration: FileTriggerConfiguration,
    target: TriggerTarget[FileEvent],
) -> TriggerRunner:
    """Poll changes.list for one fileUpdated binding with an in-memory cursor.

    It behaves as `new_file_created_trigger` does, for files whose modified time advanced.
    """
    return _new_standalone_file_trigger(
        FILE_UPDATED_TRIGGER_NAME, connection, connection_name, binding_name, configuration, target
    )


def new_file_trashed_trigger(
    *,
    connection: Connection,
    connection_name: str,
    binding_name: str,
    configuration: FileTriggerConfiguration,
    target: TriggerTarget[FileEvent],
) -> TriggerRunner:
    """Poll changes.list for one fileTrashed binding with an in-memory cursor.

    It behaves as `new_file_created_trigger` does, for files moved to the trash.
    """
    return _new_standalone_file_trigger(
        FILE_TRASHED_TRIGGER_NAME, connection, connection_name, binding_name, configuration, target
    )


__all__ = [
    "FILE_CREATED_TRIGGER_DEFINITION",
    "FILE_TRASHED_TRIGGER_DEFINITION",
    "FILE_UPDATED_TRIGGER_DEFINITION",
    "FileEvent",
    "FileTriggerConfiguration",
    "FileTriggerRoute",
    "FileTriggerRunner",
    "FileTriggerRunnerConfig",
    "ProjectFileTriggerRoute",
    "ProjectFileTriggerRunnerConfig",
    "decode_file_created_trigger_configuration",
    "decode_file_event_payload",
    "decode_file_trashed_trigger_configuration",
    "decode_file_updated_trigger_configuration",
    "define_file_created_trigger_binding",
    "define_file_trashed_trigger_binding",
    "define_file_updated_trigger_binding",
    "new_file_created_trigger",
    "new_file_trashed_trigger",
    "new_file_trigger_runner",
    "new_file_updated_trigger",
    "new_project_file_trigger_runner",
    "validate_file_trigger_configuration",
]
