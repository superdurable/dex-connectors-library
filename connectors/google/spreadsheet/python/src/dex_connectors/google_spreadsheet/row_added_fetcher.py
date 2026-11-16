# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The rowAdded change fetcher: values:batchGet polls of one tab, at behavior parity with the Go connector."""

from dataclasses import dataclass, field
from datetime import UTC, timedelta
from typing import Any, Final

from dex_connectors.sdk import TriggerBindingRef, TriggerContext, TriggerEvent, TriggerLogger, TriggerLogLevel
from dex_connectors.sdk.pollingtrigger import (
    ChangePage,
    Cursor,
    mark_cursor_invalid,
    mark_rate_limited,
    new_trigger_credential_call,
    wait_for_poll_request,
)
from dex_connectors.sdk.trigger_delivery import bind_trigger_log_attributes, log_trigger_record

from ._generated.definitions import CONNECTOR_ID
from .client import Client, ProviderResponse, RequestError
from .json_values import strict_json_loads
from .models import RowAddedEvent, RowAddedTriggerConfiguration
from .operations import _convert_cell, _retry_delay, _sheet_range
from .row_added_cursor import (
    GO_SPACE,
    RowAddedCursorToken,
    decode_row_added_cursor_token,
    hash_row_cells,
    header_keyed_values,
    row_added_event_id,
    without_trailing_empty_cells,
)

_MAXIMUM_SCAN_WINDOWS: Final = 100
_MINIMUM_SCAN_WINDOW: Final = 100
Rows = list[list[str]]


class _ResponseTooLargeError(RuntimeError):
    """A poll response exceeded maxResponseBytes; a scan halves its window and reads it again."""


@dataclass(slots=True)
class _TabScan:
    last_row: int = 1
    last_row_cells: list[str] = field(default_factory=list)
    matching_rows: list[int] = field(default_factory=list)
    cursor_row_cells: list[str] = field(default_factory=list)


def validate_row_added_trigger_configuration(configuration: RowAddedTriggerConfiguration) -> None:
    """Check that the binding names a spreadsheet and a tab; raise ValueError otherwise.

    Whitespace-only values count as blank. The tab title itself is later used verbatim.
    """
    if (
        configuration is None
        or not configuration.spreadsheet_id.strip(GO_SPACE)
        or not configuration.sheet_name.strip(GO_SPACE)
    ):
        raise ValueError("Google Sheets rowAdded spreadsheetId and sheetName are required")


