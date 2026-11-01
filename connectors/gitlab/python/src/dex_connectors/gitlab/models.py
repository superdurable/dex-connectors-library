# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed GitLab projects, issue pages, and issue label replacements."""

from dataclasses import dataclass, field


@dataclass(frozen=True, slots=True)
class GetProjectInput:
    """A numeric project ID or namespace path, such as meridian/web."""

    project_id: str = field(default="", metadata={"dex_json_name": "projectId"})


@dataclass(frozen=True, slots=True)
class Project:
    """A project's numeric ID, display name, full group and project path, and web page."""

    id: int = 0
    name: str = ""
    path_with_namespace: str = ""
    web_url: str = ""


@dataclass(frozen=True, slots=True)
class ListIssuesInput:
    """The numeric ID or namespace path of the project whose issues are listed."""

    project_id: str = field(default="", metadata={"dex_json_name": "projectId"})


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
