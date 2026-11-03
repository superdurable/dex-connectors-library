# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import AddCommentInput, AddCommentOutput, CreateTaskInput, CreateTaskOutput, CustomFieldValue, CustomFieldValueInput, EnumOptionReference, GetTaskInput, ListTasksInput, ListTasksOutput, ProjectReference, SectionReference, Task, TaskMembership, TaskReference, UpdateTaskInput, UpdateTaskOutput, UserReference, WorkspaceReference
from .definitions import CONNECTOR_ID, ADDED, ADD_COMMENT_DEFINITION, CREATED, CREATE_TASK_DEFINITION, DEFECT, FOUND, GET_TASK_DEFINITION, INVALID_RESPONSE, LISTED, LIST_TASKS_DEFINITION, NOT_FOUND, PROVIDER_REJECTED, UNCERTAIN, UPDATED, UPDATE_TASK_DEFINITION

def list_tasks_listed(value: ListTasksOutput, failure: Failure | None = None) -> Case[ListTasksOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_tasks_listed, '__module__', "dex_connectors.asana.asanamock")

def list_tasks_not_found(value: ListTasksOutput, failure: Failure | None = None) -> Case[ListTasksOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(list_tasks_not_found, '__module__', "dex_connectors.asana.asanamock")

def list_tasks_provider_rejected(value: ListTasksOutput, failure: Failure | None = None) -> Case[ListTasksOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_tasks_provider_rejected, '__module__', "dex_connectors.asana.asanamock")

def list_tasks_invalid_response(value: ListTasksOutput, failure: Failure | None = None) -> Case[ListTasksOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_tasks_invalid_response, '__module__', "dex_connectors.asana.asanamock")

def list_tasks_defect(value: ListTasksOutput, failure: Failure | None = None) -> Case[ListTasksOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_tasks_defect, '__module__', "dex_connectors.asana.asanamock")

def list_tasks_retry(failure: Failure, after_seconds: float = 0) -> Case[ListTasksOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_tasks_retry, '__module__', "dex_connectors.asana.asanamock")

def list_tasks_listed_default_answer() -> Case[ListTasksOutput]:
    return Case.branch_result(LISTED, ListTasksOutput(tasks=(Task(id='1204000000000001', name='Inspect door sensors', is_completed=False, memberships=(TaskMembership(project=ProjectReference(id='1201000000000001', name='Facilities'), section=SectionReference(id='1201000000000101', name='Approved')),), created_at=datetime.fromisoformat("2026-09-30T16:00:00Z".replace('Z', '+00:00')), modified_at=datetime.fromisoformat("2026-09-30T16:00:00Z".replace('Z', '+00:00'))),)))

setattr(list_tasks_listed_default_answer, '__module__', "dex_connectors.asana.asanamock")

def list_tasks_not_found_failure() -> Case[ListTasksOutput]:
    return Case.branch_result(NOT_FOUND, ListTasksOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "listTasks", "The project or section does not exist or the connection cannot see it."))

setattr(list_tasks_not_found_failure, '__module__', "dex_connectors.asana.asanamock")

def list_tasks_provider_rejected_failure() -> Case[ListTasksOutput]:
    return Case.branch_result(PROVIDER_REJECTED, ListTasksOutput(), Failure(FailureKind.PROVIDER_REJECTION, CONNECTOR_ID, "listTasks", "Asana conclusively rejected the list, such as an expired offset token, missing permission, or a result set too large to page."))

setattr(list_tasks_provider_rejected_failure, '__module__', "dex_connectors.asana.asanamock")

def get_task_found(value: Task, failure: Failure | None = None) -> Case[Task]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_task_found, '__module__', "dex_connectors.asana.asanamock")

def get_task_not_found(value: Task, failure: Failure | None = None) -> Case[Task]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_task_not_found, '__module__', "dex_connectors.asana.asanamock")

def get_task_provider_rejected(value: Task, failure: Failure | None = None) -> Case[Task]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_task_provider_rejected, '__module__', "dex_connectors.asana.asanamock")

