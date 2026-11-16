# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read Freshdesk's List All Tickets in updated_at order for the ticketChanged poll Trigger.

The cursor token is the Go connector's JSON document byte for byte, so Go and Python replicas share one
binding cursor.
"""

import json
import re
from collections.abc import Sequence
from dataclasses import dataclass, field
from datetime import UTC, datetime, timedelta
from email.utils import parsedate_to_datetime
from typing import Any, Final

from dex_connectors.sdk import ConnectionRef, TriggerContext, TriggerEvent
from dex_connectors.sdk.pollingtrigger import (
    ChangePage,
    Cursor,
    mark_cursor_invalid,
    mark_rate_limited,
    new_trigger_credential_call,
    wait_for_poll_request,
)
from dex_connectors.sdk.projectconfig import encode_go_json_value

from ._generated.definitions import TICKET_CHANGED_TRIGGER_DEFINITION
from .client import Client
from .models import Ticket, TicketChangedEvent, TicketChangedTriggerConfiguration
from .operations import _ticket

TICKET_CHANGE_PAGE_SIZE: Final = 100
"""The per_page of each ticketChanged list request, Freshdesk's maximum of 100."""
MAXIMUM_TICKET_LIST_PAGE: Final = 300
"""The last page Freshdesk serves for List All Tickets (30,000 tickets)."""
TICKET_CHANGE_OVERLAP: Final = timedelta(minutes=1)
"""How far before the committed updated_at high-water mark each poll window starts.

A ticket whose update commits a little after its timestamp is still read; tickets already delivered in
the window are recognized by ID and updated_at and not delivered again.
"""
TICKET_CHANGE_CREATED: Final = "created"
"""A ticket created inside the poll window that the binding has not delivered before."""
TICKET_CHANGE_UPDATED: Final = "updated"
"""Any later change that moved the ticket's updated_at, such as a status change or a new reply."""

# Bounds the cursor's dedupe set under the 64 KiB cursor token limit.
_MAXIMUM_DELIVERED_TICKET_KEYS: Final = 1500
_MAXIMUM_SEED_PAGES: Final = 15
_MAXIMUM_CLOCK_SKEW: Final = timedelta(seconds=2)
# The Go connector's defaultRequestTimeout, part of the settled window floor.
_REQUEST_TIMEOUT: Final = timedelta(seconds=12)
_OPERATION: Final = "ticketChanged"
_UNIX_EPOCH: Final = datetime(1970, 1, 1, tzinfo=UTC)
_ZERO_TIME: Final = datetime.min.replace(tzinfo=UTC)
_TICKET_LIST_TIME = re.compile(r"[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z", re.ASCII)
_GO_INTEGER = re.compile(r"[+-]?[0-9]+", re.ASCII)
_INT64_MINIMUM, _INT64_MAXIMUM = -(1 << 63), (1 << 63) - 1


class TicketChangeCursorInvalidError(ValueError):
    """A ticketChanged cursor token that is not a valid Go cursor document."""

    def __init__(self) -> None:
        """Describe the rejected token without echoing it."""
        super().__init__("Freshdesk ticketChanged cursor is not decodable")


@dataclass(slots=True)
class _TicketChangeCursor:
    # Field order is the Go struct's, so the JSON encoding matches byte for byte.
    updated_since: str = ""
    window_start: str = ""
    page: int = 0
    floor: str = ""
    delivered: list[str] = field(default_factory=list)


@dataclass(frozen=True, slots=True)
class _TicketListPage:
    tickets: tuple[Ticket, ...]
    has_next_page: bool
    server_time: datetime | None


@dataclass(frozen=True, slots=True)
class _DecodedTicketChangeCursor:
    position: _TicketChangeCursor
    since: datetime
    floor: datetime
    window_start: datetime
    page_number: int


def _unix_seconds(moment: datetime) -> int:
    return (moment - _UNIX_EPOCH) // timedelta(seconds=1)


def _truncate_to_second(moment: datetime) -> datetime:
    return moment.astimezone(UTC).replace(microsecond=0)


