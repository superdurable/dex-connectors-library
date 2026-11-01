# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetIssueInput, Issue, IssuePage, ListIssuesInput, UpdateIssueStatusInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_ISSUE_DEFINITION, INVALID_RESPONSE, LISTED, LIST_ISSUES_DEFINITION, NOT_FOUND, PROVIDER_REJECTED, UPDATED, UPDATE_ISSUE_STATUS_DEFINITION

def list_issues_listed(value: IssuePage, failure: Failure | None = None) -> Case[IssuePage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_issues_listed, '__module__', "dex_connectors.sentry.sentrymock")

def list_issues_provider_rejected(value: IssuePage, failure: Failure | None = None) -> Case[IssuePage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_issues_provider_rejected, '__module__', "dex_connectors.sentry.sentrymock")

def list_issues_invalid_response(value: IssuePage, failure: Failure | None = None) -> Case[IssuePage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_issues_invalid_response, '__module__', "dex_connectors.sentry.sentrymock")

def list_issues_defect(value: IssuePage, failure: Failure | None = None) -> Case[IssuePage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_issues_defect, '__module__', "dex_connectors.sentry.sentrymock")

def list_issues_retry(failure: Failure, after_seconds: float = 0) -> Case[IssuePage]:
    return Case.retry(failure, after_seconds)

setattr(list_issues_retry, '__module__', "dex_connectors.sentry.sentrymock")

def list_issues_default() -> Case[IssuePage]:
    return Case.branch_result(LISTED, IssuePage(issues=(Issue(id='123', short_id='APP-1', title='ExampleError', status='unresolved'),)))

setattr(list_issues_default, '__module__', "dex_connectors.sentry.sentrymock")

def get_issue_found(value: Issue, failure: Failure | None = None) -> Case[Issue]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_issue_found, '__module__', "dex_connectors.sentry.sentrymock")

def get_issue_not_found(value: Issue, failure: Failure | None = None) -> Case[Issue]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_issue_not_found, '__module__', "dex_connectors.sentry.sentrymock")

def get_issue_provider_rejected(value: Issue, failure: Failure | None = None) -> Case[Issue]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_issue_provider_rejected, '__module__', "dex_connectors.sentry.sentrymock")

def get_issue_invalid_response(value: Issue, failure: Failure | None = None) -> Case[Issue]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_issue_invalid_response, '__module__', "dex_connectors.sentry.sentrymock")

def get_issue_defect(value: Issue, failure: Failure | None = None) -> Case[Issue]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_issue_defect, '__module__', "dex_connectors.sentry.sentrymock")

def get_issue_retry(failure: Failure, after_seconds: float = 0) -> Case[Issue]:
    return Case.retry(failure, after_seconds)

setattr(get_issue_retry, '__module__', "dex_connectors.sentry.sentrymock")

def get_issue_default() -> Case[Issue]:
    return Case.branch_result(FOUND, Issue(id='123', short_id='APP-1', title='ExampleError', status='unresolved'))

setattr(get_issue_default, '__module__', "dex_connectors.sentry.sentrymock")

def update_issue_status_updated(value: Issue, failure: Failure | None = None) -> Case[Issue]:
    return Case.branch_result(UPDATED, value, failure)

setattr(update_issue_status_updated, '__module__', "dex_connectors.sentry.sentrymock")

def update_issue_status_not_found(value: Issue, failure: Failure | None = None) -> Case[Issue]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(update_issue_status_not_found, '__module__', "dex_connectors.sentry.sentrymock")

def update_issue_status_provider_rejected(value: Issue, failure: Failure | None = None) -> Case[Issue]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(update_issue_status_provider_rejected, '__module__', "dex_connectors.sentry.sentrymock")

def update_issue_status_invalid_response(value: Issue, failure: Failure | None = None) -> Case[Issue]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(update_issue_status_invalid_response, '__module__', "dex_connectors.sentry.sentrymock")

def update_issue_status_defect(value: Issue, failure: Failure | None = None) -> Case[Issue]:
    return Case.branch_result(DEFECT, value, failure)

setattr(update_issue_status_defect, '__module__', "dex_connectors.sentry.sentrymock")

def update_issue_status_retry(failure: Failure, after_seconds: float = 0) -> Case[Issue]:
    return Case.retry(failure, after_seconds)

setattr(update_issue_status_retry, '__module__', "dex_connectors.sentry.sentrymock")

def update_issue_status_default() -> Case[Issue]:
    return Case.branch_result(UPDATED, Issue(id='123', short_id='APP-1', title='ExampleError', status='resolved'))

setattr(update_issue_status_default, '__module__', "dex_connectors.sentry.sentrymock")


class Mock:

    def __init__(self, connection_name: str = "sentry") -> None:
        self.list_issues_script: Query[ListIssuesInput, IssuePage] = Query(LIST_ISSUES_DEFINITION)
        self.list_issues_script.default(list_issues_default())
        self.get_issue_script: Query[GetIssueInput, Issue] = Query(GET_ISSUE_DEFINITION)
        self.get_issue_script.default(get_issue_default())
        self.update_issue_status_script: Mutation[UpdateIssueStatusInput, Issue] = Mutation(UPDATE_ISSUE_STATUS_DEFINITION)
        self.update_issue_status_script.default(update_issue_status_default())
        self._connection = new_connection_with_operations(
            list_issues=self.list_issues_script,
            get_issue=self.get_issue_script,
            update_issue_status=self.update_issue_status_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.sentry.sentrymock")

__all__ = [
    "Mock",
    "list_issues_listed",
    "list_issues_provider_rejected",
    "list_issues_invalid_response",
    "list_issues_defect",
    "list_issues_retry",
    "list_issues_default",
    "get_issue_found",
    "get_issue_not_found",
    "get_issue_provider_rejected",
    "get_issue_invalid_response",
    "get_issue_defect",
    "get_issue_retry",
    "get_issue_default",
    "update_issue_status_updated",
    "update_issue_status_not_found",
    "update_issue_status_provider_rejected",
    "update_issue_status_invalid_response",
    "update_issue_status_defect",
    "update_issue_status_retry",
    "update_issue_status_default",
]
