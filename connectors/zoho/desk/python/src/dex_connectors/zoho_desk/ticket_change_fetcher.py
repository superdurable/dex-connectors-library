# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read Zoho Desk ticket search in ascending modified-time order for the ticketChanged poll Trigger.

The cursor token and its keyed snapshot are the Go connector's documents byte for byte, so Go and Python
replicas share one binding cursor.
"""

import hashlib
import json
import re
from collections.abc import Sequence
from dataclasses import dataclass, replace
from datetime import UTC, datetime, timedelta, timezone
from email.utils import parsedate_to_datetime
from typing import Any, Final

from dex_connectors.sdk import ConnectionRef, TriggerContext, TriggerEvent
from dex_connectors.sdk.pollingtrigger import (
    DEFAULT_KEYED_SNAPSHOT_ENTRIES,
    ChangePage,
    Cursor,
    KeyedSnapshotOptions,
    apply_keyed_snapshot_changes,
    mark_cursor_invalid,
    mark_rate_limited,
    new_trigger_credential_call,
    wait_for_poll_request,
)
from dex_connectors.sdk.projectconfig import KeyedSnapshot, encode_go_json_value

from ._generated.definitions import TICKET_CHANGED_TRIGGER_DEFINITION
from .client import Client
from .models import Ticket, TicketChangedEvent, TicketChangedTriggerConfiguration
from .operations import _contact, _count, _object, _ticket

TICKET_CHANGE_CREATED: Final = "created"
"""A ticket the Trigger had not seen that was created inside the window the poll read.

Changes made to a new ticket before the poll read it, such as an assignment rule, arrive in this delivery.
"""
TICKET_CHANGE_UPDATED: Final = "updated"
"""Any other change that moved the ticket's modified time, or a ticket created before the baseline."""
TICKET_SEARCH_INDEX_LOOKBACK: Final = timedelta(minutes=5)
"""How far before the newest modified time it read each sweep starts again.

Zoho Desk indexes changed tickets for search with a delay; a ticket version re-read inside the lookback is
recognized by the cursor snapshot and not delivered twice.
"""
TICKET_POLL_PAGE_LIMIT: Final = 100
"""The limit of each ticket search request, Zoho Desk's largest search page."""
TICKET_SNAPSHOT_OPTIONS: Final = KeyedSnapshotOptions(
    max_entries=DEFAULT_KEYED_SNAPSHOT_ENTRIES, evict_least_recently_seen=True
)
"""The cursor snapshot bound: 10,000 recent ticket versions, the least recently seen evicted first."""

# from+limit stays within the 1,000 results Zoho Desk's search documents.
_MAXIMUM_FROM: Final = 1000 - TICKET_POLL_PAGE_LIMIT
_MAXIMUM_BASELINE_PAGES: Final = DEFAULT_KEYED_SNAPSHOT_ENTRIES // TICKET_POLL_PAGE_LIMIT
_MAXIMUM_SERVER_CLOCK_SKEW: Final = timedelta(hours=24)
_OPERATION: Final = "ticketChanged"
_RFC3339 = re.compile(
    r"([0-9]{4})-([0-9]{2})-([0-9]{2})T([0-9]{2}):([0-9]{2}):([0-9]{2})(?:\.([0-9]{1,9}))?"
    r"(Z|([+-])([0-9]{2}):([0-9]{2}))",
    re.ASCII,
)
_INT64_MINIMUM, _INT64_MAXIMUM = -(1 << 63), (1 << 63) - 1


class TicketChangeCursorInvalidError(ValueError):
    """A ticketChanged cursor token that is not a valid Go cursor document for this binding."""

    def __init__(self, message: str = "Zoho Desk ticket cursor is not decodable") -> None:
        """Describe the rejected token without echoing it."""
        super().__init__(message)


@dataclass(frozen=True, slots=True)
class _SearchPosition:
    sweep_start: datetime
    floor: datetime
    offset: int
    high_water: datetime


