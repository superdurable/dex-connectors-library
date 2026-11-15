# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The rowAdded cursor token, row hashes, and event IDs, byte for byte as the Go connector writes them."""

import hashlib
import json
from collections.abc import Sequence
from dataclasses import dataclass
from typing import Any, Final

from dex_connectors.sdk.projectconfig import encode_go_json_value

ROW_ADDED_CURSOR_VERSION: Final = 1
_SHA256_HEX_LENGTH: Final = 64
_HEX_DIGITS: Final = frozenset("0123456789abcdefABCDEF")
_INT64_MINIMUM: Final = -(2**63)
_INT64_MAXIMUM: Final = 2**63 - 1
_CURSOR_FIELD_NAMES: Final = {"v": "v", "lastrow": "lastRow", "rowhash": "rowHash"}
# Go's unicode.IsSpace set; str.strip also trims U+001C-U+001F.
GO_SPACE: Final = "\t\n\v\f\r \x85\xa0                　"


@dataclass(frozen=True, slots=True)
class RowAddedCursorToken:
    """The stored poll position: the last processed row number and a SHA-256 of its cells."""

    last_row: int
    row_hash: str
    version: int = ROW_ADDED_CURSOR_VERSION

    def encode(self) -> str:
        """Return Go's json.Marshal text, `{"v":1,"lastRow":N,"rowHash":"<hex>"}`."""
        return (
            '{"v":'
            + str(self.version)
            + ',"lastRow":'
            + str(self.last_row)
            + ',"rowHash":'
            + encode_go_json_value(self.row_hash).decode("utf-8")
            + "}"
        )


class _CursorFields:
    def __init__(self, values: dict[str, Any]) -> None:
        self.values = values


def _cursor_pairs(pairs: list[tuple[str, Any]]) -> _CursorFields:
    values: dict[str, Any] = {}
    for key, value in pairs:
        name = _CURSOR_FIELD_NAMES.get(key.lower())
        if name is None:
            raise ValueError("unknown cursor field")
        if value is None:
            continue
        if name == "rowHash":
            if not isinstance(value, str):
                raise ValueError("cursor rowHash must be a string")
        elif (
            isinstance(value, bool)
            or not isinstance(value, int)
            or not _INT64_MINIMUM <= value <= _INT64_MAXIMUM
        ):
            raise ValueError("cursor field must be an integer")
        values[name] = value
    return _CursorFields(values)


def _reject_constant(value: str) -> None:
    raise ValueError("invalid JSON number")


def _reject_float(value: str) -> None:
    raise ValueError("cursor integers have no fraction or exponent")


def decode_row_added_cursor_token(encoded: str) -> RowAddedCursorToken:
    """Decode a stored token as Go's json.Decoder with DisallowUnknownFields does.

    Field names match case-insensitively, a later duplicate wins, null leaves a field zero, and text after
    the first JSON value is ignored. Raises ValueError for a token Go refuses or one of another shape.
    """
    try:
        document, _ = json.JSONDecoder(
            object_pairs_hook=_cursor_pairs, parse_constant=_reject_constant, parse_float=_reject_float
        ).raw_decode(encoded.lstrip(" \t\r\n"))
    except (ValueError, TypeError, RecursionError) as error:
        raise ValueError("Google Sheets rowAdded cursor is not valid JSON") from error
    if document is not None and not isinstance(document, _CursorFields):
        raise ValueError("Google Sheets rowAdded cursor is not valid JSON")
    fields = document.values if isinstance(document, _CursorFields) else {}
    version, last_row, row_hash = fields.get("v", 0), fields.get("lastRow", 0), fields.get("rowHash", "")
    if version != ROW_ADDED_CURSOR_VERSION or last_row < 1 or len(row_hash) != _SHA256_HEX_LENGTH:
        raise ValueError("Google Sheets rowAdded cursor has an unsupported shape")
    if not set(row_hash) <= _HEX_DIGITS:
        raise ValueError("Google Sheets rowAdded cursor hash is not hexadecimal")
    return RowAddedCursorToken(last_row, row_hash, version)


def without_trailing_empty_cells(cells: Sequence[str]) -> tuple[str, ...]:
    """Return cells without trailing empty strings, as Sheets omits them."""
    end = len(cells)
    while end > 0 and cells[end - 1] == "":
        end -= 1
    return tuple(cells[:end])


def hash_row_cells(cells: Sequence[str] | None) -> str:
    """Return the hex SHA-256 of Go's JSON encoding of cells without trailing empty cells."""
    encoded = encode_go_json_value(list(without_trailing_empty_cells(cells or ())))
    return hashlib.sha256(encoded).hexdigest()


def go_utf8(text: str) -> bytes:
    """Encode text as Go holds a JSON-decoded string: an unpaired surrogate becomes U+FFFD."""
    return text.encode("utf-16", "surrogatepass").decode("utf-16", "replace").encode("utf-8")


def row_added_event_id(spreadsheet_id: str, sheet_name: str, row_number: int, cells: Sequence[str]) -> str:
    """Return `row-<row number>-<16 hex>`, a pure function of the tab, row number, and cells."""
    digest = hashlib.sha256(
        b"\x00".join(
            (
                go_utf8(spreadsheet_id),
                go_utf8(sheet_name),
                str(row_number).encode(),
                encode_go_json_value(list(cells)),
            )
        )
    ).hexdigest()
    return f"row-{row_number}-{digest[:16]}"


def header_keyed_values(header: Sequence[str], cells: Sequence[str]) -> dict[str, str]:
    """Map each non-empty, unique header to its cell, or "" when the row is shorter."""
    counts: dict[str, int] = {}
    for name in header:
        counts[name] = counts.get(name, 0) + 1
    return {
        name: cells[column] if column < len(cells) else ""
        for column, name in enumerate(header)
        if name and counts[name] == 1
    }
