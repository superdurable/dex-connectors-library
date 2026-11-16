# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# GetValuesInput contains the provider request fields for get values.
@dataclass(frozen=True, slots=True)
class GetValuesInput:
    # SpreadsheetID is the Google Sheets spreadsheet identifier.
    spreadsheet_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'spreadsheetId'})
    # Range is an A1 notation range.
    range: str = ''


setattr(GetValuesInput, '__module__', 'dex_connectors.google_spreadsheet.models')


# GetValuesOutput contains the provider response fields for get values.
@dataclass(frozen=True, slots=True)
class GetValuesOutput:
    # Range is an A1 notation range.
    range: str = ''
    # MajorDimension is the major dimension returned by Google Sheets.
    major_dimension: str = _dataclass_field(default='', metadata={'dex_json_name': 'majorDimension'})
    # Values is the values returned by Google Sheets.
    values: tuple[tuple[str, ...], ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'values', _replace_nil_slices(self.values, 2))


GetValuesOutput.__annotations__['values'] = tuple[tuple[str, ...] | None, ...] | None
GetValuesOutput.__dataclass_fields__['values'].type = tuple[tuple[str, ...] | None, ...] | None
setattr(GetValuesOutput, '__module__', 'dex_connectors.google_spreadsheet.models')


# FindRowInput contains the provider request fields for find row.
@dataclass(frozen=True, slots=True)
class FindRowInput:
    # SpreadsheetID is the Google Sheets spreadsheet identifier.
    spreadsheet_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'spreadsheetId'})
    # SheetName is the worksheet title.
    sheet_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'sheetName'})
    # KeyColumn specifies key column for find row input.
    key_column: str = _dataclass_field(default='', metadata={'dex_json_name': 'keyColumn'})
    # KeyValue specifies key value for find row input.
    key_value: str = _dataclass_field(default='', metadata={'dex_json_name': 'keyValue'})


setattr(FindRowInput, '__module__', 'dex_connectors.google_spreadsheet.models')


# FindRowOutput contains the provider response fields for find row.
@dataclass(frozen=True, slots=True)
class FindRowOutput:
    # RowNumber is the one-based sheet row number.
    row_number: int = _dataclass_field(default=0, metadata={'dex_json_name': 'rowNumber', 'dex_json_omit_default': True})
    # Values is the values returned by Google Sheets.
    values: dict[str, str] = _dataclass_field(default_factory=dict, metadata={'dex_json_omit_default': True})
    # ConflictingRows is the conflicting rows returned by Google Sheets.
    conflicting_rows: tuple[int, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'conflictingRows', 'dex_json_omit_default': True})


setattr(FindRowOutput, '__module__', 'dex_connectors.google_spreadsheet.models')


# UpsertRowInput contains the provider request fields for upsert row.
@dataclass(frozen=True, slots=True)
class UpsertRowInput:
    # SpreadsheetID is the Google Sheets spreadsheet identifier.
    spreadsheet_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'spreadsheetId'})
    # SheetName is the worksheet title.
    sheet_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'sheetName'})
    # KeyColumn specifies key column for upsert row input.
    key_column: str = _dataclass_field(default='', metadata={'dex_json_name': 'keyColumn'})
    # KeyValue specifies key value for upsert row input.
    key_value: str = _dataclass_field(default='', metadata={'dex_json_name': 'keyValue'})
    # Values specifies values for upsert row input.
    values: dict[str, str] | None = None


setattr(UpsertRowInput, '__module__', 'dex_connectors.google_spreadsheet.models')


# UpsertRowOutput contains the provider response fields for upsert row.
@dataclass(frozen=True, slots=True)
class UpsertRowOutput:
    # Action is the action returned by Google Sheets.
    action: str = ''
    # RowNumber is the one-based sheet row number.
    row_number: int = _dataclass_field(default=0, metadata={'dex_json_name': 'rowNumber'})
    # UpdatedRange is the updated range returned by Google Sheets.
    updated_range: str = _dataclass_field(default='', metadata={'dex_json_name': 'updatedRange'})


setattr(UpsertRowOutput, '__module__', 'dex_connectors.google_spreadsheet.models')


# RowAddedEvent is one row appended below the last processed row of the polled tab.
@dataclass(frozen=True, slots=True)
class RowAddedEvent:
    # SpreadsheetID is the polled spreadsheet ID.
    spreadsheet_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'spreadsheetId'})
    # SheetName is the polled tab title.
    sheet_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'sheetName'})
    # RowNumber is the 1-based sheet row number when the row was read; row 1 is the header.
    row_number: int = _dataclass_field(default=0, metadata={'dex_json_name': 'rowNumber'})
    # Values maps each non-empty, unique header cell to this row's formatted cell, or "" when the row has
    # no value in that column. Columns whose header is empty or repeated appear only in Cells.
    values: dict[str, str] | None = None
    # Cells are the row's formatted cell values in column order, without trailing empty cells.
    cells: tuple[str, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'cells', _replace_nil_slices(self.cells, 1))


RowAddedEvent.__annotations__['cells'] = tuple[str, ...] | None
RowAddedEvent.__dataclass_fields__['cells'].type = tuple[str, ...] | None
setattr(RowAddedEvent, '__module__', 'dex_connectors.google_spreadsheet.models')


# RowAddedTriggerConfiguration selects the spreadsheet tab a rowAdded binding polls. Dex Web saves it
# from the spreadsheetPicker and sheetTabPicker units.
@dataclass(frozen=True, slots=True)
class RowAddedTriggerConfiguration:
    # SpreadsheetID is the Google spreadsheet ID stored by the spreadsheetPicker unit. Required.
    spreadsheet_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'spreadsheetId'})
    # SheetName is the exact tab title stored by the sheetTabPicker unit, used verbatim, including any
    # leading or trailing spaces. Required. Row 1 of the tab is the header row whose cells name
    # RowAddedEvent.Values.
    sheet_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'sheetName'})


setattr(RowAddedTriggerConfiguration, '__module__', 'dex_connectors.google_spreadsheet.models')
