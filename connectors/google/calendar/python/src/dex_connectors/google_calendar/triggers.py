# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Receive Google Calendar event changes through the ``eventChanged`` poll Trigger.

Each binding polls one calendar's ``events.list`` incremental sync feed with the Connector SDK poll kit
(`dex_connectors.sdk.pollingtrigger`), at behavior parity with the Go connector:

- the first poll runs a full sync and keeps only the final ``nextSyncToken``, so existing events are a
  baseline and are never delivered;
- each later poll sends ``syncToken`` (plus ``pageToken`` within a sequence) and ``maxResults`` only, and
  recurring events stay unexpanded;
- 410 Gone records a new baseline and logs a WARN record; a 429, or a 403 ``rateLimitExceeded``,
  ``userRateLimitExceeded``, ``quotaExceeded``, or ``dailyLimitExceeded`` reason, pauses every binding of
  the connection for Retry-After (seconds or an HTTP date), or backs off exponentially without it;
- each change's Trigger event ID hashes the calendar, event ID, ETag, updated time, and status, so a
  refetch after a crash yields the same ID.

The sync cursor document is the Go connector's byte for byte, so Go and Python replicas share it.
`new_project_event_trigger_runner` records every change in the binding's project Trigger inbox before
the sync token commits in its project poll cursor.
"""

import hashlib
import json
import re
from collections.abc import Sequence
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from enum import StrEnum
from typing import Any, Final

from dex import CodecRegistry, Value, WireKind
from dex_connectors.sdk import (
    Call,
    ConnectionRef,
    Failure,
    TriggerBindingRef,
    TriggerConfig,
    TriggerContext,
    TriggerEvent,
    TriggerLogger,
    TriggerRunner,
    TriggerTarget,
    new_trigger,
)
from dex_connectors.sdk.pollingtrigger import (
    MAXIMUM_CHANGES_PER_PAGE,
    BindingPollerConfig,
    ChangePage,
    Cursor,
    CursorStore,
    Poller,
    PollRunner,
    PollRunnerConfig,
    PollSchedule,
    mark_cursor_invalid,
    mark_rate_limited,
    new_binding_poller,
    new_binding_trigger_source,
    new_memory_cursor_store,
    new_trigger_credential_call,
    wait_for_poll_request,
)
from dex_connectors.sdk.projectconfig import (
    LoadedProject,
    ObjectNotFoundError,
    PollCursorKey,
    TriggerInboxKey,
    decode_trigger_configuration,
    encode_go_json_value,
    new_durable_trigger_target,
)
from dex_connectors.sdk.providerhttp import parse_retry_after

from ._generated.definitions import CONNECTOR_ID, EVENT_CHANGED_TRIGGER_DEFINITION
from ._generated.triggers import (
    decode_event_changed_trigger_configuration,
    define_event_changed_trigger_binding,
)
from .client import Client, ProviderRequestError, calendar_path, classify_status, google_error_reason
from .connection import Connection
from .models import Event, EventChangedEvent, EventChangedTriggerConfiguration
from .operations import _decode_event

DEFAULT_TRIGGER_CALENDAR_ID: Final = "primary"
"""The calendar an ``eventChanged`` binding watches when it saves no calendar."""

_TRIGGER_NAME: Final = EVENT_CHANGED_TRIGGER_DEFINITION.trigger.trigger_name
# Google reports no change type, so the kind is derived from created and updated times this close.
_CREATED_UPDATED_TOLERANCE_NANOSECONDS: Final = 1_000_000_000
# Below MAXIMUM_CHANGES_PER_PAGE and Google's own default.
_EVENT_SYNC_PAGE_SIZE: Final = 250
# Bounds a first full sync at one million events.
_MAXIMUM_BASELINE_PAGES: Final = 4000
_THROTTLE_REASONS: Final = frozenset(
    {"rateLimitExceeded", "userRateLimitExceeded", "quotaExceeded", "dailyLimitExceeded"}
)
_RFC3339: Final = re.compile(
    r"(\d{4})-(\d\d)-(\d\d)T(\d\d):(\d\d):(\d\d)(?:\.(\d{1,9}))?(Z|[+-]\d\d:\d\d)", re.ASCII
)
_UNIX_EPOCH: Final = datetime(1970, 1, 1, tzinfo=UTC)


class EventChangeKind(StrEnum):
    """The derived kind of one ``eventChanged`` delivery, stored as `EventChangedEvent.change_kind`."""

    CREATED = "created"
    """A new event: Google's updated time is within one second of its created time."""
    UPDATED = "updated"
    """Any other change to an event that is not cancelled, including a reschedule, a guest change, or an
    edit made soon after creation but before the next poll read it."""
    CANCELLED = "cancelled"
    """A deleted event or a cancelled instance of a recurring event."""


