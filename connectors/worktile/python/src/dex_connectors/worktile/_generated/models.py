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


# ListProjectsInput selects one zero-based page of projects.
@dataclass(frozen=True, slots=True)
class ListProjectsInput:
    # PageIndex starts at zero.
    page_index: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageIndex'})
    # PageSize is the number of projects to request, from one through one hundred.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize'})


setattr(ListProjectsInput, '__module__', 'dex_connectors.worktile.models')


# Project contains non-personal Worktile project metadata.
@dataclass(frozen=True, slots=True)
class Project:
    # ID is the project identifier.
    id: str = ''
    # Name is the project name.
    name: str = ''
    # Identifier is the project code.
    identifier: str = ''
    # Visibility is one for public and two for private projects.
    visibility: int = 0


setattr(Project, '__module__', 'dex_connectors.worktile.models')


# ListProjectsOutput contains one page of projects.
@dataclass(frozen=True, slots=True)
class ListProjectsOutput:
    # PageIndex is the provider's zero-based page number.
    page_index: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageIndex'})
    # PageSize is the provider's page size.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize'})
    # HasMore indicates another page is available.
    has_more: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasMore'})
    # Projects contains the projects in this page.
    projects: tuple[Project, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'projects', _replace_nil_slices(self.projects, 1))


ListProjectsOutput.__annotations__['projects'] = tuple[Project, ...] | None
ListProjectsOutput.__dataclass_fields__['projects'].type = tuple[Project, ...] | None
setattr(ListProjectsOutput, '__module__', 'dex_connectors.worktile.models')
