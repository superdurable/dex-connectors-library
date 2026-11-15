# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Poll one Notion data source's rows by last edited time for the poll Trigger kit, as the Go fetcher does."""

import hashlib
import json
from dataclasses import dataclass
from datetime import timedelta
from typing import Final

from dex_connectors.sdk import Call, ConnectionRef, FailureKind, TriggerContext, TriggerEvent
from dex_connectors.sdk.pollingtrigger import (
    ChangePage,
    Cursor,
    mark_cursor_invalid,
    mark_rate_limited,
    new_trigger_credential_call,
    wait_for_poll_request,
)
from dex_connectors.sdk.providerhttp import read_error_tokens

from ._database_row_cursor import (
    DATABASE_ROW_CURSOR_VERSION,
    DATABASE_ROW_LOOKBACK_NANOSECONDS,
    GO_ZERO_TIME,
    DatabaseRowCursor,
    DatabaseRowCursorDecodeError,
    SeenRows,
    decode_database_row_cursor,
    encode_database_row_cursor,
)
from ._generated.definitions import DATABASE_ROW_CHANGED_TRIGGER_DEFINITION
from ._go_time import UNIX_EPOCH, GoTime, parse_go_time
from .client import Client, classify_read, escaped_path
from .encoding import cursor as validate_cursor
from .models import DatabaseRowChangedEvent, Page
from .provider_models import decode_page

_OPERATION_ID: Final = "databaseRowChanged"
_LAST_EDITED_TIME: Final = "last_edited_time"
_EVENT_ID_PREFIX: Final = "notion-row-"
# Bounds the baseline read of the lookback window.
_MAXIMUM_SEED_PAGES: Final = 20
# An empty data source's first query starts at the Unix epoch, not the host clock.
_EMPTY_DATA_SOURCE_HIGH_WATER: Final = UNIX_EPOCH.add(DATABASE_ROW_LOOKBACK_NANOSECONDS)


class DatabaseRowPollError(RuntimeError):
    """A failed data source query; the kit backs off and retries without moving the cursor."""


class _StaleStartCursorError(DatabaseRowPollError):
    """Notion answered 400 validation_error for the stored start cursor; the query restarts."""


@dataclass(frozen=True, slots=True)
class _QueriedRow:
    page: Page
    last_edited_time: GoTime


@dataclass(frozen=True, slots=True)
class _QueriedRows:
    rows: tuple[_QueriedRow, ...]
    next_cursor: str