def validate_event_changed_trigger_configuration(configuration: EventChangedTriggerConfiguration) -> None:
    """Reject a calendar ID with surrounding whitespace; a blank calendar ID is valid.

    Raises ValueError, as the Go configuration's ``Validate`` returns an error.
    """
    if configuration.calendar_id != configuration.calendar_id.strip():
        raise ValueError("eventChanged calendarId must not have surrounding whitespace")


def watched_calendar_id(configuration: EventChangedTriggerConfiguration) -> str:
    """Return the configured calendar ID, or `DEFAULT_TRIGGER_CALENDAR_ID` when it is blank."""
    return configuration.calendar_id or DEFAULT_TRIGGER_CALENDAR_ID


def _event_change_id(calendar_id: str, event: Event) -> str:
    """Hash the event identity and version, so the ID stays short for any event ID length."""
    identity = "\x00".join((calendar_id, event.id, event.etag, event.updated, event.status))
    return "gcal-" + hashlib.sha256(identity.encode()).hexdigest()[:32]


def _parse_rfc3339_nanoseconds(value: str) -> int | None:
    """Return Unix nanoseconds for an RFC 3339 time as Go's time.RFC3339Nano parses it, else None."""
    match = _RFC3339.fullmatch(value)
    if match is None:
        return None
    year, month, day, hour, minute, second, fraction, offset = match.groups()
    try:
        moment = datetime(int(year), int(month), int(day), int(hour), int(minute), int(second), tzinfo=UTC)
    except ValueError:
        return None
    offset_seconds = 0
    if offset != "Z":
        offset_hours, offset_minutes = int(offset[1:3]), int(offset[4:6])
        if offset_hours > 23 or offset_minutes > 59:
            return None
        offset_seconds = (offset_hours * 60 + offset_minutes) * 60 * (1 if offset[0] == "+" else -1)
    whole_seconds = (moment - _UNIX_EPOCH) // timedelta(seconds=1) - offset_seconds
    return whole_seconds * 1_000_000_000 + int((fraction or "").ljust(9, "0"))


def _event_change_kind(event: Event) -> EventChangeKind:
    if event.status == EventChangeKind.CANCELLED.value:
        return EventChangeKind.CANCELLED
    created = _parse_rfc3339_nanoseconds(event.created)
    updated = _parse_rfc3339_nanoseconds(event.updated)
    if created is not None and updated is not None:
        if abs(updated - created) <= _CREATED_UPDATED_TOLERANCE_NANOSECONDS:
            return EventChangeKind.CREATED
    return EventChangeKind.UPDATED


