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


# ListMemberProjectsInput selects a one-based project page; zero values use page 1 and 20 projects.
@dataclass(frozen=True, slots=True)
class ListMemberProjectsInput:
    # Page is the one-based project page number.
    page: int = 0
    # PerPage is the number of projects requested, from 1 to 100.
    per_page: int = _dataclass_field(default=0, metadata={'dex_json_name': 'perPage'})


setattr(ListMemberProjectsInput, '__module__', 'dex_connectors.jihugitlab.models')


# MemberProject contains only stable JiHu GitLab project metadata.
@dataclass(frozen=True, slots=True)
class MemberProject:
    # ID is the project ID.
    id: int = 0
    # Name is the project name.
    name: str = ''
    # Path is the project path with namespace.
    path: str = ''


setattr(MemberProject, '__module__', 'dex_connectors.jihugitlab.models')


# ListMemberProjectsOutput contains one page and the next page number, or zero at the end.
@dataclass(frozen=True, slots=True)
class ListMemberProjectsOutput:
    # Projects are the returned member projects in provider order.
    projects: tuple[MemberProject, ...] = ()
    # NextPage is the next one-based page number, or zero if the page is terminal.
    next_page: int = _dataclass_field(default=0, metadata={'dex_json_name': 'nextPage'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'projects', _replace_nil_slices(self.projects, 1))


ListMemberProjectsOutput.__annotations__['projects'] = tuple[MemberProject, ...] | None
ListMemberProjectsOutput.__dataclass_fields__['projects'].type = tuple[MemberProject, ...] | None
setattr(ListMemberProjectsOutput, '__module__', 'dex_connectors.jihugitlab.models')