class DatabaseRowFetcher:
    """The `ChangeFetcher` of one databaseRowChanged binding: one data source's rows by last edited time."""

    def __init__(self, client: Client, connection: ConnectionRef, data_source_id: str) -> None:
        """Bind the client and the dashed watched data source ID; it sends nothing."""
        self._client = client
        self._call: Call = new_trigger_credential_call(
            connection, DATABASE_ROW_CHANGED_TRIGGER_DEFINITION.trigger
        )
        self._data_source_id = data_source_id

    async def seed_cursor(self, context: TriggerContext) -> Cursor:
        """Read the newest last edited time, then remember every row edited inside the lookback before it,
        so no existing row is delivered."""
        newest = await self._query(context, None, "", 1, is_newest_first=True)
        position = DatabaseRowCursor(
            DATABASE_ROW_CURSOR_VERSION, self._data_source_id, _EMPTY_DATA_SOURCE_HIGH_WATER
        )
        if newest.rows:
            position.high_water = newest.rows[0].last_edited_time
        seen = SeenRows([])
        query_from = position.high_water.add(-DATABASE_ROW_LOOKBACK_NANOSECONDS)
        start_cursor = ""
        for _ in range(_MAXIMUM_SEED_PAGES):
            page = await self._query(context, query_from, start_cursor, self._client.poll_page_size)
            for row in page.rows:
                seen.add(row.last_edited_time, row.page.id)
                position.high_water = _later_time(position.high_water, row.last_edited_time)
            if not page.next_cursor:
                break
            start_cursor = page.next_cursor
        position.seen = seen.groups_within(position.high_water)
        return _encode(position)

    async def fetch_changes(
        self, context: TriggerContext, cursor: Cursor
    ) -> ChangePage[TriggerEvent[DatabaseRowChangedEvent]]:
        """Query rows edited at or after the lookback before the high water, oldest first, and return each
        row version not yet seen.

        An undecodable cursor, or one for another data source, is marked invalid so the kit records a new
        baseline; a start cursor Notion rejects restarts the query instead.
        """
        try:
            position = decode_database_row_cursor(cursor.token)
        except DatabaseRowCursorDecodeError:
            position = None
        if (
            position is None
            or position.version != DATABASE_ROW_CURSOR_VERSION
            or position.high_water.unix_nanoseconds == GO_ZERO_TIME.unix_nanoseconds
        ):
            raise mark_cursor_invalid(
                DatabaseRowPollError("Notion databaseRowChanged cursor is not decodable")
            )
        if position.data_source_id != self._data_source_id:
            raise mark_cursor_invalid(
                DatabaseRowPollError("Notion databaseRowChanged cursor belongs to another data source")
            )
        query_from = position.high_water.add(-DATABASE_ROW_LOOKBACK_NANOSECONDS)
        if position.start_cursor and position.query_from is not None:
            query_from = position.query_from
        try:
            queried = await self._query(
                context, query_from, position.start_cursor, self._client.poll_page_size
            )
        except _StaleStartCursorError:
            position.start_cursor, position.query_from = "", None
            return ChangePage((), _encode(position), True)
        seen = SeenRows(position.seen)
        changes: list[TriggerEvent[DatabaseRowChangedEvent]] = []
        for row in queried.rows:
            if row.last_edited_time.is_before(query_from) or seen.contains(row.last_edited_time, row.page.id):
                continue
            seen.add(row.last_edited_time, row.page.id)
            position.high_water = _later_time(position.high_water, row.last_edited_time)
            changes.append(
                new_database_row_changed_trigger_event(self._data_source_id, row.page, row.last_edited_time)
            )
        position.start_cursor, position.query_from = "", None
        has_more_pages = bool(queried.next_cursor)
        if has_more_pages:
            position.start_cursor, position.query_from = queried.next_cursor, query_from
        position.seen = seen.groups_within(position.high_water)
        return ChangePage(changes, _encode(position), has_more_pages)

    async def _query(
        self,
        context: TriggerContext,
        edited_on_or_after: GoTime | None,
        start_cursor: str,
        page_size: int,
        *,
        is_newest_first: bool = False,
    ) -> _QueriedRows:
        await wait_for_poll_request(context)
        token, _, _ = await self._client.resolve_credentials(self._call, _OPERATION_ID)
        if token is None:
            raise DatabaseRowPollError(
                "Notion databaseRowChanged credentials are unavailable; save the connection's API token again"
            )
        body: dict[str, object] = {}
        if edited_on_or_after is not None:
            body["filter"] = {
                _LAST_EDITED_TIME: {"on_or_after": edited_on_or_after.in_utc().format_rfc3339_nano()},
                "timestamp": _LAST_EDITED_TIME,
            }
        body["sorts"] = [
            {"direction": "descending" if is_newest_first else "ascending", "timestamp": _LAST_EDITED_TIME}
        ]
        if start_cursor:
            body["start_cursor"] = start_cursor
        body["page_size"] = page_size
        body["result_type"] = "page"
        response = await self._client.request(
            token, "POST", "/data_sources/" + escaped_path(self._data_source_id) + "/query", payload=body
        )
        if (
            start_cursor
            and response.error is None
            and not response.not_built
            and response.status == 400
            and tuple(read_error_tokens(response.body, ("/code",)))[:1] == ("validation_error",)
        ):
            raise _StaleStartCursorError("Notion rejected the stored query start cursor")
        classification = classify_read(_OPERATION_ID, response)
        failure = classification.failure
        if classification.outcome == "success":
            return _decode_rows_page(response.body, page_size)
        message = failure.message if failure is not None else "Notion databaseRowChanged query failed"
        if (
            classification.outcome == "retry"
            and failure is not None
            and failure.kind == FailureKind.RATE_LIMIT
        ):
            raise mark_rate_limited(
                DatabaseRowPollError(message), timedelta(seconds=classification.retry_after)
            )
        if classification.outcome == "notFound":
            raise DatabaseRowPollError(
                "Notion could not find the watched data source; share its database with the connection "
                "or pick another data source"
            )
        raise DatabaseRowPollError(message)