def _event_change_time(event: Event, now: datetime) -> datetime:
    for value in (event.updated, event.created):
        nanoseconds = _parse_rfc3339_nanoseconds(value)
        if nanoseconds is not None:
            return _UNIX_EPOCH + timedelta(microseconds=nanoseconds // 1000)
    return now.astimezone(UTC)


def _new_event_changed_trigger_event(
    calendar_id: str, event: Event, now: datetime
) -> TriggerEvent[EventChangedEvent]:
    """Convert one sync item; its ID is a pure function of the event version."""
    change = EventChangedEvent(
        calendar_id=calendar_id, change_kind=_event_change_kind(event).value, event=event
    )
    return TriggerEvent(_event_change_id(calendar_id, event), _event_change_time(event, now), change)


@dataclass(frozen=True, slots=True)
class _EventSyncCursor:
    """The committed position; page_token is set while an incremental sync spans pages."""

    calendar_id: str
    sync_token: str
    page_token: str = ""

    def encode(self) -> Cursor:
        """Encode as Go's json.Marshal of eventSyncCursor: field order kept, empty pageToken omitted."""
        encoded = (
            '{"calendarId":'
            + encode_go_json_value(self.calendar_id).decode()
            + ',"syncToken":'
            + encode_go_json_value(self.sync_token).decode()
        )
        if self.page_token:
            encoded += ',"pageToken":' + encode_go_json_value(self.page_token).decode()
        return Cursor(encoded + "}")

    @staticmethod
    def decode(cursor: Cursor) -> "_EventSyncCursor | None":
        """Decode a stored cursor; None when it is not a usable sync position."""
        try:
            position = json.loads(cursor.token)
        except ValueError:
            return None
        if not isinstance(position, dict):
            return None
        values = [position.get(name) for name in ("calendarId", "syncToken", "pageToken")]
        if any(value is not None and not isinstance(value, str) for value in values):
            return None
        calendar_id, sync_token, page_token = (value or "" for value in values)
        if not sync_token:
            return None
        return _EventSyncCursor(calendar_id, sync_token, page_token)


@dataclass(frozen=True, slots=True)
class _EventSyncPage:
    next_page_token: str
    next_sync_token: str
    items: list[Any]


class _EventSyncFetcher:
    """Reads one calendar's events.list incremental sync feed for the poll Trigger kit."""

    def __init__(self, client: Client, connection: ConnectionRef, calendar_id: str) -> None:
        self._client = client
        self._call: Call = new_trigger_credential_call(connection, EVENT_CHANGED_TRIGGER_DEFINITION.trigger)
        self._calendar_id = calendar_id

    async def seed_cursor(self, context: TriggerContext) -> Cursor:
        """Run a full sync without filters and keep only the final nextSyncToken."""
        page_token = ""
        requested_page_tokens: set[str] = set()
        for _ in range(_MAXIMUM_BASELINE_PAGES):
            listed = await self._list_sync_page(context, "", page_token)
            if listed.next_sync_token:
                return _EventSyncCursor(self._calendar_id, listed.next_sync_token).encode()
            if not listed.next_page_token or listed.next_page_token in requested_page_tokens:
                raise RuntimeError(
                    "Google Calendar full sync page has neither a new page token nor a sync token"
                )
            requested_page_tokens.add(listed.next_page_token)
            page_token = listed.next_page_token
        raise RuntimeError(f"Google Calendar full sync exceeded {_MAXIMUM_BASELINE_PAGES} pages")

    async def fetch_changes(
        self, context: TriggerContext, cursor: Cursor
    ) -> ChangePage[TriggerEvent[EventChangedEvent]]:
        """Read one incremental page; an expired, undecodable, or foreign cursor is marked invalid."""
        position = _EventSyncCursor.decode(cursor)
        if position is None:
            raise mark_cursor_invalid(RuntimeError("Google Calendar sync cursor is not decodable"))
        if position.calendar_id != self._calendar_id:
            raise mark_cursor_invalid(RuntimeError("Google Calendar sync cursor belongs to another calendar"))
        listed = await self._list_sync_page(context, position.sync_token, position.page_token)
        if len(listed.items) > MAXIMUM_CHANGES_PER_PAGE:
            raise RuntimeError(
                f"Google Calendar sync page has {len(listed.items)} events, "
                f"more than {MAXIMUM_CHANGES_PER_PAGE}"
            )
        now = datetime.now(UTC)
        changes = []
        for item in listed.items:
            try:
                event = _decode_event(item, self._calendar_id)
            except (ValueError, KeyError, TypeError) as error:
                raise RuntimeError(f"Google Calendar sync page has an invalid event: {error}") from error
            changes.append(_new_event_changed_trigger_event(self._calendar_id, event, now))
        if listed.next_page_token:
            following = _EventSyncCursor(self._calendar_id, position.sync_token, listed.next_page_token)
            return ChangePage(changes, following.encode(), True)
        if listed.next_sync_token:
            return ChangePage(changes, _EventSyncCursor(self._calendar_id, listed.next_sync_token).encode())
        raise RuntimeError("Google Calendar sync page has neither a page token nor a sync token")

    async def _list_sync_page(
        self, context: TriggerContext, sync_token: str, page_token: str
    ) -> _EventSyncPage:
        """Send one events.list request with only maxResults, syncToken, and pageToken."""
        await wait_for_poll_request(context)
        credentials = await self._client.resolve(self._call, _TRIGGER_NAME)
        if isinstance(credentials, Failure):
            raise RuntimeError("Google Calendar Trigger credentials are unavailable")
        query = {"maxResults": str(_EVENT_SYNC_PAGE_SIZE)}
        if sync_token:
            query["syncToken"] = sync_token
        if page_token:
            query["pageToken"] = page_token
        try:
            response = await self._client.send(
                self._call, credentials, "GET", calendar_path(self._calendar_id) + "/events", query
            )
        except ProviderRequestError as error:
            raise RuntimeError(f"Google Calendar sync request failed: {error}") from None
        if response.status == 410 and sync_token:
            raise mark_cursor_invalid(RuntimeError("Google Calendar sync token expired (HTTP 410)"))
        if not 200 <= response.status < 300:
            reason = google_error_reason(response.body)
            if reason in _THROTTLE_REASONS or response.status == 429:
                # Quotas are per user and project; an HTTP-date Retry-After still pauses the connection.
                suffix = f" ({reason})" if reason else ""
                retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
                raise mark_rate_limited(
                    RuntimeError(f"Google Calendar sync request: provider rate limited the request{suffix}"),
                    timedelta(seconds=retry_after),
                )
            _, _, message, _ = classify_status(response)
            raise RuntimeError(f"Google Calendar sync request: {message}")
        return _decode_event_sync_page(response.body)


def _null_as(value: Any, empty: Any) -> Any:
    return empty if value is None else value


def _decode_event_sync_page(body: bytes) -> _EventSyncPage:
    invalid = "Google Calendar sync page is not valid JSON"
    try:
        listed = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        raise RuntimeError(invalid) from None
    if not isinstance(listed, dict):
        raise RuntimeError(invalid)
    # As Go's json.Unmarshal: a missing or null field is empty, and any other non-string or non-list fails.
    next_page_token = _null_as(listed.get("nextPageToken"), "")
    next_sync_token = _null_as(listed.get("nextSyncToken"), "")
    items = _null_as(listed.get("items"), [])
    if not (
        isinstance(next_page_token, str) and isinstance(next_sync_token, str) and isinstance(items, list)
    ):
        raise RuntimeError(invalid)
    return _EventSyncPage(next_page_token, next_sync_token, items)


def _decode_event_changed_payload(payload_json: str, payload_bytes: bytes) -> EventChangedEvent:
    """Decode a replayed change, including one a Go replica recorded."""
    decoded = CodecRegistry().resolve(EventChangedEvent).decode(Value(WireKind.JSON, payload_json))
    if not isinstance(decoded, EventChangedEvent) or not decoded.event.id:
        raise ValueError("stored Google Calendar event change is invalid")
    return decoded


def _connection_client(connection: Connection) -> Client:
    connection.reference.validate()
    client = connection._client
    if client is None:
        raise ValueError("Google Calendar Triggers require a provider connection, not a mock connection")
    return client


def _poll_schedule(client: Client) -> PollSchedule:
    # A zero maximum backoff uses the kit's cap, which grows with a long poll interval.
    return PollSchedule(timedelta(seconds=client.config.poll_interval_seconds))


@dataclass(frozen=True, slots=True)
class EventChangedTriggerRoute:
    """Bind one ``eventChanged`` configuration to its application target and cursor."""

    binding_name: str
    """The stable application binding name, unique within the runner."""
    configuration: EventChangedTriggerConfiguration
    """Selects the watched calendar."""
    target: TriggerTarget[EventChangedEvent]
    """Routes each change into the application.

    A target from `new_durable_trigger_target` records each change before the cursor commits past it;
    any other target consumes it first.
    """
    cursor: CursorStore | None = None
    """Stores the binding's sync token, such as `LoadedProject.poll_cursor_store`.

    None keeps it in memory, so every process start records a new baseline and changes made while
    stopped are lost.
    """


def new_event_trigger_runner(
    connection: Connection,
    routes: Sequence[EventChangedTriggerRoute],
    logger: TriggerLogger | None = None,
) -> PollRunner:
    """Create one poll runner for the ``eventChanged`` bindings polled through one connection.

    `PollRunner.run` replays each durable target, records a baseline for a binding without a cursor,
    then polls every binding each ``pollInterval`` with jitter. A 410 Gone records a new baseline and
    logs a WARN record; changes made between the last poll and the new baseline are not delivered. A
    429 or a 403 rate-limit or quota reason pauses every binding for Retry-After, or backs off
    exponentially without it; other failures back off per binding up to 15 minutes. Delivery is at
    least once, and each change keeps one Trigger event ID across refetches. logger receives the
    runner's records, which carry binding identity and never event payloads or credentials; None uses
    the standard ``dex_connectors.sdk.trigger`` logger. Raises ValueError for no routes, a blank or
    repeated binding name, a missing target, an invalid configuration, or a mock connection.
    """
    client = _connection_client(connection)
    if not routes:
        raise ValueError("Google Calendar eventChanged routes are required")
    pollers: list[Poller[Any]] = [
        _new_event_changed_poller(client, connection.reference, route) for route in routes
    ]
    return PollRunner(PollRunnerConfig(connection.reference, _poll_schedule(client), pollers, logger=logger))


def _new_event_changed_poller(
    client: Client, connection: ConnectionRef, route: EventChangedTriggerRoute
) -> Poller[TriggerEvent[EventChangedEvent]]:
    if not (route.binding_name or "").strip() or route.target is None:
        raise ValueError("Google Calendar eventChanged binding name and target are required")
    try:
        validate_event_changed_trigger_configuration(route.configuration)
    except ValueError as error:
        raise ValueError(f"google-calendar eventChanged configuration: {error}") from error
    binding = TriggerBindingRef(connection, EVENT_CHANGED_TRIGGER_DEFINITION.trigger, route.binding_name)
    fetcher = _EventSyncFetcher(client, connection, watched_calendar_id(route.configuration))
    cursor = route.cursor if route.cursor is not None else new_memory_cursor_store()
    return new_binding_poller(
        BindingPollerConfig(EVENT_CHANGED_TRIGGER_DEFINITION, binding, cursor, fetcher, route.target)
    )


@dataclass(frozen=True, slots=True)
class ProjectEventChangedTriggerRoute:
    """Select one ``eventChanged`` binding saved in the project configuration."""

    binding_name: str
    """The saved binding's name."""
    target: TriggerTarget[EventChangedEvent]
    """Routes each change into the application, usually `new_dex_flow_trigger_target`."""


def new_project_event_trigger_runner(
    project: LoadedProject,
    connection: Connection,
    routes: Sequence[ProjectEventChangedTriggerRoute],
    logger: TriggerLogger | None = None,
) -> PollRunner:
    """Create a durable runner for ``eventChanged`` bindings saved in the loaded project.

    connection is the project's connection, usually from `new_project_connection`; the caller keeps
    ownership and closes it after the runner stops. Each binding's saved calendar is loaded (an unsaved
    binding watches the primary calendar), and each route records changes in its binding's project
    Trigger inbox before its sync token commits in the binding's project poll cursor, so a restarted or
    replaced replica neither misses nor repeats a change. logger also receives the inboxes' records.
    Raises ValueError for an invalid saved configuration or route, as `new_event_trigger_runner` does.
    """
    if project is None:
        raise ValueError("Google Calendar Trigger runner requires the loaded project configuration")
    _connection_client(connection)
    durable_routes = []
    for route in routes:
        if route.target is None:
            raise ValueError(
                f"Google Calendar eventChanged binding {route.binding_name!r} target is required"
            )
        key = TriggerInboxKey(CONNECTOR_ID, connection.reference.name, _TRIGGER_NAME, route.binding_name)
        try:
            configuration = decode_trigger_configuration(
                project.configuration, key, decode_event_changed_trigger_configuration
            )
        except ObjectNotFoundError:
            # An unsaved binding watches the primary calendar, as a blank picker does.
            configuration = EventChangedTriggerConfiguration()
        except ValueError as error:
            raise ValueError(
                f"google-calendar eventChanged binding {route.binding_name!r} configuration: {error}"
            ) from error
        target = new_durable_trigger_target(
            project.trigger_inbox(key),
            route.target,
            decode_payload=_decode_event_changed_payload,
            logger=logger,
        )
        cursor = project.poll_cursor_store(PollCursorKey.from_trigger_inbox_key(key))
        durable_routes.append(EventChangedTriggerRoute(route.binding_name, configuration, target, cursor))
    return new_event_trigger_runner(connection, durable_routes, logger)


def new_event_changed_trigger(
    *,
    connection: Connection,
    binding_name: str,
    configuration: EventChangedTriggerConfiguration,
    target: TriggerTarget[EventChangedEvent],
    logger: TriggerLogger | None = None,
) -> TriggerRunner:
    """Create one ``eventChanged`` binding's TriggerRunner, as Go's generated NewEventChangedTrigger does.

    It has no project storage, so it keeps the cursor in memory and records a new baseline on every
    start. Use `new_project_event_trigger_runner` for a durable cursor and inbox. Raises ValueError for
    an invalid configuration, a blank binding name, or a mock connection.
    """
    client = _connection_client(connection)
    validate_event_changed_trigger_configuration(configuration)
    binding = TriggerBindingRef(connection.reference, EVENT_CHANGED_TRIGGER_DEFINITION.trigger, binding_name)
    fetcher = _EventSyncFetcher(client, connection.reference, watched_calendar_id(configuration))
    source = new_binding_trigger_source(
        BindingPollerConfig(EVENT_CHANGED_TRIGGER_DEFINITION, binding, new_memory_cursor_store(), fetcher),
        _poll_schedule(client),
    )
    return new_trigger(TriggerConfig(EVENT_CHANGED_TRIGGER_DEFINITION, binding, source, target, logger))


__all__ = [
    "DEFAULT_TRIGGER_CALENDAR_ID",
    "EVENT_CHANGED_TRIGGER_DEFINITION",
    "EventChangeKind",
    "EventChangedEvent",
    "EventChangedTriggerConfiguration",
    "EventChangedTriggerRoute",
    "ProjectEventChangedTriggerRoute",
    "decode_event_changed_trigger_configuration",
    "define_event_changed_trigger_binding",
    "new_event_changed_trigger",
    "new_event_trigger_runner",
    "new_project_event_trigger_runner",
    "validate_event_changed_trigger_configuration",
    "watched_calendar_id",
]
