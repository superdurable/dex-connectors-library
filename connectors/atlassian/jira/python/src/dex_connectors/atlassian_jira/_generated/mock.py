# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import AccountReference, AddCommentInput, AddCommentOutput, CreateIssueInput, CreateIssueOutput, GetIssueInput, Issue, IssueSearchFilter, IssueStatus, IssueTypeReference, PriorityReference, ProjectReference, SearchIssuesInput, SearchIssuesOutput, TransitionIssueInput, TransitionIssueOutput, TransitionReference
from .definitions import CONNECTOR_ID, PROVIDER, ADDED, ADD_COMMENT_DEFINITION, CREATED, CREATE_ISSUE_DEFINITION, DEFECT, FOUND, GET_ISSUE_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED, SEARCHED, SEARCH_ISSUES_DEFINITION, TRANSITIONED, TRANSITION_ISSUE_DEFINITION, TRANSITION_UNAVAILABLE, UNCERTAIN

def search_issues_searched(value: SearchIssuesOutput, failure: Failure | None = None) -> Case[SearchIssuesOutput]:
    return Case.branch_result(SEARCHED, value, failure)

setattr(search_issues_searched, '__module__', "dex_connectors.atlassian_jira.jiramock")

def search_issues_provider_rejected(value: SearchIssuesOutput, failure: Failure | None = None) -> Case[SearchIssuesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(search_issues_provider_rejected, '__module__', "dex_connectors.atlassian_jira.jiramock")

def search_issues_invalid_response(value: SearchIssuesOutput, failure: Failure | None = None) -> Case[SearchIssuesOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(search_issues_invalid_response, '__module__', "dex_connectors.atlassian_jira.jiramock")

def search_issues_defect(value: SearchIssuesOutput, failure: Failure | None = None) -> Case[SearchIssuesOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(search_issues_defect, '__module__', "dex_connectors.atlassian_jira.jiramock")

def search_issues_retry(failure: Failure, after_seconds: float = 0) -> Case[SearchIssuesOutput]:
    return Case.retry(failure, after_seconds)

setattr(search_issues_retry, '__module__', "dex_connectors.atlassian_jira.jiramock")

def search_issues_searched_default_answer() -> Case[SearchIssuesOutput]:
    return Case.branch_result(SEARCHED, SearchIssuesOutput(jql='project in ("OPS") AND statusCategory in (2, 4) AND summary ~ "\\"Loading dock light out\\"" ORDER BY created DESC', issues=()))

setattr(search_issues_searched_default_answer, '__module__', "dex_connectors.atlassian_jira.jiramock")

def search_issues_provider_rejected_failure() -> Case[SearchIssuesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, SearchIssuesOutput(), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "searchIssues", "Jira rejected the search query."))

setattr(search_issues_provider_rejected_failure, '__module__', "dex_connectors.atlassian_jira.jiramock")

def search_issues_invalid_response_failure() -> Case[SearchIssuesOutput]:
    return Case.branch_result(INVALID_RESPONSE, SearchIssuesOutput(), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "searchIssues", "Jira returned an invalid search page."))

setattr(search_issues_invalid_response_failure, '__module__', "dex_connectors.atlassian_jira.jiramock")

def get_issue_found(value: Issue, failure: Failure | None = None) -> Case[Issue]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_issue_found, '__module__', "dex_connectors.atlassian_jira.jiramock")

def get_issue_not_found(value: Issue, failure: Failure | None = None) -> Case[Issue]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_issue_not_found, '__module__', "dex_connectors.atlassian_jira.jiramock")

def get_issue_provider_rejected(value: Issue, failure: Failure | None = None) -> Case[Issue]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_issue_provider_rejected, '__module__', "dex_connectors.atlassian_jira.jiramock")

def get_issue_invalid_response(value: Issue, failure: Failure | None = None) -> Case[Issue]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_issue_invalid_response, '__module__', "dex_connectors.atlassian_jira.jiramock")

def get_issue_defect(value: Issue, failure: Failure | None = None) -> Case[Issue]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_issue_defect, '__module__', "dex_connectors.atlassian_jira.jiramock")

def get_issue_retry(failure: Failure, after_seconds: float = 0) -> Case[Issue]:
    return Case.retry(failure, after_seconds)

setattr(get_issue_retry, '__module__', "dex_connectors.atlassian_jira.jiramock")