def _format_ticket_list_time(moment: datetime) -> str:
    utc = moment.astimezone(UTC)
    return f"{utc.year:04d}-{utc.month:02d}-{utc.day:02d}T{utc.hour:02d}:{utc.minute:02d}:{utc.second:02d}Z"


def _parse_ticket_list_time(text: Any) -> datetime:
    if not isinstance(text, str) or not _TICKET_LIST_TIME.fullmatch(text):
        raise TicketChangeCursorInvalidError()
    try:
        return datetime.strptime(text, "%Y-%m-%dT%H:%M:%SZ").replace(tzinfo=UTC)
    except ValueError:
        raise TicketChangeCursorInvalidError() from None


def delivered_ticket_key(ticket: Ticket) -> str:
    """Name one delivered ticket version as "<id>@<updated_at Unix seconds>", as the cursor stores it."""
    return f"{ticket.id}@{_unix_seconds(ticket.updated_at)}"


def ticket_change_event_id(ticket: Ticket) -> str:
    """Return the Trigger event ID "freshdesk-ticket-<id>-<updated_at Unix seconds>".

    It is a pure function of the ticket version, so a refetch after a crash yields the same ID and a later
    change yields a new one.
    """
    return f"freshdesk-ticket-{ticket.id}-{_unix_seconds(ticket.updated_at)}"


def delivers_ticket_change_kind(configuration: TicketChangedTriggerConfiguration, kind: str) -> bool:
    """Report whether the binding delivers kind: every kind when `change_kinds` is empty."""
    return not configuration.change_kinds or kind in configuration.change_kinds


def _steady_ticket_window_start(high_water: datetime, floor: datetime) -> datetime:
    window_start = high_water - TICKET_CHANGE_OVERLAP
    return floor if floor > window_start else window_start


def _format_ticket_cursor_floor(floor: datetime, high_water: datetime) -> str:
    # The floor is kept only while it still raises the steady window start.
    if not floor > high_water - TICKET_CHANGE_OVERLAP:
        return ""
    return _format_ticket_list_time(floor)


def _parse_go_int64(text: str) -> int | None:
    if not _GO_INTEGER.fullmatch(text):
        return None
    value = int(text)
    return value if _INT64_MINIMUM <= value <= _INT64_MAXIMUM else None


def _prune_delivered_ticket_keys(keys: Sequence[str], window_start: datetime) -> tuple[list[str], datetime]:
    """Keep keys updated at or after window_start, the newest 1,500.

    When the bound drops keys, the returned floor is the earliest second whose keys are all kept, so a
    later window that starts there rereads no dropped version; otherwise it is the zero time.
    """
    window_second = _unix_seconds(window_start)
    kept: list[tuple[str, int]] = []
    seen: set[str] = set()
    for key in keys:
        parts = key.split("@", 1)
        if len(parts) != 2 or key in seen:
            continue
        updated_at = _parse_go_int64(parts[1])
        if updated_at is None or updated_at < window_second:
            continue
        seen.add(key)
        kept.append((key, updated_at))
    kept.sort(key=lambda entry: entry[1])
    floor = _ZERO_TIME
    dropped = len(kept) - _MAXIMUM_DELIVERED_TICKET_KEYS
    if dropped > 0:
        floor_second = kept[dropped][1]
        if kept[dropped - 1][1] == floor_second:
            # Part of this second was dropped, so the window starts after it and its kept keys go too.
            floor_second += 1
        floor = _UNIX_EPOCH + timedelta(seconds=floor_second)
        kept = [entry for entry in kept[dropped:] if entry[1] >= floor_second]
    return [entry[0] for entry in kept], floor


def _encode_ticket_change_cursor(position: _TicketChangeCursor) -> Cursor:
    """Encode the cursor as Go's json.Marshal does: struct field order, omitempty, HTML-safe strings."""
    members = ['"updatedSince":' + encode_go_json_value(position.updated_since).decode()]
    if position.window_start:
        members.append('"windowStart":' + encode_go_json_value(position.window_start).decode())
    if position.page:
        members.append(f'"page":{position.page}')
    if position.floor:
        members.append('"floor":' + encode_go_json_value(position.floor).decode())
    if position.delivered:
        members.append('"delivered":' + encode_go_json_value(position.delivered).decode())
    return Cursor(token="{" + ",".join(members) + "}")


