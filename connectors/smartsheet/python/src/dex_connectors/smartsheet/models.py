# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed Smartsheet sheet pages and sheets."""

from dataclasses import dataclass, field
from typing import Any


@dataclass(frozen=True, slots=True)
class ListSheetsInput:
    """Page of sheets; page zero means 1 and page_size zero means 100, at most 1000."""

    page: int = field(default=0, metadata={"dex_json_omit_default": True})
    page_size: int = field(default=0, metadata={"dex_json_name": "pageSize", "dex_json_omit_default": True})


@dataclass(frozen=True, slots=True)
class SheetSummary:
    """Sheet identity and name from a sheet page."""

    id: int = 0
    name: str = ""


@dataclass(frozen=True, slots=True)
class SheetPage:
    """One page of sheets with Smartsheet's page number and total count."""

    sheets: tuple[SheetSummary, ...] = ()
    page_number: int = field(default=0, metadata={"dex_json_name": "pageNumber"})
    total_count: int = field(default=0, metadata={"dex_json_name": "totalCount"})


@dataclass(frozen=True, slots=True)
class GetSheetInput:
    """Positive Smartsheet sheet ID."""

    sheet_id: int = field(default=0, metadata={"dex_json_name": "sheetId"})


@dataclass(frozen=True, slots=True)
class SheetColumn:
    """Column identity and title."""

    id: int = 0
    title: str = ""


@dataclass(frozen=True, slots=True)
class SheetRow:
    """Row identity and its provider cell objects as untouched JSON."""

    id: int = 0
    cells: tuple[Any, ...] = ()


@dataclass(frozen=True, slots=True)
class Sheet:
    """Sheet with its columns and rows."""

    id: int = 0
    name: str = ""
    columns: tuple[SheetColumn, ...] = ()
    rows: tuple[SheetRow, ...] = ()
