# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one project's Asana Events API stream for the poll Trigger kit, as the Go fetcher does."""

import hashlib
import re
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from typing import Final

from dex_connectors.sdk import Call, ConnectionRef, TriggerContext, TriggerEvent
from dex_connectors.sdk.pollingtrigger import (
    ChangePage,
    Cursor,
    mark_cursor_invalid,
    mark_rate_limited,
    new_trigger_credential_call,
    wait_for_poll_request,
)
from dex_connectors.sdk.projectconfig import encode_go_json_value

from ._event_json import InvalidEventJSONError, RawJSONValue, compact_raw_json, parse_raw_json
from ._generated.definitions import TASK_CHANGED_TRIGGER_DEFINITION
from .client import Client, Exchange
from .models import EventParentReference, TaskChangedEvent, TaskReference, UserReference

# Keeps task and subtask events; project, section, and story events are dropped.
_TASK_EVENT_RESOURCE_TYPE: Final = "task"
# Asana's documented cap of events per sync token response.
MAXIMUM_EVENTS_PER_PAGE: Final = 100
# Asks for names and change details; gids are always returned.
TASK_EVENT_FIELDS: Final = (
    "action,created_at,user.name,resource.name,resource.resource_type,resource.resource_subtype,"
    "parent.name,parent.resource_type,change.field,change.action,change.new_value,change.added_value,"
    "change.removed_value"
)
KNOWN_TASK_CHANGE_ACTIONS: Final = frozenset({"added", "changed", "removed", "deleted", "undeleted"})
_GID: Final = re.compile(r"[0-9]{1,64}", re.ASCII)
_RFC3339: Final = re.compile(
    r"([0-9]{4})-([0-9]{2})-([0-9]{2})T([0-9]{2}):([0-9]{2}):([0-9]{2})(\.[0-9]+)?(Z|[+-][0-9]{2}:[0-9]{2})"
)
# Go's unicode.IsSpace set, which strings.TrimSpace removes.
_GO_SPACE: Final = (
    "\t\n\v\f\r \x85\xa0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a"
    "\u2028\u2029\u202f\u205f\u3000"
)


class TaskEventStreamError(RuntimeError):
    """A failed Events API poll; the kit backs off and retries without moving the cursor."""


def is_asana_gid(value: str) -> bool:
    """Report whether value is an Asana gid: 1 to 64 ASCII digits."""
    return _GID.fullmatch(value) is not None


@dataclass(frozen=True, slots=True)
class _EventListing:
    events: tuple[RawJSONValue, ...]
    sync_token: str
    has_more: bool
    is_sync_invalid: bool = False