def get_task_invalid_response(value: Task, failure: Failure | None = None) -> Case[Task]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_task_invalid_response, '__module__', "dex_connectors.asana.asanamock")

def get_task_defect(value: Task, failure: Failure | None = None) -> Case[Task]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_task_defect, '__module__', "dex_connectors.asana.asanamock")

def get_task_retry(failure: Failure, after_seconds: float = 0) -> Case[Task]:
    return Case.retry(failure, after_seconds)

setattr(get_task_retry, '__module__', "dex_connectors.asana.asanamock")

def get_task_found_default_answer() -> Case[Task]:
    return Case.branch_result(FOUND, Task(id='1204000000000002', name='[REQ-1042] Replace badge reader', is_completed=False, due_on='2026-10-15', memberships=(TaskMembership(project=ProjectReference(id='1201000000000001', name='Facilities'), section=SectionReference(id='1201000000000101', name='Approved')),), workspace=WorkspaceReference(id='1200000000000001', name='Operations'), notes='Door 4 is offline.', created_at=datetime.fromisoformat("2026-09-30T16:15:00Z".replace('Z', '+00:00')), modified_at=datetime.fromisoformat("2026-09-30T16:15:00Z".replace('Z', '+00:00'))))

setattr(get_task_found_default_answer, '__module__', "dex_connectors.asana.asanamock")

