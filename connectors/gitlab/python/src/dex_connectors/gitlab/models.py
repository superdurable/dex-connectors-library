# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from ._generated.models import (
    GetProjectInput as GetProjectInput,
)
from ._generated.models import Issue as Issue
from ._generated.models import IssuesPage as IssuesPage
from ._generated.models import (
    ListIssuesInput as ListIssuesInput,
)
from ._generated.models import (
    Project as Project,
)
from ._generated.models import UpdateIssueLabelsInput as UpdateIssueLabelsInput

Issue.__doc__ = "An issue's global ID, project-local number, title, complete label set, and web page."
IssuesPage.__doc__ = "GitLab's default issue page, possibly empty."
UpdateIssueLabelsInput.__doc__ = (
    "Project, positive project-local issue number, and the complete label set; empty clears all labels."
)

GetProjectInput.__doc__ = "A numeric project ID or namespace path, such as meridian/web."
Project.__doc__ = "A project's numeric ID, display name, full group and project path, and web page."
ListIssuesInput.__doc__ = "The numeric ID or namespace path of the project whose issues are listed."