class TaskEventFetcher:
    """The `ChangeFetcher` of one taskChanged binding: one project's event stream."""

    def __init__(self, client: Client, connection: ConnectionRef, project_id: str) -> None:
        """Bind the client and the watched project gid; it sends nothing."""
        self._client = client
        self._call: Call = new_trigger_credential_call(connection, TASK_CHANGED_TRIGGER_DEFINITION.trigger)
        self._project_id = project_id

    async def seed_cursor(self, context: TriggerContext) -> Cursor:
        """Ask for the events without a sync token; Asana's 412 carries a fresh token, the baseline."""
        listing = await self._list_events(context, "")
        if not listing.sync_token:
            raise TaskEventStreamError("Asana event stream returned no sync token")
        return self._encode_cursor(listing.sync_token)

    async def fetch_changes(
        self, context: TriggerContext, cursor: Cursor
    ) -> ChangePage[TriggerEvent[TaskChangedEvent]]:
        """Read the events after the cursor's sync token.

        An expired token (HTTP 412), an undecodable cursor, or a cursor for another project is marked
        invalid, so the kit records a new baseline.
        """
        cursor_project_id = sync_token = ""
        try:
            position = parse_raw_json(cursor.token.encode())
            if position.kind not in ("object", "null"):
                raise InvalidEventJSONError("cursor must be an object")
            cursor_project_id = position.string_member("projectId") if position.kind == "object" else ""
            sync_token = position.string_member("sync") if position.kind == "object" else ""
        except InvalidEventJSONError:
            sync_token = ""
        if not sync_token:
            raise mark_cursor_invalid(TaskEventStreamError("Asana event cursor is not decodable"))
        if cursor_project_id != self._project_id:
            raise mark_cursor_invalid(TaskEventStreamError("Asana event cursor belongs to another project"))
        listing = await self._list_events(context, sync_token)
        if listing.is_sync_invalid:
            raise mark_cursor_invalid(
                TaskEventStreamError("Asana sync token expired or is invalid (HTTP 412)")
            )
        if not listing.sync_token:
            raise TaskEventStreamError("Asana event page has no sync token")
        if len(listing.events) > MAXIMUM_EVENTS_PER_PAGE:
            raise TaskEventStreamError(
                f"Asana event page has {len(listing.events)} events, more than {MAXIMUM_EVENTS_PER_PAGE}"
            )
        changes = self.convert_events(sync_token, listing.events)
        return ChangePage(changes, self._encode_cursor(listing.sync_token), listing.has_more)

    def convert_events(
        self, requested_sync_token: str, events: tuple[RawJSONValue, ...]
    ) -> list[TriggerEvent[TaskChangedEvent]]:
        """Keep task events in Asana's order, each with a stable ID.

        Asana returns the same events for a re-requested sync token, so the requested token plus an
        ordinal among identical events of the page keeps IDs stable across refetches, and identical events
        on either side of a page boundary still get distinct IDs.
        """
        now = self._client.now()
        occurrences: dict[bytes, int] = {}
        changes: list[TriggerEvent[TaskChangedEvent]] = []
        for event in events:
            resource = event.optional_object("resource") if event.kind == "object" else None
            if resource is None or resource.string_member("resource_type") != _TASK_EVENT_RESOURCE_TYPE:
                continue
            try:
                payload = _convert_task_event(self._project_id, event, resource)
            except ValueError as error:
                raise TaskEventStreamError(f"Asana event page has an invalid task event: {error}") from error
            identity = requested_sync_token.encode() + b"\x00" + _task_event_identity(self._project_id, event)
            occurrences[identity] = occurrences.get(identity, 0) + 1
            if occurrences[identity] > 1:
                identity += b"\x00" + str(occurrences[identity]).encode()
            digest = hashlib.sha256(identity).digest()
            changes.append(
                TriggerEvent(
                    "asana-" + digest[:16].hex(), _parse_event_time(payload.occurred_at, now), payload
                )
            )
        return changes

    async def _list_events(self, context: TriggerContext, sync_token: str) -> _EventListing:
        await wait_for_poll_request(context)
        token = await self._client.token(self._call)
        if token is None:
            raise TaskEventStreamError(
                "Asana Trigger credentials are unavailable; save a personal access token for the connection"
            )
        query = {"resource": self._project_id, "opt_fields": TASK_EVENT_FIELDS}
        if sync_token:
            query["sync"] = sync_token
        exchange = await self._client.exchange(self._call, token, "GET", "/events", query)
        return self._classify_event_listing(exchange)

    def _classify_event_listing(self, exchange: Exchange) -> _EventListing:
        if exchange.error:
            raise TaskEventStreamError("Asana event stream request failed before a response")
        if exchange.status == 412:
            listing = None if exchange.reflects_secret else _decode_event_page(exchange.body)
            if listing is None or not listing.sync_token:
                raise TaskEventStreamError("Asana answered HTTP 412 without a fresh sync token")
            return _EventListing((), listing.sync_token, False, is_sync_invalid=True)
        if exchange.status == 429:
            # Asana's limits are per user and per domain; the kit pauses every binding of the connection.
            raise mark_rate_limited(
                TaskEventStreamError("Asana rate limited the event stream request (HTTP 429)"),
                timedelta(seconds=self._client.retry_after(exchange)),
            )
        if not 200 <= exchange.status < 300:
            raise TaskEventStreamError(f"Asana rejected the event stream request with HTTP {exchange.status}")
        if exchange.is_body_too_large:
            raise TaskEventStreamError("Asana event page exceeds the configured maxResponseBytes")
        if exchange.has_body_read_failed:
            raise TaskEventStreamError("Asana event page response was interrupted")
        if exchange.reflects_secret:
            raise TaskEventStreamError("Asana event page contains credential material")
        listing = _decode_event_page(exchange.body)
        if listing is None:
            raise TaskEventStreamError("Asana event page is not valid JSON")
        return listing

    def _encode_cursor(self, sync_token: str) -> Cursor:
        return Cursor(encode_go_json_value({"projectId": self._project_id, "sync": sync_token}).decode())


