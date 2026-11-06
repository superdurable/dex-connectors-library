# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import AddCommentInput, AddCommentOutput, CreateIssueInput, CreateIssueOutput, CycleReference, FindUserByEmailInput, GetIssueInput, Issue, IssueLabel, IssueReference, IssueSearchFilter, IssueSummary, ListWorkflowStatesInput, ListWorkflowStatesOutput, ProjectReference, SearchIssuesInput, SearchIssuesOutput, TeamReference, UpdateIssueInput, UpdateIssueOutput, User, UserReference, WorkflowState, WorkflowStateReference
from .definitions import CONNECTOR_ID, ADDED, ADD_COMMENT_DEFINITION, CREATED, CREATE_ISSUE_DEFINITION, DEFECT, FIND_USER_BY_EMAIL_DEFINITION, FOUND, GET_ISSUE_DEFINITION, INVALID_RESPONSE, LISTED, LIST_WORKFLOW_STATES_DEFINITION, NOT_FOUND, PROVIDER_REJECTED, SEARCHED, SEARCH_ISSUES_DEFINITION, UPDATED, UPDATE_ISSUE_DEFINITION

def search_issues_searched(value: SearchIssuesOutput, failure: Failure | None = None) -> Case[SearchIssuesOutput]:
    return Case.branch_result(SEARCHED, value, failure)

setattr(search_issues_searched, '__module__', "dex_connectors.linear.linearmock")

def search_issues_provider_rejected(value: SearchIssuesOutput, failure: Failure | None = None) -> Case[SearchIssuesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(search_issues_provider_rejected, '__module__', "dex_connectors.linear.linearmock")

def search_issues_invalid_response(value: SearchIssuesOutput, failure: Failure | None = None) -> Case[SearchIssuesOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(search_issues_invalid_response, '__module__', "dex_connectors.linear.linearmock")

def search_issues_defect(value: SearchIssuesOutput, failure: Failure | None = None) -> Case[SearchIssuesOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(search_issues_defect, '__module__', "dex_connectors.linear.linearmock")

def search_issues_retry(failure: Failure, after_seconds: float = 0) -> Case[SearchIssuesOutput]:
    return Case.retry(failure, after_seconds)

setattr(search_issues_retry, '__module__', "dex_connectors.linear.linearmock")

def search_issues_engineering_issues() -> Case[SearchIssuesOutput]:
    return Case.paged(SEARCHED, (SearchIssuesOutput(issues=(IssueSummary(id='66666666-6666-4666-8666-666666666666', identifier='ENG-41', title='Quarterly access audit', url='https://linear.app/contoso/issue/ENG-41/quarterly-access-audit', team=TeamReference(id='2f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4', key='ENG', name='Engineering'), state=WorkflowStateReference(id='22222222-2222-4222-8222-222222222222', name='Todo', type='unstarted'), priority=0, created_at='2026-10-20T09:00:00Z', updated_at='2026-10-25T09:00:00Z'),), next_cursor='linear-page-2'), SearchIssuesOutput(issues=(IssueSummary(id='66666666-6666-4666-8666-666666666666', identifier='ENG-41', title='Quarterly access audit', url='https://linear.app/contoso/issue/ENG-41/quarterly-access-audit', team=TeamReference(id='2f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4', key='ENG', name='Engineering'), state=WorkflowStateReference(id='22222222-2222-4222-8222-222222222222', name='Todo', type='unstarted'), priority=0, created_at='2026-10-20T09:00:00Z', updated_at='2026-10-25T09:00:00Z'), IssueSummary(id='77777777-7777-4777-8777-777777777777', identifier='ENG-12', title='Archived fire drill', url='https://linear.app/contoso/issue/ENG-12/archived-fire-drill', team=TeamReference(id='2f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4', key='ENG', name='Engineering'), state=WorkflowStateReference(id='22222222-2222-4222-8222-222222222222', name='Todo', type='unstarted'), priority=0, created_at='2026-09-20T09:00:00Z', updated_at='2026-10-22T09:00:00Z', archived_at='2026-10-23T09:00:00Z')), next_cursor='linear-page-3'), SearchIssuesOutput(issues=(IssueSummary(id='77777777-7777-4777-8777-777777777777', identifier='ENG-12', title='Archived fire drill', url='https://linear.app/contoso/issue/ENG-12/archived-fire-drill', team=TeamReference(id='2f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4', key='ENG', name='Engineering'), state=WorkflowStateReference(id='22222222-2222-4222-8222-222222222222', name='Todo', type='unstarted'), priority=0, created_at='2026-09-20T09:00:00Z', updated_at='2026-10-22T09:00:00Z', archived_at='2026-10-23T09:00:00Z'),))))

setattr(search_issues_engineering_issues, '__module__', "dex_connectors.linear.linearmock")

def search_issues_no_open_fire_drill_issue() -> Case[SearchIssuesOutput]:
    return Case.branch_result(SEARCHED, SearchIssuesOutput(issues=()))

setattr(search_issues_no_open_fire_drill_issue, '__module__', "dex_connectors.linear.linearmock")

def search_issues_invalid_issue_cursor() -> Case[SearchIssuesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, SearchIssuesOutput(), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "searchIssues", "Linear rejected the search cursor."))

setattr(search_issues_invalid_issue_cursor, '__module__', "dex_connectors.linear.linearmock")

def get_issue_found(value: Issue, failure: Failure | None = None) -> Case[Issue]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_issue_found, '__module__', "dex_connectors.linear.linearmock")

def get_issue_not_found(value: Issue, failure: Failure | None = None) -> Case[Issue]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_issue_not_found, '__module__', "dex_connectors.linear.linearmock")

