# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from dataclasses import dataclass, field
from typing import Any

from ._generated.models import (
    GetSheetInput as GetSheetInput,
)
from ._generated.models import (
    ListSheetsInput as ListSheetsInput,
)
from ._generated.models import (
    SheetColumn as SheetColumn,
)
from ._generated.models import (
    SheetSummary as SheetSummary,
)


@dataclass(frozen=True, slots=True)
class SheetPage:
    """One page of sheets with Smartsheet's page number and total count."""

    sheets: tuple[SheetSummary, ...] = ()
    page_number: int = field(default=0, metadata={"dex_json_name": "pageNumber"})
    total_count: int = field(default=0, metadata={"dex_json_name": "totalCount"})


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
