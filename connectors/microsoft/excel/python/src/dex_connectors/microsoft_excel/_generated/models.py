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


# GetTableRowsInput identifies one Excel table.
@dataclass(frozen=True, slots=True)
class GetTableRowsInput:
    # DriveID is the Microsoft Graph drive ID of the OneDrive or SharePoint
    # document library that holds the workbook, as the workbookPicker unit stores it.
    drive_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'driveId'})
    # WorkbookID is the workbook's drive item ID, as the workbookPicker unit stores it.
    workbook_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'workbookId'})
    # Table is the table's ID, as the tablePicker unit stores it, or its name,
    # such as ApprovalPolicy. An ID keeps working after the table is renamed.
    table: str = ''


setattr(GetTableRowsInput, '__module__', 'dex_connectors.microsoft_excel.models')


# TableRow is one data row of an Excel table.
@dataclass(frozen=True, slots=True)
class TableRow:
    # Index is the row's zero-based position among the table's data rows.
    index: int = 0
    # Values maps every column name to the row's cell value in that column.
    values: dict[str, Any] | None = None


setattr(TableRow, '__module__', 'dex_connectors.microsoft_excel.models')


# GetTableRowsOutput is every data row of one table.
@dataclass(frozen=True, slots=True)
class GetTableRowsOutput:
    # Columns lists the table's column names in table order.
    columns: tuple[str, ...] = ()
    # Rows lists the table's data rows in table order, without the header and
    # total rows. A table always has at least one data row, which is empty in a
    # new table.
    rows: tuple[TableRow, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'columns', _replace_nil_slices(self.columns, 1))
        object.__setattr__(self, 'rows', _replace_nil_slices(self.rows, 1))


GetTableRowsOutput.__annotations__['columns'] = tuple[str, ...] | None
GetTableRowsOutput.__dataclass_fields__['columns'].type = tuple[str, ...] | None
GetTableRowsOutput.__annotations__['rows'] = tuple[TableRow, ...] | None
GetTableRowsOutput.__dataclass_fields__['rows'].type = tuple[TableRow, ...] | None
setattr(GetTableRowsOutput, '__module__', 'dex_connectors.microsoft_excel.models')


# GetValuesInput identifies one bounded range of one worksheet.
@dataclass(frozen=True, slots=True)
class GetValuesInput:
    # DriveID is the Microsoft Graph drive ID of the OneDrive or SharePoint
    # document library that holds the workbook, as the workbookPicker unit stores it.
    drive_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'driveId'})
    # WorkbookID is the workbook's drive item ID, as the workbookPicker unit stores it.
    workbook_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'workbookId'})
    # Worksheet is the worksheet's ID in braces, as the worksheetPicker unit
    # stores it, or its name, such as Summary. An ID keeps working after a rename.
    worksheet: str = ''
    # Address is one cell or rectangular range in A1 form, such as A2:E2,
    # without a worksheet name, $ markers, or whole columns or rows. It may
    # cover at most the configured maxCells.
    address: str = ''


setattr(GetValuesInput, '__module__', 'dex_connectors.microsoft_excel.models')


# GetValuesOutput holds one range's cells, row by row.
@dataclass(frozen=True, slots=True)
class GetValuesOutput:
    # Address is the range as Excel reports it, qualified by its worksheet name, such as Summary!A2:E2.
    address: str = ''
    # RowCount is the number of rows read.
    row_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'rowCount'})
    # ColumnCount is the number of columns read.
    column_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'columnCount'})
    # Values holds every cell's raw value, row by row: text, a number, a
    # Boolean, or empty text for an empty cell. A date is its serial number.
    values: tuple[tuple[Any, ...], ...] = ()
    # Text holds every cell's text as Excel displays it with its number
    # format, such as 10/1/2026 for a date, row by row.
    text: tuple[tuple[str, ...], ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'values', _replace_nil_slices(self.values, 2))
        object.__setattr__(self, 'text', _replace_nil_slices(self.text, 2))


GetValuesOutput.__annotations__['values'] = tuple[tuple[Any, ...] | None, ...] | None
GetValuesOutput.__dataclass_fields__['values'].type = tuple[tuple[Any, ...] | None, ...] | None
GetValuesOutput.__annotations__['text'] = tuple[tuple[str, ...] | None, ...] | None
GetValuesOutput.__dataclass_fields__['text'].type = tuple[tuple[str, ...] | None, ...] | None
setattr(GetValuesOutput, '__module__', 'dex_connectors.microsoft_excel.models')