class RowAddedFetcher:
    """Reads one tab's rows after the cursor row; a poll kit `ChangeFetcher` of `RowAddedEvent`."""

    def __init__(
        self,
        client: Client,
        binding: TriggerBindingRef,
        configuration: RowAddedTriggerConfiguration,
        logger: TriggerLogger | None = None,
    ) -> None:
        validate_row_added_trigger_configuration(configuration)
        self._client = client
        self._call = new_trigger_credential_call(binding.connection, binding.trigger)
        self._spreadsheet_id = configuration.spreadsheet_id.strip(GO_SPACE)
        self._sheet_name = configuration.sheet_name
        self._page_size = client.config.poll_page_size
        self._logger = bind_trigger_log_attributes(
            logger,
            {
                "connector": CONNECTOR_ID,
                "connection": binding.connection.name,
                "trigger": binding.trigger.trigger_name,
                "binding": binding.name,
            },
        )

    async def seed_cursor(self, context: TriggerContext) -> Cursor:
        """Position the cursor after the tab's current last row, so existing rows are not delivered."""
        scan = await self._scan_tab(context, None)
        return Cursor(RowAddedCursorToken(scan.last_row, hash_row_cells(scan.last_row_cells)).encode())

    async def fetch_changes(
        self, context: TriggerContext, cursor: Cursor
    ) -> ChangePage[TriggerEvent[RowAddedEvent]]:
        """Read the header row and up to pollPageSize rows after the cursor row, realigning a moved cursor."""
        try:
            token = decode_row_added_cursor_token(cursor.token)
        except ValueError as error:
            raise mark_cursor_invalid(error) from None
        header, window = await self._read_window(context, token.last_row)
        if not _window_starts_at_cursor_row(window, token):
            token = await self._realign_cursor(context, token)
            header, window = await self._read_window(context, token.last_row)
            if not _window_starts_at_cursor_row(window, token):
                raise RuntimeError("Google Sheets rows moved during the rowAdded poll")
        occurred_at = self._client.now().astimezone(UTC)
        changes: list[TriggerEvent[RowAddedEvent]] = []
        for offset in range(1, len(window)):
            cells = without_trailing_empty_cells(window[offset])
            if not cells:
                continue
            row_number = token.last_row + offset
            event_id = row_added_event_id(self._spreadsheet_id, self._sheet_name, row_number, cells)
            payload = RowAddedEvent(
                spreadsheet_id=self._spreadsheet_id,
                sheet_name=self._sheet_name,
                row_number=row_number,
                values=header_keyed_values(header, cells),
                cells=cells,
            )
            changes.append(TriggerEvent(event_id, occurred_at, payload))
        if len(window) > 1:
            token = RowAddedCursorToken(token.last_row + len(window) - 1, hash_row_cells(window[-1]))
        return ChangePage(changes, Cursor(token.encode()), len(window) == self._page_size + 1)

    async def _realign_cursor(
        self, context: TriggerContext, token: RowAddedCursorToken
    ) -> RowAddedCursorToken:
        """Find the cursor row's cells nearest its old number after rows above it moved.

        Without a match the row was edited in place and keeps its number; when the tab now ends above it,
        the cursor is invalid, so the poller reseeds and logs the gap.
        """
        scan = await self._scan_tab(context, token)
        nearest = 0
        for row_number in scan.matching_rows:
            if nearest == 0 or abs(row_number - token.last_row) < abs(nearest - token.last_row):
                nearest = row_number
        if nearest != 0:
            self._log(
                context,
                TriggerLogLevel.INFO,
                "sheet rows moved; cursor realigned",
                {"from_row": token.last_row, "to_row": nearest},
            )
            return RowAddedCursorToken(nearest, token.row_hash)
        if token.last_row <= scan.last_row:
            self._log(
                context,
                TriggerLogLevel.INFO,
                "last processed row was edited; keeping its position",
                {"row": token.last_row},
            )
            return RowAddedCursorToken(token.last_row, hash_row_cells(scan.cursor_row_cells))
        raise mark_cursor_invalid(
            RuntimeError(
                f"Google Sheets tab now ends at row {scan.last_row}, above the last processed row "
                f"{token.last_row}"
            )
        )

    async def _scan_tab(self, context: TriggerContext, cursor: RowAddedCursorToken | None) -> _TabScan:
        """Read the tab from row 1 in maxRows windows until one ends early, halving oversized windows."""
        scan = _TabScan()
        row_hash, cursor_row = (cursor.row_hash, cursor.last_row) if cursor is not None else ("", 0)
        window_size = self._client.config.max_rows
        minimum_window_size = min(window_size, _MINIMUM_SCAN_WINDOW)
        maximum_scan_rows = _MAXIMUM_SCAN_WINDOWS * window_size
        first_row = 1
        while first_row <= maximum_scan_rows:
            try:
                ranges = await self._read_ranges(
                    context, self._row_range(first_row, first_row + window_size - 1)
                )
            except _ResponseTooLargeError:
                if window_size <= minimum_window_size:
                    raise
                window_size = max(window_size // 2, minimum_window_size)
                self._log(
                    context,
                    TriggerLogLevel.DEBUG,
                    "sheet scan window exceeds maxResponseBytes; halving it",
                    {"window_rows": window_size},
                )
                continue
            rows = ranges[0]
            for offset, row in enumerate(rows):
                row_number = first_row + offset
                if row_hash and row_number > 1 and hash_row_cells(row) == row_hash:
                    scan.matching_rows.append(row_number)
                if row_number == cursor_row:
                    scan.cursor_row_cells = row
            if rows:
                scan.last_row, scan.last_row_cells = first_row + len(rows) - 1, rows[-1]
            if len(rows) < window_size:
                return scan
            first_row += window_size
        raise RuntimeError(f"Google Sheets tab has more than {maximum_scan_rows} rows to scan")

    async def _read_window(self, context: TriggerContext, last_row: int) -> tuple[list[str], Rows]:
        """Return the header row and the rows from last_row through last_row+pollPageSize."""
        ranges = await self._read_ranges(
            context, self._row_range(1, 1), self._row_range(last_row, last_row + self._page_size)
        )
        return (ranges[0][0] if ranges[0] else []), ranges[1]

    async def _read_ranges(self, context: TriggerContext, *ranges: str) -> list[Rows]:
        """Make one values:batchGet request for whole-row ranges and return each range's rows."""
        await wait_for_poll_request(context)
        credential, failure, _ = await self._client.resolve(self._call, "rowAdded")
        if failure is not None or credential is None:
            message = failure.message if failure is not None else "credentials are unavailable"
            raise RuntimeError(f"Google Sheets rowAdded poll {message}")
        query = {"ranges": list(ranges), "majorDimension": "ROWS", "valueRenderOption": "FORMATTED_VALUE"}
        try:
            response, _ = await self._client.request(
                self._call, credential, "GET", self._spreadsheet_id, "/values:batchGet", query
            )
        except RequestError as error:
            raise RuntimeError(f"Google Sheets rowAdded poll {error}") from None
        if response.too_large:
            raise _ResponseTooLargeError(
                "Google Sheets rowAdded poll provider response exceeds configured limit"
            )
        if response.status == 429:
            # An unreadable Retry-After falls back to the runner's exponential backoff.
            seconds, is_readable = _retry_delay(response)
            raise mark_rate_limited(
                RuntimeError("Google Sheets rate limited the rowAdded poll with HTTP 429"),
                timedelta(seconds=seconds if is_readable else 0),
            )
        if not 200 <= response.status < 300:
            raise RuntimeError(f"Google Sheets rejected the rowAdded poll with HTTP {response.status}")
        return self._decode_value_ranges(response, len(ranges))

    def _decode_value_ranges(self, response: ProviderResponse, range_count: int) -> list[Rows]:
        maximum_rows = max(self._client.config.max_rows, self._page_size + 1)
        try:
            value_ranges = _value_ranges(strict_json_loads(response.body))
        except (ValueError, TypeError, UnicodeDecodeError, RecursionError):
            value_ranges = None
        if value_ranges is None or len(value_ranges) != range_count:
            raise RuntimeError("Google Sheets returned an invalid rowAdded poll response")
        if any(len(rows) > maximum_rows for rows in value_ranges):
            raise RuntimeError("Google Sheets returned more rows than the rowAdded poll requested")
        return value_ranges

    def _row_range(self, first_row: int, last_row: int) -> str:
        """Return the A1 whole-row range of the polled tab, such as 'Leads'!5:105."""
        return f"{_sheet_range(self._sheet_name)}!{first_row}:{last_row}"

    def _log(
        self, context: TriggerContext, level: TriggerLogLevel, message: str, attributes: dict[str, int]
    ) -> None:
        """Write one record with the binding identity; never cell values or the tab title."""
        log_trigger_record(self._logger, context, level, message, dict(attributes))


def _value_ranges(document: Any) -> list[Rows] | None:
    """Decode `valueRanges` as Go's json.Unmarshal into [][]any does; None for an invalid shape."""
    if not isinstance(document, dict):
        return None
    listed = document.get("valueRanges")
    if listed is None:
        return []
    if not isinstance(listed, list):
        return None
    decoded: list[Rows] = []
    for value_range in listed:
        if value_range is None:
            decoded.append([])
            continue
        if not isinstance(value_range, dict):
            return None
        if any(
            value_range.get(name) is not None and not isinstance(value_range.get(name), str)
            for name in ("range", "majorDimension")
        ):
            return None
        rows = value_range.get("values")
        if rows is None:
            decoded.append([])
            continue
        if not isinstance(rows, list) or any(row is not None and not isinstance(row, list) for row in rows):
            return None
        decoded.append([[_convert_cell(cell) for cell in (row or [])] for row in rows])
    return decoded


def _window_starts_at_cursor_row(window: Rows, token: RowAddedCursorToken) -> bool:
    """Report whether the window's first row still holds the cursor row's cells; row 1 never moves."""
    if token.last_row == 1:
        return True
    return hash_row_cells(window[0] if window else []) == token.row_hash
