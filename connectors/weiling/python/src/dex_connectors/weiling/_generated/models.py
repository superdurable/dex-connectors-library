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


# ListDepartmentsInput requests the accessible department directory without caller-supplied identifiers.
@dataclass(frozen=True, slots=True)
class ListDepartmentsInput:
    pass


setattr(ListDepartmentsInput, '__module__', 'dex_connectors.weiling.models')


# DepartmentSummary excludes Weiling leader and employee identifiers.
@dataclass(frozen=True, slots=True)
class DepartmentSummary:
    # ID is the provider's department ID.
    id: str = ''
    # Name is the department name.
    name: str = ''
    # ParentID is the optional parent department ID.
    parent_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'parentID'})


setattr(DepartmentSummary, '__module__', 'dex_connectors.weiling.models')


# ListDepartmentsOutput contains the accessible department directory.
@dataclass(frozen=True, slots=True)
class ListDepartmentsOutput:
    # Departments contains accessible department metadata without leader IDs.
    departments: tuple[DepartmentSummary, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'departments', _replace_nil_slices(self.departments, 1))


ListDepartmentsOutput.__annotations__['departments'] = tuple[DepartmentSummary, ...] | None
ListDepartmentsOutput.__dataclass_fields__['departments'].type = tuple[DepartmentSummary, ...] | None
setattr(ListDepartmentsOutput, '__module__', 'dex_connectors.weiling.models')
