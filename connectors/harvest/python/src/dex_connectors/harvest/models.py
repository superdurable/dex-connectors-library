# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed Harvest project and time entry pages."""

from dataclasses import dataclass, field


@dataclass(frozen=True, slots=True)
class ListProjectsInput:
    """Cursor page of projects; per_page zero means 100, at most 2000."""

    cursor: str = field(default="", metadata={"dex_json_omit_default": True})
    per_page: int = field(default=0, metadata={"dex_json_name": "perPage", "dex_json_omit_default": True})


@dataclass(frozen=True, slots=True)
class Project:
    """Harvest project identity, name, and active flag."""

    id: int = 0
    name: str = ""
    is_active: bool = field(default=False, metadata={"dex_json_name": "isActive"})


@dataclass(frozen=True, slots=True)
class ProjectPage:
    """One page of projects; next_cursor is blank on the last page."""

    projects: tuple[Project, ...] = ()
    next_cursor: str = field(default="", metadata={"dex_json_name": "nextCursor", "dex_json_omit_default": True})


@dataclass(frozen=True, slots=True)
class ListTimeEntriesInput:
    """Cursor page of time entries; per_page zero means 100, at most 2000."""

    cursor: str = field(default="", metadata={"dex_json_omit_default": True})
    per_page: int = field(default=0, metadata={"dex_json_name": "perPage", "dex_json_omit_default": True})


@dataclass(frozen=True, slots=True)
class TimeEntryProject:
    """Project reference on a time entry."""

    id: int = 0


@dataclass(frozen=True, slots=True)
class TimeEntry:
    """Logged hours for one project on spent_date, Harvest's YYYY-MM-DD text."""

    id: int = 0
    project: TimeEntryProject = field(default_factory=TimeEntryProject)
    hours: float = 0.0
    spent_date: str = field(default="", metadata={"dex_json_name": "spentDate"})


@dataclass(frozen=True, slots=True)
class TimeEntryPage:
    """One page of time entries; next_cursor is blank on the last page."""

    time_entries: tuple[TimeEntry, ...] = field(default=(), metadata={"dex_json_name": "timeEntries"})
    next_cursor: str = field(default="", metadata={"dex_json_name": "nextCursor", "dex_json_omit_default": True})
