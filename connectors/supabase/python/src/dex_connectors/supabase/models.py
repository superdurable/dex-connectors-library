# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed Supabase PostgREST row selections and upserts."""

from dataclasses import dataclass, field
from typing import Any


@dataclass(frozen=True, slots=True)
class SelectRowsInput:
    """Exposed table, comma-separated columns (blank is all), limit 1-1000 (zero is 100), and offset."""

    table: str = ""
    columns: str = field(default="", metadata={"dex_json_omit_default": True})
    limit: int = field(default=0, metadata={"dex_json_omit_default": True})
    offset: int = field(default=0, metadata={"dex_json_omit_default": True})


@dataclass(frozen=True, slots=True)
class SelectRowsOutput:
    """The selected JSON row array, kept untouched."""

    data: Any = None


@dataclass(frozen=True, slots=True)
class UpsertRowsInput:
    """Exposed table, database-enforced unique conflict columns, and 1-100 complete replacement rows."""

    table: str = ""
    on_conflict: str = field(default="", metadata={"dex_json_name": "onConflict"})
    rows: tuple[dict[str, Any], ...] = ()


@dataclass(frozen=True, slots=True)
class UpsertRowsOutput:
    """The JSON row array PostgREST returned after the merge, kept untouched."""

    data: Any = None
