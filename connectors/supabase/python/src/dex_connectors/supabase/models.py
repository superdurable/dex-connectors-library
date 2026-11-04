# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from dataclasses import dataclass, field
from typing import Any

from ._generated.models import (
    SelectRowsInput as SelectRowsInput,
)
from ._generated.models import (
    SelectRowsOutput as SelectRowsOutput,
)
from ._generated.models import (
    UpsertRowsOutput as UpsertRowsOutput,
)


@dataclass(frozen=True, slots=True)
class UpsertRowsInput:
    """Exposed table, database-enforced unique conflict columns, and 1-100 complete replacement rows."""

    table: str = ""
    on_conflict: str = field(default="", metadata={"dex_json_name": "onConflict"})
    rows: tuple[dict[str, Any], ...] = ()


SelectRowsInput.__doc__ = (
    "Exposed table, comma-separated columns (blank is all), limit 1-1000 (zero is 100), and offset."
)
SelectRowsOutput.__doc__ = "The selected JSON row array, kept untouched."
UpsertRowsOutput.__doc__ = "The JSON row array PostgREST returned after the merge, kept untouched."
