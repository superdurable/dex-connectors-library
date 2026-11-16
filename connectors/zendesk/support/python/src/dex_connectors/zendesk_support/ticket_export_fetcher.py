# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read Zendesk's cursor-based incremental ticket export for the ticketChanged poll Trigger.

The cursor token is the Go connector's JSON document byte for byte, so Go and Python replicas share one
binding cursor, and each ticket version keeps the Go connector's Trigger event ID.
"""

import hashlib
import json
from dataclasses import dataclass, replace
from datetime import UTC, datetime, timedelta
from typing import Any, Final

from dex_connectors.sdk import ConnectionRef, FailureKind, TriggerContext, TriggerEvent
from dex_connectors.sdk.pollingtrigger import (
    MAXIMUM_CHANGES_PER_PAGE,
    ChangePage,
    Cursor,
    mark_cursor_invalid,
    mark_rate_limited,
    new_trigger_credential_call,
    wait_for_poll_request,
)
from dex_connectors.sdk.projectconfig import encode_go_json_value

from ._generated.definitions import TICKET_CHANGED_TRIGGER_DEFINITION
from .client import Client, Exchange, decode_ticket
from .models import Ticket, TicketChangedEvent, TicketChangedTriggerConfiguration

TICKET_STATUS_DELETED: Final = "deleted"
"""The status the incremental ticket export reports for a deleted ticket; no operation accepts it."""
TICKET_CHANGE_CREATED: Final = "created"
"""A new ticket: its updated_at is within one second of its created_at."""
TICKET_CHANGE_UPDATED: Final = "updated"
"""Any other change to a ticket that is not deleted, such as a reply or a status or assignee change."""
TICKET_CHANGE_DELETED: Final = "deleted"
"""A ticket the export reports with status deleted; bindings without includeDeleted never receive it."""

_TICKET_EXPORT_PATH: Final = "/incremental/tickets/cursor"
_OPERATION: Final = "ticketChanged"
# Keeps a page of tickets with descriptions well under the default response limit.
_TICKET_EXPORT_PAGE_SIZE: Final = 100
_MINIMUM_TICKET_EXPORT_PAGE_SIZE: Final = 10
_MAXIMUM_BASELINE_PAGES: Final = 100
# Zendesk serves no data for the latest minute and rejects a start_time inside it.
_TICKET_EXPORT_START_TIME_LAG: Final = timedelta(seconds=61)
# Tolerates a Worker clock up to four minutes ahead of Zendesk's.
_TICKET_EXPORT_SEED_LAG: Final = timedelta(minutes=5)
_CREATED_UPDATED_TOLERANCE: Final = timedelta(seconds=1)
_UNIX_EPOCH: Final = datetime(1970, 1, 1, tzinfo=UTC)
_ZERO_TIME: Final = datetime.min.replace(tzinfo=UTC)
_INT64_MINIMUM, _INT64_MAXIMUM = -(1 << 63), (1 << 63) - 1


class TicketExportCursorInvalidError(ValueError):
    """A ticketChanged cursor token that is not a valid Go cursor document."""

    def __init__(self) -> None:
        """Describe the rejected token without echoing it."""
        super().__init__("Zendesk incremental export cursor is not decodable")


class _TicketExportCursorRejectedError(RuntimeError):
    """An HTTP 400 or 422 answer to a cursor request."""


@dataclass(frozen=True, slots=True)
class _TicketExportCursor:
    # resume_at is at or before every change after after_cursor, so a restart from it leaves no gap.
    after_cursor: str = ""
    resume_at: int = 0
    include_deleted: bool = False


@dataclass(frozen=True, slots=True)
class _ExportTicket:
    ticket: Ticket
    generated_timestamp: int


@dataclass(frozen=True, slots=True)
class _TicketExportPage:
    tickets: tuple[_ExportTicket, ...]
    after_cursor: str | None
    is_end_of_stream: bool


@dataclass(frozen=True, slots=True)
class _TicketExportPosition:
    # An export request starts at cursor, or at start_time when cursor is empty.
    cursor: str = ""
    start_time: int = 0


def _unix_seconds(moment: datetime) -> int:
    return (moment - _UNIX_EPOCH) // timedelta(seconds=1)


def _format_go_rfc3339_seconds(moment: datetime) -> str:
    utc = moment.astimezone(UTC)
    return f"{utc.year:04d}-{utc.month:02d}-{utc.day:02d}T{utc.hour:02d}:{utc.minute:02d}:{utc.second:02d}Z"


def ticket_change_kind(ticket: Ticket) -> str:
    """Derive the change kind: deleted from the status, created when updated_at is within 1s of created_at."""
    if ticket.status == TICKET_STATUS_DELETED:
        return TICKET_CHANGE_DELETED
    if (
        ticket.created_at != _ZERO_TIME
        and abs(ticket.updated_at - ticket.created_at) <= _CREATED_UPDATED_TOLERANCE
    ):
        return TICKET_CHANGE_CREATED
    return TICKET_CHANGE_UPDATED


def ticket_change_event_id(ticket: Ticket, generated_timestamp: int) -> str:
    """Return the Trigger event ID, a hash of the ticket ID, generated_timestamp, updated_at, and status.

    It is a pure function of the ticket version, so a refetch after a crash or a cursor restart yields the
    same ID, and it equals the Go connector's.
    """
    identity = "\x00".join(
        (
            str(ticket.id),
            str(generated_timestamp),
            _format_go_rfc3339_seconds(ticket.updated_at),
            ticket.status,
        )
    )
    return "zendesk-ticket-" + hashlib.sha256(identity.encode()).hexdigest()[:32]


def new_ticket_changed_trigger_event(
    ticket: Ticket, generated_timestamp: int
) -> TriggerEvent[TicketChangedEvent]:
    """Convert one export ticket; OccurredAt is the later of updated_at and generated_timestamp."""
    generated_at = _ZERO_TIME
    if generated_timestamp > 0:
        generated_at = _UNIX_EPOCH + timedelta(seconds=generated_timestamp)
    event = TicketChangedEvent(
        change_kind=ticket_change_kind(ticket), ticket=ticket, generated_at=generated_at
    )
    occurred_at = generated_at if generated_at > ticket.updated_at else ticket.updated_at
    return TriggerEvent(ticket_change_event_id(ticket, generated_timestamp), occurred_at, event)


def _encode_ticket_export_cursor(position: _TicketExportCursor) -> Cursor:
    """Encode the cursor as Go's json.Marshal does: struct field order, omitempty, HTML-safe strings."""
    members = []
    if position.after_cursor:
        members.append('"afterCursor":' + encode_go_json_value(position.after_cursor).decode())
    members.append(f'"resumeAt":{position.resume_at}')
    if position.include_deleted:
        members.append('"includeDeleted":true')
    return Cursor(token="{" + ",".join(members) + "}")