@dataclass(frozen=True, slots=True)
class _SearchPage:
    tickets: tuple[Ticket, ...]
    received: int
    last: datetime | None
    server_time: datetime | None


@dataclass(frozen=True, slots=True)
class _TicketVersion:
    ticket: Ticket

    def snapshot_key(self) -> str:
        return self.ticket.id

    def snapshot_hash(self) -> str:
        return ticket_version_hash(self.ticket)

    def snapshot_value(self) -> bytes | None:
        return None


def format_zoho_timestamp(moment: datetime) -> str:
    """Format moment as Zoho Desk's UTC millisecond timestamp, such as 2026-03-02T09:00:00.000Z."""
    utc = moment.astimezone(UTC)
    return (
        f"{utc.year:04d}-{utc.month:02d}-{utc.day:02d}T{utc.hour:02d}:{utc.minute:02d}:{utc.second:02d}"
        f".{utc.microsecond // 1000:03d}Z"
    )


def ticket_change_event_id(organization_id: str, ticket: Ticket) -> str:
    """Return the Trigger event ID of one ticket version: zdesk- and 32 hex digits of SHA-256.

    It hashes the organization, ticket ID, and modified time, so a refetch after a crash yields the same ID
    and a later version yields a new one.
    """
    material = "\x00".join((organization_id, ticket.id, format_zoho_timestamp(ticket.modified_at)))
    return "zdesk-" + hashlib.sha256(material.encode()).digest()[:16].hex()


def ticket_version_hash(ticket: Ticket) -> str:
    """Return the keyed-snapshot hash of one ticket version, as the Go connector computes it."""
    material = ticket.id + "\x00" + format_zoho_timestamp(ticket.modified_at)
    return hashlib.sha256(material.encode()).digest()[:16].hex()


def ticket_change_kind(ticket: Ticket, is_known: bool, window_start: datetime) -> str:
    """Report created for an unknown ticket created at or after window_start, else updated."""
    if not is_known and not ticket.created_at < window_start:
        return TICKET_CHANGE_CREATED
    return TICKET_CHANGE_UPDATED


def _parse_rfc3339(value: str) -> datetime:
    """Parse value as Go's time.Parse(time.RFC3339Nano) does, truncated to microseconds, in UTC."""
    match = _RFC3339.fullmatch(value)
    if match is None:
        raise TicketChangeCursorInvalidError()
    year, month, day, hour, minute, second, fraction, zone, sign, zone_hour, zone_minute = match.groups()
    try:
        offset = timedelta(0)
        if zone != "Z":
            if int(zone_hour) > 23 or int(zone_minute) > 59:
                raise ValueError("offset out of range")
            offset = timedelta(hours=int(zone_hour), minutes=int(zone_minute)) * (1 if sign == "+" else -1)
        moment = datetime(
            int(year),
            int(month),
            int(day),
            int(hour),
            int(minute),
            int(second),
            int((fraction or "")[:6].ljust(6, "0")),
            tzinfo=timezone(offset),
        )
        return moment.astimezone(UTC)
    except (ValueError, OverflowError):
        raise TicketChangeCursorInvalidError() from None


def _reject_json_constant(constant: str) -> None:
    raise TicketChangeCursorInvalidError()


_CURSOR_STRING_FIELDS: Final = {
    "orgid": "orgId",
    "departmentid": "departmentId",
    "sweepstart": "sweepStart",
    "floor": "floor",
    "highwater": "highWater",
}


