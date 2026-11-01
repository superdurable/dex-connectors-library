# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from ._generated.models import (
    ListProjectsInput as ListProjectsInput,
)
from ._generated.models import (
    ListTimeEntriesInput as ListTimeEntriesInput,
)
from ._generated.models import (
    Project as Project,
)
from ._generated.models import ProjectPage as ProjectPage
from ._generated.models import (
    TimeEntry as TimeEntry,
)
from ._generated.models import TimeEntryPage as TimeEntryPage
from ._generated.models import (
    TimeEntryProject as TimeEntryProject,
)

ProjectPage.__doc__ = "One page of projects; next_cursor is blank on the last page."
TimeEntryPage.__doc__ = "One page of time entries; next_cursor is blank on the last page."
