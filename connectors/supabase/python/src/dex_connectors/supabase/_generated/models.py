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


# UpsertRowsInput replaces rows on a database-enforced unique conflict key.
@dataclass(frozen=True, slots=True)
class UpsertRowsInput:
    # Table is an exposed table name.
    table: str = ''
    # OnConflict is a comma-separated list of unique columns; the database must enforce uniqueness.
    on_conflict: str = _dataclass_field(default='', metadata={'dex_json_name': 'onConflict'})
    # Rows contain complete replacement values for all fields the caller owns.
    rows: tuple[dict[str, Any] | None, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'rows', _replace_nil_slices(self.rows, 1))


UpsertRowsInput.__annotations__['rows'] = tuple[dict[str, Any] | None, ...] | None
UpsertRowsInput.__dataclass_fields__['rows'].type = tuple[dict[str, Any] | None, ...] | None
setattr(UpsertRowsInput, '__module__', 'dex_connectors.supabase.models')


# UpsertRowsOutput preserves returned PostgREST rows.
@dataclass(frozen=True, slots=True)
class UpsertRowsOutput:
    # Data is the JSON row array returned after the merge.
    data: Any = None


setattr(UpsertRowsOutput, '__module__', 'dex_connectors.supabase.models')


# SelectRowsInput reads a bounded PostgREST table page.
@dataclass(frozen=True, slots=True)
class SelectRowsInput:
    # Table is an exposed table name.
    table: str = ''
    # Columns is a comma-separated PostgREST select expression; blank selects all columns.
    columns: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Limit caps rows, from 1 through 1000; zero selects 100.
    limit: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # Offset skips this many rows.
    offset: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(SelectRowsInput, '__module__', 'dex_connectors.supabase.models')


# SelectRowsOutput preserves the selected JSON row array.
@dataclass(frozen=True, slots=True)
class SelectRowsOutput:
    # Data is the JSON array of selected rows.
    data: Any = None


setattr(SelectRowsOutput, '__module__', 'dex_connectors.supabase.models')