# UpdateValuesInput is the literal content of one bounded range.
@dataclass(frozen=True, slots=True)
class UpdateValuesInput:
    # DriveID is the Microsoft Graph drive ID of the OneDrive or SharePoint
    # document library that holds the workbook, as the workbookPicker unit stores it.
    drive_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'driveId'})
    # WorkbookID is the workbook's drive item ID, as the workbookPicker unit stores it.
    workbook_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'workbookId'})
    # Worksheet is the worksheet's ID in braces, as the worksheetPicker unit
    # stores it, or its name, such as Summary. An ID keeps working after a rename.
    worksheet: str = ''
    # Address is one cell or rectangular range in A1 form, such as A2:E2,
    # without a worksheet name, $ markers, or whole columns or rows. It may
    # cover at most the configured maxCells.
    address: str = ''
    # Values holds every cell of the range, row by row, with exactly the
    # address's number of rows and columns. Every value must be present; use
    # EmptyCellValue to clear a cell.
    values: tuple[tuple[Any, ...], ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'values', _replace_nil_slices(self.values, 2))


UpdateValuesInput.__annotations__['values'] = tuple[tuple[Any, ...] | None, ...] | None
UpdateValuesInput.__dataclass_fields__['values'].type = tuple[tuple[Any, ...] | None, ...] | None
setattr(UpdateValuesInput, '__module__', 'dex_connectors.microsoft_excel.models')


# UpdateValuesOutput identifies the written range.
@dataclass(frozen=True, slots=True)
class UpdateValuesOutput:
    # Address is the range as Excel reports it, such as Summary!A2:E2, or the
    # requested address when Excel's response could not be read.
    address: str = ''
    # RowCount is the number of rows written.
    row_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'rowCount'})
    # ColumnCount is the number of columns written.
    column_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'columnCount'})


setattr(UpdateValuesOutput, '__module__', 'dex_connectors.microsoft_excel.models')


# AppendTableRowsInput is a batch of rows for one Excel table.
@dataclass(frozen=True, slots=True)
class AppendTableRowsInput:
    # DriveID is the Microsoft Graph drive ID of the OneDrive or SharePoint
    # document library that holds the workbook, as the workbookPicker unit stores it.
    drive_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'driveId'})
    # WorkbookID is the workbook's drive item ID, as the workbookPicker unit stores it.
    workbook_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'workbookId'})
    # Table is the table's ID, as the tablePicker unit stores it, or its name,
    # such as Decisions. An ID keeps working after the table is renamed.
    table: str = ''
    # KeyColumn is the exact name of the table column whose value identifies a
    # row, such as RequestId. A row whose key the column already holds is not
    # appended again, whichever Flow or person added it.
    key_column: str = _dataclass_field(default='', metadata={'dex_json_name': 'keyColumn'})
    # Rows lists the rows to append, in order, each mapping column names to
    # values. Every row sets KeyColumn to non-empty text, a number, or a Boolean
    # that no other row uses; a column a row leaves out is written empty. The
    # rows times the table's columns may cover at most the configured maxCells.
    rows: tuple[dict[str, Any] | None, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'rows', _replace_nil_slices(self.rows, 1))


AppendTableRowsInput.__annotations__['rows'] = tuple[dict[str, Any] | None, ...] | None
AppendTableRowsInput.__dataclass_fields__['rows'].type = tuple[dict[str, Any] | None, ...] | None
setattr(AppendTableRowsInput, '__module__', 'dex_connectors.microsoft_excel.models')


# AppendTableRowsOutput reports which requested rows the table now holds and how.
@dataclass(frozen=True, slots=True)
class AppendTableRowsOutput:
    # AppendedKeys lists, in input order, the keys of the rows this attempt appended.
    appended_keys: tuple[Any, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'appendedKeys'})
    # AlreadyPresentKeys lists, in input order, the keys the key column already
    # held, whose rows were therefore not appended again.
    already_present_keys: tuple[Any, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'alreadyPresentKeys'})
    # IsFromEarlierAttempt reports that an earlier attempt of the same Step
    # execution sent the append and this attempt found every key in the table.
    is_from_earlier_attempt: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isFromEarlierAttempt', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'appended_keys', _replace_nil_slices(self.appended_keys, 1))
        object.__setattr__(self, 'already_present_keys', _replace_nil_slices(self.already_present_keys, 1))


AppendTableRowsOutput.__annotations__['appended_keys'] = tuple[Any, ...] | None
AppendTableRowsOutput.__dataclass_fields__['appended_keys'].type = tuple[Any, ...] | None
AppendTableRowsOutput.__annotations__['already_present_keys'] = tuple[Any, ...] | None
AppendTableRowsOutput.__dataclass_fields__['already_present_keys'].type = tuple[Any, ...] | None
setattr(AppendTableRowsOutput, '__module__', 'dex_connectors.microsoft_excel.models')