class _JSONObjectMembers(list[tuple[str, Any]]):
    """The members of one JSON object in document order, distinct from a JSON array."""


def _reject_json_constant(constant: str) -> None:
    raise TicketExportCursorInvalidError()


def _decode_ticket_export_cursor(token: str) -> _TicketExportCursor:
    """Decode the token as Go's encoding/json fills the cursor struct.

    Keys match case-insensitively, the last duplicate wins, unknown keys and null values are ignored, and
    a value of the wrong JSON type rejects the token, as does a resumeAt below 1.
    """
    try:
        members = json.loads(
            token, object_pairs_hook=_JSONObjectMembers, parse_constant=_reject_json_constant
        )
    except (ValueError, RecursionError):
        raise TicketExportCursorInvalidError() from None
    after_cursor, resume_at, include_deleted = "", 0, False
    if members is not None and not isinstance(members, _JSONObjectMembers):
        raise TicketExportCursorInvalidError()
    for name, value in members or []:
        folded = name.casefold()
        if value is None:
            continue
        if folded == "aftercursor":
            if not isinstance(value, str):
                raise TicketExportCursorInvalidError()
            after_cursor = value
        elif folded == "resumeat":
            if type(value) is not int or not _INT64_MINIMUM <= value <= _INT64_MAXIMUM:
                raise TicketExportCursorInvalidError()
            resume_at = value
        elif folded == "includedeleted":
            if not isinstance(value, bool):
                raise TicketExportCursorInvalidError()
            include_deleted = value
    if resume_at < 1:
        raise TicketExportCursorInvalidError()
    return _TicketExportCursor(after_cursor, resume_at, include_deleted)


def _advance_ticket_export_cursor(
    position: _TicketExportCursor, listed: _TicketExportPage
) -> _TicketExportCursor:
    """Keep the page's after_cursor and move resume_at to the newest generated_timestamp.

    Later pages may hold more changes in that second; at the end of the stream every change of that second
    was served, so resume_at moves one second past it.
    """
    after_cursor = listed.after_cursor if listed.after_cursor else position.after_cursor
    resume_at = position.resume_at
    for exported in listed.tickets:
        candidate = exported.generated_timestamp + (1 if listed.is_end_of_stream else 0)
        resume_at = max(resume_at, candidate)
    return replace(position, after_cursor=after_cursor, resume_at=resume_at)