def _decode_cursor_fields(token: str) -> dict[str, Any]:
    """Decode token as Go's encoding/json fills the cursor struct.

    Keys match case-insensitively, the last duplicate wins, unknown keys and null values are ignored, and
    a value of the wrong JSON type rejects the token.
    """
    try:
        members = json.loads(token, object_pairs_hook=list, parse_constant=_reject_json_constant)
    except (ValueError, RecursionError):
        raise TicketChangeCursorInvalidError() from None
    fields: dict[str, Any] = {
        "orgId": "",
        "departmentId": "",
        "sweepStart": "",
        "floor": "",
        "from": 0,
        "highWater": "",
    }
    if members is None:
        return fields
    if not isinstance(members, list) or any(not isinstance(member, tuple) for member in members):
        raise TicketChangeCursorInvalidError()
    for name, value in members:
        folded = name.casefold()
        if value is None:
            continue
        if folded in _CURSOR_STRING_FIELDS:
            if not isinstance(value, str):
                raise TicketChangeCursorInvalidError()
            fields[_CURSOR_STRING_FIELDS[folded]] = value
        elif folded == "from":
            if type(value) is not int or not _INT64_MINIMUM <= value <= _INT64_MAXIMUM:
                raise TicketChangeCursorInvalidError()
            fields["from"] = value
    return fields


def _encode_timestamp(moment: datetime) -> str:
    return encode_go_json_value(format_zoho_timestamp(moment)).decode()


def _encode_cursor(
    organization_id: str, department_id: str, position: _SearchPosition, snapshot: KeyedSnapshot | None
) -> Cursor:
    """Encode the position as Go's json.Marshal does: struct field order, omitempty, HTML-safe strings."""
    members = ['"orgId":' + encode_go_json_value(organization_id).decode()]
    if department_id:
        members.append('"departmentId":' + encode_go_json_value(department_id).decode())
    members.append('"sweepStart":' + _encode_timestamp(position.sweep_start))
    members.append('"floor":' + _encode_timestamp(position.floor))
    if position.offset:
        members.append(f'"from":{position.offset}')
    members.append('"highWater":' + _encode_timestamp(position.high_water))
    return Cursor(token="{" + ",".join(members) + "}", snapshot=snapshot)


def _next_search_position(position: _SearchPosition, page: _SearchPage) -> tuple[_SearchPosition, bool]:
    """Advance by modified time; a full page of one instant pages by offset, then steps one millisecond.

    A short page ends the sweep, and the next sweep starts the lookback before the newest modified time.
    """
    high_water = position.high_water
    for ticket in page.tickets:
        if ticket.modified_at > high_water:
            high_water = ticket.modified_at
    if page.received < TICKET_POLL_PAGE_LIMIT:
        start = high_water - TICKET_SEARCH_INDEX_LOOKBACK
        return _SearchPosition(start, start, 0, high_water), False
    last = page.last if page.last is not None else position.floor
    if last > position.floor:
        return _SearchPosition(position.sweep_start, last, 0, high_water), True
    if position.offset + TICKET_POLL_PAGE_LIMIT <= _MAXIMUM_FROM:
        offset = position.offset + TICKET_POLL_PAGE_LIMIT
        return _SearchPosition(position.sweep_start, position.floor, offset, high_water), True
    next_floor = position.floor + timedelta(milliseconds=1)
    return _SearchPosition(position.sweep_start, next_floor, 0, high_water), True


def _server_time(value: str | None) -> datetime | None:
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
    return parsed.astimezone(UTC)


def _is_server_clock_skewed(server_time: datetime | None, now: datetime) -> bool:
    """Report skew beyond the Date header's one-second precision but under 24 hours."""
    if server_time is None:
        return False
    skew = abs(server_time - now)
    return timedelta(seconds=1) < skew < _MAXIMUM_SERVER_CLOCK_SKEW


