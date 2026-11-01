# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed Coda request inputs and provider JSON outputs."""

from dataclasses import dataclass, field
from typing import Any


@dataclass(frozen=True, slots=True)
class ListRowsInput:
    """Doc and table IDs, an optional row limit of at most 500, and an optional page token."""

    doc_id: str = field(default="", metadata={"dex_json_name": "docId"})
    table_id: str = field(default="", metadata={"dex_json_name": "tableId"})
    limit: int = field(default=0, metadata={"dex_json_omit_default": True})
    page_token: str = field(
        default="", metadata={"dex_json_name": "pageToken", "dex_json_omit_default": True}
    )


@dataclass(frozen=True, slots=True)
class ListRowsOutput:
    """The provider's row page JSON, kept untouched."""

    data: Any = None


@dataclass(frozen=True, slots=True)
class GetRowInput:
    """Doc, table, and row IDs."""

    doc_id: str = field(default="", metadata={"dex_json_name": "docId"})
    table_id: str = field(default="", metadata={"dex_json_name": "tableId"})
    row_id: str = field(default="", metadata={"dex_json_name": "rowId"})


@dataclass(frozen=True, slots=True)
class GetRowOutput:
    """The provider's row JSON, kept untouched."""

    data: Any = None
