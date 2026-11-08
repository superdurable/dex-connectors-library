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


# ListTablesInput selects the non-secret ClickHouse database to inspect.
@dataclass(frozen=True, slots=True)
class ListTablesInput:
    # Database is the exact database name shown by SHOW DATABASES.
    database: str = ''


setattr(ListTablesInput, '__module__', 'dex_connectors.clickhouse.models')


# TablesPage contains at most 100 sorted table names.
@dataclass(frozen=True, slots=True)
class TablesPage:
    # Database is the database that was inspected.
    database: str = ''
    # Names contains the first 100 table names in sorted order.
    names: tuple[str, ...] = ()
    # HasMore reports whether another table was present after the first 100.
    has_more: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasMore'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'names', _replace_nil_slices(self.names, 1))


TablesPage.__annotations__['names'] = tuple[str, ...] | None
TablesPage.__dataclass_fields__['names'].type = tuple[str, ...] | None
setattr(TablesPage, '__module__', 'dex_connectors.clickhouse.models')
