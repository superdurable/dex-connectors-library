# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated Python dataclasses from Go operation types."""

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

@dataclass(frozen=True, slots=True)
class ListSheetsInput:
    """Typed ListSheetsInput value from the Go connector contract."""

    page: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})


setattr(ListSheetsInput, '__module__', 'dex_connectors.smartsheet.models')


@dataclass(frozen=True, slots=True)
class SheetSummary:
    """Typed SheetSummary value from the Go connector contract."""

    id: int = 0
    name: str = ''


setattr(SheetSummary, '__module__', 'dex_connectors.smartsheet.models')


@dataclass(frozen=True, slots=True)
class SheetPage:
    """Typed SheetPage value from the Go connector contract."""

    sheets: tuple[SheetSummary, ...] | None = None
    page_number: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageNumber'})
    total_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalCount'})


setattr(SheetPage, '__module__', 'dex_connectors.smartsheet.models')


@dataclass(frozen=True, slots=True)
class GetSheetInput:
    """Typed GetSheetInput value from the Go connector contract."""

    sheet_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'sheetId'})


setattr(GetSheetInput, '__module__', 'dex_connectors.smartsheet.models')


@dataclass(frozen=True, slots=True)
class SheetColumn:
    """Typed SheetColumn value from the Go connector contract."""

    id: int = 0
    title: str = ''


setattr(SheetColumn, '__module__', 'dex_connectors.smartsheet.models')


@dataclass(frozen=True, slots=True)
class SheetRow:
    """Typed SheetRow value from the Go connector contract."""

    id: int = 0
    cells: tuple[Any, ...] | None = None


setattr(SheetRow, '__module__', 'dex_connectors.smartsheet.models')


@dataclass(frozen=True, slots=True)
class Sheet:
    """Typed Sheet value from the Go connector contract."""

    id: int = 0
    name: str = ''
    columns: tuple[SheetColumn, ...] | None = None
    rows: tuple[SheetRow, ...] | None = None


setattr(Sheet, '__module__', 'dex_connectors.smartsheet.models')