def get_issue_provider_rejected(value: Issue, failure: Failure | None = None) -> Case[Issue]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_issue_provider_rejected, '__module__', "dex_connectors.linear.linearmock")

def get_issue_invalid_response(value: Issue, failure: Failure | None = None) -> Case[Issue]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_issue_invalid_response, '__module__', "dex_connectors.linear.linearmock")

def get_issue_defect(value: Issue, failure: Failure | None = None) -> Case[Issue]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_issue_defect, '__module__', "dex_connectors.linear.linearmock")

def get_issue_retry(failure: Failure, after_seconds: float = 0) -> Case[Issue]:
    return Case.retry(failure, after_seconds)

setattr(get_issue_retry, '__module__', "dex_connectors.linear.linearmock")

def get_issue_moved_fire_drill_issue() -> Case[Issue]:
    return Case.branch_result(FOUND, Issue(id='44444444-4444-4444-8444-444444444444', identifier='ENG-210', title='Monthly Fire Drill Checklist - February', url='https://linear.app/contoso/issue/ENG-210/monthly-fire-drill-checklist-february', team=TeamReference(id='2f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4', key='ENG', name='Engineering'), state=WorkflowStateReference(id='33333333-3333-4333-8333-333333333333', name='In Progress', type='started'), assignee_id='7c3d4e5f-6a7b-4c8d-ae9f-1a2b3c4d5e6f', priority=0, created_at='2026-10-26T09:00:00Z', updated_at='2026-10-26T09:10:00Z', number=210, description='Check every panel on **all** floors.'))

setattr(get_issue_moved_fire_drill_issue, '__module__', "dex_connectors.linear.linearmock")

