# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read Gorgias's event log in created_datetime order for the poll Trigger kit, as the Go fetcher does."""

import json
import re
from collections.abc import Callable
from dataclasses import dataclass, field
from datetime import timedelta
from typing import Any, Final
from urllib.parse import quote_plus

from dex_connectors.sdk import Call, ConnectionRef, TriggerContext, TriggerEvent
from dex_connectors.sdk.pollingtrigger import (
    ChangePage,
    Cursor,
    mark_cursor_invalid,
    mark_rate_limited,
    new_trigger_credential_call,
    wait_for_poll_request,
)

from ._generated.definitions import TICKET_CHANGED_TRIGGER_DEFINITION
from ._go_time import GoTime, parse_gorgias_time, parse_http_date
from ._ticket_event_cursor import (
    TICKET_EVENT_OVERLAP_NANOSECONDS,
    JSONObject,
    TicketEventCursor,
    TicketEventCursorDecodeError,
    decode_ticket_event_cursor,
    encode_ticket_event_cursor,
    fold_json_members,
    prune_seen_event_keys,
    refuse_json_constant,
    seen_event_key,
)
from ._ticket_event_types import TICKET_EVENT_TYPES, delivers_event_type
from .client import Client
from .models import TicketChangedEvent, TicketChangedTriggerConfiguration

TICKET_EVENT_PAGE_SIZE: Final = 100
"""The limit of each ticketChanged event request, Gorgias's maximum of 100."""

_OPERATION: Final = "ticketChanged"
_TICKET_OBJECT_TYPE: Final = "Ticket"
_EVENT_ID_PREFIX: Final = "gorgias-event-"
# Bounds the seed listing of events recorded within the overlap before the baseline.
_MAXIMUM_SEED_PAGES: Final = 10
_MICROSECOND_NANOSECONDS: Final = 1000
_INT64_MINIMUM, _INT64_MAXIMUM = -(1 << 63), (1 << 63) - 1
_API_CALL_LIMIT: Final = re.compile(r"[0-9]{1,6}/[0-9]{1,6}", re.ASCII)
_LIST_CURSOR: Final = re.compile(r"[A-Za-z0-9+/=_.:-]{1,2048}", re.ASCII)
_EVENT_FIELDS: Final = ("id", "type", "object_id", "object_type", "user_id", "context", "created_datetime")


class TicketEventPollError(RuntimeError):
    """A failed event log read; the kit backs off and retries from the same cursor."""


@dataclass(slots=True)
class _EventWire:
    id: int = 0
    type: str = ""
    object_id: int | None = None
    object_type: str | None = None
    user_id: int | None = None
    context: str | None = None
    created_datetime: str = ""


@dataclass(slots=True)
class _EventPage:
    events: list[_EventWire]
    created_at: list[GoTime]
    server_time: GoTime | None = None
    is_rate_budget_low: bool = False
    is_page_size_filled: bool = field(default=False)