def _decode_event_page(body: bytes) -> _EventListing | None:
    """Decode GET /events as Go's json.Unmarshal does, or None where Go would fail."""
    try:
        page = parse_raw_json(body)
        if page.kind == "null":
            return _EventListing((), "", False)
        if page.kind != "object":
            return None
        events = page.array_member("data")
        for event in events:
            _validate_event_types(event)
        return _EventListing(events, page.string_member("sync"), page.boolean_member("has_more"))
    except InvalidEventJSONError:
        return None


def _validate_event_types(event: RawJSONValue) -> None:
    """Raise InvalidEventJSONError where Go's decoding into asanaEvent reports a type mismatch."""
    if event.kind == "null":
        return
    if event.kind != "object":
        raise InvalidEventJSONError("event must be an object")
    event.string_member("action")
    event.string_member("created_at")
    for name, fields in (
        ("user", ("gid", "name")),
        ("resource", ("gid", "name", "resource_type", "resource_subtype")),
        ("parent", ("gid", "name", "resource_type", "resource_subtype")),
        ("change", ("field", "action")),
    ):
        nested = event.optional_object(name)
        if nested is None:
            continue
        for field_name in fields:
            nested.string_member(field_name)


def _convert_task_event(project_id: str, event: RawJSONValue, resource: RawJSONValue) -> TaskChangedEvent:
    action = event.string_member("action")
    if not is_asana_gid(resource.string_member("gid")):
        raise ValueError("task gid is not an Asana gid")
    if action not in KNOWN_TASK_CHANGE_ACTIONS:
        raise ValueError(f"unknown action {_go_quote(action)}")
    change = event.optional_object("change")
    parent = event.optional_object("parent")
    user = event.optional_object("user")
    is_changed = change is not None and action == "changed"
    return TaskChangedEvent(
        project_id=project_id,
        action=action,
        occurred_at=event.string_member("created_at"),
        task=TaskReference(id=resource.string_member("gid"), name=resource.string_member("name")),
        task_subtype=resource.string_member("resource_subtype"),
        changed_field=change.string_member("field") if change is not None and is_changed else "",
        change_action=change.string_member("action") if change is not None and is_changed else "",
        parent=(
            EventParentReference(
                id=parent.string_member("gid"),
                resource_type=parent.string_member("resource_type"),
                name=parent.string_member("name"),
            )
            if parent is not None and is_asana_gid(parent.string_member("gid"))
            else None
        ),
        user=(
            UserReference(id=user.string_member("gid"), name=user.string_member("name"))
            if user is not None and is_asana_gid(user.string_member("gid"))
            else None
        ),
    )


def _task_event_identity(project_id: str, event: RawJSONValue) -> bytes:
    """Join the event's immutable facts; names are left out so a rename keeps the ID."""
    resource = event.optional_object("resource")
    user = event.optional_object("user")
    parent = event.optional_object("parent")
    change = event.optional_object("change")
    parts = [
        project_id.encode(),
        (resource.string_member("gid") if resource is not None else "").encode(),
        event.string_member("action").encode(),
        event.string_member("created_at").encode(),
        (user.string_member("gid") if user is not None else "").encode(),
        (parent.string_member("gid") if parent is not None else "").encode(),
    ]
    if change is not None:
        parts += [
            change.string_member("field").encode(),
            change.string_member("action").encode(),
            _change_value_identity(change.member("new_value")),
            _change_value_identity(change.member("added_value")),
            _change_value_identity(change.member("removed_value")),
        ]
    return b"\x00".join(parts)


def _change_value_identity(value: RawJSONValue | None) -> bytes:
    """A referenced object's gid, or the compacted JSON of any other value, as Go computes it."""
    if value is None:
        return b""
    if value.kind == "object":
        reference = value.member("gid")
        if reference is not None and reference.kind == "string" and reference.text:
            return b"gid:" + reference.text.encode()
    return compact_raw_json(value.raw)


def _parse_event_time(value: str, now: datetime) -> datetime:
    """Read Asana's created_at as RFC 3339, falling back to now for a missing or unparsable time."""
    match = _RFC3339.fullmatch(value.strip(_GO_SPACE))
    if match is not None:
        year, month, day, hour, minute, second, fraction, offset = match.groups()
        try:
            parsed = datetime.fromisoformat(
                f"{year}-{month}-{day}T{hour}:{minute}:{second}"
                f"{(fraction or '')[:7]}{'+00:00' if offset == 'Z' else offset}"
            )
            return parsed.astimezone(UTC)
        except ValueError:
            pass
    return now.astimezone(UTC)


def _go_quote(value: str) -> str:
    return encode_go_json_value(value).decode()