def _reject_json_constant(constant: str) -> None:
    raise TicketChangeCursorInvalidError()


def _decode_cursor_position(token: str) -> _TicketChangeCursor:
    """Decode the token as Go's encoding/json fills the cursor struct.

    Keys match case-insensitively, the last duplicate wins, unknown keys and null values are ignored, and
    a value of the wrong JSON type rejects the token.
    """
    try:
        members = json.loads(token, object_pairs_hook=list, parse_constant=_reject_json_constant)
    except (ValueError, RecursionError):
        raise TicketChangeCursorInvalidError() from None
    position = _TicketChangeCursor()
    if members is None:
        return position
    if not isinstance(members, list) or any(not isinstance(member, tuple) for member in members):
        raise TicketChangeCursorInvalidError()
    for name, value in members:
        folded = name.casefold()
        if value is None:
            continue
        if folded in ("updatedsince", "windowstart", "floor"):
            if not isinstance(value, str):
                raise TicketChangeCursorInvalidError()
            attribute = {"updatedsince": "updated_since", "windowstart": "window_start"}.get(folded, folded)
            setattr(position, attribute, value)
        elif folded == "page":
            if type(value) is not int or not _INT64_MINIMUM <= value <= _INT64_MAXIMUM:
                raise TicketChangeCursorInvalidError()
            position.page = value
        elif folded == "delivered":
            if not isinstance(value, list) or any(
                item is not None and not isinstance(item, str) for item in value
            ):
                raise TicketChangeCursorInvalidError()
            position.delivered = [item if isinstance(item, str) else "" for item in value]
    return position


def _decode_ticket_change_cursor(cursor: Cursor) -> _DecodedTicketChangeCursor:
    position = _decode_cursor_position(cursor.token)
    since = _parse_ticket_list_time(position.updated_since)
    floor = _parse_ticket_list_time(position.floor) if position.floor else _ZERO_TIME
    window_start, page_number = _steady_ticket_window_start(since, floor), 1
    if position.page == 0 and position.window_start == "":
        return _DecodedTicketChangeCursor(position, since, floor, window_start, page_number)
    backlog_start = _parse_ticket_list_time(position.window_start)
    if (
        backlog_start > since
        or position.page == 1
        or position.page < 0
        or position.page > MAXIMUM_TICKET_LIST_PAGE
    ):
        raise TicketChangeCursorInvalidError()
    if position.page == 0:
        # A backlog window never starts before the floor.
        window_start = floor if floor > backlog_start else backlog_start
        return _DecodedTicketChangeCursor(position, since, floor, window_start, page_number)
    # Offset paging repeats the exact query of its first page.
    return _DecodedTicketChangeCursor(position, since, floor, backlog_start, position.page)


def _parse_http_date(value: str | None) -> datetime | None:
    if not value:
        return None
    try:
        parsed = parsedate_to_datetime(value)
    except (TypeError, ValueError, IndexError):
        return None
    if parsed is None:
        return None
    if parsed.tzinfo is None:
        parsed = parsed.replace(tzinfo=UTC)
    return _truncate_to_second(parsed)


def _has_next_page(link_headers: Sequence[str]) -> bool:
    return any('rel="next"' in link for link in link_headers)


def _decode_ticket_list(body: bytes) -> tuple[Ticket, ...]:
    try:
        rows = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        raise ValueError("the response is not a ticket array") from None
    if rows is None:
        return ()
    if not isinstance(rows, list):
        raise ValueError("the response is not a ticket array")
    if len(rows) > TICKET_CHANGE_PAGE_SIZE:
        raise ValueError(f"the page has {len(rows)} tickets, more than {TICKET_CHANGE_PAGE_SIZE}")
    return tuple(_ticket(row) for row in rows)


def _new_ticket_changed_trigger_event(kind: str, ticket: Ticket) -> TriggerEvent[TicketChangedEvent]:
    return TriggerEvent(ticket_change_event_id(ticket), ticket.updated_at, TicketChangedEvent(kind, ticket))


