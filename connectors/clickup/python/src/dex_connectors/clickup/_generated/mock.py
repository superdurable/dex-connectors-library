# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import AddCommentInput, AddCommentOutput, CreateTaskInput, CreateTaskOutput, CustomFieldInput, CustomFieldValue, FindMemberByEmailInput, GetTaskInput, Member, SearchTasksInput, SearchTasksOutput, Task, TaskStatus, UpdateTaskInput, UpdateTaskOutput, UpdateTaskTagsInput, UpdateTaskTagsOutput, User
from .definitions import CONNECTOR_ID, ADDED, ADD_COMMENT_DEFINITION, CREATED, CREATE_TASK_DEFINITION, DEFECT, FIND_MEMBER_BY_EMAIL_DEFINITION, FOUND, GET_TASK_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED, SEARCHED, SEARCH_TASKS_DEFINITION, UNCERTAIN, UPDATED, UPDATE_TASK_DEFINITION, UPDATE_TASK_TAGS_DEFINITION

def search_tasks_searched(value: SearchTasksOutput, failure: Failure | None = None) -> Case[SearchTasksOutput]:
    return Case.branch_result(SEARCHED, value, failure)

setattr(search_tasks_searched, '__module__', "dex_connectors.clickup.clickupmock")

def search_tasks_not_found(value: SearchTasksOutput, failure: Failure | None = None) -> Case[SearchTasksOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(search_tasks_not_found, '__module__', "dex_connectors.clickup.clickupmock")

def search_tasks_provider_rejected(value: SearchTasksOutput, failure: Failure | None = None) -> Case[SearchTasksOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(search_tasks_provider_rejected, '__module__', "dex_connectors.clickup.clickupmock")

def search_tasks_invalid_response(value: SearchTasksOutput, failure: Failure | None = None) -> Case[SearchTasksOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(search_tasks_invalid_response, '__module__', "dex_connectors.clickup.clickupmock")

def search_tasks_defect(value: SearchTasksOutput, failure: Failure | None = None) -> Case[SearchTasksOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(search_tasks_defect, '__module__', "dex_connectors.clickup.clickupmock")

def search_tasks_retry(failure: Failure, after_seconds: float = 0) -> Case[SearchTasksOutput]:
    return Case.retry(failure, after_seconds)

setattr(search_tasks_retry, '__module__', "dex_connectors.clickup.clickupmock")

def search_tasks_unrelated_escalations() -> Case[SearchTasksOutput]:
    return Case.paged(SEARCHED, (SearchTasksOutput(tasks=(Task(id='86b2x2', name='Escalation: another task [86b2x2]', url='https://app.clickup.com/t/86b2x2', status=TaskStatus(name='open', type='open'), assignees=(), tags=('escalation',), workspace_id='9014123456', list_id='901410000099'),), page=0, next_page=1), SearchTasksOutput(tasks=(Task(id='86b2x2', name='Escalation: another task [86b2x2]', url='https://app.clickup.com/t/86b2x2', status=TaskStatus(name='open', type='open'), assignees=(), tags=('escalation',), workspace_id='9014123456', list_id='901410000099'), Task(id='86b2x3', name='Escalation: older task [86b2x3]', url='https://app.clickup.com/t/86b2x3', status=TaskStatus(name='closed', type='closed'), assignees=(), tags=('escalation',), workspace_id='9014123456', list_id='901410000099')), page=1, next_page=2), SearchTasksOutput(tasks=(Task(id='86b2x4', name='Escalation: backlog [86b2x4]', url='https://app.clickup.com/t/86b2x4', status=TaskStatus(name='open', type='open'), assignees=(), tags=('escalation',), workspace_id='9014123456', list_id='901410000099'),), page=2)))

setattr(search_tasks_unrelated_escalations, '__module__', "dex_connectors.clickup.clickupmock")

def search_tasks_unknown_workspace_search() -> Case[SearchTasksOutput]:
    return Case.branch_result(NOT_FOUND, SearchTasksOutput(tasks=(), page=0), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "searchTasks", "ClickUp found no such Workspace."))

setattr(search_tasks_unknown_workspace_search, '__module__', "dex_connectors.clickup.clickupmock")

def search_tasks_search_denied() -> Case[SearchTasksOutput]:
    return Case.branch_result(PROVIDER_REJECTED, SearchTasksOutput(tasks=(), page=0), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "searchTasks", "ClickUp denied the task search."))

setattr(search_tasks_search_denied, '__module__', "dex_connectors.clickup.clickupmock")

def get_task_found(value: Task, failure: Failure | None = None) -> Case[Task]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_task_found, '__module__', "dex_connectors.clickup.clickupmock")

def get_task_not_found(value: Task, failure: Failure | None = None) -> Case[Task]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_task_not_found, '__module__', "dex_connectors.clickup.clickupmock")

