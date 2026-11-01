# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated Python dataclasses from Go operation types."""

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

@dataclass(frozen=True, slots=True)
class SelectRowsInput:
    """Typed SelectRowsInput value from the Go connector contract."""

    table: str = ''
    columns: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    limit: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    offset: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(SelectRowsInput, '__module__', 'dex_connectors.supabase.models')


@dataclass(frozen=True, slots=True)
class SelectRowsOutput:
    """Typed SelectRowsOutput value from the Go connector contract."""

    data: Any = None


setattr(SelectRowsOutput, '__module__', 'dex_connectors.supabase.models')


@dataclass(frozen=True, slots=True)
class UpsertRowsInput:
    """Typed UpsertRowsInput value from the Go connector contract."""

    table: str = ''
    on_conflict: str = _dataclass_field(default='', metadata={'dex_json_name': 'onConflict'})
    rows: tuple[dict[str, Any] | None, ...] | None = None


setattr(UpsertRowsInput, '__module__', 'dex_connectors.supabase.models')


@dataclass(frozen=True, slots=True)
class UpsertRowsOutput:
    """Typed UpsertRowsOutput value from the Go connector contract."""

    data: Any = None


setattr(UpsertRowsOutput, '__module__', 'dex_connectors.supabase.models')