class TicketEventFetcher:
    """The `ChangeFetcher` of one ticketChanged binding: the account's ticket events, oldest first."""

    def __init__(
        self, client: Client, connection: ConnectionRef, configuration: TicketChangedTriggerConfiguration
    ) -> None:
        """Bind the client and the binding's event type selection; it sends nothing."""
        self._client = client
        self._call: Call = new_trigger_credential_call(connection, TICKET_CHANGED_TRIGGER_DEFINITION.trigger)
        self._configuration = configuration
        # Go reads the local clock at nanosecond precision; tests replace this to replay a Go recording.
        self._local_now: Callable[[], GoTime] = lambda: GoTime.from_datetime(self._client.now())

    async def seed_cursor(self, context: TriggerContext) -> Cursor:
        """Record the baseline: Gorgias's Date header, or the local clock only without one, and the events
        recorded within the overlap before it, so no earlier event is delivered."""
        local_now = self._local_now()
        start = local_now.add(-TICKET_EVENT_OVERLAP_NANOSECONDS)
        seen: list[str] = []
        baseline = local_now
        for page_number in range(1, _MAXIMUM_SEED_PAGES + 1):
            listed = await self._list_event_page(context, start)
            if page_number == 1 and listed.server_time is not None:
                baseline = listed.server_time
            for event, created_at in zip(listed.events, listed.created_at, strict=True):
                seen.append(seen_event_key(event.id, created_at))
                if created_at.is_after(baseline):
                    baseline = created_at
            if not listed.is_page_size_filled:
                break
            start = _next_scan_start(start, listed.created_at[-1])
        position = TicketEventCursor(
            since=baseline.format_rfc3339_nano(),
            seen=prune_seen_event_keys(seen, baseline.add(-TICKET_EVENT_OVERLAP_NANOSECONDS)),
        )
        return Cursor(encode_ticket_event_cursor(position))

    async def fetch_changes(
        self, context: TriggerContext, cursor: Cursor
    ) -> ChangePage[TriggerEvent[TicketChangedEvent]]:
        """Read one page of ticket events recorded since the cursor's window start, oldest first.

        Only an undecodable cursor is marked invalid; a created_datetime filter never expires, so a
        Gorgias 400 backs off and retries from the same position. A 429 pauses the connection.
        """
        try:
            decoded = decode_ticket_event_cursor(cursor.token)
        except TicketEventCursorDecodeError as error:
            raise mark_cursor_invalid(
                TicketEventPollError("Gorgias ticketChanged cursor is not decodable")
            ) from error
        start = decoded.start
        listed = await self._list_event_page(context, start)
        seen = set(decoded.position.seen)
        next_seen = list(decoded.position.seen)
        high_water = decoded.since
        changes: list[TriggerEvent[TicketChangedEvent]] = []
        for event, created_at in zip(listed.events, listed.created_at, strict=True):
            key = seen_event_key(event.id, created_at)
            if key in seen or created_at.is_before(start):
                continue
            seen.add(key)
            next_seen.append(key)
            if created_at.is_after(high_water):
                high_water = created_at
            change = self._ticket_change(event, created_at)
            if change is not None:
                changes.append(new_ticket_changed_trigger_event(change, created_at))
        position = TicketEventCursor(since=high_water.format_rfc3339_nano())
        has_more_pages = False
        if listed.is_page_size_filled:
            # A full page may hide more events at or after its last one, so the next request starts there.
            position.scan_from = _next_scan_start(start, listed.created_at[-1]).format_rfc3339_nano()
            # Near Gorgias's rate limit, the rest waits for the next scheduled poll.
            has_more_pages = not listed.is_rate_budget_low
        position.seen = prune_seen_event_keys(next_seen, high_water.add(-TICKET_EVENT_OVERLAP_NANOSECONDS))
        return ChangePage(changes, Cursor(encode_ticket_event_cursor(position)), has_more_pages)

    def _ticket_change(self, event: _EventWire, created_at: GoTime) -> TicketChangedEvent | None:
        # Other types and non-ticket objects only move the cursor.
        if (
            not delivers_event_type(self._configuration, event.type)
            or event.object_type != _TICKET_OBJECT_TYPE
            or event.object_id is None
            or event.object_id < 1
        ):
            return None
        return TicketChangedEvent(
            event_id=event.id,
            event_type=event.type,
            ticket_id=event.object_id,
            occurred_at=created_at.to_datetime(),
            user_id=event.user_id if event.user_id is not None and event.user_id > 0 else 0,
            context=event.context or "",
        )

    async def _list_event_page(self, context: TriggerContext, start: GoTime) -> _EventPage:
        await wait_for_poll_request(context)
        credentials, _ = await self._client.resolve_credentials(self._call, _OPERATION)
        if credentials is None:
            raise TicketEventPollError("Gorgias ticketChanged credentials are unavailable")
        query = [
            ("order_by", "created_datetime:asc"),
            ("created_datetime[gte]", start.format_query_microseconds()),
            ("limit", str(TICKET_EVENT_PAGE_SIZE)),
        ]
        # Gorgias filters by every read type, so a binding's selection never changes what the cursor passes.
        query += [("types", event_type) for event_type in TICKET_EVENT_TYPES]
        exchange = await self._client.exchange(
            self._call, credentials, _OPERATION, "GET", "/events?" + encode_go_query(query)
        )
        message = "Gorgias event list request: " + (
            exchange.failure.message if exchange.failure is not None else "failed"
        )
        if exchange.outcome == "rateLimited":
            raise mark_rate_limited(TicketEventPollError(message), timedelta(seconds=exchange.retry_after))
        if exchange.outcome != "success":
            raise TicketEventPollError(message)
        try:
            listed = decode_event_page(exchange.body)
        except ValueError as error:
            raise TicketEventPollError(f"Gorgias event list page is invalid: {error}") from error
        headers = exchange.headers
        if headers is not None:
            listed.server_time = parse_http_date(headers.get("Date", ""))
            api_call_limit = headers.get("X-Gorgias-Account-Api-Call-Limit", "")
            if _API_CALL_LIMIT.fullmatch(api_call_limit):
                listed.is_rate_budget_low = is_rate_budget_low(api_call_limit)
        return listed


def new_ticket_changed_trigger_event(
    change: TicketChangedEvent, created_at: GoTime
) -> TriggerEvent[TicketChangedEvent]:
    """Convert one event; its ID is a pure function of Gorgias's event ID, so a refetch yields it again."""
    return TriggerEvent(ticket_event_key(change.event_id), created_at.to_datetime(), change)


def ticket_event_key(event_id: int) -> str:
    """Name one event as "gorgias-event-<id>"."""
    return f"{_EVENT_ID_PREFIX}{event_id}"


