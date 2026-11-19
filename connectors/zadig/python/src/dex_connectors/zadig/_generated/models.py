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


# ListProjectsInput selects one project page. Zero page number and size use 1 and 20.
@dataclass(frozen=True, slots=True)
class ListProjectsInput:
    # PageNum is the one-based page number; zero selects the first page.
    page_num: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageNum'})
    # PageSize is the number of projects to request, from 1 to 100; zero uses 20.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize'})


setattr(ListProjectsInput, '__module__', 'dex_connectors.zadig.models')


# Project contains non-secret Zadig project metadata.
@dataclass(frozen=True, slots=True)
class Project:
    # Key is the stable project identifier.
    key: str = ''
    # Name is the project display name.
    name: str = ''
    # DeployType is the provider's project deployment type.
    deploy_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'deployType'})


setattr(Project, '__module__', 'dex_connectors.zadig.models')


# ListProjectsOutput contains one project page and the total project count.
@dataclass(frozen=True, slots=True)
class ListProjectsOutput:
    # Projects are the returned projects in provider order.
    projects: tuple[Project, ...] = ()
    # Total is the total count reported by Zadig.
    total: int = 0

    def __post_init__(self) -> None:
        object.__setattr__(self, 'projects', _replace_nil_slices(self.projects, 1))


ListProjectsOutput.__annotations__['projects'] = tuple[Project, ...] | None
ListProjectsOutput.__dataclass_fields__['projects'].type = tuple[Project, ...] | None
setattr(ListProjectsOutput, '__module__', 'dex_connectors.zadig.models')
