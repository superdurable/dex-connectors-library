# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return value
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# ListProjectsInput selects a Harvest project page; Cursor comes from the prior result.
@dataclass(frozen=True, slots=True)
class ListProjectsInput:
    # Cursor is the opaque token from the prior page; blank selects the first.
    cursor: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # PerPage caps records in one page; zero uses 100.
    per_page: int = _dataclass_field(default=0, metadata={'dex_json_name': 'perPage', 'dex_json_omit_default': True})


setattr(ListProjectsInput, '__module__', 'dex_connectors.harvest.models')


# Project is a Harvest project identity, name, and active state.
@dataclass(frozen=True, slots=True)
class Project:
    # ID is the provider-assigned resource identity.
    id: int = 0
    # Name is the resource display name.
    name: str = ''
    # IsActive reports whether the project is active.
    is_active: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isActive'})


setattr(Project, '__module__', 'dex_connectors.harvest.models')


# ProjectPage contains projects and a cursor for the next page.
@dataclass(frozen=True, slots=True)
class ProjectPage:
    # Projects contains the returned Harvest projects.
    projects: tuple[Project, ...] = ()
    # NextCursor selects the next page; blank means this was the last page.
    next_cursor: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextCursor', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'projects', _replace_nil_slices(self.projects, 1))


ProjectPage.__annotations__['projects'] = tuple[Project, ...] | None
setattr(ProjectPage, '__module__', 'dex_connectors.harvest.models')


# ListTimeEntriesInput selects a Harvest time-entry page; Cursor comes from the prior result.
@dataclass(frozen=True, slots=True)
class ListTimeEntriesInput:
    # Cursor is the opaque token from the prior page; blank selects the first.
    cursor: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # PerPage caps records in one page; zero uses 100.
    per_page: int = _dataclass_field(default=0, metadata={'dex_json_name': 'perPage', 'dex_json_omit_default': True})


setattr(ListTimeEntriesInput, '__module__', 'dex_connectors.harvest.models')


@dataclass(frozen=True, slots=True)
class TimeEntryProject:
    # ID is the provider-assigned resource identity.
    id: int = 0


setattr(TimeEntryProject, '__module__', 'dex_connectors.harvest.models')


# TimeEntry is a tracked-time record with its project identity.
@dataclass(frozen=True, slots=True)
class TimeEntry:
    # ID is the provider-assigned resource identity.
    id: int = 0
    # Project identifies the time entry's Harvest project.
    project: TimeEntryProject = _dataclass_field(default_factory=TimeEntryProject)
    # Hours is the tracked duration in decimal hours.
    hours: float = 0.0
    # SpentDate is the tracked calendar day in YYYY-MM-DD format.
    spent_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'spentDate'})


setattr(TimeEntry, '__module__', 'dex_connectors.harvest.models')


# TimeEntryPage contains tracked time and a cursor for the next page.
@dataclass(frozen=True, slots=True)
class TimeEntryPage:
    # TimeEntries contains the returned Harvest time records.
    time_entries: tuple[TimeEntry, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'timeEntries'})
    # NextCursor selects the next page; blank means this was the last page.
    next_cursor: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextCursor', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'time_entries', _replace_nil_slices(self.time_entries, 1))


TimeEntryPage.__annotations__['time_entries'] = tuple[TimeEntry, ...] | None
setattr(TimeEntryPage, '__module__', 'dex_connectors.harvest.models')