def _decode_search_page(status: int, body: bytes, server_time: datetime | None) -> _SearchPage:
    """Decode a {data, count} page in Zoho Desk's order; a twice-listed ticket keeps its newest version."""
    if status == 204:
        return _SearchPage((), 0, None, server_time)
    try:
        document = _object(json.loads(body))
        rows = document.get("data")
        if not isinstance(rows, list):
            raise ValueError("no data array")
        _count(document.get("count"))
    except (ValueError, TypeError, UnicodeDecodeError):
        raise ValueError("Zoho Desk returned an invalid ticket search page") from None
    if len(rows) > TICKET_POLL_PAGE_LIMIT:
        raise ValueError("Zoho Desk ticket search page holds more tickets than requested")
    tickets: list[Ticket] = []
    positions: dict[str, int] = {}
    last: datetime | None = None
    for index, row in enumerate(rows):
        try:
            ticket, contact = _ticket(row)
            _contact(contact)
        except (ValueError, TypeError) as error:
            raise ValueError(f"Zoho Desk ticket search result {index}: {error}") from None
        ticket = replace(ticket, description_html="", is_description_truncated=False)
        last = ticket.modified_at
        listed = positions.get(ticket.id)
        if listed is not None:
            if ticket.modified_at > tickets[listed].modified_at:
                tickets[listed] = ticket
            continue
        positions[ticket.id] = len(tickets)
        tickets.append(ticket)
    return _SearchPage(tuple(tickets), len(rows), last, server_time)


def _apply_ticket_versions(snapshot: KeyedSnapshot | None, tickets: Sequence[Ticket]) -> KeyedSnapshot:
    return apply_keyed_snapshot_changes(
        snapshot, [_TicketVersion(ticket) for ticket in tickets], (), TICKET_SNAPSHOT_OPTIONS
    )