def _optional_int(value: dict[str, Any], key: str) -> int:
    field = value.get(key)
    if field is None:
        return 0
    if type(field) is not int or not _INT64_MINIMUM <= field <= _INT64_MAXIMUM:
        raise ValueError(f"{key} is not an integer")
    return field


def _decode_ticket_export_page(body: bytes, agent_ticket_url: str) -> _TicketExportPage:
    """Decode one export page as the Go connector's strict struct decoding accepts it."""
    try:
        document = json.loads(body)
    except (ValueError, UnicodeDecodeError, RecursionError):
        raise ValueError("Zendesk incremental export page is not valid JSON") from None
    if document is None:
        document = {}
    if not isinstance(document, dict):
        raise ValueError("Zendesk incremental export page is not valid JSON")
    rows = document.get("tickets")
    after_cursor = document.get("after_cursor")
    is_end_of_stream = document.get("end_of_stream")
    if (
        rows is not None
        and not isinstance(rows, list)
        or after_cursor is not None
        and not isinstance(after_cursor, str)
        or is_end_of_stream is not None
        and not isinstance(is_end_of_stream, bool)
    ):
        raise ValueError("Zendesk incremental export page is not valid JSON")
    rows = rows or []
    if len(rows) > MAXIMUM_CHANGES_PER_PAGE:
        raise ValueError(
            f"Zendesk incremental export page has {len(rows)} tickets, more than {MAXIMUM_CHANGES_PER_PAGE}"
        )
    tickets = []
    for row in rows:
        try:
            ticket, _ = decode_ticket(row, agent_ticket_url)
            generated_timestamp = _optional_int(row, "generated_timestamp")
            if generated_timestamp > _unix_seconds(datetime.max.replace(tzinfo=UTC)):
                raise ValueError("generated_timestamp is out of range")
        except (ValueError, TypeError, OverflowError) as error:
            raise ValueError(f"Zendesk incremental export page has an invalid ticket: {error}") from None
        tickets.append(_ExportTicket(ticket, generated_timestamp))
    return _TicketExportPage(tuple(tickets), after_cursor, bool(is_end_of_stream))


def _classify_ticket_export_failure(result: Exchange, position: _TicketExportPosition) -> Exception:
    """Map a failed export request to the kit's signals; messages never carry the token."""
    message = result.failure.message if result.failure is not None else "Zendesk returned no response"
    failure = RuntimeError("Zendesk incremental export: " + message)
    if result.status == 429:
        # The limit is per account, so every binding of the connection pauses.
        return mark_rate_limited(failure, timedelta(seconds=result.retry_after))
    if position.cursor and result.status in (400, 422):
        return _TicketExportCursorRejectedError(f"Zendesk rejected the incremental export cursor: {failure}")
    if result.status == 403:
        return RuntimeError(
            f"{failure}; Zendesk allows the incremental ticket export only for administrators, so connect an "
            "administrator's email and API token"
        )
    return failure


