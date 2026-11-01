# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetProjectInput identifies a project by numeric ID or namespace path.
@dataclass(frozen=True, slots=True)
class GetProjectInput:
    # ProjectID is a numeric ID or namespace path, such as meridian/web.
    project_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'projectId'})


setattr(GetProjectInput, '__module__', 'dex_connectors.gitlab.models')


# Project is a GitLab project summary.
@dataclass(frozen=True, slots=True)
class Project:
    # ID is the project's numeric identifier.
    id: int = 0
    # Name is the project display name.
    name: str = ''
    # PathWithNamespace is the full group and project path.
    path_with_namespace: str = ''
    # WebURL links to the project's GitLab page.
    web_url: str = ''


setattr(Project, '__module__', 'dex_connectors.gitlab.models')


# ListIssuesInput identifies the project whose issues are listed.
@dataclass(frozen=True, slots=True)
class ListIssuesInput:
    # ProjectID is a numeric ID or namespace path.
    project_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'projectId'})


setattr(ListIssuesInput, '__module__', 'dex_connectors.gitlab.models')


# Issue is a GitLab issue summary.
@dataclass(frozen=True, slots=True)
class Issue:
    # ID is the global issue identifier.
    id: int = 0
    # IID is the issue number inside its project.
    iid: int = 0
    # Title is the issue title.
    title: str = ''
    # Labels is the issue's complete label set.
    labels: tuple[str, ...] | None = None
    # WebURL links to the issue in GitLab.
    web_url: str = ''


setattr(Issue, '__module__', 'dex_connectors.gitlab.models')


# IssuesPage contains GitLab's default issue page.
@dataclass(frozen=True, slots=True)
class IssuesPage:
    # Issues contains one provider page, possibly empty.
    issues: tuple[Issue, ...] | None = None


setattr(IssuesPage, '__module__', 'dex_connectors.gitlab.models')


# UpdateIssueLabelsInput replaces every label on one issue; an empty list clears them.
@dataclass(frozen=True, slots=True)
class UpdateIssueLabelsInput:
    # ProjectID is a numeric ID or namespace path.
    project_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'projectId'})
    # IssueIID is the positive project-local issue number.
    issue_iid: int = _dataclass_field(default=0, metadata={'dex_json_name': 'issueIid'})
    # Labels is the desired complete label set; empty clears all labels.
    labels: tuple[str, ...] | None = None


setattr(UpdateIssueLabelsInput, '__module__', 'dex_connectors.gitlab.models')