def get_issue_found_default_answer() -> Case[Issue]:
    return Case.branch_result(FOUND, Issue(id='10004', key='OPS-4', summary='Loading dock light out', status=IssueStatus(id='1', name='To Do', category_key='new'), issue_type=IssueTypeReference(id='10001', name='Task'), project=ProjectReference(id='10000', key='OPS', name='Operations'), labels=('facilities',), created_at=datetime.fromisoformat("2026-09-30T09:15:00Z".replace('Z', '+00:00')), updated_at=datetime.fromisoformat("2026-09-30T09:15:00Z".replace('Z', '+00:00'))))

setattr(get_issue_found_default_answer, '__module__', "dex_connectors.atlassian_jira.jiramock")

def get_issue_not_found_failure() -> Case[Issue]:
    return Case.branch_result(NOT_FOUND, Issue(status=IssueStatus(), issue_type=IssueTypeReference(), project=ProjectReference()), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getIssue", "Jira issue was not found."))

setattr(get_issue_not_found_failure, '__module__', "dex_connectors.atlassian_jira.jiramock")

def get_issue_provider_rejected_failure() -> Case[Issue]:
    return Case.branch_result(PROVIDER_REJECTED, Issue(status=IssueStatus(), issue_type=IssueTypeReference(), project=ProjectReference()), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "getIssue", "Jira rejected the issue read."))

setattr(get_issue_provider_rejected_failure, '__module__', "dex_connectors.atlassian_jira.jiramock")

def get_issue_invalid_response_failure() -> Case[Issue]:
    return Case.branch_result(INVALID_RESPONSE, Issue(status=IssueStatus(), issue_type=IssueTypeReference(), project=ProjectReference()), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "getIssue", "Jira returned an invalid issue."))

setattr(get_issue_invalid_response_failure, '__module__', "dex_connectors.atlassian_jira.jiramock")

def create_issue_created(value: CreateIssueOutput, failure: Failure | None = None) -> Case[CreateIssueOutput]:
    return Case.branch_result(CREATED, value, failure)

setattr(create_issue_created, '__module__', "dex_connectors.atlassian_jira.jiramock")

