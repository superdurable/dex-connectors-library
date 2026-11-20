# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# ListDepartmentsInput requests the department catalog visible to the OpenAPI authorization.
@dataclass(frozen=True, slots=True)
class ListDepartmentsInput:
    pass


setattr(ListDepartmentsInput, '__module__', 'dex_connectors.qince.models')


# Department contains only Qince organization structure metadata.
@dataclass(frozen=True, slots=True)
class Department:
    # ID is the Qince department identifier.
    id: str = ''
    # Code is the department code.
    code: str = ''
    # Name is the department name.
    name: str = ''
    # Active indicates Qince org_status 1.
    active: bool = False


setattr(Department, '__module__', 'dex_connectors.qince.models')


# ListDepartmentsOutput contains the returned department catalog in provider order.
@dataclass(frozen=True, slots=True)
class ListDepartmentsOutput:
    departments: tuple[Department, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'departments', _replace_nil_slices(self.departments, 1))


ListDepartmentsOutput.__annotations__['departments'] = tuple[Department, ...] | None
ListDepartmentsOutput.__dataclass_fields__['departments'].type = tuple[Department, ...] | None
setattr(ListDepartmentsOutput, '__module__', 'dex_connectors.qince.models')
