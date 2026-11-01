# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated Python dataclasses from Go operation types."""

from dataclasses import dataclass, field as _dataclass_field

@dataclass(frozen=True, slots=True)
class ListProjectsInput:
    """Typed ListProjectsInput value from the Go connector contract."""

    cursor: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    per_page: int = _dataclass_field(default=0, metadata={'dex_json_name': 'perPage', 'dex_json_omit_default': True})


setattr(ListProjectsInput, '__module__', 'dex_connectors.harvest.models')


@dataclass(frozen=True, slots=True)
class Project:
    """Typed Project value from the Go connector contract."""

    id: int = 0
    name: str = ''
    is_active: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isActive'})


setattr(Project, '__module__', 'dex_connectors.harvest.models')


@dataclass(frozen=True, slots=True)
class ProjectPage:
    """Typed ProjectPage value from the Go connector contract."""

    projects: tuple[Project, ...] | None = None
    next_cursor: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextCursor', 'dex_json_omit_default': True})


setattr(ProjectPage, '__module__', 'dex_connectors.harvest.models')


@dataclass(frozen=True, slots=True)
class ListTimeEntriesInput:
    """Typed ListTimeEntriesInput value from the Go connector contract."""

    cursor: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    per_page: int = _dataclass_field(default=0, metadata={'dex_json_name': 'perPage', 'dex_json_omit_default': True})


setattr(ListTimeEntriesInput, '__module__', 'dex_connectors.harvest.models')


@dataclass(frozen=True, slots=True)
class TimeEntryProject:
    """Typed TimeEntryProject value from the Go connector contract."""

    id: int = 0


setattr(TimeEntryProject, '__module__', 'dex_connectors.harvest.models')


@dataclass(frozen=True, slots=True)
class TimeEntry:
    """Typed TimeEntry value from the Go connector contract."""

    id: int = 0
    project: TimeEntryProject = _dataclass_field(default_factory=TimeEntryProject)
    hours: float = 0.0
    spent_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'spentDate'})


setattr(TimeEntry, '__module__', 'dex_connectors.harvest.models')


@dataclass(frozen=True, slots=True)
class TimeEntryPage:
    """Typed TimeEntryPage value from the Go connector contract."""

    time_entries: tuple[TimeEntry, ...] | None = _dataclass_field(default=None, metadata={'dex_json_name': 'timeEntries'})
    next_cursor: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextCursor', 'dex_json_omit_default': True})


setattr(TimeEntryPage, '__module__', 'dex_connectors.harvest.models')
