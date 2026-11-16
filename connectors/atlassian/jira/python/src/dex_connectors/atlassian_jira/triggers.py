# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start Flows when Jira issues are created or updated, through the ``issueChanged`` poll Trigger.

Each binding polls Jira's enhanced JQL search on the updated time with the poll Trigger kit, at parity
with the Go connector, and shares its cursor documents byte for byte. The first poll records a baseline
and delivers no existing issue. `new_project_issue_trigger_runner` wires stored bindings to durable
project inboxes and poll cursors, so a restarted Worker neither misses nor repeats a change.
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

from ._generated.definitions import CONNECTOR_ID, ISSUE_CHANGED_TRIGGER_DEFINITION
from ._generated.triggers import (
    decode_issue_changed_trigger_configuration,
    define_issue_changed_trigger_binding,
)
from .client import Client
from .connection import Connection, new_project_connection
from .issue_change_fetcher import (
    ISSUE_CHANGE_CREATED,
    ISSUE_CHANGE_UPDATED,
    IssueChangeFetcher,
    issue_change_event_id,
    validate_issue_changed_trigger_configuration,
)
from .models import IssueChangedEvent, IssueChangedTriggerConfiguration

_TRIGGER_NAME: Final = ISSUE_CHANGED_TRIGGER_DEFINITION.trigger.trigger_name


@dataclass(frozen=True, slots=True)
class IssueChangedTriggerRoute:
    """Bind one ``issueChanged`` configuration to its application target and cursor."""

    binding_name: str
    """The stable application binding name, unique within the runner."""
    configuration: IssueChangedTriggerConfiguration
    """Selects the watched project; blank watches every project the account can browse."""
    target: TriggerTarget[IssueChangedEvent]
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
class IssueTriggerRunnerConfig:
    """Configure every Jira Trigger binding polled through one connection."""

    connection: Connection
    """A connection from `new_connection` with a `Client`, or `new_project_connection`."""
    issue_changed_routes: Sequence[IssueChangedTriggerRoute]
    """The ``issueChanged`` bindings; at least one is required."""
    logger: TriggerLogger | None = None
    """Receives seed, reseed, failed-poll, and rate-limit records with binding identity, never payloads
    or credentials; None uses the standard logging adapter."""


def new_issue_trigger_runner(config: IssueTriggerRunnerConfig) -> PollRunner:
    """Create one poll runner for the configured ``issueChanged`` bindings of one connection.

    `PollRunner.run` replays each durable target, records a baseline for a binding without a cursor, then
    polls every binding each ``pollInterval`` with jitter. A 429, including an exhausted hourly points
    quota, or a 503 with Retry-After pauses every binding of the connection for Retry-After, or backs off
    exponentially without it; other failures back off per binding up to 15 minutes, and none of them
    moves the cursor. Delivery is at least once, and each issue version keeps one Trigger event ID across
    refetches. Raises ValueError for empty routes, a blank binding name or missing target, an invalid
    configuration, or a connection without a Jira `Client`.
    """
    client = _client_of(config.connection)
    if not config.issue_changed_routes:
        raise ValueError("Jira issueChanged routes are required")
    pollers = []
    for route in config.issue_changed_routes:
        if not (route.binding_name or "").strip() or route.target is None:
            raise ValueError("Jira issueChanged binding name and target are required")
        _validate_configuration(route.configuration)
        pollers.append(
            new_binding_poller(
                BindingPollerConfig(
                    definition=ISSUE_CHANGED_TRIGGER_DEFINITION,
                    binding=TriggerBindingRef(
                        config.connection.reference,
                        ISSUE_CHANGED_TRIGGER_DEFINITION.trigger,
                        route.binding_name,
                    ),
                    cursor=route.cursor if route.cursor is not None else new_memory_cursor_store(),
                    fetcher=IssueChangeFetcher(client, config.connection.reference, route.configuration),
                    target=route.target,
                )
            )
        )
    return PollRunner(
        PollRunnerConfig(config.connection.reference, _poll_schedule(client), pollers, logger=config.logger)
    )


def new_issue_changed_trigger(
    *,
    connection: Connection,
    binding_name: str,
    configuration: IssueChangedTriggerConfiguration,
    target: TriggerTarget[IssueChangedEvent],
    logger: TriggerLogger | None = None,
) -> TriggerRunner:
    """Create one ``issueChanged`` binding as a `TriggerRunner`, polling on its own schedule.

    It has no project storage, so it keeps the cursor in memory and records a new baseline on every
    start; use `new_project_issue_trigger_runner` for a durable cursor. Raises ValueError for an invalid
    configuration or a connection without a Jira `Client`.
    """
    client = _client_of(connection)
    _validate_configuration(configuration)
    binding = TriggerBindingRef(connection.reference, ISSUE_CHANGED_TRIGGER_DEFINITION.trigger, binding_name)
    source = new_binding_trigger_source(
        BindingPollerConfig(
            definition=ISSUE_CHANGED_TRIGGER_DEFINITION,
            binding=binding,
            cursor=new_memory_cursor_store(),
            fetcher=IssueChangeFetcher(client, connection.reference, configuration),
        ),
        _poll_schedule(client),
    )
    return new_trigger(
        TriggerConfig(
            definition=ISSUE_CHANGED_TRIGGER_DEFINITION,
            binding=binding,
            source=source,
            target=target,
            logger=logger,
        )
    )