def get_task_provider_rejected(value: Task, failure: Failure | None = None) -> Case[Task]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_task_provider_rejected, '__module__', "dex_connectors.clickup.clickupmock")

def get_task_invalid_response(value: Task, failure: Failure | None = None) -> Case[Task]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_task_invalid_response, '__module__', "dex_connectors.clickup.clickupmock")

def get_task_defect(value: Task, failure: Failure | None = None) -> Case[Task]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_task_defect, '__module__', "dex_connectors.clickup.clickupmock")

def get_task_retry(failure: Failure, after_seconds: float = 0) -> Case[Task]:
    return Case.retry(failure, after_seconds)

setattr(get_task_retry, '__module__', "dex_connectors.clickup.clickupmock")

def get_task_blocked_login_task() -> Case[Task]:
    return Case.branch_result(FOUND, Task(id='86b2x1', name='Fix login for SSO customers', url='https://app.clickup.com/t/86b2x1', status=TaskStatus(name='blocked', type='custom'), priority=3, created_at=datetime.fromisoformat("2026-10-20T09:00:00Z".replace('Z', '+00:00')), updated_at=datetime.fromisoformat("2026-10-25T09:00:00Z".replace('Z', '+00:00')), assignees=(), tags=('customer',), workspace_id='9014123456', list_id='901410000001'))

setattr(get_task_blocked_login_task, '__module__', "dex_connectors.clickup.clickupmock")

def get_task_missing_task() -> Case[Task]:
    return Case.branch_result(NOT_FOUND, Task(status=TaskStatus()), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getTask", "ClickUp found no such task."))

setattr(get_task_missing_task, '__module__', "dex_connectors.clickup.clickupmock")

def get_task_task_read_denied() -> Case[Task]:
    return Case.branch_result(PROVIDER_REJECTED, Task(status=TaskStatus()), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "getTask", "ClickUp denied the task read."))

setattr(get_task_task_read_denied, '__module__', "dex_connectors.clickup.clickupmock")

def create_task_created(value: CreateTaskOutput, failure: Failure | None = None) -> Case[CreateTaskOutput]:
    return Case.branch_result(CREATED, value, failure)

setattr(create_task_created, '__module__', "dex_connectors.clickup.clickupmock")