def is_rate_budget_low(api_call_limit: str) -> bool:
    """Report an X-Gorgias-Account-Api-Call-Limit value, such as 31/40, at three quarters or more."""
    used, separator, limit = api_call_limit.partition("/")
    if not separator or not used.isdecimal() or not limit.isdecimal():
        return False
    return int(limit) > 0 and int(used) * 4 >= int(limit) * 3


def encode_go_query(pairs: list[tuple[str, str]]) -> str:
    """Encode query pairs as Go's url.Values.Encode does: keys sorted, values in order, QueryEscape."""
    ordered = sorted(enumerate(pairs), key=lambda entry: (entry[1][0], entry[0]))
    return "&".join(
        quote_plus(key, safe="") + "=" + quote_plus(value, safe="") for _, (key, value) in ordered
    )


def decode_event_page(body: bytes) -> _EventPage:
    """Decode the documented bare array or Gorgias's usual {"data": [...]} list envelope as Go does.

    Raises ValueError naming the problem.
    """
    try:
        document = json.loads(body, object_pairs_hook=JSONObject, parse_constant=refuse_json_constant)
    except (ValueError, RecursionError, UnicodeDecodeError) as error:
        raise ValueError("the response is neither an event array nor a Gorgias list") from error
    try:
        events = _decode_events(document)
    except _WireTypeError:
        try:
            events = _decode_list_envelope(document)
        except _WireTypeError as error:
            raise ValueError("the response is neither an event array nor a Gorgias list") from error
    if len(events) > TICKET_EVENT_PAGE_SIZE:
        raise ValueError(f"the page has {len(events)} events, more than {TICKET_EVENT_PAGE_SIZE}")
    created_at: list[GoTime] = []
    for index, event in enumerate(events):
        if event.id < 1:
            raise ValueError("an event has no ID")
        try:
            created = parse_gorgias_time(event.created_datetime)
        except ValueError as error:
            raise ValueError("an event created_datetime is not an ISO 8601 timestamp") from error
        # The cursor relies on ascending order; a page out of order would skip events.
        if index > 0 and created.is_before(created_at[index - 1]):
            raise ValueError("the events are not in ascending created_datetime order")
        created_at.append(created)
    return _EventPage(events, created_at, is_page_size_filled=len(events) == TICKET_EVENT_PAGE_SIZE)


class _WireTypeError(ValueError):
    """Go's json.Unmarshal reports a type error for this value."""


def _decode_events(document: Any) -> list[_EventWire]:
    if document is None:
        return []
    if not isinstance(document, list) or isinstance(document, JSONObject):
        raise _WireTypeError("not an array")
    return [_decode_event(item) for item in document]


def _decode_list_envelope(document: Any) -> list[_EventWire]:
    if not isinstance(document, JSONObject):
        raise _WireTypeError("not a list envelope")
    data: list[_EventWire] | None = None
    next_cursor: str | None = None
    for name, value in fold_json_members(document, ("data", "meta")):
        if name == "data":
            data = None if value is None else _decode_events(value)
            continue
        if value is None:
            next_cursor = None
            continue
        if not isinstance(value, JSONObject):
            raise _WireTypeError("meta is not an object")
        for _, cursor_value in fold_json_members(value, ("next_cursor",)):
            if cursor_value is not None and not isinstance(cursor_value, str):
                raise _WireTypeError("next_cursor is not a string")
            next_cursor = cursor_value
    if data is None:
        raise _WireTypeError("the event page has no data array")
    if next_cursor and not _LIST_CURSOR.fullmatch(next_cursor):
        raise _WireTypeError("the event page has an invalid next cursor")
    return data


def _decode_event(item: Any) -> _EventWire:
    event = _EventWire()
    if item is None:
        return event
    if not isinstance(item, JSONObject):
        raise _WireTypeError("event is not an object")
    for name, value in fold_json_members(item, _EVENT_FIELDS):
        if name in ("id", "type", "created_datetime"):
            if value is None:
                continue
            if name == "id":
                event.id = _decode_int64(value)
            elif name == "type":
                event.type = _decode_string(value)
            else:
                event.created_datetime = _decode_string(value)
        elif name in ("object_id", "user_id"):
            setattr(event, name, None if value is None else _decode_int64(value))
        else:
            setattr(event, name, None if value is None else _decode_string(value))
    return event


def _decode_int64(value: Any) -> int:
    if isinstance(value, bool) or not isinstance(value, int) or not _INT64_MINIMUM <= value <= _INT64_MAXIMUM:
        raise _WireTypeError("integer field has another type")
    return value


def _decode_string(value: Any) -> str:
    if not isinstance(value, str):
        raise _WireTypeError("string field has another type")
    return value


def _next_scan_start(start: GoTime, last_created_at: GoTime) -> GoTime:
    # When every event of a full page shares start's microsecond, step one microsecond past it.
    if last_created_at.is_after(start):
        return last_created_at
    return start.add(_MICROSECOND_NANOSECONDS)