@dataclass(frozen=True, slots=True)
class ProjectIssueChangedTriggerRoute:
    """Select one ``issueChanged`` binding saved in the project configuration."""

    binding_name: str
    """The saved binding name."""
    target: TriggerTarget[IssueChangedEvent]
    """Routes each change into the application, usually `new_dex_flow_trigger_target`."""


class IssueTriggerRunner:
    """Poll the project's ``issueChanged`` bindings through the connection it opened.

    Run it for the life of the Worker process and close it with `aclose` afterwards.
    """

    def __init__(self, poll_runner: PollRunner, connection: Connection) -> None:
        """Wrap a poll runner; applications use `new_project_issue_trigger_runner`."""
        if poll_runner is None or connection is None:
            raise ValueError("Jira issue Trigger runner and connection are required")
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


async def new_project_issue_trigger_runner(
    project: LoadedProject,
    connection_name: str,
    routes: Sequence[ProjectIssueChangedTriggerRoute],
    *,
    http_client: httpx.AsyncClient | None = None,
    logger: TriggerLogger | None = None,
) -> IssueTriggerRunner:
    """Open the connection and its saved bindings and create one durable poll runner.

    Each route records changes in its binding's project Trigger inbox before its position commits in the
    binding's project poll cursor, so a restarted or replaced replica neither misses nor repeats a change.
    An unsaved binding watches every project the account can browse, as a blank picker does. Raises
    ValueError for empty routes, a blank or repeated binding name, or an invalid saved configuration.
    """
    if project is None:
        raise ValueError("Jira issue Trigger runner requires the loaded project configuration")
    if not routes:
        raise ValueError("Jira issueChanged routes are required")
    connection = new_project_connection(project, connection_name, http_client)
    try:
        durable_routes = []
        binding_names: set[str] = set()
        for route in routes:
            binding_name = (route.binding_name or "").strip()
            if not binding_name or route.target is None:
                raise ValueError("Jira issueChanged binding name and target are required")
            if binding_name in binding_names:
                raise ValueError(f"Jira issueChanged binding name {json.dumps(binding_name)} is duplicated")
            binding_names.add(binding_name)
            key = TriggerInboxKey(CONNECTOR_ID, connection_name, _TRIGGER_NAME, binding_name)
            try:
                configuration = decode_trigger_configuration(
                    project.configuration, key, decode_issue_changed_trigger_configuration
                )
            except ObjectNotFoundError:
                configuration = IssueChangedTriggerConfiguration()
            except ValueError as error:
                raise ValueError(
                    f"jira issueChanged binding {json.dumps(binding_name)} configuration: {error}"
                ) from error
            target = new_durable_trigger_target(
                project.trigger_inbox(key),
                route.target,
                decode_payload=decode_issue_changed_event_payload,
                logger=logger,
            )
            durable_routes.append(
                IssueChangedTriggerRoute(
                    binding_name,
                    configuration,
                    target,
                    project.poll_cursor_store(PollCursorKey.from_trigger_inbox_key(key)),
                )
            )
        poll_runner = new_issue_trigger_runner(IssueTriggerRunnerConfig(connection, durable_routes, logger))
    except BaseException:
        await connection.aclose()
        raise
    return IssueTriggerRunner(poll_runner, connection)


def decode_issue_changed_event_payload(payload_json: str, payload_bytes: bytes) -> IssueChangedEvent:
    """Decode a replayed inbox payload written by this package or by the Go connector.

    Raises ValueError for a payload that is not an ``IssueChangedEvent``.
    """
    try:
        decoded = (
            dex.CodecRegistry().resolve(IssueChangedEvent).decode(dex.Value(dex.WireKind.JSON, payload_json))
        )
    except (TypeError, ValueError) as error:
        raise ValueError("pending Jira issue change is invalid") from error
    if not isinstance(decoded, IssueChangedEvent):
        raise ValueError("pending Jira issue change is invalid")
    return decoded


def _validate_configuration(configuration: IssueChangedTriggerConfiguration) -> None:
    try:
        validate_issue_changed_trigger_configuration(configuration)
    except ValueError as error:
        raise ValueError(f"jira issueChanged configuration: {error}") from error


def _client_of(connection: Connection) -> Client:
    client = getattr(connection, "_client", None)
    if not isinstance(client, Client):
        raise ValueError("Jira Triggers require a connection from new_project_connection or new_connection")
    return client


def _poll_schedule(client: Client) -> PollSchedule:
    # Zero maximum backoff uses the kit's cap, which grows with a long poll interval.
    return PollSchedule(timedelta(seconds=client.config.poll_interval_seconds))


__all__ = [
    "ISSUE_CHANGED_TRIGGER_DEFINITION",
    "ISSUE_CHANGE_CREATED",
    "ISSUE_CHANGE_UPDATED",
    "IssueChangedEvent",
    "IssueChangedTriggerConfiguration",
    "IssueChangedTriggerRoute",
    "IssueTriggerRunner",
    "IssueTriggerRunnerConfig",
    "ProjectIssueChangedTriggerRoute",
    "decode_issue_changed_event_payload",
    "decode_issue_changed_trigger_configuration",
    "define_issue_changed_trigger_binding",
    "issue_change_event_id",
    "new_issue_changed_trigger",
    "new_issue_trigger_runner",
    "new_project_issue_trigger_runner",
    "validate_issue_changed_trigger_configuration",
]
