# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Receive each change of the connected LinkedIn member's own OpenID Connect profile claims.

The ``profileChanged`` Trigger reads ``GET https://api.linkedin.com/v2/userinfo`` every connection
``pollInterval`` and compares name, given and family name, email, email verification, picture, and locale
with the profile its durable cursor holds, at behavior parity with the Go connector, whose cursor documents
and event IDs it shares byte for byte. It only watches the member who authorized the connection, never
other members. The first poll records a baseline and delivers nothing.
`new_project_profile_trigger_runner` records each change in the binding's project Trigger inbox before the
profile commits, so a restarted replica neither misses nor repeats one.
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

from ._generated.definitions import CONNECTOR_ID, PROFILE_CHANGED_TRIGGER_DEFINITION
from ._generated.triggers import (
    decode_profile_changed_trigger_configuration,
    define_profile_changed_trigger_binding,
)
from ._profile_change import ProfileField
from ._profile_fetcher import ProfileFetcher
from .client import Client
from .connection import Connection
from .models import ProfileChangedEvent

_TRIGGER_NAME: Final = PROFILE_CHANGED_TRIGGER_DEFINITION.trigger.trigger_name


@dataclass(frozen=True, slots=True)
class ProfileChangedTriggerRoute:
    """Binds one ``profileChanged`` binding to its application target and cursor.

    The binding has no configuration: it always watches the connected member's own profile.
    """

    binding_name: str
    """The stable application binding name, unique within the runner."""
    target: TriggerTarget[ProfileChangedEvent]
    """Routes each change into the application.

    A target wrapped with `new_durable_trigger_target` records each change before the cursor commits past
    it; any other target consumes it first.
    """
    cursor: CursorStore | None = None
    """Stores the binding's last profile, such as `LoadedProject.poll_cursor_store`.

    None keeps it in memory, so every process start records a new baseline and changes made while
    stopped are lost.
    """


@dataclass(frozen=True, slots=True)
class ProjectProfileChangedTriggerRoute:
    """Selects one ``profileChanged`` binding saved in the project configuration."""

    binding_name: str
    """Names the saved binding."""
    target: TriggerTarget[ProfileChangedEvent]
    """Routes each change into the application, usually `new_dex_flow_trigger_target`."""


def new_profile_trigger_runner(
    connection: Connection,
    profile_changed_routes: Sequence[ProfileChangedTriggerRoute],
    *,
    logger: TriggerLogger | None = None,
) -> PollRunner:
    """Create one poll runner for the connection's ``profileChanged`` bindings.

    Each binding watches only the connected member's own profile. `PollRunner.run` replays each durable
    target, records the current profile as a baseline for a binding without a cursor, then reads LinkedIn
    UserInfo each `Client.poll_interval` with jitter and delivers one change whenever the profile's claims
    differ from the stored ones. A stored cursor that is undecodable, or a connection re-authorized as
    another member, records a new baseline and logs a WARN record; differences in that gap are not
    delivered. A 429 pauses every binding of the connection for Retry-After, or backs off exponentially
    without it; other failures, including a 401 that a credential refresh did not fix, back off per
    binding. Delivery is at least once, and each change keeps one Trigger event ID across refetches.
    logger receives poll records without profile claims or credentials; None uses the standard logging
    adapter. Raises ValueError for a connection without a client, no routes, a blank or repeated binding
    name, or a missing target.
    """
    client = _connection_client(connection)
    if not profile_changed_routes:
        raise ValueError("LinkedIn profileChanged routes are required")
    pollers: list[Poller[TriggerEvent[ProfileChangedEvent]]] = [
        _new_profile_changed_poller(connection, client, route) for route in profile_changed_routes
    ]
    return PollRunner(PollRunnerConfig(connection.reference, _poll_schedule(client), pollers, logger=logger))


def new_project_profile_trigger_runner(
    project: LoadedProject,
    connection: Connection,
    profile_changed_routes: Sequence[ProjectProfileChangedTriggerRoute],
    *,
    logger: TriggerLogger | None = None,
) -> PollRunner:
    """Create a durable runner for ``profileChanged`` bindings saved in the loaded project.

    connection is the one `new_project_connection` opened from project; the caller keeps owning it. Each
    route checks its binding's saved configuration (an unsaved binding uses the empty one) and records
    changes in the binding's project Trigger inbox before its stored profile commits in the binding's
    project poll cursor, so a restarted or replaced replica neither misses nor repeats a change. The inbox
    payload is the Go connector's JSON, so Go and Python replicas share it. Raises ValueError for an
    invalid saved configuration and as `new_profile_trigger_runner` does.
    """
    if project is None:
        raise ValueError("LinkedIn profileChanged runner requires the loaded project configuration")
    routes: list[ProfileChangedTriggerRoute] = []
    for route in profile_changed_routes:
        if route.target is None:
            raise ValueError(
                f"LinkedIn profileChanged binding {json.dumps(route.binding_name)} target is required"
            )
        key = TriggerInboxKey(CONNECTOR_ID, connection.reference.name, _TRIGGER_NAME, route.binding_name)
        try:
            decode_trigger_configuration(
                project.configuration, key, decode_profile_changed_trigger_configuration
            )
        except ObjectNotFoundError:
            pass  # An unsaved binding uses the empty configuration.
        except ValueError as error:
            raise ValueError(
                f"linkedin profileChanged binding {json.dumps(route.binding_name)} configuration: {error}"
            ) from error
        target = new_durable_trigger_target(
            project.trigger_inbox(key),
            route.target,
            decode_payload=decode_profile_changed_event_payload,
            logger=logger,
        )
        cursor = project.poll_cursor_store(PollCursorKey.from_trigger_inbox_key(key))
        routes.append(ProfileChangedTriggerRoute(route.binding_name, target, cursor))
    return new_profile_trigger_runner(connection, routes, logger=logger)


