# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""List one table's records by its Last modified time field, oldest change first, as the Go fetcher does."""

from dataclasses import dataclass
from datetime import timedelta
from typing import Any, Final

from dex_connectors.sdk import Call, FailureKind, TriggerBindingRef, TriggerContext, TriggerEvent
from dex_connectors.sdk.pollingtrigger import (
    ChangePage,
    Cursor,
    mark_cursor_invalid,
    mark_rate_limited,
    new_trigger_credential_call,
    wait_for_poll_request,
)

from ._generated.definitions import RECORD_CHANGED_TRIGGER_DEFINITION
from ._record_changed_cursor import (
    RecordChangedCursorError,
    RecordChangedPosition,
    RecordChangedScan,
    decode_record_changed_cursor,
    encode_record_changed_cursor,
)
from ._record_changed_time import (
    NANOSECONDS_PER_MILLISECOND,
    NANOSECONDS_PER_SECOND,
    datetime_to_nanoseconds,
    format_event_id_time,
    format_milliseconds,
    format_rfc3339_nanoseconds,
    nanoseconds_to_datetime,
    parse_rfc3339_nanoseconds,
    truncate_to_millisecond,
)
from .client import (
    Client,
    _decode_json,
    _decode_record_page,
    _DecodeError,
    _Disposition,
    _dump_json,
    _escape,
    _Outcome,
    _record_path,
)
from .models import Record, RecordChangedEvent, RecordChangedTriggerConfiguration

# Pages one fetch follows past already processed records; the open scan resumes on the next fetch.
MAXIMUM_RECORD_CHANGED_SCAN_PAGES: Final = 20
_LAST_MODIFIED_TIME_FIELD_TYPE: Final = "lastModifiedTime"
_TRIGGER_NAME: Final = RECORD_CHANGED_TRIGGER_DEFINITION.trigger.trigger_name


class RecordChangedPollError(RuntimeError):
    """A failed recordChanged poll; the kit backs off and retries without moving the cursor."""


class _ListOffsetExpiredError(RecordChangedPollError):
    """Airtable answered 422 LIST_RECORDS_ITERATOR_NOT_AVAILABLE for a page offset."""


@dataclass(frozen=True, slots=True)
class _ChangedRecord:
    record: Record
    modified_at: int


def record_changed_event_id(record_id: str, last_modified_time: int) -> str:
    """Return Go's event ID: the record ID and its modification time, a pure function of the change."""
    return record_id + "-" + format_event_id_time(last_modified_time)