def new_database_row_changed_trigger_event(
    data_source_id: str, row: Page, last_edited_time: GoTime
) -> TriggerEvent[DatabaseRowChangedEvent]:
    """Convert one queried row; its ID is a pure function of the row version, so a refetch yields it again."""
    return TriggerEvent(
        database_row_change_id(data_source_id, row.id, last_edited_time),
        last_edited_time.to_datetime(),
        DatabaseRowChangedEvent(
            data_source_id=data_source_id, change_kind=database_row_change_kind(row), row=row
        ),
    )


def database_row_change_id(data_source_id: str, row_id: str, last_edited_time: GoTime) -> str:
    """Hash the data source, row, and last edited time into Go's short stable Trigger event ID."""
    identity = "\x00".join((data_source_id, row_id, last_edited_time.in_utc().format_rfc3339_nano()))
    return _EVENT_ID_PREFIX + hashlib.sha256(identity.encode()).digest()[:16].hex()


def database_row_change_kind(row: Page) -> str:
    """Return created when the row's created and last edited times are the same instant, else updated."""
    try:
        created, edited = parse_go_time(row.created_time), parse_go_time(row.last_edited_time)
    except ValueError:
        return "updated"
    return "created" if created.unix_nanoseconds == edited.unix_nanoseconds else "updated"


def _decode_rows_page(body: bytes, page_size: int) -> _QueriedRows:
    invalid_page = DatabaseRowPollError("Notion returned an invalid databaseRowChanged query page")
    try:
        page = json.loads(body, parse_constant=_refuse_constant)
    except (ValueError, RecursionError) as error:
        raise invalid_page from error
    if page is None:
        return _QueriedRows((), "")
    if not isinstance(page, dict):
        raise invalid_page
    results, next_cursor, has_more = page.get("results"), page.get("next_cursor"), page.get("has_more")
    if (
        not isinstance(results, list | None)
        or not isinstance(next_cursor, str | None)
        or not isinstance(has_more, bool | None)
        or not isinstance(page.get("request_status"), dict | None)
        or len(results or ()) > page_size
    ):
        raise invalid_page
    try:
        validated_cursor = validate_cursor(next_cursor or "")
    except ValueError as error:
        raise DatabaseRowPollError("Notion returned an invalid databaseRowChanged query cursor") from error
    rows = []
    for index, resource in enumerate(results or ()):
        try:
            row = decode_page(resource)
        except (TypeError, ValueError) as error:
            raise DatabaseRowPollError(
                f"Notion returned an invalid row {index} in the databaseRowChanged query page"
            ) from error
        try:
            last_edited_time = parse_go_time(row.last_edited_time).in_utc()
        except ValueError as error:
            raise DatabaseRowPollError(
                f"Notion returned row {index} without a valid last_edited_time"
            ) from error
        rows.append(_QueriedRow(row, last_edited_time))
    return _QueriedRows(tuple(rows), validated_cursor if has_more else "")


def _refuse_constant(name: str) -> None:
    raise ValueError(f"JSON does not allow {name}")


def _later_time(current: GoTime, candidate: GoTime) -> GoTime:
    return candidate if candidate.is_after(current) else current


def _encode(position: DatabaseRowCursor) -> Cursor:
    try:
        return Cursor(encode_database_row_cursor(position))
    except ValueError as error:
        raise DatabaseRowPollError("encode Notion databaseRowChanged cursor") from error