def new_profile_changed_trigger(
    *,
    connection: Connection,
    binding_name: str,
    target: TriggerTarget[ProfileChangedEvent],
    logger: TriggerLogger | None = None,
) -> TriggerRunner:
    """Create a single ``profileChanged`` Trigger runner, as Go's generated NewProfileChangedTrigger does.

    It has no project storage, so it keeps the cursor in memory and records a new baseline on every
    start; changes made while stopped are lost. Use `new_project_profile_trigger_runner` for durable
    delivery. Raises ValueError as `new_profile_trigger_runner` does.
    """
    client = _connection_client(connection)
    if not (binding_name or "").strip() or target is None:
        raise ValueError("LinkedIn profileChanged binding name and target are required")
    binding = TriggerBindingRef(
        connection.reference, PROFILE_CHANGED_TRIGGER_DEFINITION.trigger, binding_name
    )
    source = new_binding_trigger_source(
        BindingPollerConfig(
            PROFILE_CHANGED_TRIGGER_DEFINITION,
            binding,
            new_memory_cursor_store(),
            ProfileFetcher(client, connection.reference, binding.trigger),
        ),
        _poll_schedule(client),
    )
    return new_trigger(TriggerConfig(PROFILE_CHANGED_TRIGGER_DEFINITION, binding, source, target, logger))


def decode_profile_changed_event_payload(payload_json: str, payload_bytes: bytes) -> ProfileChangedEvent:
    """Decode a replayed inbox payload, written by a Go or Python replica, as a ProfileChangedEvent.

    Raises ValueError for a payload that is not a profileChanged event.
    """
    try:
        decoded = CodecRegistry().resolve(ProfileChangedEvent).decode(Value(WireKind.JSON, payload_json))
    except (TypeError, ValueError) as error:
        raise ValueError("stored LinkedIn profileChanged payload is invalid") from error
    if (
        not isinstance(decoded, ProfileChangedEvent)
        or not decoded.subject
        or not decoded.changed_fields
        or any(name not in tuple(ProfileField) for name in decoded.changed_fields)
    ):
        raise ValueError("stored LinkedIn profileChanged payload is invalid")
    return decoded


def _new_profile_changed_poller(
    connection: Connection, client: Client, route: ProfileChangedTriggerRoute
) -> Poller[TriggerEvent[ProfileChangedEvent]]:
    if not (route.binding_name or "").strip() or route.target is None:
        raise ValueError("LinkedIn profileChanged binding name and target are required")
    binding = TriggerBindingRef(
        connection.reference, PROFILE_CHANGED_TRIGGER_DEFINITION.trigger, route.binding_name
    )
    return new_binding_poller(
        BindingPollerConfig(
            PROFILE_CHANGED_TRIGGER_DEFINITION,
            binding,
            route.cursor if route.cursor is not None else new_memory_cursor_store(),
            ProfileFetcher(client, connection.reference, binding.trigger),
            route.target,
        )
    )


def _connection_client(connection: Connection) -> Client:
    client = getattr(connection, "_client", None) if connection is not None else None
    if not isinstance(client, Client):
        raise ValueError(
            "LinkedIn profileChanged Trigger requires a connection opened with a LinkedIn client"
        )
    return client


def _poll_schedule(client: Client) -> PollSchedule:
    # A zero maximum backoff uses the kit's cap, which grows with a long poll interval.
    return PollSchedule(client.poll_interval)


__all__ = [
    "PROFILE_CHANGED_TRIGGER_DEFINITION",
    "ProfileChangedTriggerRoute",
    "ProfileField",
    "ProjectProfileChangedTriggerRoute",
    "decode_profile_changed_event_payload",
    "decode_profile_changed_trigger_configuration",
    "define_profile_changed_trigger_binding",
    "new_profile_changed_trigger",
    "new_profile_trigger_runner",
    "new_project_profile_trigger_runner",
]