class RecordChangedFetcher:
    """The `ChangeFetcher` of one recordChanged binding: one table's records by modification time."""

    def __init__(
        self, client: Client, binding: TriggerBindingRef, configuration: RecordChangedTriggerConfiguration
    ) -> None:
        """Bind the client and the binding's table; it sends nothing."""
        self._client = client
        self._configuration = configuration
        self._call: Call = new_trigger_credential_call(binding.connection, binding.trigger)
        # The watched field's current name, resolved from the base schema; blank until resolved.
        self._field_name = ""

    async def seed_cursor(self, context: TriggerContext) -> Cursor:
        """Position the cursor at the table's most recent change, so existing records are not delivered."""
        await self._resolve_field_name(context)
        records, next_offset = await self._list_records(context, None, "desc", "")
        position = RecordChangedPosition(self._configuration.field_id)
        if next_offset and records:
            # Older existing records than this page, including its oldest time's unlisted ties, stay hidden.
            oldest = min(changed.modified_at for changed in records)
            position.floor = truncate_to_millisecond(oldest) + NANOSECONDS_PER_MILLISECOND
        for changed in records:
            position.deliver(changed.record.id, changed.modified_at)
        return encode_record_changed_cursor(position)

    async def fetch_changes(
        self, context: TriggerContext, cursor: Cursor
    ) -> ChangePage[TriggerEvent[RecordChangedEvent]]:
        """List records modified since the cursor's lower bound, oldest first, and return those not yet
        delivered. An open scan resumes from its stored Airtable offset."""
        try:
            position = decode_record_changed_cursor(cursor.token)
        except RecordChangedCursorError as error:
            raise mark_cursor_invalid(error) from error
        if position.field_id != self._configuration.field_id:
            raise mark_cursor_invalid(
                RecordChangedPollError("the binding now watches a different Last modified time field")
            )
        await self._resolve_field_name(context)
        since, offset = position.filter_since(), ""
        if position.scan is not None:
            since, offset = parse_rfc3339_nanoseconds(position.scan.since) or 0, position.scan.offset
        has_restarted_scan = False
        changes: list[TriggerEvent[RecordChangedEvent]] = []
        for _ in range(MAXIMUM_RECORD_CHANGED_SCAN_PAGES):
            try:
                records, next_offset = await self._list_records(context, since, "asc", offset)
            except _ListOffsetExpiredError:
                if not offset or has_restarted_scan:
                    raise
                # Airtable iterators time out; the scan starts again, and remembered records stay skipped.
                has_restarted_scan, position.scan = True, None
                since, offset = position.filter_since(), ""
                continue
            for changed in records:
                if position.is_delivered(changed.record.id, changed.modified_at):
                    continue
                changes.append(self._new_event(changed))
                position.deliver(changed.record.id, changed.modified_at)
            position.scan = None
            if not next_offset:
                position.close_burst()
            else:
                position.scan = RecordChangedScan(format_rfc3339_nanoseconds(since), next_offset)
            if changes or not next_offset:
                return ChangePage(changes, encode_record_changed_cursor(position), bool(next_offset))
            offset = next_offset
        # A long run of delivered records: commit the open scan so the next fetch continues it.
        return ChangePage(changes, encode_record_changed_cursor(position), True)

    def _new_event(self, changed: _ChangedRecord) -> TriggerEvent[RecordChangedEvent]:
        created_at = datetime_to_nanoseconds(changed.record.created_time)
        occurred_at = nanoseconds_to_datetime(changed.modified_at)
        return TriggerEvent(
            record_changed_event_id(changed.record.id, changed.modified_at),
            occurred_at,
            RecordChangedEvent(
                base_id=self._configuration.base_id,
                table_id=self._configuration.table_id,
                last_modified_time=occurred_at,
                is_created_record=abs(changed.modified_at - created_at) <= NANOSECONDS_PER_SECOND,
                record=changed.record,
            ),
        )

    async def _list_records(
        self, context: TriggerContext, since: int | None, direction: str, offset: str
    ) -> tuple[list[_ChangedRecord], str]:
        """Read one page sorted by the watched field, of records whose field value is after since."""
        field_id = self._configuration.field_id
        body: dict[str, Any] = {"pageSize": self._client.poll_page_size}
        if offset:
            body["offset"] = offset
        body["filterByFormula"] = (
            "IS_AFTER({" + field_id + '}, DATETIME_PARSE("' + format_milliseconds(since or 0) + '"))'
        )
        body["sort"] = [{"field": field_id, "direction": direction}]
        base_id = self._configuration.base_id
        response = await self._exchange(
            context,
            "POST",
            _record_path(base_id, self._configuration.table_id) + "/listRecords",
            _dump_json(body),
        )
        try:
            page = _decode_record_page(response, self._client.poll_page_size, "")
        except _DecodeError as error:
            raise RecordChangedPollError(str(error)) from error
        records = [
            _ChangedRecord(record, self._read_last_modified_time(record)) for record in page.records or ()
        ]
        return records, page.offset

    def _read_last_modified_time(self, record: Record) -> int:
        """Parse the watched field of a record the filter matched, so the field has a value."""
        fields = record.fields or {}
        if self._field_name not in fields:
            # The field was renamed since the schema was read; the next poll reads the schema again.
            self._field_name = ""
            raise RecordChangedPollError(
                "Airtable returned a changed record without the Last modified time field; "
                "the field name is read again"
            )
        value = fields[self._field_name]
        modified_at = parse_rfc3339_nanoseconds(value) if isinstance(value, str) else None
        if modified_at is None:
            raise RecordChangedPollError("Airtable returned a Last modified time value without a time of day")
        return modified_at

    async def _resolve_field_name(self, context: TriggerContext) -> None:
        """Read the base schema once, and again after a rename, to find the watched field's current name,
        since listed records key their fields by name."""
        if self._field_name:
            return
        base_id = self._configuration.base_id
        response = await self._exchange(
            context, "GET", "/v0/meta/bases/" + _escape(base_id) + "/tables", None
        )
        try:
            tables = _schema_tables(_decode_json(response))
        except _DecodeError as error:
            raise RecordChangedPollError(str(error)) from error
        for table in tables:
            if table["id"] != self._configuration.table_id:
                continue
            for field in table["fields"]:
                if field["id"] != self._configuration.field_id:
                    continue
                if field["type"] != _LAST_MODIFIED_TIME_FIELD_TYPE or field["name"] == "":
                    raise RecordChangedPollError(
                        "the Airtable recordChanged field is not a Last modified time field"
                    )
                if field["result_type"] is not None and field["result_type"] != "dateTime":
                    raise RecordChangedPollError(
                        "the Airtable recordChanged Last modified time field must include a time of day"
                    )
                self._field_name = field["name"]
                return
            raise RecordChangedPollError("the Airtable table has no field with the recordChanged field ID")
        raise RecordChangedPollError("the Airtable base schema has no table with the recordChanged table ID")

    async def _exchange(self, context: TriggerContext, method: str, path: str, body: bytes | None) -> bytes:
        """Wait on the runner's request limiter, then send one paced request through the Client."""
        await wait_for_poll_request(context)
        base_id = self._configuration.base_id
        result = await self._client.exchange(self._call, _TRIGGER_NAME, method, path, base_id, body, base_id)
        if not isinstance(result, _Outcome):
            return result
        if result.disposition is _Disposition.OFFSET_EXPIRED:
            raise _ListOffsetExpiredError("Airtable no longer recognizes the list offset")
        error = RecordChangedPollError(result.failure.message)
        if result.disposition is _Disposition.RETRY and result.failure.kind is FailureKind.RATE_LIMIT:
            # 429 and a full per-base pacing queue pause every binding of the connection.
            raise mark_rate_limited(error, timedelta(seconds=result.retry_after_seconds))
        raise error


def _schema_tables(document: Any) -> list[dict[str, Any]]:
    """Read the part of GET /v0/meta/bases/{baseId}/tables the fetcher uses, typed as Go decodes it."""
    tables: list[dict[str, Any]] = []
    for table in _members(document, "tables", list) or []:
        fields = []
        for field in _members(table, "fields", list) or []:
            options = _members(field, "options", dict) or {}
            result = _members(options, "result", dict)
            fields.append(
                {
                    "id": _members(field, "id", str) or "",
                    "name": _members(field, "name", str) or "",
                    "type": _members(field, "type", str) or "",
                    "result_type": None if result is None else (_members(result, "type", str) or ""),
                }
            )
        tables.append({"id": _members(table, "id", str) or "", "fields": fields})
    return tables


def _members(document: Any, name: str, expected: type) -> Any:
    if document is None:
        return None
    if not isinstance(document, dict):
        raise _DecodeError("Airtable returned a malformed response")
    value = document.get(name)
    if value is not None and type(value) is not expected:
        raise _DecodeError("Airtable returned a malformed response")
    return value