def get_issue_issue_missing() -> Case[Issue]:
    return Case.branch_result(NOT_FOUND, Issue(team=TeamReference(), state=WorkflowStateReference(type='')), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getIssue", "Linear has no issue with this ID that the connection can see."))

setattr(get_issue_issue_missing, '__module__', "dex_connectors.linear.linearmock")

def get_issue_issue_not_visible() -> Case[Issue]:
    return Case.branch_result(NOT_FOUND, Issue(team=TeamReference(), state=WorkflowStateReference(type='')), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getIssue", "Linear has no issue with this ID that the connection can see."))

setattr(get_issue_issue_not_visible, '__module__', "dex_connectors.linear.linearmock")

def get_issue_issue_read_key_revoked() -> Case[Issue]:
    return Case.branch_result(PROVIDER_REJECTED, Issue(team=TeamReference(), state=WorkflowStateReference(type='')), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "getIssue", "Linear rejected the credential (HTTP 401)"))

setattr(get_issue_issue_read_key_revoked, '__module__', "dex_connectors.linear.linearmock")

def create_issue_created(value: CreateIssueOutput, failure: Failure | None = None) -> Case[CreateIssueOutput]:
    return Case.branch_result(CREATED, value, failure)

setattr(create_issue_created, '__module__', "dex_connectors.linear.linearmock")

def create_issue_provider_rejected(value: CreateIssueOutput, failure: Failure | None = None) -> Case[CreateIssueOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(create_issue_provider_rejected, '__module__', "dex_connectors.linear.linearmock")

def create_issue_defect(value: CreateIssueOutput, failure: Failure | None = None) -> Case[CreateIssueOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(create_issue_defect, '__module__', "dex_connectors.linear.linearmock")

def create_issue_retry(failure: Failure, after_seconds: float = 0) -> Case[CreateIssueOutput]:
    return Case.retry(failure, after_seconds)

setattr(create_issue_retry, '__module__', "dex_connectors.linear.linearmock")

def create_issue_created_fire_drill_issue() -> Case[CreateIssueOutput]:
    return Case.branch_result(CREATED, CreateIssueOutput(team_id='2f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4', title='Monthly Fire Drill Checklist - February', issue_id='44444444-4444-4444-8444-444444444444', issue=IssueSummary(id='44444444-4444-4444-8444-444444444444', identifier='ENG-210', title='Monthly Fire Drill Checklist - February', url='https://linear.app/contoso/issue/ENG-210/monthly-fire-drill-checklist-february', team=TeamReference(id='2f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4', key='ENG', name='Engineering'), state=WorkflowStateReference(id='22222222-2222-4222-8222-222222222222', name='Todo', type='unstarted'), assignee_id='7c3d4e5f-6a7b-4c8d-ae9f-1a2b3c4d5e6f', priority=0, created_at='2026-10-26T09:00:00Z', updated_at='2026-10-26T09:00:00Z')))

setattr(create_issue_created_fire_drill_issue, '__module__', "dex_connectors.linear.linearmock")

def create_issue_unknown_team_rejects_create() -> Case[CreateIssueOutput]:
    return Case.branch_result(PROVIDER_REJECTED, CreateIssueOutput(team_id='00000000-0000-4000-8000-000000000000', title='Monthly Fire Drill Checklist - February'), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "createIssue", "Linear rejected the unknown team."))

setattr(create_issue_unknown_team_rejects_create, '__module__', "dex_connectors.linear.linearmock")

def update_issue_updated(value: UpdateIssueOutput, failure: Failure | None = None) -> Case[UpdateIssueOutput]:
    return Case.branch_result(UPDATED, value, failure)

setattr(update_issue_updated, '__module__', "dex_connectors.linear.linearmock")

def update_issue_not_found(value: UpdateIssueOutput, failure: Failure | None = None) -> Case[UpdateIssueOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(update_issue_not_found, '__module__', "dex_connectors.linear.linearmock")

def update_issue_provider_rejected(value: UpdateIssueOutput, failure: Failure | None = None) -> Case[UpdateIssueOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(update_issue_provider_rejected, '__module__', "dex_connectors.linear.linearmock")

def update_issue_invalid_response(value: UpdateIssueOutput, failure: Failure | None = None) -> Case[UpdateIssueOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(update_issue_invalid_response, '__module__', "dex_connectors.linear.linearmock")

def update_issue_defect(value: UpdateIssueOutput, failure: Failure | None = None) -> Case[UpdateIssueOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(update_issue_defect, '__module__', "dex_connectors.linear.linearmock")

def update_issue_retry(failure: Failure, after_seconds: float = 0) -> Case[UpdateIssueOutput]:
    return Case.retry(failure, after_seconds)

setattr(update_issue_retry, '__module__', "dex_connectors.linear.linearmock")

def update_issue_moved_fire_drill_to_progress() -> Case[UpdateIssueOutput]:
    return Case.branch_result(UPDATED, UpdateIssueOutput(issue_id='44444444-4444-4444-8444-444444444444', issue=IssueSummary(id='44444444-4444-4444-8444-444444444444', identifier='ENG-210', title='Monthly Fire Drill Checklist - February', url='https://linear.app/contoso/issue/ENG-210/monthly-fire-drill-checklist-february', team=TeamReference(id='2f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4', key='ENG', name='Engineering'), state=WorkflowStateReference(id='33333333-3333-4333-8333-333333333333', name='In Progress', type='started'), assignee_id='7c3d4e5f-6a7b-4c8d-ae9f-1a2b3c4d5e6f', priority=0, created_at='2026-10-26T09:00:00Z', updated_at='2026-10-26T09:10:00Z')))

setattr(update_issue_moved_fire_drill_to_progress, '__module__', "dex_connectors.linear.linearmock")

def update_issue_update_missing_issue() -> Case[UpdateIssueOutput]:
    return Case.branch_result(NOT_FOUND, UpdateIssueOutput(issue_id='ENG-999'), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "updateIssue", "Linear has no issue with this ID that the connection can see."))

setattr(update_issue_update_missing_issue, '__module__', "dex_connectors.linear.linearmock")

def update_issue_state_from_another_team() -> Case[UpdateIssueOutput]:
    return Case.branch_result(PROVIDER_REJECTED, UpdateIssueOutput(), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "updateIssue", "Linear rejected the workflow state of another team."))

setattr(update_issue_state_from_another_team, '__module__', "dex_connectors.linear.linearmock")

def add_comment_added(value: AddCommentOutput, failure: Failure | None = None) -> Case[AddCommentOutput]:
    return Case.branch_result(ADDED, value, failure)

setattr(add_comment_added, '__module__', "dex_connectors.linear.linearmock")

def add_comment_not_found(value: AddCommentOutput, failure: Failure | None = None) -> Case[AddCommentOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(add_comment_not_found, '__module__', "dex_connectors.linear.linearmock")

def add_comment_provider_rejected(value: AddCommentOutput, failure: Failure | None = None) -> Case[AddCommentOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(add_comment_provider_rejected, '__module__', "dex_connectors.linear.linearmock")

def add_comment_defect(value: AddCommentOutput, failure: Failure | None = None) -> Case[AddCommentOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(add_comment_defect, '__module__', "dex_connectors.linear.linearmock")

def add_comment_retry(failure: Failure, after_seconds: float = 0) -> Case[AddCommentOutput]:
    return Case.retry(failure, after_seconds)

setattr(add_comment_retry, '__module__', "dex_connectors.linear.linearmock")

def add_comment_commented_fire_drill() -> Case[AddCommentOutput]:
    return Case.branch_result(ADDED, AddCommentOutput(issue_id='44444444-4444-4444-8444-444444444444', comment_id='55555555-5555-4555-8555-555555555555', url='https://linear.app/contoso/issue/ENG-210/monthly-fire-drill-checklist-february#comment-55555555', created_at='2026-10-26T09:11:00Z'))

setattr(add_comment_commented_fire_drill, '__module__', "dex_connectors.linear.linearmock")

def add_comment_comment_issue_missing() -> Case[AddCommentOutput]:
    return Case.branch_result(NOT_FOUND, AddCommentOutput(issue_id='ENG-999'), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "addComment", "Linear has no issue with this ID that the connection can see."))

setattr(add_comment_comment_issue_missing, '__module__', "dex_connectors.linear.linearmock")

def add_comment_comment_forbidden() -> Case[AddCommentOutput]:
    return Case.branch_result(PROVIDER_REJECTED, AddCommentOutput(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "addComment", "Linear denied permission to comment."))

setattr(add_comment_comment_forbidden, '__module__', "dex_connectors.linear.linearmock")

def find_user_by_email_found(value: User, failure: Failure | None = None) -> Case[User]:
    return Case.branch_result(FOUND, value, failure)

setattr(find_user_by_email_found, '__module__', "dex_connectors.linear.linearmock")

def find_user_by_email_not_found(value: User, failure: Failure | None = None) -> Case[User]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(find_user_by_email_not_found, '__module__', "dex_connectors.linear.linearmock")

def find_user_by_email_provider_rejected(value: User, failure: Failure | None = None) -> Case[User]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(find_user_by_email_provider_rejected, '__module__', "dex_connectors.linear.linearmock")

def find_user_by_email_invalid_response(value: User, failure: Failure | None = None) -> Case[User]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(find_user_by_email_invalid_response, '__module__', "dex_connectors.linear.linearmock")

def find_user_by_email_defect(value: User, failure: Failure | None = None) -> Case[User]:
    return Case.branch_result(DEFECT, value, failure)

setattr(find_user_by_email_defect, '__module__', "dex_connectors.linear.linearmock")

def find_user_by_email_retry(failure: Failure, after_seconds: float = 0) -> Case[User]:
    return Case.retry(failure, after_seconds)

setattr(find_user_by_email_retry, '__module__', "dex_connectors.linear.linearmock")

def find_user_by_email_alice_assignee() -> Case[User]:
    return Case.branch_result(FOUND, User(id='7c3d4e5f-6a7b-4c8d-ae9f-1a2b3c4d5e6f', name='Alice Nguyen', display_name='alice', email='alice@example.com', active=True, url='https://linear.app/contoso/profiles/alice'))

setattr(find_user_by_email_alice_assignee, '__module__', "dex_connectors.linear.linearmock")

def find_user_by_email_assignee_missing() -> Case[User]:
    return Case.branch_result(NOT_FOUND, User(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "findUserByEmail", "Linear has no active user with that email."))

setattr(find_user_by_email_assignee_missing, '__module__', "dex_connectors.linear.linearmock")

def find_user_by_email_assignee_lookup_forbidden() -> Case[User]:
    return Case.branch_result(PROVIDER_REJECTED, User(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "findUserByEmail", "Linear denied permission to find users."))

setattr(find_user_by_email_assignee_lookup_forbidden, '__module__', "dex_connectors.linear.linearmock")

def list_workflow_states_listed(value: ListWorkflowStatesOutput, failure: Failure | None = None) -> Case[ListWorkflowStatesOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_workflow_states_listed, '__module__', "dex_connectors.linear.linearmock")

def list_workflow_states_not_found(value: ListWorkflowStatesOutput, failure: Failure | None = None) -> Case[ListWorkflowStatesOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(list_workflow_states_not_found, '__module__', "dex_connectors.linear.linearmock")

def list_workflow_states_provider_rejected(value: ListWorkflowStatesOutput, failure: Failure | None = None) -> Case[ListWorkflowStatesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_workflow_states_provider_rejected, '__module__', "dex_connectors.linear.linearmock")

def list_workflow_states_invalid_response(value: ListWorkflowStatesOutput, failure: Failure | None = None) -> Case[ListWorkflowStatesOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_workflow_states_invalid_response, '__module__', "dex_connectors.linear.linearmock")

def list_workflow_states_defect(value: ListWorkflowStatesOutput, failure: Failure | None = None) -> Case[ListWorkflowStatesOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_workflow_states_defect, '__module__', "dex_connectors.linear.linearmock")

def list_workflow_states_retry(failure: Failure, after_seconds: float = 0) -> Case[ListWorkflowStatesOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_workflow_states_retry, '__module__', "dex_connectors.linear.linearmock")

def list_workflow_states_engineering_states() -> Case[ListWorkflowStatesOutput]:
    return Case.branch_result(LISTED, ListWorkflowStatesOutput(team_id='2f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4', states=(WorkflowState(id='11111111-1111-4111-8111-111111111111', name='Backlog', type='backlog', position=0, color='#bec2c8'), WorkflowState(id='22222222-2222-4222-8222-222222222222', name='Todo', type='unstarted', position=0, color='#e2e2e2'), WorkflowState(id='33333333-3333-4333-8333-333333333333', name='In Progress', type='started', position=0, color='#f2c94c'), WorkflowState(id='99999999-9999-4999-8999-999999999999', name='Done', type='completed', position=0, color='#5e6ad2'))))

setattr(list_workflow_states_engineering_states, '__module__', "dex_connectors.linear.linearmock")

def list_workflow_states_team_missing() -> Case[ListWorkflowStatesOutput]:
    return Case.branch_result(NOT_FOUND, ListWorkflowStatesOutput(team_id='00000000-0000-4000-8000-000000000000', states=()), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "listWorkflowStates", "Linear has no team with this ID that the connection can see."))

setattr(list_workflow_states_team_missing, '__module__', "dex_connectors.linear.linearmock")

def list_workflow_states_team_states_not_visible() -> Case[ListWorkflowStatesOutput]:
    return Case.branch_result(NOT_FOUND, ListWorkflowStatesOutput(team_id='2f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4', states=()), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "listWorkflowStates", "Linear has no team with this ID that the connection can see."))

setattr(list_workflow_states_team_states_not_visible, '__module__', "dex_connectors.linear.linearmock")

def list_workflow_states_team_states_key_revoked() -> Case[ListWorkflowStatesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, ListWorkflowStatesOutput(team_id='2f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4', states=()), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "listWorkflowStates", "Linear rejected the credential (HTTP 401)"))

setattr(list_workflow_states_team_states_key_revoked, '__module__', "dex_connectors.linear.linearmock")


class Mock:

    def __init__(self, connection_name: str = "linear") -> None:
        self.search_issues_script: Query[SearchIssuesInput, SearchIssuesOutput] = Query(SEARCH_ISSUES_DEFINITION)
        self.search_issues_script.default(search_issues_engineering_issues())
        self.get_issue_script: Query[GetIssueInput, Issue] = Query(GET_ISSUE_DEFINITION)
        self.get_issue_script.default(get_issue_moved_fire_drill_issue())
        self.create_issue_script: Mutation[CreateIssueInput, CreateIssueOutput] = Mutation(CREATE_ISSUE_DEFINITION)
        self.create_issue_script.default(create_issue_created_fire_drill_issue())
        self.update_issue_script: Mutation[UpdateIssueInput, UpdateIssueOutput] = Mutation(UPDATE_ISSUE_DEFINITION)
        self.update_issue_script.default(update_issue_moved_fire_drill_to_progress())
        self.add_comment_script: Mutation[AddCommentInput, AddCommentOutput] = Mutation(ADD_COMMENT_DEFINITION)
        self.add_comment_script.default(add_comment_commented_fire_drill())
        self.find_user_by_email_script: Query[FindUserByEmailInput, User] = Query(FIND_USER_BY_EMAIL_DEFINITION)
        self.find_user_by_email_script.default(find_user_by_email_alice_assignee())
        self.list_workflow_states_script: Query[ListWorkflowStatesInput, ListWorkflowStatesOutput] = Query(LIST_WORKFLOW_STATES_DEFINITION)
        self.list_workflow_states_script.default(list_workflow_states_engineering_states())
        self._connection = new_connection_with_operations(
            search_issues=self.search_issues_script,
            get_issue=self.get_issue_script,
            create_issue=self.create_issue_script,
            update_issue=self.update_issue_script,
            add_comment=self.add_comment_script,
            find_user_by_email=self.find_user_by_email_script,
            list_workflow_states=self.list_workflow_states_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.linear.linearmock")

__all__ = [
    "Mock",
    "search_issues_searched",
    "search_issues_provider_rejected",
    "search_issues_invalid_response",
    "search_issues_defect",
    "search_issues_retry",
    "search_issues_engineering_issues",
    "search_issues_no_open_fire_drill_issue",
    "search_issues_invalid_issue_cursor",
    "get_issue_found",
    "get_issue_not_found",
    "get_issue_provider_rejected",
    "get_issue_invalid_response",
    "get_issue_defect",
    "get_issue_retry",
    "get_issue_moved_fire_drill_issue",
    "get_issue_issue_missing",
    "get_issue_issue_not_visible",
    "get_issue_issue_read_key_revoked",
    "create_issue_created",
    "create_issue_provider_rejected",
    "create_issue_defect",
    "create_issue_retry",
    "create_issue_created_fire_drill_issue",
    "create_issue_unknown_team_rejects_create",
    "update_issue_updated",
    "update_issue_not_found",
    "update_issue_provider_rejected",
    "update_issue_invalid_response",
    "update_issue_defect",
    "update_issue_retry",
    "update_issue_moved_fire_drill_to_progress",
    "update_issue_update_missing_issue",
    "update_issue_state_from_another_team",
    "add_comment_added",
    "add_comment_not_found",
    "add_comment_provider_rejected",
    "add_comment_defect",
    "add_comment_retry",
    "add_comment_commented_fire_drill",
    "add_comment_comment_issue_missing",
    "add_comment_comment_forbidden",
    "find_user_by_email_found",
    "find_user_by_email_not_found",
    "find_user_by_email_provider_rejected",
    "find_user_by_email_invalid_response",
    "find_user_by_email_defect",
    "find_user_by_email_retry",
    "find_user_by_email_alice_assignee",
    "find_user_by_email_assignee_missing",
    "find_user_by_email_assignee_lookup_forbidden",
    "list_workflow_states_listed",
    "list_workflow_states_not_found",
    "list_workflow_states_provider_rejected",
    "list_workflow_states_invalid_response",
    "list_workflow_states_defect",
    "list_workflow_states_retry",
    "list_workflow_states_engineering_states",
    "list_workflow_states_team_missing",
    "list_workflow_states_team_states_not_visible",
    "list_workflow_states_team_states_key_revoked",
]
