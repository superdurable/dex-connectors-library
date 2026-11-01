# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from dataclasses import dataclass, field

from ._generated.models import (
    GetProjectInput as GetProjectInput,
)
from ._generated.models import (
    ListIssuesInput as ListIssuesInput,
)
from ._generated.models import (
    Project as Project,
)


@dataclass(frozen=True, slots=True)
class Issue:
    """An issue's global ID, project-local number, title, complete label set, and web page."""

    id: int = 0
    iid: int = 0
    title: str = ""
    labels: tuple[str, ...] = ()
    web_url: str = ""


@dataclass(frozen=True, slots=True)
class IssuesPage:
    """GitLab's default issue page, possibly empty."""

    issues: tuple[Issue, ...] = ()


@dataclass(frozen=True, slots=True)
class UpdateIssueLabelsInput:
    """Project, positive project-local issue number, and the complete label set; empty clears all labels."""

    project_id: str = field(default="", metadata={"dex_json_name": "projectId"})
    issue_iid: int = field(default=0, metadata={"dex_json_name": "issueIid"})
    labels: tuple[str, ...] = ()
