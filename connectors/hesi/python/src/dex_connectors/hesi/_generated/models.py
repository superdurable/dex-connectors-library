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


# ListDepartmentsInput selects a zero-based department window; zero count uses 20.
@dataclass(frozen=True, slots=True)
class ListDepartmentsInput:
    # Start is the zero-based department offset.
    start: int = 0
    # Count is the number of departments requested, from 1 to 100.
    count: int = 0


setattr(ListDepartmentsInput, '__module__', 'dex_connectors.hesi.models')


# DepartmentSummary excludes Hesi legal entity and cost center data.
@dataclass(frozen=True, slots=True)
class DepartmentSummary:
    # ID is the provider's stable department ID.
    id: str = ''
    # Name is the department name.
    name: str = ''
    # Code is the optional enterprise department code.
    code: str = ''
    # Active reports whether the department is enabled.
    active: bool = False


setattr(DepartmentSummary, '__module__', 'dex_connectors.hesi.models')


# ListDepartmentsOutput contains one department page and a next offset.
@dataclass(frozen=True, slots=True)
class ListDepartmentsOutput:
    # Departments are returned in provider order.
    departments: tuple[DepartmentSummary, ...] = ()
    # Total is the number of departments reported by Hesi.
    total: int = 0
    # NextStart is the next zero-based offset, or zero when the page is terminal.
    next_start: int = _dataclass_field(default=0, metadata={'dex_json_name': 'nextStart'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'departments', _replace_nil_slices(self.departments, 1))


ListDepartmentsOutput.__annotations__['departments'] = tuple[DepartmentSummary, ...] | None
ListDepartmentsOutput.__dataclass_fields__['departments'].type = tuple[DepartmentSummary, ...] | None
setattr(ListDepartmentsOutput, '__module__', 'dex_connectors.hesi.models')
