# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

# ListSheetsInput selects one page of accessible sheets.
@dataclass(frozen=True, slots=True)
class ListSheetsInput:
    # Page is the 1-based page number; zero selects page one.
    page: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # PageSize is records per page; zero uses 100.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})


setattr(ListSheetsInput, '__module__', 'dex_connectors.smartsheet.models')


# SheetSummary is a sheet identity and display name.
@dataclass(frozen=True, slots=True)
class SheetSummary:
    # ID is the provider-assigned resource identity.
    id: int = 0
    # Name is the resource display name.
    name: str = ''


setattr(SheetSummary, '__module__', 'dex_connectors.smartsheet.models')


# SheetPage is one Smartsheet list page.
@dataclass(frozen=True, slots=True)
class SheetPage:
    # Sheets contains the accessible sheets on this page.
    sheets: tuple[SheetSummary, ...] | None = None
    # PageNumber is the provider-returned 1-based page number.
    page_number: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageNumber'})
    # TotalCount counts all accessible matching sheets.
    total_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalCount'})


setattr(SheetPage, '__module__', 'dex_connectors.smartsheet.models')


# GetSheetInput identifies one sheet.
@dataclass(frozen=True, slots=True)
class GetSheetInput:
    # SheetID is the positive numeric Smartsheet sheet ID.
    sheet_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'sheetId'})


setattr(GetSheetInput, '__module__', 'dex_connectors.smartsheet.models')


# SheetColumn is a column identity and title.
@dataclass(frozen=True, slots=True)
class SheetColumn:
    # ID is the provider-assigned resource identity.
    id: int = 0
    # Title is the column heading.
    title: str = ''


setattr(SheetColumn, '__module__', 'dex_connectors.smartsheet.models')


# SheetRow is a sheet row with its cell values.
@dataclass(frozen=True, slots=True)
class SheetRow:
    # ID is the provider-assigned resource identity.
    id: int = 0
    # Cells preserves each row cell as provider JSON.
    cells: tuple[Any, ...] | None = None


setattr(SheetRow, '__module__', 'dex_connectors.smartsheet.models')


# Sheet contains the requested sheet's metadata, columns, and rows.
@dataclass(frozen=True, slots=True)
class Sheet:
    # ID is the provider-assigned resource identity.
    id: int = 0
    # Name is the resource display name.
    name: str = ''
    # Columns describes the sheet columns.
    columns: tuple[SheetColumn, ...] | None = None
    # Rows contains the sheet rows returned by the API.
    rows: tuple[SheetRow, ...] | None = None


setattr(Sheet, '__module__', 'dex_connectors.smartsheet.models')