class TicketExportFetcher:
    """Read the account's cursor-based incremental ticket export for the poll Trigger kit.

    It implements `ChangeFetcher[TriggerEvent[TicketChangedEvent]]` at parity with the Go connector. A
    poller owns one fetcher and calls it from one task at a time; its page size starts at 100 and halves,
    down to 10, after a response larger than maxResponseBytes, for the life of the fetcher.
    """

    def __init__(
        self, client: Client, connection: ConnectionRef, configuration: TicketChangedTriggerConfiguration
    ) -> None:
        """Bind the fetcher to one connection's client and one binding's configuration."""
        if client is None or connection is None or configuration is None:
            raise ValueError("Zendesk ticketChanged client, connection, and configuration are required")
        self._client = client
        self._call = new_trigger_credential_call(connection, TICKET_CHANGED_TRIGGER_DEFINITION.trigger)
        self._is_including_deleted = configuration.include_deleted
        self._page_size = _TICKET_EXPORT_PAGE_SIZE

    async def seed_cursor(self, context: TriggerContext) -> Cursor:
        """Start the export five minutes ago and drain it to the end, keeping only the final cursor.

        Tickets changed before the first poll are never delivered. Raises after 100 pages without the end
        of the stream.
        """
        position = _TicketExportCursor(
            resume_at=_unix_seconds(self._client.now() - _TICKET_EXPORT_SEED_LAG),
            include_deleted=self._is_including_deleted,
        )
        request = _TicketExportPosition(start_time=position.resume_at)
        for _ in range(_MAXIMUM_BASELINE_PAGES):
            listed = await self._list_export_page(context, request)
            position = _advance_ticket_export_cursor(position, listed)
            if listed.is_end_of_stream:
                return _encode_ticket_export_cursor(position)
            if not position.after_cursor or position.after_cursor == request.cursor:
                raise ValueError(
                    "Zendesk incremental export page has neither end_of_stream nor a new after_cursor"
                )
            request = _TicketExportPosition(cursor=position.after_cursor)
        raise ValueError(f"Zendesk incremental export baseline exceeded {_MAXIMUM_BASELINE_PAGES} pages")

    async def fetch_changes(
        self, context: TriggerContext, cursor: Cursor
    ) -> ChangePage[TriggerEvent[TicketChangedEvent]]:
        """Read one export page after cursor.

        A cursor Zendesk rejects with HTTP 400 or 422, a cursor without an after_cursor, or a cursor saved
        with another includeDeleted restarts from its resumeAt time. An undecodable cursor raises
        `mark_cursor_invalid`, so the kit records a new baseline. A rejected start_time restart fails the
        poll instead, because a new baseline would also lose the changes after resumeAt. A 429 raises
        `mark_rate_limited` with Retry-After.
        """
        try:
            position = _decode_ticket_export_cursor(cursor.token)
        except TicketExportCursorInvalidError as error:
            raise mark_cursor_invalid(error) from None
        restart_at = _unix_seconds(self._client.now() - _TICKET_EXPORT_START_TIME_LAG)
        restart = _TicketExportPosition(start_time=min(position.resume_at, restart_at))
        request = restart
        if position.after_cursor and position.include_deleted == self._is_including_deleted:
            request = _TicketExportPosition(cursor=position.after_cursor)
        try:
            listed = await self._list_export_page(context, request)
        except _TicketExportCursorRejectedError:
            position, request = replace(position, after_cursor=""), restart
            listed = await self._list_export_page(context, request)
        changes = []
        for exported in listed.tickets:
            if exported.ticket.status == TICKET_STATUS_DELETED and not self._is_including_deleted:
                continue
            changes.append(new_ticket_changed_trigger_event(exported.ticket, exported.generated_timestamp))
        position = replace(position, include_deleted=self._is_including_deleted)
        next_position = _advance_ticket_export_cursor(position, listed)
        has_more_pages = False
        if not listed.is_end_of_stream:
            if not next_position.after_cursor or next_position.after_cursor == request.cursor:
                raise ValueError(
                    "Zendesk incremental export page has neither end_of_stream nor a new after_cursor"
                )
            has_more_pages = True
        return ChangePage(tuple(changes), _encode_ticket_export_cursor(next_position), has_more_pages)

    async def _list_export_page(
        self, context: TriggerContext, position: _TicketExportPosition
    ) -> _TicketExportPage:
        # Halves per_page while Zendesk's answer exceeds maxResponseBytes.
        while True:
            listed = await self._send_export_request(context, position)
            if listed is not None:
                return listed
            if self._page_size <= _MINIMUM_TICKET_EXPORT_PAGE_SIZE:
                raise ValueError(
                    f"Zendesk incremental export page of {self._page_size} tickets exceeds "
                    "maxResponseBytes; raise maxResponseBytes"
                )
            self._page_size = max(self._page_size // 2, _MINIMUM_TICKET_EXPORT_PAGE_SIZE)

    async def _send_export_request(
        self, context: TriggerContext, position: _TicketExportPosition
    ) -> _TicketExportPage | None:
        # Returns None for an oversized response.
        await wait_for_poll_request(context)
        await self._client.export_request_limiter.wait_for_request_start(context)
        credentials, failure = await self._client.resolve_credentials(self._call, _OPERATION)
        if failure is not None or credentials is None:
            raise RuntimeError("Zendesk Trigger credentials are unavailable")
        query = {"per_page": str(self._page_size)}
        if position.cursor:
            query["cursor"] = position.cursor
        else:
            query["start_time"] = str(position.start_time)
        if not self._is_including_deleted:
            query["exclude_deleted"] = "true"
        result = await self._client.exchange(
            self._call, credentials, _OPERATION, "GET", _TICKET_EXPORT_PATH, query
        )
        if result.outcome == "invalid":
            if result.failure is not None and result.failure.kind == FailureKind.RESPONSE_TOO_LARGE:
                return None
            message = result.failure.message if result.failure is not None else "invalid response"
            raise RuntimeError("Zendesk incremental export: " + message)
        if result.outcome != "success":
            raise _classify_ticket_export_failure(result, position)
        return _decode_ticket_export_page(result.body, self._client.agent_ticket_url)
