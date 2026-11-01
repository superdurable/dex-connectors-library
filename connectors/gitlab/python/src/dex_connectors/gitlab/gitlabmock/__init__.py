# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest-backed GitLab scripts for consuming Flow tests."""

from dex_connectors.sdk import ConnectionRef, Failure
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..client import (
    CONNECTOR_ID,
    FOUND,
    GET_PROJECT_DEFINITION,
    LIST_ISSUES_DEFINITION,
    LISTED,
    NOT_FOUND,
    UPDATE_ISSUE_LABELS_DEFINITION,
    UPDATED,
)
from ..connection import Connection, new_connection_with_operations
from ..models import GetProjectInput, Issue, IssuesPage, ListIssuesInput, Project, UpdateIssueLabelsInput

_ISSUE_URL = "https://gitlab.com/meridian/web/-/issues/7"


def get_project_found(value: Project, failure: Failure | None = None) -> Case[Project]:
    """Script the found route."""
    return Case.branch_result(FOUND, value, failure)


def get_project_not_found(value: Project, failure: Failure | None = None) -> Case[Project]:
    """Script a missing or inaccessible project."""
    return Case.branch_result(NOT_FOUND, value, failure)


def get_project_default_answer() -> Case[Project]:
    """Return the manifest's getProject default answer."""
    return get_project_found(Project(42, "Meridian Web", "meridian/web", "https://gitlab.com/meridian/web"))


def list_issues_listed(value: IssuesPage, failure: Failure | None = None) -> Case[IssuesPage]:
    """Script the listed route."""
    return Case.branch_result(LISTED, value, failure)


def list_issues_not_found(value: IssuesPage, failure: Failure | None = None) -> Case[IssuesPage]:
    """Script a missing or inaccessible project."""
    return Case.branch_result(NOT_FOUND, value, failure)


def list_issues_default_answer() -> Case[IssuesPage]:
    """Return the manifest's listIssues default answer."""
    return list_issues_listed(IssuesPage((Issue(123, 7, "Review release notes", ("review",), _ISSUE_URL),)))


def update_issue_labels_updated(value: Issue, failure: Failure | None = None) -> Case[Issue]:
    """Script the updated route."""
    return Case.branch_result(UPDATED, value, failure)


def update_issue_labels_not_found(value: Issue, failure: Failure | None = None) -> Case[Issue]:
    """Script a missing or inaccessible issue."""
    return Case.branch_result(NOT_FOUND, value, failure)


def update_issue_labels_default_answer() -> Case[Issue]:
    """Return the manifest's updateIssueLabels default answer."""
    return update_issue_labels_updated(Issue(123, 7, "Review release notes", ("review", "ready"), _ISSUE_URL))


class Mock:
    """Script GitLab operations, defaulting to their manifest answers."""

    def __init__(self, connection_name: str = "gitlab") -> None:
        self.get_project_script: Query[GetProjectInput, Project] = Query(GET_PROJECT_DEFINITION)
        self.get_project_script.default(get_project_default_answer())
        self.list_issues_script: Query[ListIssuesInput, IssuesPage] = Query(LIST_ISSUES_DEFINITION)
        self.list_issues_script.default(list_issues_default_answer())
        self.update_issue_labels_script: Mutation[UpdateIssueLabelsInput, Issue] = Mutation(
            UPDATE_ISSUE_LABELS_DEFINITION
        )
        self.update_issue_labels_script.default(update_issue_labels_default_answer())
        self._connection = new_connection_with_operations(
            self.get_project_script,
            self.list_issues_script,
            self.update_issue_labels_script,
            ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        """Return this mock's named connection for Step registration."""
        return self._connection


__all__ = [
    "Mock",
    "get_project_found",
    "get_project_not_found",
    "get_project_default_answer",
    "list_issues_listed",
    "list_issues_not_found",
    "list_issues_default_answer",
    "update_issue_labels_updated",
    "update_issue_labels_not_found",
    "update_issue_labels_default_answer",
]
