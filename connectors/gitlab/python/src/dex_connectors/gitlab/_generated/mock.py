# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetProjectInput, Issue, IssuesPage, ListIssuesInput, Project, UpdateIssueLabelsInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_PROJECT_DEFINITION, INVALID_RESPONSE, LISTED, LIST_ISSUES_DEFINITION, NOT_FOUND, PROVIDER_REJECTED, UPDATED, UPDATE_ISSUE_LABELS_DEFINITION

def get_project_found(value: Project, failure: Failure | None = None) -> Case[Project]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_project_found, '__module__', "dex_connectors.gitlab.gitlabmock")

def get_project_not_found(value: Project, failure: Failure | None = None) -> Case[Project]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_project_not_found, '__module__', "dex_connectors.gitlab.gitlabmock")

def get_project_provider_rejected(value: Project, failure: Failure | None = None) -> Case[Project]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_project_provider_rejected, '__module__', "dex_connectors.gitlab.gitlabmock")

def get_project_invalid_response(value: Project, failure: Failure | None = None) -> Case[Project]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_project_invalid_response, '__module__', "dex_connectors.gitlab.gitlabmock")

def get_project_defect(value: Project, failure: Failure | None = None) -> Case[Project]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_project_defect, '__module__', "dex_connectors.gitlab.gitlabmock")

def get_project_retry(failure: Failure, after_seconds: float = 0) -> Case[Project]:
    return Case.retry(failure, after_seconds)

setattr(get_project_retry, '__module__', "dex_connectors.gitlab.gitlabmock")

def get_project_found_default() -> Case[Project]:
    return Case.branch_result(FOUND, Project(id=42, name='Meridian Web', path_with_namespace='meridian/web', web_url='https://gitlab.com/meridian/web'))

setattr(get_project_found_default, '__module__', "dex_connectors.gitlab.gitlabmock")

def list_issues_listed(value: IssuesPage, failure: Failure | None = None) -> Case[IssuesPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_issues_listed, '__module__', "dex_connectors.gitlab.gitlabmock")

def list_issues_not_found(value: IssuesPage, failure: Failure | None = None) -> Case[IssuesPage]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(list_issues_not_found, '__module__', "dex_connectors.gitlab.gitlabmock")

def list_issues_provider_rejected(value: IssuesPage, failure: Failure | None = None) -> Case[IssuesPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_issues_provider_rejected, '__module__', "dex_connectors.gitlab.gitlabmock")

def list_issues_invalid_response(value: IssuesPage, failure: Failure | None = None) -> Case[IssuesPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_issues_invalid_response, '__module__', "dex_connectors.gitlab.gitlabmock")

def list_issues_defect(value: IssuesPage, failure: Failure | None = None) -> Case[IssuesPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_issues_defect, '__module__', "dex_connectors.gitlab.gitlabmock")

def list_issues_retry(failure: Failure, after_seconds: float = 0) -> Case[IssuesPage]:
    return Case.retry(failure, after_seconds)

setattr(list_issues_retry, '__module__', "dex_connectors.gitlab.gitlabmock")

def list_issues_listed_default() -> Case[IssuesPage]:
    return Case.branch_result(LISTED, IssuesPage(issues=(Issue(id=123, iid=7, title='Review release notes', labels=('review',), web_url='https://gitlab.com/meridian/web/-/issues/7'),)))

setattr(list_issues_listed_default, '__module__', "dex_connectors.gitlab.gitlabmock")

def update_issue_labels_updated(value: Issue, failure: Failure | None = None) -> Case[Issue]:
    return Case.branch_result(UPDATED, value, failure)

setattr(update_issue_labels_updated, '__module__', "dex_connectors.gitlab.gitlabmock")

def update_issue_labels_not_found(value: Issue, failure: Failure | None = None) -> Case[Issue]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(update_issue_labels_not_found, '__module__', "dex_connectors.gitlab.gitlabmock")

def update_issue_labels_provider_rejected(value: Issue, failure: Failure | None = None) -> Case[Issue]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(update_issue_labels_provider_rejected, '__module__', "dex_connectors.gitlab.gitlabmock")

def update_issue_labels_invalid_response(value: Issue, failure: Failure | None = None) -> Case[Issue]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(update_issue_labels_invalid_response, '__module__', "dex_connectors.gitlab.gitlabmock")

def update_issue_labels_defect(value: Issue, failure: Failure | None = None) -> Case[Issue]:
    return Case.branch_result(DEFECT, value, failure)

setattr(update_issue_labels_defect, '__module__', "dex_connectors.gitlab.gitlabmock")

def update_issue_labels_retry(failure: Failure, after_seconds: float = 0) -> Case[Issue]:
    return Case.retry(failure, after_seconds)

setattr(update_issue_labels_retry, '__module__', "dex_connectors.gitlab.gitlabmock")

def update_issue_labels_updated_default() -> Case[Issue]:
    return Case.branch_result(UPDATED, Issue(id=123, iid=7, title='Review release notes', labels=('review', 'ready'), web_url='https://gitlab.com/meridian/web/-/issues/7'))

setattr(update_issue_labels_updated_default, '__module__', "dex_connectors.gitlab.gitlabmock")


class Mock:

    def __init__(self, connection_name: str = "gitlab") -> None:
        self.get_project_script: Query[GetProjectInput, Project] = Query(GET_PROJECT_DEFINITION)
        self.get_project_script.default(get_project_found_default())
        self.list_issues_script: Query[ListIssuesInput, IssuesPage] = Query(LIST_ISSUES_DEFINITION)
        self.list_issues_script.default(list_issues_listed_default())
        self.update_issue_labels_script: Mutation[UpdateIssueLabelsInput, Issue] = Mutation(UPDATE_ISSUE_LABELS_DEFINITION)
        self.update_issue_labels_script.default(update_issue_labels_updated_default())
        self._connection = new_connection_with_operations(
            get_project=self.get_project_script,
            list_issues=self.list_issues_script,
            update_issue_labels=self.update_issue_labels_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.gitlab.gitlabmock")

__all__ = [
    "Mock",
    "get_project_found",
    "get_project_not_found",
    "get_project_provider_rejected",
    "get_project_invalid_response",
    "get_project_defect",
    "get_project_retry",
    "get_project_found_default",
    "list_issues_listed",
    "list_issues_not_found",
    "list_issues_provider_rejected",
    "list_issues_invalid_response",
    "list_issues_defect",
    "list_issues_retry",
    "list_issues_listed_default",
    "update_issue_labels_updated",
    "update_issue_labels_not_found",
    "update_issue_labels_provider_rejected",
    "update_issue_labels_invalid_response",
    "update_issue_labels_defect",
    "update_issue_labels_retry",
    "update_issue_labels_updated_default",
]
