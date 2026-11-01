# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated Python dataclasses from Go operation types."""

from dataclasses import dataclass, field as _dataclass_field

@dataclass(frozen=True, slots=True)
class GetProjectInput:
    """Typed GetProjectInput value from the Go connector contract."""

    project_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'projectId'})


setattr(GetProjectInput, '__module__', 'dex_connectors.gitlab.models')


@dataclass(frozen=True, slots=True)
class Project:
    """Typed Project value from the Go connector contract."""

    id: int = 0
    name: str = ''
    path_with_namespace: str = ''
    web_url: str = ''


setattr(Project, '__module__', 'dex_connectors.gitlab.models')


@dataclass(frozen=True, slots=True)
class ListIssuesInput:
    """Typed ListIssuesInput value from the Go connector contract."""

    project_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'projectId'})


setattr(ListIssuesInput, '__module__', 'dex_connectors.gitlab.models')


@dataclass(frozen=True, slots=True)
class Issue:
    """Typed Issue value from the Go connector contract."""

    id: int = 0
    iid: int = 0
    title: str = ''
    labels: tuple[str, ...] | None = None
    web_url: str = ''


setattr(Issue, '__module__', 'dex_connectors.gitlab.models')


@dataclass(frozen=True, slots=True)
class IssuesPage:
    """Typed IssuesPage value from the Go connector contract."""

    issues: tuple[Issue, ...] | None = None


setattr(IssuesPage, '__module__', 'dex_connectors.gitlab.models')


@dataclass(frozen=True, slots=True)
class UpdateIssueLabelsInput:
    """Typed UpdateIssueLabelsInput value from the Go connector contract."""

    project_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'projectId'})
    issue_iid: int = _dataclass_field(default=0, metadata={'dex_json_name': 'issueIid'})
    labels: tuple[str, ...] | None = None


setattr(UpdateIssueLabelsInput, '__module__', 'dex_connectors.gitlab.models')
