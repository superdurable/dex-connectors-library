# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""An in-process Google Sheets tab served through values:batchGet, and values.get for example read-backs.

It behaves as the Go connector's sheetsfake does.
"""

import json
import threading
from dataclasses import dataclass
from urllib.parse import unquote

import httpx

SHEETS_ENDPOINT = "https://sheets.test/v4"


@dataclass(frozen=True, slots=True)
class ScriptedResponse:
    """One failure answered instead of the next batchGet request."""

    status: int
    retry_after: str = ""


@dataclass(frozen=True, slots=True)
class RecordedRequest:
    """One recorded batchGet request."""

    spreadsheet_id: str
    ranges: tuple[str, ...]
    authorization: str


def _trim_trailing_empty_cells(cells: list[str]) -> list[str]:
    end = len(cells)
    while end > 0 and cells[end - 1] == "":
        end -= 1
    return list(cells[:end])


class SheetsFake:
    """A mutable tab whose rows[0] is the header row; it omits trailing empty rows and cells, as Sheets does.

    Serve it with `transport()`; it is safe to mutate from the test while a runner polls.
    """

    def __init__(self, tab: str, *rows: list[str]) -> None:
        self._lock = threading.Lock()
        self._tab = tab
        self._rows = [list(row) for row in rows]
        self._scripted: list[ScriptedResponse] = []
        self._requests: list[RecordedRequest] = []
        self._completed = 0
        self._deferred: dict[int, list[list[str]]] = {}
        self._reads: list[str] = []

    def transport(self) -> httpx.MockTransport:
        """Return an httpx transport that answers every request with this tab."""
        return httpx.MockTransport(self.handle)

    def append_rows(self, *rows: list[str]) -> None:
        """Add rows at the bottom of the tab."""
        with self._lock:
            self._rows.extend(list(row) for row in rows)

    def append_rows_after_requests(self, count: int, *rows: list[str]) -> None:
        """Append rows right after the count-th answered request, such as after a seed."""
        with self._lock:
            self._deferred.setdefault(count, []).extend(list(row) for row in rows)

    def delete_row(self, row_number: int) -> None:
        """Remove the 1-based row, shifting later rows up."""
        with self._lock:
            del self._rows[row_number - 1]

    def replace_row(self, row_number: int, cells: list[str]) -> None:
        """Replace the cells of the 1-based row."""
        with self._lock:
            self._rows[row_number - 1] = list(cells)

    def truncate_rows(self, count: int) -> None:
        """Keep only the first count rows."""
        with self._lock:
            del self._rows[count:]

    def script(self, *responses: ScriptedResponse) -> None:
        """Answer the next requests with responses, in order, before serving rows again."""
        with self._lock:
            self._scripted.extend(responses)

    def requests(self) -> list[RecordedRequest]:
        """Return every recorded request."""
        with self._lock:
            return list(self._requests)

    def read_ranges(self) -> list[str]:
        """Return the range of every values.get request in order.

        Those reads are not batchGet requests: they are not counted, recorded by `requests`, or answered
        by `script`.
        """
        with self._lock:
            return list(self._reads)

    def completed_requests(self) -> int:
        """Count the batchGet requests answered so far."""
        with self._lock:
            return self._completed

    def handle(self, request: httpx.Request) -> httpx.Response:
        """Answer one request."""
        with self._lock:
            read = self._answer_values_get(request)
            if read is not None:
                return read
            try:
                return self._answer(request)
            finally:
                self._completed += 1
                self._rows.extend(self._deferred.pop(self._completed, []))

    def _answer(self, request: httpx.Request) -> httpx.Response:
        path = request.url.raw_path.decode().split("?", 1)[0]
        prefix, suffix = "/v4/spreadsheets/", "/values:batchGet"
        if request.method != "GET" or not path.startswith(prefix) or not path.endswith(suffix):
            return httpx.Response(404)
        spreadsheet_id = unquote(path[len(prefix) : -len(suffix)])
        ranges = tuple(value for key, value in request.url.params.multi_items() if key == "ranges")
        self._requests.append(
            RecordedRequest(spreadsheet_id, ranges, request.headers.get("Authorization", ""))
        )
        if self._scripted:
            scripted = self._scripted.pop(0)
            headers = {"Retry-After": scripted.retry_after} if scripted.retry_after else {}
            return httpx.Response(scripted.status, headers=headers, json={"error": {"code": scripted.status}})
        value_ranges: list[dict[str, object]] = []
        for value_range in ranges:
            bounds = self._row_bounds(value_range)
            if bounds is None:
                return httpx.Response(400, json={"error": {"code": 400}})
            served: dict[str, object] = {"majorDimension": "ROWS", "range": value_range}
            rows = self._rows_in(*bounds)
            if rows:
                served["values"] = rows
            value_ranges.append(served)
        body = json.dumps(
            {"spreadsheetId": spreadsheet_id, "valueRanges": value_ranges},
            sort_keys=True,
            separators=(",", ":"),
        )
        return httpx.Response(
            200, headers={"Content-Type": "application/json"}, content=body.encode() + b"\n"
        )

    def _answer_values_get(self, request: httpx.Request) -> httpx.Response | None:
        """Answer GET /v4/spreadsheets/{id}/values/{range}, or return None for any other request."""
        path = request.url.raw_path.decode().split("?", 1)[0]
        prefix = "/v4/spreadsheets/"
        if request.method != "GET" or not path.startswith(prefix) or "/values/" not in path:
            return None
        _, _, encoded_range = path[len(prefix) :].partition("/values/")
        value_range = unquote(encoded_range)
        self._reads.append(value_range)
        bounds = self._row_bounds(value_range)
        if bounds is None:
            return httpx.Response(400, json={"error": {"code": 400}})
        served: dict[str, object] = {"majorDimension": "ROWS", "range": value_range}
        rows = self._rows_in(*bounds)
        if rows:
            served["values"] = rows
        return httpx.Response(200, json=served)

    def _row_bounds(self, value_range: str) -> tuple[int, int] | None:
        quoted_tab = "'" + self._tab.replace("'", "''") + "'!"
        if not value_range.startswith(quoted_tab):
            return None
        first, separator, last = value_range[len(quoted_tab) :].partition(":")
        if not separator or not first.isdigit() or not last.isdigit():
            return None
        first_row, last_row = int(first), int(last)
        return (first_row, last_row) if 1 <= first_row <= last_row else None

    def _rows_in(self, first_row: int, last_row: int) -> list[list[str]]:
        end = min(last_row, len(self._rows))
        while end >= first_row and not _trim_trailing_empty_cells(self._rows[end - 1]):
            end -= 1
        return [_trim_trailing_empty_cells(self._rows[number - 1]) for number in range(first_row, end + 1)]
