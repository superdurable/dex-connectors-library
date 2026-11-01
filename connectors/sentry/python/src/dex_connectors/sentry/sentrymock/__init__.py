# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest-backed Sentry scripts for consuming Flow tests."""

from dex_connectors.sdk import ConnectionRef, Failure
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..client import (
    CONNECTOR_ID,
    FOUND,
    GET_ISSUE_DEFINITION,
    LIST_ISSUES_DEFINITION,
    LISTED,
    NOT_FOUND,
    PROVIDER_REJECTED,
    UPDATE_ISSUE_STATUS_DEFINITION,
    UPDATED,
)
from ..connection import Connection, new_connection_with_operations
from ..models import GetIssueInput, Issue, IssuePage, ListIssuesInput, UpdateIssueStatusInput

_DEFAULT_ISSUE = Issue("123", "APP-1", "ExampleError", "unresolved")


def list_issues_listed(value: IssuePage, failure: Failure | None = None) -> Case[IssuePage]:
    """Script the successful listed route."""
    return Case.branch_result(LISTED, value, failure)


def list_issues_default() -> Case[IssuePage]:
    """Return the manifest's listIssuesDefault answer."""
    return list_issues_listed(IssuePage((_DEFAULT_ISSUE,)))


def get_issue_found(value: Issue, failure: Failure | None = None) -> Case[Issue]:
    """Script the successful found route."""
    return Case.branch_result(FOUND, value, failure)


def get_issue_not_found(value: Issue, failure: Failure | None = None) -> Case[Issue]:
    """Script a missing issue."""
    return Case.branch_result(NOT_FOUND, value, failure)


def get_issue_default() -> Case[Issue]:
    """Return the manifest's getIssueDefault answer."""
    return get_issue_found(_DEFAULT_ISSUE)


def update_issue_status_updated(value: Issue, failure: Failure | None = None) -> Case[Issue]:
    """Script the successful updated route."""
    return Case.branch_result(UPDATED, value, failure)


def update_issue_status_provider_rejected(value: Issue, failure: Failure | None = None) -> Case[Issue]:
    """Script a definite rejection of the status update."""
    return Case.branch_result(PROVIDER_REJECTED, value, failure)


def update_issue_status_default() -> Case[Issue]:
    """Return the manifest's updateIssueStatusDefault answer."""
    return update_issue_status_updated(Issue("123", "APP-1", "ExampleError", "resolved"))


class Mock:
    """Script Sentry operations, defaulting to their manifest answers."""

    def __init__(self, connection_name: str = "sentry") -> None:
        self.list_issues_script: Query[ListIssuesInput, IssuePage] = Query(LIST_ISSUES_DEFINITION)
        self.list_issues_script.default(list_issues_default())
        self.get_issue_script: Query[GetIssueInput, Issue] = Query(GET_ISSUE_DEFINITION)
        self.get_issue_script.default(get_issue_default())
        self.update_issue_status_script: Mutation[UpdateIssueStatusInput, Issue] = Mutation(
            UPDATE_ISSUE_STATUS_DEFINITION
        )
        self.update_issue_status_script.default(update_issue_status_default())
        self._connection = new_connection_with_operations(
            self.list_issues_script,
            self.get_issue_script,
            self.update_issue_status_script,
            ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        """Return this mock's named connection for Step registration."""
        return self._connection


__all__ = [
    "Mock",
    "list_issues_listed",
    "list_issues_default",
    "get_issue_found",
    "get_issue_not_found",
    "get_issue_default",
    "update_issue_status_updated",
    "update_issue_status_provider_rejected",
    "update_issue_status_default",
]