class TicketChangeFetcher:
    """Read Freshdesk's List All Tickets in updated_at order for the poll Trigger kit.

    It implements `ChangeFetcher[TriggerEvent[TicketChangedEvent]]` at parity with the Go connector: the
    cursor is the updated_at high-water mark, a one-minute overlap window, and the "<id>@<updated_at>" keys
    delivered inside it. A poller owns one fetcher and calls it from one task at a time.
    """

    def __init__(
        self, client: Client, connection: ConnectionRef, configuration: TicketChangedTriggerConfiguration
    ) -> None:
        """Bind the fetcher to one connection's client and one binding's configuration."""
        if client is None or connection is None or configuration is None:
            raise ValueError("Freshdesk ticketChanged client, connection, and configuration are required")
        self._client = client
        self._call = new_trigger_credential_call(connection, TICKET_CHANGED_TRIGGER_DEFINITION.trigger)
        self._configuration = configuration

    async def seed_cursor(self, context: TriggerContext) -> Cursor:
        """Record the baseline without reporting existing tickets.

        The high-water mark is Freshdesk's current time from the response Date header, or the local clock
        without one, and tickets changed within the overlap before it are remembered as delivered.
        """
        baseline = _truncate_to_second(self._client._now())
        cursor, server_time = await self._list_seed_window(context, baseline)
        # A skewed local clock would put the window in the wrong place, so list again on Freshdesk's clock.
        if server_time is not None and abs(server_time - baseline) > _MAXIMUM_CLOCK_SKEW:
            cursor, _ = await self._list_seed_window(context, server_time)
        return _encode_ticket_change_cursor(cursor)

    async def _list_seed_window(
        self, context: TriggerContext, baseline: datetime
    ) -> tuple[_TicketChangeCursor, datetime | None]:
        cursor = _TicketChangeCursor()
        server_time: datetime | None = None
        since = baseline
        for page_number in range(1, _MAXIMUM_SEED_PAGES + 1):
            listed = await self._list_ticket_page(context, baseline - TICKET_CHANGE_OVERLAP, page_number)
            if page_number == 1:
                server_time = listed.server_time
            for ticket in listed.tickets:
                cursor.delivered.append(delivered_ticket_key(ticket))
                if ticket.updated_at > since:
                    since = ticket.updated_at
            if not listed.has_next_page:
                break
        cursor.updated_since = _format_ticket_list_time(since)
        cursor.delivered, floor = _prune_delivered_ticket_keys(
            cursor.delivered, since - TICKET_CHANGE_OVERLAP
        )
        cursor.floor = _format_ticket_cursor_floor(floor, since)
        return cursor, server_time

    async def fetch_changes(
        self, context: TriggerContext, cursor: Cursor
    ) -> ChangePage[TriggerEvent[TicketChangedEvent]]:
        """Read one page of tickets updated since the cursor's window start, oldest first.

        An undecodable cursor and a Freshdesk 400 for the cursor's query raise `mark_cursor_invalid`, so the
        kit records a new baseline; a 429 raises `mark_rate_limited` with Retry-After.
        """
        try:
            decoded = _decode_ticket_change_cursor(cursor)
        except TicketChangeCursorInvalidError as error:
            raise mark_cursor_invalid(error) from None
        listed = await self._list_ticket_page(context, decoded.window_start, decoded.page_number)
        delivered = set(decoded.position.delivered)
        delivered_ticket_ids = {key.split("@", 1)[0] for key in decoded.position.delivered}
        # A backlog window starts at the last second read, so creation is judged from the committed position.
        created_floor = min(decoded.since - TICKET_CHANGE_OVERLAP, decoded.window_start)
        next_position = _TicketChangeCursor(delivered=list(decoded.position.delivered))
        high_water = decoded.since
        changes: list[TriggerEvent[TicketChangedEvent]] = []
        for ticket in listed.tickets:
            key = delivered_ticket_key(ticket)
            if key in delivered or ticket.updated_at < decoded.window_start:
                continue
            kind = TICKET_CHANGE_UPDATED
            # A ticket created inside the window that the binding never delivered is new to the binding.
            if ticket.created_at >= created_floor and str(ticket.id) not in delivered_ticket_ids:
                kind = TICKET_CHANGE_CREATED
            delivered.add(key)
            next_position.delivered.append(key)
            if ticket.updated_at > high_water:
                high_water = ticket.updated_at
            if delivers_ticket_change_kind(self._configuration, kind):
                changes.append(_new_ticket_changed_trigger_event(kind, ticket))
        next_position.updated_since = _format_ticket_list_time(high_water)
        floor = decoded.floor
        next_window_start = _steady_ticket_window_start(high_water, floor)
        is_last_served_page = (
            decoded.page_number >= MAXIMUM_TICKET_LIST_PAGE and len(listed.tickets) == TICKET_CHANGE_PAGE_SIZE
        )
        has_more_pages = False
        if is_last_served_page:
            # Freshdesk serves no page past 300 and names none, so the rest of this one second is skipped.
            has_more_pages = True
            floor = decoded.window_start + timedelta(seconds=1)
            next_window_start = _steady_ticket_window_start(high_water, floor)
        elif not listed.has_next_page or not listed.tickets:
            pass
        elif listed.tickets[-1].updated_at > decoded.window_start:
            # Offsets shift when a listed ticket changes, so restart at the last second read.
            has_more_pages = True
            next_window_start = listed.tickets[-1].updated_at
            next_position.window_start = _format_ticket_list_time(next_window_start)
        else:
            # More than a page shares one second, so only an offset makes progress.
            has_more_pages = True
            next_window_start = decoded.window_start
            next_position.window_start = _format_ticket_list_time(decoded.window_start)
            next_position.page = decoded.page_number + 1
        if not has_more_pages and listed.server_time is not None:
            # Every update older than this has committed and been read, so a quiet helpdesk stops rereading.
            settled = listed.server_time - TICKET_CHANGE_OVERLAP - _REQUEST_TIMEOUT
            if settled > floor:
                floor = settled
            next_window_start = _steady_ticket_window_start(high_water, floor)
        prune_from = min(_steady_ticket_window_start(high_water, floor), next_window_start)
        next_position.delivered, overflow_floor = _prune_delivered_ticket_keys(
            next_position.delivered, prune_from
        )
        if overflow_floor > floor:
            floor = overflow_floor
        next_position.floor = _format_ticket_cursor_floor(floor, high_water)
        return ChangePage(tuple(changes), _encode_ticket_change_cursor(next_position), has_more_pages)

    async def _list_ticket_page(
        self, context: TriggerContext, window_start: datetime, page_number: int
    ) -> _TicketListPage:
        await wait_for_poll_request(context)
        key, failure, _ = await self._client.resolve_credentials(self._call, _OPERATION)
        if failure is not None:
            raise RuntimeError("Freshdesk ticketChanged credentials are unavailable")
        query = {
            "updated_since": _format_ticket_list_time(window_start),
            "order_by": "updated_at",
            "order_type": "asc",
            "per_page": str(TICKET_CHANGE_PAGE_SIZE),
            "page": str(page_number),
        }
        result = await self._client.exchange(self._call, _OPERATION, key, "GET", "/tickets", query)
        message = result.failure.message if result.failure is not None else "Freshdesk returned no response"
        failure_error = RuntimeError("Freshdesk ticket list request: " + message)
        if result.outcome == "rateLimited":
            raise mark_rate_limited(failure_error, timedelta(seconds=result.retry_after))
        if result.outcome == "rejected" and result.status == 400:
            raise mark_cursor_invalid(failure_error)
        if result.outcome != "succeeded":
            raise failure_error
        try:
            tickets = _decode_ticket_list(result.body)
        except (ValueError, TypeError) as error:
            raise ValueError(f"Freshdesk ticket list page is invalid: {error}") from None
        headers = result.headers
        return _TicketListPage(
            tickets,
            _has_next_page(headers.get_list("Link") if headers is not None else []),
            _parse_http_date(headers.get("Date") if headers is not None else None),
        )