def create_issue_provider_rejected(value: CreateIssueOutput, failure: Failure | None = None) -> Case[CreateIssueOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(create_issue_provider_rejected, '__module__', "dex_connectors.atlassian_jira.jiramock")

def create_issue_uncertain(value: CreateIssueOutput, failure: Failure) -> Case[CreateIssueOutput]:
    return Case.uncertain(value, failure)

setattr(create_issue_uncertain, '__module__', "dex_connectors.atlassian_jira.jiramock")

def create_issue_defect(value: CreateIssueOutput, failure: Failure | None = None) -> Case[CreateIssueOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(create_issue_defect, '__module__', "dex_connectors.atlassian_jira.jiramock")

def create_issue_retry(failure: Failure, after_seconds: float = 0) -> Case[CreateIssueOutput]:
    return Case.retry(failure, after_seconds)

setattr(create_issue_retry, '__module__', "dex_connectors.atlassian_jira.jiramock")

def create_issue_created_default_answer() -> Case[CreateIssueOutput]:
    return Case.branch_result(CREATED, CreateIssueOutput(issue_id='10004', issue_key='OPS-4', project_key='OPS', summary='Loading dock light out'))

setattr(create_issue_created_default_answer, '__module__', "dex_connectors.atlassian_jira.jiramock")

def create_issue_provider_rejected_failure() -> Case[CreateIssueOutput]:
    return Case.branch_result(PROVIDER_REJECTED, CreateIssueOutput(project_key='OPS', summary='Loading dock light out', rejected_field_ids=('issuetype',)), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "createIssue", "Jira rejected the issue type."))

setattr(create_issue_provider_rejected_failure, '__module__', "dex_connectors.atlassian_jira.jiramock")

def transition_issue_transitioned(value: TransitionIssueOutput, failure: Failure | None = None) -> Case[TransitionIssueOutput]:
    return Case.branch_result(TRANSITIONED, value, failure)

setattr(transition_issue_transitioned, '__module__', "dex_connectors.atlassian_jira.jiramock")

def transition_issue_not_found(value: TransitionIssueOutput, failure: Failure | None = None) -> Case[TransitionIssueOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(transition_issue_not_found, '__module__', "dex_connectors.atlassian_jira.jiramock")

def transition_issue_transition_unavailable(value: TransitionIssueOutput, failure: Failure | None = None) -> Case[TransitionIssueOutput]:
    return Case.branch_result(TRANSITION_UNAVAILABLE, value, failure)

setattr(transition_issue_transition_unavailable, '__module__', "dex_connectors.atlassian_jira.jiramock")

def transition_issue_provider_rejected(value: TransitionIssueOutput, failure: Failure | None = None) -> Case[TransitionIssueOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(transition_issue_provider_rejected, '__module__', "dex_connectors.atlassian_jira.jiramock")

def transition_issue_invalid_response(value: TransitionIssueOutput, failure: Failure | None = None) -> Case[TransitionIssueOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(transition_issue_invalid_response, '__module__', "dex_connectors.atlassian_jira.jiramock")

def transition_issue_defect(value: TransitionIssueOutput, failure: Failure | None = None) -> Case[TransitionIssueOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(transition_issue_defect, '__module__', "dex_connectors.atlassian_jira.jiramock")

def transition_issue_retry(failure: Failure, after_seconds: float = 0) -> Case[TransitionIssueOutput]:
    return Case.retry(failure, after_seconds)

setattr(transition_issue_retry, '__module__', "dex_connectors.atlassian_jira.jiramock")

def transition_issue_transitioned_default_answer() -> Case[TransitionIssueOutput]:
    return Case.branch_result(TRANSITIONED, TransitionIssueOutput(issue_id='10004', issue_key='OPS-4', previous_status=IssueStatus(id='1', name='To Do', category_key='new'), status=IssueStatus(id='3', name='In Progress', category_key='indeterminate'), transition=TransitionReference(id='11', name='Start Progress', destination_status=IssueStatus(id='3', name='In Progress', category_key='indeterminate'))))

setattr(transition_issue_transitioned_default_answer, '__module__', "dex_connectors.atlassian_jira.jiramock")

def transition_issue_not_found_failure() -> Case[TransitionIssueOutput]:
    return Case.branch_result(NOT_FOUND, TransitionIssueOutput(previous_status=IssueStatus(), status=IssueStatus()), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "transitionIssue", "Jira issue was not found."))

setattr(transition_issue_not_found_failure, '__module__', "dex_connectors.atlassian_jira.jiramock")

def transition_issue_transition_unavailable_failure() -> Case[TransitionIssueOutput]:
    return Case.branch_result(TRANSITION_UNAVAILABLE, TransitionIssueOutput(previous_status=IssueStatus(), status=IssueStatus()), Failure(FailureKind.CONFLICT, CONNECTOR_ID, "transitionIssue", "Jira transition is unavailable."))

setattr(transition_issue_transition_unavailable_failure, '__module__', "dex_connectors.atlassian_jira.jiramock")

def transition_issue_provider_rejected_failure() -> Case[TransitionIssueOutput]:
    return Case.branch_result(PROVIDER_REJECTED, TransitionIssueOutput(previous_status=IssueStatus(), status=IssueStatus()), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "transitionIssue", "Jira rejected the transition."))

setattr(transition_issue_provider_rejected_failure, '__module__', "dex_connectors.atlassian_jira.jiramock")

def transition_issue_invalid_response_failure() -> Case[TransitionIssueOutput]:
    return Case.branch_result(INVALID_RESPONSE, TransitionIssueOutput(previous_status=IssueStatus(), status=IssueStatus()), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "transitionIssue", "Jira returned an invalid transition list."))

setattr(transition_issue_invalid_response_failure, '__module__', "dex_connectors.atlassian_jira.jiramock")

def add_comment_added(value: AddCommentOutput, failure: Failure | None = None) -> Case[AddCommentOutput]:
    return Case.branch_result(ADDED, value, failure)

setattr(add_comment_added, '__module__', "dex_connectors.atlassian_jira.jiramock")

def add_comment_not_found(value: AddCommentOutput, failure: Failure | None = None) -> Case[AddCommentOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(add_comment_not_found, '__module__', "dex_connectors.atlassian_jira.jiramock")

def add_comment_provider_rejected(value: AddCommentOutput, failure: Failure | None = None) -> Case[AddCommentOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(add_comment_provider_rejected, '__module__', "dex_connectors.atlassian_jira.jiramock")

def add_comment_uncertain(value: AddCommentOutput, failure: Failure) -> Case[AddCommentOutput]:
    return Case.uncertain(value, failure)

setattr(add_comment_uncertain, '__module__', "dex_connectors.atlassian_jira.jiramock")

def add_comment_defect(value: AddCommentOutput, failure: Failure | None = None) -> Case[AddCommentOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(add_comment_defect, '__module__', "dex_connectors.atlassian_jira.jiramock")

def add_comment_retry(failure: Failure, after_seconds: float = 0) -> Case[AddCommentOutput]:
    return Case.retry(failure, after_seconds)

setattr(add_comment_retry, '__module__', "dex_connectors.atlassian_jira.jiramock")

def add_comment_added_default_answer() -> Case[AddCommentOutput]:
    return Case.branch_result(ADDED, AddCommentOutput(issue_id_or_key='OPS-4', comment_id='10501', author_account_id='5b10ac8d82e05b22cc7d4ef5', created_at=datetime.fromisoformat("2026-09-30T10:05:00Z".replace('Z', '+00:00'))))

setattr(add_comment_added_default_answer, '__module__', "dex_connectors.atlassian_jira.jiramock")

def add_comment_not_found_failure() -> Case[AddCommentOutput]:
    return Case.branch_result(NOT_FOUND, AddCommentOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "addComment", "Jira issue was not found."))

setattr(add_comment_not_found_failure, '__module__', "dex_connectors.atlassian_jira.jiramock")

def add_comment_provider_rejected_failure() -> Case[AddCommentOutput]:
    return Case.branch_result(PROVIDER_REJECTED, AddCommentOutput(), Failure(FailureKind.PROVIDER_REJECTION, CONNECTOR_ID, "addComment", "Jira rejected the comment."))

setattr(add_comment_provider_rejected_failure, '__module__', "dex_connectors.atlassian_jira.jiramock")


class Mock:

    def __init__(self, connection_name: str = "jira") -> None:
        self.search_issues_script: Query[SearchIssuesInput, SearchIssuesOutput] = Query(SEARCH_ISSUES_DEFINITION)
        self.search_issues_script.default(search_issues_searched_default_answer())
        self.get_issue_script: Query[GetIssueInput, Issue] = Query(GET_ISSUE_DEFINITION)
        self.get_issue_script.default(get_issue_found_default_answer())
        self.create_issue_script: Mutation[CreateIssueInput, CreateIssueOutput] = Mutation(CREATE_ISSUE_DEFINITION)
        self.create_issue_script.default(create_issue_created_default_answer())
        self.transition_issue_script: Mutation[TransitionIssueInput, TransitionIssueOutput] = Mutation(TRANSITION_ISSUE_DEFINITION)
        self.transition_issue_script.default(transition_issue_transitioned_default_answer())
        self.add_comment_script: Mutation[AddCommentInput, AddCommentOutput] = Mutation(ADD_COMMENT_DEFINITION)
        self.add_comment_script.default(add_comment_added_default_answer())
        self._connection = new_connection_with_operations(
            search_issues=self.search_issues_script,
            get_issue=self.get_issue_script,
            create_issue=self.create_issue_script,
            transition_issue=self.transition_issue_script,
            add_comment=self.add_comment_script,
            reference=ConnectionRef(PROVIDER, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.atlassian_jira.jiramock")

__all__ = [
    "Mock",
    "search_issues_searched",
    "search_issues_provider_rejected",
    "search_issues_invalid_response",
    "search_issues_defect",
    "search_issues_retry",
    "search_issues_searched_default_answer",
    "search_issues_provider_rejected_failure",
    "search_issues_invalid_response_failure",
    "get_issue_found",
    "get_issue_not_found",
    "get_issue_provider_rejected",
    "get_issue_invalid_response",
    "get_issue_defect",
    "get_issue_retry",
    "get_issue_found_default_answer",
    "get_issue_not_found_failure",
    "get_issue_provider_rejected_failure",
    "get_issue_invalid_response_failure",
    "create_issue_created",
    "create_issue_provider_rejected",
    "create_issue_uncertain",
    "create_issue_defect",
    "create_issue_retry",
    "create_issue_created_default_answer",
    "create_issue_provider_rejected_failure",
    "transition_issue_transitioned",
    "transition_issue_not_found",
    "transition_issue_transition_unavailable",
    "transition_issue_provider_rejected",
    "transition_issue_invalid_response",
    "transition_issue_defect",
    "transition_issue_retry",
    "transition_issue_transitioned_default_answer",
    "transition_issue_not_found_failure",
    "transition_issue_transition_unavailable_failure",
    "transition_issue_provider_rejected_failure",
    "transition_issue_invalid_response_failure",
    "add_comment_added",
    "add_comment_not_found",
    "add_comment_provider_rejected",
    "add_comment_uncertain",
    "add_comment_defect",
    "add_comment_retry",
    "add_comment_added_default_answer",
    "add_comment_not_found_failure",
    "add_comment_provider_rejected_failure",
]
