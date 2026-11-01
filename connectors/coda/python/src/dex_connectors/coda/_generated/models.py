# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated Python dataclasses from Go operation types."""

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

@dataclass(frozen=True, slots=True)
class ListRowsInput:
    """Typed ListRowsInput value from the Go connector contract."""

    doc_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'docId'})
    table_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'tableId'})
    limit: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageToken', 'dex_json_omit_default': True})


setattr(ListRowsInput, '__module__', 'dex_connectors.coda.models')


@dataclass(frozen=True, slots=True)
class ListRowsOutput:
    """Typed ListRowsOutput value from the Go connector contract."""

    data: Any = None


setattr(ListRowsOutput, '__module__', 'dex_connectors.coda.models')


@dataclass(frozen=True, slots=True)
class GetRowInput:
    """Typed GetRowInput value from the Go connector contract."""

    doc_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'docId'})
    table_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'tableId'})
    row_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'rowId'})


setattr(GetRowInput, '__module__', 'dex_connectors.coda.models')


@dataclass(frozen=True, slots=True)
class GetRowOutput:
    """Typed GetRowOutput value from the Go connector contract."""

    data: Any = None


setattr(GetRowOutput, '__module__', 'dex_connectors.coda.models')