class TicketChangeFetcher:
    """Read one organization's tickets in ascending modified-time order for the poll Trigger kit.

    It implements `ChangeFetcher[TriggerEvent[TicketChangedEvent]]` at parity with the Go connector: the
    cursor is a modified-time keyset position (sweep start, floor, offset within one millisecond, and
    high-water mark) plus a keyed snapshot of up to 10,000 recent ticket versions, so tickets changed during
    a sweep are never skipped and re-read versions are never delivered twice. A poller owns one fetcher and
    calls it from one task at a time.
    """

    def __init__(
        self, client: Client, connection: ConnectionRef, configuration: TicketChangedTriggerConfiguration
    ) -> None:
        """Bind the fetcher to one connection's client and one binding's department."""
        if client is None or connection is None or configuration is None:
            raise ValueError("Zoho Desk ticketChanged client, connection, and configuration are required")
        self._client = client
        self._organization_id = client.config.org_id.strip()
        self._department_id = configuration.department_id
        self._call = new_trigger_credential_call(connection, TICKET_CHANGED_TRIGGER_DEFINITION.trigger)

    async def seed_cursor(self, context: TriggerContext) -> Cursor:
        """Record every ticket modified inside the lookback before now as the baseline and deliver none.

        A local clock skewed from Zoho Desk's Date header by more than a second yields to it, so a fast
        local clock skips nothing. Raises RuntimeError past 100 baseline pages.
        """
        now = self._client._now().astimezone(UTC)
        start = now - TICKET_SEARCH_INDEX_LOOKBACK
        position = _SearchPosition(start, start, 0, now)
        snapshot: KeyedSnapshot | None = None
        for page_number in range(_MAXIMUM_BASELINE_PAGES):
            page = await self._search_page(context, position)
            snapshot = _apply_ticket_versions(snapshot, page.tickets)
            if page_number == 0 and _is_server_clock_skewed(page.server_time, now):
                assert page.server_time is not None
                position = replace(position, high_water=page.server_time)
            position, has_more_pages = _next_search_position(position, page)
            if not has_more_pages:
                return _encode_cursor(self._organization_id, self._department_id, position, snapshot)
        raise RuntimeError(
            f"Zoho Desk ticketChanged baseline exceeded {_MAXIMUM_BASELINE_PAGES} search pages"
        )

    async def fetch_changes(
        self, context: TriggerContext, cursor: Cursor
    ) -> ChangePage[TriggerEvent[TicketChangedEvent]]:
        """Read one search page after cursor and return the ticket versions the snapshot lacks.

        An undecodable cursor, one for another organization or department, or a 400 or 422 naming
        modifiedTimeRange or from raises `mark_cursor_invalid`, so the kit records a new baseline; a 429
        raises `mark_rate_limited` with Retry-After.
        """
        try:
            position = self._decode_cursor(cursor.token)
        except TicketChangeCursorInvalidError as error:
            raise mark_cursor_invalid(error) from None
        page = await self._search_page(context, position)
        known = {entry.key: entry.hash for entry in cursor.snapshot.entries} if cursor.snapshot else {}
        changes: list[TriggerEvent[TicketChangedEvent]] = []
        for ticket in page.tickets:
            stored_hash = known.get(ticket.id)
            if stored_hash is not None and stored_hash == ticket_version_hash(ticket):
                continue
            kind = ticket_change_kind(ticket, stored_hash is not None, position.sweep_start)
            payload = TicketChangedEvent(
                org_id=self._organization_id,
                department_id=self._department_id,
                change_kind=kind,
                ticket=ticket,
            )
            changes.append(
                TriggerEvent(
                    ticket_change_event_id(self._organization_id, ticket),
                    ticket.modified_at.astimezone(UTC),
                    payload,
                )
            )
        snapshot = _apply_ticket_versions(cursor.snapshot, page.tickets)
        next_position, has_more_pages = _next_search_position(position, page)
        return ChangePage(
            tuple(changes),
            _encode_cursor(self._organization_id, self._department_id, next_position, snapshot),
            has_more_pages,
        )

    def _decode_cursor(self, token: str) -> _SearchPosition:
        fields = _decode_cursor_fields(token)
        if fields["orgId"] != self._organization_id or fields["departmentId"] != self._department_id:
            raise TicketChangeCursorInvalidError(
                "Zoho Desk ticket cursor belongs to another organization or department"
            )
        offset = fields["from"]
        if offset < 0 or offset > _MAXIMUM_FROM:
            raise TicketChangeCursorInvalidError()
        return _SearchPosition(
            _parse_rfc3339(fields["sweepStart"]),
            _parse_rfc3339(fields["floor"]),
            offset,
            _parse_rfc3339(fields["highWater"]),
        )

    def _search_parameters(self, position: _SearchPosition) -> dict[str, str]:
        upper = self._client._now().astimezone(UTC)
        if upper < position.floor:
            upper = position.floor
        parameters = {
            "modifiedTimeRange": format_zoho_timestamp(position.floor) + "," + format_zoho_timestamp(upper),
            "sortBy": "modifiedTime",
            "from": str(position.offset),
            "limit": str(TICKET_POLL_PAGE_LIMIT),
        }
        if self._department_id:
            parameters["departmentId"] = self._department_id
        return parameters

    async def _search_page(self, context: TriggerContext, position: _SearchPosition) -> _SearchPage:
        await wait_for_poll_request(context)
        credentials, base_url, route = await self._client.resolve(self._call, _OPERATION)
        if route or credentials is None or base_url is None:
            raise RuntimeError("Zoho Desk ticketChanged poll: the connection credential is unavailable")
        result = await self._client.exchange(
            self._call,
            _OPERATION,
            credentials,
            base_url,
            "GET",
            "/tickets/search",
            self._search_parameters(position),
        )
        message = result.failure.message if result.failure is not None else "Zoho Desk returned no response"
        if result.outcome == "rate_limited":
            # Zoho Desk meters API credits per organization, so every binding of the connection pauses.
            raise mark_rate_limited(RuntimeError(message), timedelta(seconds=result.retry_after_seconds))
        if result.outcome == "rejected" and result.status in (400, 422):
            if any(
                field.startswith("/modifiedTimeRange") or field.startswith("/from")
                for field in result.field_errors
            ):
                raise mark_cursor_invalid(RuntimeError(message))
        if result.outcome != "success":
            raise RuntimeError(message)
        headers = result.headers
        return _decode_search_page(
            result.status, result.body, _server_time(headers.get("Date") if headers is not None else None)
        )
