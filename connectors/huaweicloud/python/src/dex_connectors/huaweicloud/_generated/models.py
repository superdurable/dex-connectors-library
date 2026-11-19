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


# ListAccessibleProjectsInput has no user-supplied query parameters.
@dataclass(frozen=True, slots=True)
class ListAccessibleProjectsInput:
    pass


setattr(ListAccessibleProjectsInput, '__module__', 'dex_connectors.huaweicloud.models')


# AccessibleProject contains non-secret IAM project metadata.
@dataclass(frozen=True, slots=True)
class AccessibleProject:
    # ID is the provider's stable project identifier.
    id: str = ''
    # Name is the project name, which can be empty in Huawei Cloud responses.
    name: str = ''
    # Enabled reports whether the project is enabled.
    enabled: bool = False
    # DomainID is the owning account identifier.
    domain_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'domainId'})


setattr(AccessibleProject, '__module__', 'dex_connectors.huaweicloud.models')


# ListAccessibleProjectsOutput contains all projects returned for the user.
@dataclass(frozen=True, slots=True)
class ListAccessibleProjectsOutput:
    # Projects is the accessible project set; an empty set is valid.
    projects: tuple[AccessibleProject, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'projects', _replace_nil_slices(self.projects, 1))


ListAccessibleProjectsOutput.__annotations__['projects'] = tuple[AccessibleProject, ...] | None
ListAccessibleProjectsOutput.__dataclass_fields__['projects'].type = tuple[AccessibleProject, ...] | None
setattr(ListAccessibleProjectsOutput, '__module__', 'dex_connectors.huaweicloud.models')
