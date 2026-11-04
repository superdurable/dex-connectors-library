# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

# ListRowsInput selects a Coda table page.
@dataclass(frozen=True, slots=True)
class ListRowsInput:
    # DocID is the ID from the document URL.
    doc_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'docId'})
    # TableID is the stable table ID, such as grid-abc.
    table_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'tableId'})
    # Limit caps rows at 500; zero uses Coda's default.
    limit: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # PageToken is the opaque nextPageToken from a prior page.
    page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageToken', 'dex_json_omit_default': True})


setattr(ListRowsInput, '__module__', 'dex_connectors.coda.models')


# ListRowsOutput preserves the Coda row page.
@dataclass(frozen=True, slots=True)
class ListRowsOutput:
    # Data contains items and a possible nextPageToken.
    data: Any = None


setattr(ListRowsOutput, '__module__', 'dex_connectors.coda.models')


# GetRowInput selects one Coda row.
@dataclass(frozen=True, slots=True)
class GetRowInput:
    # DocID is the document ID.
    doc_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'docId'})
    # TableID is the stable table ID.
    table_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'tableId'})
    # RowID is the stable row ID, such as i-abc.
    row_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'rowId'})


setattr(GetRowInput, '__module__', 'dex_connectors.coda.models')


# GetRowOutput preserves one Coda row.
@dataclass(frozen=True, slots=True)
class GetRowOutput:
    # Data is the row JSON object.
    data: Any = None


setattr(GetRowOutput, '__module__', 'dex_connectors.coda.models')
