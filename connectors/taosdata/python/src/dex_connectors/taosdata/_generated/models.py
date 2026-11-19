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


# ListDatabasesInput has no caller-supplied SQL or filters.
@dataclass(frozen=True, slots=True)
class ListDatabasesInput:
    pass


setattr(ListDatabasesInput, '__module__', 'dex_connectors.taosdata.models')


# DatabaseMetadata contains the three documented metadata columns.
@dataclass(frozen=True, slots=True)
class DatabaseMetadata:
    # Name is the database name.
    name: str = ''
    # TableCount is the number of tables in the database.
    table_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'tableCount'})
    # Status is the provider's database status.
    status: str = ''


setattr(DatabaseMetadata, '__module__', 'dex_connectors.taosdata.models')


# ListDatabasesOutput contains accessible database metadata.
@dataclass(frozen=True, slots=True)
class ListDatabasesOutput:
    # Databases is ordered as returned by TDengine Cloud.
    databases: tuple[DatabaseMetadata, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'databases', _replace_nil_slices(self.databases, 1))


ListDatabasesOutput.__annotations__['databases'] = tuple[DatabaseMetadata, ...] | None
ListDatabasesOutput.__dataclass_fields__['databases'].type = tuple[DatabaseMetadata, ...] | None
setattr(ListDatabasesOutput, '__module__', 'dex_connectors.taosdata.models')
