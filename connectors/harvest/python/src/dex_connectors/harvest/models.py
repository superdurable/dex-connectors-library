# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from dataclasses import dataclass, field

from ._generated.models import (
    ListProjectsInput as ListProjectsInput,
)
from ._generated.models import (
    ListTimeEntriesInput as ListTimeEntriesInput,
)
from ._generated.models import (
    Project as Project,
)
from ._generated.models import (
    TimeEntry as TimeEntry,
)
from ._generated.models import (
    TimeEntryProject as TimeEntryProject,
)


@dataclass(frozen=True, slots=True)
class ProjectPage:
    """One page of projects; next_cursor is blank on the last page."""

    projects: tuple[Project, ...] = ()
    next_cursor: str = field(
        default="", metadata={"dex_json_name": "nextCursor", "dex_json_omit_default": True}
    )


@dataclass(frozen=True, slots=True)
class TimeEntryPage:
    """One page of time entries; next_cursor is blank on the last page."""

    time_entries: tuple[TimeEntry, ...] = field(default=(), metadata={"dex_json_name": "timeEntries"})
    next_cursor: str = field(
        default="", metadata={"dex_json_name": "nextCursor", "dex_json_omit_default": True}
    )