def get_task_not_found_failure() -> Case[Task]:
    return Case.branch_result(NOT_FOUND, Task(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getTask", "The task does not exist or the connection cannot see it."))

setattr(get_task_not_found_failure, '__module__', "dex_connectors.asana.asanamock")

def get_task_provider_rejected_failure() -> Case[Task]:
    return Case.branch_result(PROVIDER_REJECTED, Task(), Failure(FailureKind.PROVIDER_REJECTION, CONNECTOR_ID, "getTask", "Asana conclusively rejected the task read."))

setattr(get_task_provider_rejected_failure, '__module__', "dex_connectors.asana.asanamock")

def create_task_created(value: CreateTaskOutput, failure: Failure | None = None) -> Case[CreateTaskOutput]:
    return Case.branch_result(CREATED, value, failure)

setattr(create_task_created, '__module__', "dex_connectors.asana.asanamock")

def create_task_provider_rejected(value: CreateTaskOutput, failure: Failure | None = None) -> Case[CreateTaskOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(create_task_provider_rejected, '__module__', "dex_connectors.asana.asanamock")

def create_task_uncertain(value: CreateTaskOutput, failure: Failure) -> Case[CreateTaskOutput]:
    return Case.uncertain(value, failure)

setattr(create_task_uncertain, '__module__', "dex_connectors.asana.asanamock")

def create_task_defect(value: CreateTaskOutput, failure: Failure | None = None) -> Case[CreateTaskOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(create_task_defect, '__module__', "dex_connectors.asana.asanamock")

def create_task_retry(failure: Failure, after_seconds: float = 0) -> Case[CreateTaskOutput]:
    return Case.retry(failure, after_seconds)

setattr(create_task_retry, '__module__', "dex_connectors.asana.asanamock")

def create_task_created_default_answer() -> Case[CreateTaskOutput]:
    return Case.branch_result(CREATED, CreateTaskOutput(task_id='1204000000000002', name='[REQ-1042] Replace badge reader', project_id='1201000000000001', section_id='1201000000000101', permalink_url='https://app.asana.com/0/1201000000000001/1204000000000002', created_at=datetime.fromisoformat("2026-09-30T16:15:00Z".replace('Z', '+00:00'))))

setattr(create_task_created_default_answer, '__module__', "dex_connectors.asana.asanamock")

def create_task_provider_rejected_failure() -> Case[CreateTaskOutput]:
    return Case.branch_result(PROVIDER_REJECTED, CreateTaskOutput(), Failure(FailureKind.PROVIDER_REJECTION, CONNECTOR_ID, "createTask", "Asana conclusively rejected the task, such as an unknown project, section, assignee, or custom field value, or missing permission; nothing was created."))

setattr(create_task_provider_rejected_failure, '__module__', "dex_connectors.asana.asanamock")

def update_task_updated(value: UpdateTaskOutput, failure: Failure | None = None) -> Case[UpdateTaskOutput]:
    return Case.branch_result(UPDATED, value, failure)

setattr(update_task_updated, '__module__', "dex_connectors.asana.asanamock")

def update_task_not_found(value: UpdateTaskOutput, failure: Failure | None = None) -> Case[UpdateTaskOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(update_task_not_found, '__module__', "dex_connectors.asana.asanamock")

def update_task_provider_rejected(value: UpdateTaskOutput, failure: Failure | None = None) -> Case[UpdateTaskOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(update_task_provider_rejected, '__module__', "dex_connectors.asana.asanamock")

def update_task_invalid_response(value: UpdateTaskOutput, failure: Failure | None = None) -> Case[UpdateTaskOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(update_task_invalid_response, '__module__', "dex_connectors.asana.asanamock")

def update_task_defect(value: UpdateTaskOutput, failure: Failure | None = None) -> Case[UpdateTaskOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(update_task_defect, '__module__', "dex_connectors.asana.asanamock")

def update_task_retry(failure: Failure, after_seconds: float = 0) -> Case[UpdateTaskOutput]:
    return Case.retry(failure, after_seconds)

setattr(update_task_retry, '__module__', "dex_connectors.asana.asanamock")

def update_task_updated_default_answer() -> Case[UpdateTaskOutput]:
    return Case.branch_result(UPDATED, UpdateTaskOutput(task_id='1204000000000002', task=Task(id='1204000000000002', name='[REQ-1042] Replace badge reader', is_completed=False, assignee=UserReference(id='1203000000000001', name='Ada Lovelace'), due_on='2026-10-15', memberships=(TaskMembership(project=ProjectReference(id='1201000000000001', name='Facilities'), section=SectionReference(id='1201000000000101', name='Approved')),), workspace=WorkspaceReference(id='1200000000000001', name='Operations'), notes='Door 4 is offline.', created_at=datetime.fromisoformat("2026-09-30T16:15:00Z".replace('Z', '+00:00')), modified_at=datetime.fromisoformat("2026-09-30T16:17:00Z".replace('Z', '+00:00'))), is_moved_to_section=True))

setattr(update_task_updated_default_answer, '__module__', "dex_connectors.asana.asanamock")

def update_task_not_found_failure() -> Case[UpdateTaskOutput]:
    return Case.branch_result(NOT_FOUND, UpdateTaskOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "updateTask", "The task or section does not exist or the connection cannot see it."))

setattr(update_task_not_found_failure, '__module__', "dex_connectors.asana.asanamock")

def update_task_provider_rejected_failure() -> Case[UpdateTaskOutput]:
    return Case.branch_result(PROVIDER_REJECTED, UpdateTaskOutput(), Failure(FailureKind.PROVIDER_REJECTION, CONNECTOR_ID, "updateTask", "Asana conclusively rejected a change, such as an unknown assignee, a section of a project the task is not in, or missing permission."))

setattr(update_task_provider_rejected_failure, '__module__', "dex_connectors.asana.asanamock")

def add_comment_added(value: AddCommentOutput, failure: Failure | None = None) -> Case[AddCommentOutput]:
    return Case.branch_result(ADDED, value, failure)

setattr(add_comment_added, '__module__', "dex_connectors.asana.asanamock")

def add_comment_not_found(value: AddCommentOutput, failure: Failure | None = None) -> Case[AddCommentOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(add_comment_not_found, '__module__', "dex_connectors.asana.asanamock")

def add_comment_provider_rejected(value: AddCommentOutput, failure: Failure | None = None) -> Case[AddCommentOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(add_comment_provider_rejected, '__module__', "dex_connectors.asana.asanamock")

def add_comment_uncertain(value: AddCommentOutput, failure: Failure) -> Case[AddCommentOutput]:
    return Case.uncertain(value, failure)

setattr(add_comment_uncertain, '__module__', "dex_connectors.asana.asanamock")

def add_comment_defect(value: AddCommentOutput, failure: Failure | None = None) -> Case[AddCommentOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(add_comment_defect, '__module__', "dex_connectors.asana.asanamock")

def add_comment_retry(failure: Failure, after_seconds: float = 0) -> Case[AddCommentOutput]:
    return Case.retry(failure, after_seconds)

setattr(add_comment_retry, '__module__', "dex_connectors.asana.asanamock")

def add_comment_added_default_answer() -> Case[AddCommentOutput]:
    return Case.branch_result(ADDED, AddCommentOutput(task_id='1204000000000002', comment_id='1205000000000001', author=UserReference(id='1203000000000001', name='Ada Lovelace'), created_at=datetime.fromisoformat("2026-09-30T16:20:00Z".replace('Z', '+00:00'))))

setattr(add_comment_added_default_answer, '__module__', "dex_connectors.asana.asanamock")

def add_comment_not_found_failure() -> Case[AddCommentOutput]:
    return Case.branch_result(NOT_FOUND, AddCommentOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "addComment", "The task does not exist or the connection cannot see it; nothing was added."))

setattr(add_comment_not_found_failure, '__module__', "dex_connectors.asana.asanamock")

def add_comment_provider_rejected_failure() -> Case[AddCommentOutput]:
    return Case.branch_result(PROVIDER_REJECTED, AddCommentOutput(), Failure(FailureKind.PROVIDER_REJECTION, CONNECTOR_ID, "addComment", "Asana conclusively rejected the comment, such as missing permission or invalid text; nothing was added."))

setattr(add_comment_provider_rejected_failure, '__module__', "dex_connectors.asana.asanamock")


class Mock:

    def __init__(self, connection_name: str = "asana") -> None:
        self.list_tasks_script: Query[ListTasksInput, ListTasksOutput] = Query(LIST_TASKS_DEFINITION)
        self.list_tasks_script.default(list_tasks_listed_default_answer())
        self.get_task_script: Query[GetTaskInput, Task] = Query(GET_TASK_DEFINITION)
        self.get_task_script.default(get_task_found_default_answer())
        self.create_task_script: Mutation[CreateTaskInput, CreateTaskOutput] = Mutation(CREATE_TASK_DEFINITION)
        self.create_task_script.default(create_task_created_default_answer())
        self.update_task_script: Mutation[UpdateTaskInput, UpdateTaskOutput] = Mutation(UPDATE_TASK_DEFINITION)
        self.update_task_script.default(update_task_updated_default_answer())
        self.add_comment_script: Mutation[AddCommentInput, AddCommentOutput] = Mutation(ADD_COMMENT_DEFINITION)
        self.add_comment_script.default(add_comment_added_default_answer())
        self._connection = new_connection_with_operations(
            list_tasks=self.list_tasks_script,
            get_task=self.get_task_script,
            create_task=self.create_task_script,
            update_task=self.update_task_script,
            add_comment=self.add_comment_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.asana.asanamock")

__all__ = [
    "Mock",
    "list_tasks_listed",
    "list_tasks_not_found",
    "list_tasks_provider_rejected",
    "list_tasks_invalid_response",
    "list_tasks_defect",
    "list_tasks_retry",
    "list_tasks_listed_default_answer",
    "list_tasks_not_found_failure",
    "list_tasks_provider_rejected_failure",
    "get_task_found",
    "get_task_not_found",
    "get_task_provider_rejected",
    "get_task_invalid_response",
    "get_task_defect",
    "get_task_retry",
    "get_task_found_default_answer",
    "get_task_not_found_failure",
    "get_task_provider_rejected_failure",
    "create_task_created",
    "create_task_provider_rejected",
    "create_task_uncertain",
    "create_task_defect",
    "create_task_retry",
    "create_task_created_default_answer",
    "create_task_provider_rejected_failure",
    "update_task_updated",
    "update_task_not_found",
    "update_task_provider_rejected",
    "update_task_invalid_response",
    "update_task_defect",
    "update_task_retry",
    "update_task_updated_default_answer",
    "update_task_not_found_failure",
    "update_task_provider_rejected_failure",
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