def create_task_not_found(value: CreateTaskOutput, failure: Failure | None = None) -> Case[CreateTaskOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(create_task_not_found, '__module__', "dex_connectors.clickup.clickupmock")

def create_task_provider_rejected(value: CreateTaskOutput, failure: Failure | None = None) -> Case[CreateTaskOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(create_task_provider_rejected, '__module__', "dex_connectors.clickup.clickupmock")

def create_task_uncertain(value: CreateTaskOutput, failure: Failure) -> Case[CreateTaskOutput]:
    return Case.uncertain(value, failure)

setattr(create_task_uncertain, '__module__', "dex_connectors.clickup.clickupmock")

def create_task_defect(value: CreateTaskOutput, failure: Failure | None = None) -> Case[CreateTaskOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(create_task_defect, '__module__', "dex_connectors.clickup.clickupmock")

def create_task_retry(failure: Failure, after_seconds: float = 0) -> Case[CreateTaskOutput]:
    return Case.retry(failure, after_seconds)

setattr(create_task_retry, '__module__', "dex_connectors.clickup.clickupmock")

def create_task_created_login_escalation() -> Case[CreateTaskOutput]:
    return Case.branch_result(CREATED, CreateTaskOutput(list_id='901410000099', name='Escalation: Fix login for SSO customers [86b2x1]', task=Task(id='86b2x9', name='Escalation: Fix login for SSO customers [86b2x1]', url='https://app.clickup.com/t/86b2x9', status=TaskStatus(name='open', type='open'), priority=1, created_at=datetime.fromisoformat("2026-10-25T09:01:00Z".replace('Z', '+00:00')), updated_at=datetime.fromisoformat("2026-10-25T09:01:00Z".replace('Z', '+00:00')), assignees=(User(id=183, username='Ada Lovelace'),), tags=('escalation',), workspace_id='9014123456', list_id='901410000099')))

setattr(create_task_created_login_escalation, '__module__', "dex_connectors.clickup.clickupmock")

def create_task_missing_escalation_list() -> Case[CreateTaskOutput]:
    return Case.branch_result(NOT_FOUND, CreateTaskOutput(list_id='999999999999', name='Escalation: Fix login for SSO customers [86b2x1]', task=Task(status=TaskStatus())), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "createTask", "ClickUp found no such List."))

setattr(create_task_missing_escalation_list, '__module__', "dex_connectors.clickup.clickupmock")

def create_task_create_denied() -> Case[CreateTaskOutput]:
    return Case.branch_result(PROVIDER_REJECTED, CreateTaskOutput(list_id='901410000099', name='Escalation: Fix login for SSO customers [86b2x1]', task=Task(status=TaskStatus())), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "createTask", "ClickUp denied task creation."))

setattr(create_task_create_denied, '__module__', "dex_connectors.clickup.clickupmock")

def update_task_updated(value: UpdateTaskOutput, failure: Failure | None = None) -> Case[UpdateTaskOutput]:
    return Case.branch_result(UPDATED, value, failure)

setattr(update_task_updated, '__module__', "dex_connectors.clickup.clickupmock")

def update_task_not_found(value: UpdateTaskOutput, failure: Failure | None = None) -> Case[UpdateTaskOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(update_task_not_found, '__module__', "dex_connectors.clickup.clickupmock")

def update_task_provider_rejected(value: UpdateTaskOutput, failure: Failure | None = None) -> Case[UpdateTaskOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(update_task_provider_rejected, '__module__', "dex_connectors.clickup.clickupmock")

def update_task_invalid_response(value: UpdateTaskOutput, failure: Failure | None = None) -> Case[UpdateTaskOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(update_task_invalid_response, '__module__', "dex_connectors.clickup.clickupmock")

def update_task_defect(value: UpdateTaskOutput, failure: Failure | None = None) -> Case[UpdateTaskOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(update_task_defect, '__module__', "dex_connectors.clickup.clickupmock")

def update_task_retry(failure: Failure, after_seconds: float = 0) -> Case[UpdateTaskOutput]:
    return Case.retry(failure, after_seconds)

setattr(update_task_retry, '__module__', "dex_connectors.clickup.clickupmock")

def update_task_assigned_blocked_task() -> Case[UpdateTaskOutput]:
    return Case.branch_result(UPDATED, UpdateTaskOutput(task=Task(id='86b2x1', name='Fix login for SSO customers', url='https://app.clickup.com/t/86b2x1', status=TaskStatus(name='blocked', type='custom'), priority=1, created_at=datetime.fromisoformat("2026-10-20T09:00:00Z".replace('Z', '+00:00')), updated_at=datetime.fromisoformat("2026-10-25T09:02:00Z".replace('Z', '+00:00')), assignees=(User(id=183, username='Ada Lovelace'),), tags=('customer', 'escalated'), workspace_id='9014123456', list_id='901410000001')))

setattr(update_task_assigned_blocked_task, '__module__', "dex_connectors.clickup.clickupmock")

def update_task_missing_task_update() -> Case[UpdateTaskOutput]:
    return Case.branch_result(NOT_FOUND, UpdateTaskOutput(task=Task(status=TaskStatus())), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "updateTask", "ClickUp found no such task."))

setattr(update_task_missing_task_update, '__module__', "dex_connectors.clickup.clickupmock")

def update_task_task_update_denied() -> Case[UpdateTaskOutput]:
    return Case.branch_result(PROVIDER_REJECTED, UpdateTaskOutput(task=Task(status=TaskStatus())), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "updateTask", "ClickUp denied the task update."))

setattr(update_task_task_update_denied, '__module__', "dex_connectors.clickup.clickupmock")

def update_task_tags_updated(value: UpdateTaskTagsOutput, failure: Failure | None = None) -> Case[UpdateTaskTagsOutput]:
    return Case.branch_result(UPDATED, value, failure)

setattr(update_task_tags_updated, '__module__', "dex_connectors.clickup.clickupmock")

def update_task_tags_not_found(value: UpdateTaskTagsOutput, failure: Failure | None = None) -> Case[UpdateTaskTagsOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(update_task_tags_not_found, '__module__', "dex_connectors.clickup.clickupmock")

def update_task_tags_provider_rejected(value: UpdateTaskTagsOutput, failure: Failure | None = None) -> Case[UpdateTaskTagsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(update_task_tags_provider_rejected, '__module__', "dex_connectors.clickup.clickupmock")

def update_task_tags_invalid_response(value: UpdateTaskTagsOutput, failure: Failure | None = None) -> Case[UpdateTaskTagsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(update_task_tags_invalid_response, '__module__', "dex_connectors.clickup.clickupmock")

def update_task_tags_defect(value: UpdateTaskTagsOutput, failure: Failure | None = None) -> Case[UpdateTaskTagsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(update_task_tags_defect, '__module__', "dex_connectors.clickup.clickupmock")

def update_task_tags_retry(failure: Failure, after_seconds: float = 0) -> Case[UpdateTaskTagsOutput]:
    return Case.retry(failure, after_seconds)

setattr(update_task_tags_retry, '__module__', "dex_connectors.clickup.clickupmock")

def update_task_tags_tagged_blocked_task() -> Case[UpdateTaskTagsOutput]:
    return Case.branch_result(UPDATED, UpdateTaskTagsOutput(task_id='86b2x1', tags=('customer', 'escalated'), applied_changes=1))

setattr(update_task_tags_tagged_blocked_task, '__module__', "dex_connectors.clickup.clickupmock")

def update_task_tags_missing_task_tags() -> Case[UpdateTaskTagsOutput]:
    return Case.branch_result(NOT_FOUND, UpdateTaskTagsOutput(task_id='86b2missing', tags=(), applied_changes=0), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "updateTaskTags", "ClickUp found no such task."))

setattr(update_task_tags_missing_task_tags, '__module__', "dex_connectors.clickup.clickupmock")

def update_task_tags_tag_update_denied() -> Case[UpdateTaskTagsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, UpdateTaskTagsOutput(task_id='86b2x1', tags=(), applied_changes=0), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "updateTaskTags", "ClickUp denied the tag update."))

setattr(update_task_tags_tag_update_denied, '__module__', "dex_connectors.clickup.clickupmock")

def add_comment_added(value: AddCommentOutput, failure: Failure | None = None) -> Case[AddCommentOutput]:
    return Case.branch_result(ADDED, value, failure)

setattr(add_comment_added, '__module__', "dex_connectors.clickup.clickupmock")

def add_comment_not_found(value: AddCommentOutput, failure: Failure | None = None) -> Case[AddCommentOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(add_comment_not_found, '__module__', "dex_connectors.clickup.clickupmock")

def add_comment_provider_rejected(value: AddCommentOutput, failure: Failure | None = None) -> Case[AddCommentOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(add_comment_provider_rejected, '__module__', "dex_connectors.clickup.clickupmock")

def add_comment_uncertain(value: AddCommentOutput, failure: Failure) -> Case[AddCommentOutput]:
    return Case.uncertain(value, failure)

setattr(add_comment_uncertain, '__module__', "dex_connectors.clickup.clickupmock")

def add_comment_defect(value: AddCommentOutput, failure: Failure | None = None) -> Case[AddCommentOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(add_comment_defect, '__module__', "dex_connectors.clickup.clickupmock")

def add_comment_retry(failure: Failure, after_seconds: float = 0) -> Case[AddCommentOutput]:
    return Case.retry(failure, after_seconds)

setattr(add_comment_retry, '__module__', "dex_connectors.clickup.clickupmock")

def add_comment_escalation_link_comment() -> Case[AddCommentOutput]:
    return Case.branch_result(ADDED, AddCommentOutput(task_id='86b2x1', comment_id='700001', created_at=datetime.fromisoformat("2026-10-25T09:03:00Z".replace('Z', '+00:00'))))

setattr(add_comment_escalation_link_comment, '__module__', "dex_connectors.clickup.clickupmock")

def add_comment_missing_comment_task() -> Case[AddCommentOutput]:
    return Case.branch_result(NOT_FOUND, AddCommentOutput(task_id='86b2missing'), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "addComment", "ClickUp found no such task."))

setattr(add_comment_missing_comment_task, '__module__', "dex_connectors.clickup.clickupmock")

def add_comment_comment_denied() -> Case[AddCommentOutput]:
    return Case.branch_result(PROVIDER_REJECTED, AddCommentOutput(task_id='86b2x1'), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "addComment", "ClickUp denied the comment."))

setattr(add_comment_comment_denied, '__module__', "dex_connectors.clickup.clickupmock")

def find_member_by_email_found(value: Member, failure: Failure | None = None) -> Case[Member]:
    return Case.branch_result(FOUND, value, failure)

setattr(find_member_by_email_found, '__module__', "dex_connectors.clickup.clickupmock")

def find_member_by_email_not_found(value: Member, failure: Failure | None = None) -> Case[Member]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(find_member_by_email_not_found, '__module__', "dex_connectors.clickup.clickupmock")

def find_member_by_email_provider_rejected(value: Member, failure: Failure | None = None) -> Case[Member]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(find_member_by_email_provider_rejected, '__module__', "dex_connectors.clickup.clickupmock")

def find_member_by_email_invalid_response(value: Member, failure: Failure | None = None) -> Case[Member]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(find_member_by_email_invalid_response, '__module__', "dex_connectors.clickup.clickupmock")

def find_member_by_email_defect(value: Member, failure: Failure | None = None) -> Case[Member]:
    return Case.branch_result(DEFECT, value, failure)

setattr(find_member_by_email_defect, '__module__', "dex_connectors.clickup.clickupmock")

def find_member_by_email_retry(failure: Failure, after_seconds: float = 0) -> Case[Member]:
    return Case.retry(failure, after_seconds)

setattr(find_member_by_email_retry, '__module__', "dex_connectors.clickup.clickupmock")

def find_member_by_email_escalation_manager() -> Case[Member]:
    return Case.branch_result(FOUND, Member(id=183, username='Ada Lovelace', email='manager@acme.example.com', role=2, workspace_id='9014123456'))

setattr(find_member_by_email_escalation_manager, '__module__', "dex_connectors.clickup.clickupmock")

def find_member_by_email_unknown_manager() -> Case[Member]:
    return Case.branch_result(NOT_FOUND, Member(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "findMemberByEmail", "ClickUp found no member with this email."))

setattr(find_member_by_email_unknown_manager, '__module__', "dex_connectors.clickup.clickupmock")

def find_member_by_email_member_read_denied() -> Case[Member]:
    return Case.branch_result(PROVIDER_REJECTED, Member(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "findMemberByEmail", "ClickUp denied the Workspace read."))

setattr(find_member_by_email_member_read_denied, '__module__', "dex_connectors.clickup.clickupmock")


class Mock:

    def __init__(self, connection_name: str = "clickup") -> None:
        self.search_tasks_script: Query[SearchTasksInput, SearchTasksOutput] = Query(SEARCH_TASKS_DEFINITION)
        self.search_tasks_script.default(search_tasks_unrelated_escalations())
        self.get_task_script: Query[GetTaskInput, Task] = Query(GET_TASK_DEFINITION)
        self.get_task_script.default(get_task_blocked_login_task())
        self.create_task_script: Mutation[CreateTaskInput, CreateTaskOutput] = Mutation(CREATE_TASK_DEFINITION)
        self.create_task_script.default(create_task_created_login_escalation())
        self.update_task_script: Mutation[UpdateTaskInput, UpdateTaskOutput] = Mutation(UPDATE_TASK_DEFINITION)
        self.update_task_script.default(update_task_assigned_blocked_task())
        self.update_task_tags_script: Mutation[UpdateTaskTagsInput, UpdateTaskTagsOutput] = Mutation(UPDATE_TASK_TAGS_DEFINITION)
        self.update_task_tags_script.default(update_task_tags_tagged_blocked_task())
        self.add_comment_script: Mutation[AddCommentInput, AddCommentOutput] = Mutation(ADD_COMMENT_DEFINITION)
        self.add_comment_script.default(add_comment_escalation_link_comment())
        self.find_member_by_email_script: Query[FindMemberByEmailInput, Member] = Query(FIND_MEMBER_BY_EMAIL_DEFINITION)
        self.find_member_by_email_script.default(find_member_by_email_escalation_manager())
        self._connection = new_connection_with_operations(
            search_tasks=self.search_tasks_script,
            get_task=self.get_task_script,
            create_task=self.create_task_script,
            update_task=self.update_task_script,
            update_task_tags=self.update_task_tags_script,
            add_comment=self.add_comment_script,
            find_member_by_email=self.find_member_by_email_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.clickup.clickupmock")

__all__ = [
    "Mock",
    "search_tasks_searched",
    "search_tasks_not_found",
    "search_tasks_provider_rejected",
    "search_tasks_invalid_response",
    "search_tasks_defect",
    "search_tasks_retry",
    "search_tasks_unrelated_escalations",
    "search_tasks_unknown_workspace_search",
    "search_tasks_search_denied",
    "get_task_found",
    "get_task_not_found",
    "get_task_provider_rejected",
    "get_task_invalid_response",
    "get_task_defect",
    "get_task_retry",
    "get_task_blocked_login_task",
    "get_task_missing_task",
    "get_task_task_read_denied",
    "create_task_created",
    "create_task_not_found",
    "create_task_provider_rejected",
    "create_task_uncertain",
    "create_task_defect",
    "create_task_retry",
    "create_task_created_login_escalation",
    "create_task_missing_escalation_list",
    "create_task_create_denied",
    "update_task_updated",
    "update_task_not_found",
    "update_task_provider_rejected",
    "update_task_invalid_response",
    "update_task_defect",
    "update_task_retry",
    "update_task_assigned_blocked_task",
    "update_task_missing_task_update",
    "update_task_task_update_denied",
    "update_task_tags_updated",
    "update_task_tags_not_found",
    "update_task_tags_provider_rejected",
    "update_task_tags_invalid_response",
    "update_task_tags_defect",
    "update_task_tags_retry",
    "update_task_tags_tagged_blocked_task",
    "update_task_tags_missing_task_tags",
    "update_task_tags_tag_update_denied",
    "add_comment_added",
    "add_comment_not_found",
    "add_comment_provider_rejected",
    "add_comment_uncertain",
    "add_comment_defect",
    "add_comment_retry",
    "add_comment_escalation_link_comment",
    "add_comment_missing_comment_task",
    "add_comment_comment_denied",
    "find_member_by_email_found",
    "find_member_by_email_not_found",
    "find_member_by_email_provider_rejected",
    "find_member_by_email_invalid_response",
    "find_member_by_email_defect",
    "find_member_by_email_retry",
    "find_member_by_email_escalation_manager",
    "find_member_by_email_unknown_manager",
    "find_member_by_email_member_read_denied",
]
