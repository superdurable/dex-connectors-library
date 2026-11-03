# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import CreateTaskInput, DeleteTaskInput, DeleteTaskOutcome, GetTaskInput, ListTasksInput, Task, TaskPage, UpdateTaskInput
from .definitions import CONNECTOR_ID, ALREADY_ABSENT, CREATED, CREATE_TASK_DEFINITION, DEFECT, DELETED, DELETE_TASK_DEFINITION, FOUND, GET_TASK_DEFINITION, INVALID_RESPONSE, LISTED, LIST_TASKS_DEFINITION, NOT_FOUND, PROVIDER_REJECTED, UNCERTAIN, UPDATED, UPDATE_TASK_CONTENT_DEFINITION

def get_task_found(value: Task, failure: Failure | None = None) -> Case[Task]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_task_found, '__module__', "dex_connectors.doist_todoist.todoistmock")

def get_task_not_found(value: Task, failure: Failure | None = None) -> Case[Task]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_task_not_found, '__module__', "dex_connectors.doist_todoist.todoistmock")

def get_task_provider_rejected(value: Task, failure: Failure | None = None) -> Case[Task]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_task_provider_rejected, '__module__', "dex_connectors.doist_todoist.todoistmock")

def get_task_invalid_response(value: Task, failure: Failure | None = None) -> Case[Task]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_task_invalid_response, '__module__', "dex_connectors.doist_todoist.todoistmock")

def get_task_defect(value: Task, failure: Failure | None = None) -> Case[Task]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_task_defect, '__module__', "dex_connectors.doist_todoist.todoistmock")

def get_task_retry(failure: Failure, after_seconds: float = 0) -> Case[Task]:
    return Case.retry(failure, after_seconds)

setattr(get_task_retry, '__module__', "dex_connectors.doist_todoist.todoistmock")

def get_task_created_task() -> Case[Task]:
    return Case.branch_result(FOUND, Task(id='created-task', project_id='inbox-project', content='Original title', description='Original description', is_completed=False))

setattr(get_task_created_task, '__module__', "dex_connectors.doist_todoist.todoistmock")

def get_task_deleted_task_missing() -> Case[Task]:
    return Case.branch_result(NOT_FOUND, Task(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getTask", "Todoist found no active task with that ID."))

setattr(get_task_deleted_task_missing, '__module__', "dex_connectors.doist_todoist.todoistmock")

def get_task_task_read_denied() -> Case[Task]:
    return Case.branch_result(PROVIDER_REJECTED, Task(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "getTask", "Todoist denied the task read."))

setattr(get_task_task_read_denied, '__module__', "dex_connectors.doist_todoist.todoistmock")

def update_task_content_updated(value: Task, failure: Failure | None = None) -> Case[Task]:
    return Case.branch_result(UPDATED, value, failure)

setattr(update_task_content_updated, '__module__', "dex_connectors.doist_todoist.todoistmock")

def update_task_content_not_found(value: Task, failure: Failure | None = None) -> Case[Task]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(update_task_content_not_found, '__module__', "dex_connectors.doist_todoist.todoistmock")

def update_task_content_provider_rejected(value: Task, failure: Failure | None = None) -> Case[Task]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(update_task_content_provider_rejected, '__module__', "dex_connectors.doist_todoist.todoistmock")

def update_task_content_defect(value: Task, failure: Failure | None = None) -> Case[Task]:
    return Case.branch_result(DEFECT, value, failure)

setattr(update_task_content_defect, '__module__', "dex_connectors.doist_todoist.todoistmock")

def update_task_content_retry(failure: Failure, after_seconds: float = 0) -> Case[Task]:
    return Case.retry(failure, after_seconds)

setattr(update_task_content_retry, '__module__', "dex_connectors.doist_todoist.todoistmock")

def update_task_content_lifecycle_title_updated() -> Case[Task]:
    return Case.branch_result(UPDATED, Task(id='created-task', project_id='inbox-project', content='Original title - updated', description='Original description', is_completed=False))

setattr(update_task_content_lifecycle_title_updated, '__module__', "dex_connectors.doist_todoist.todoistmock")

def update_task_content_task_missing_during_update() -> Case[Task]:
    return Case.branch_result(NOT_FOUND, Task(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "updateTaskContent", "Todoist found no active task to update."))

setattr(update_task_content_task_missing_during_update, '__module__', "dex_connectors.doist_todoist.todoistmock")

def update_task_content_task_update_denied() -> Case[Task]:
    return Case.branch_result(PROVIDER_REJECTED, Task(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "updateTaskContent", "Todoist denied the task update."))

setattr(update_task_content_task_update_denied, '__module__', "dex_connectors.doist_todoist.todoistmock")

def list_tasks_listed(value: TaskPage, failure: Failure | None = None) -> Case[TaskPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_tasks_listed, '__module__', "dex_connectors.doist_todoist.todoistmock")

def list_tasks_provider_rejected(value: TaskPage, failure: Failure | None = None) -> Case[TaskPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_tasks_provider_rejected, '__module__', "dex_connectors.doist_todoist.todoistmock")

def list_tasks_invalid_response(value: TaskPage, failure: Failure | None = None) -> Case[TaskPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_tasks_invalid_response, '__module__', "dex_connectors.doist_todoist.todoistmock")

def list_tasks_defect(value: TaskPage, failure: Failure | None = None) -> Case[TaskPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_tasks_defect, '__module__', "dex_connectors.doist_todoist.todoistmock")

def list_tasks_retry(failure: Failure, after_seconds: float = 0) -> Case[TaskPage]:
    return Case.retry(failure, after_seconds)

setattr(list_tasks_retry, '__module__', "dex_connectors.doist_todoist.todoistmock")

def list_tasks_inbox_tasks_across_pages() -> Case[TaskPage]:
    return Case.paged(LISTED, (TaskPage(tasks=(Task(id='other-2', project_id='inbox-project', content='Prepare report', description='', is_completed=False), Task(id='other-1', project_id='inbox-project', content='Check draft', description='', is_completed=False)), next_cursor='inbox-page-2'), TaskPage(tasks=(Task(id='other-1', project_id='inbox-project', content='Check draft', description='', is_completed=False),), next_cursor='inbox-page-3'), TaskPage(tasks=(Task(id='created-task', project_id='inbox-project', content='Original title - updated', description='Original description', is_completed=False),))))

setattr(list_tasks_inbox_tasks_across_pages, '__module__', "dex_connectors.doist_todoist.todoistmock")

def list_tasks_task_list_denied() -> Case[TaskPage]:
    return Case.branch_result(PROVIDER_REJECTED, TaskPage(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "listTasks", "Todoist denied the task listing."))

setattr(list_tasks_task_list_denied, '__module__', "dex_connectors.doist_todoist.todoistmock")

def create_task_created(value: Task, failure: Failure | None = None) -> Case[Task]:
    return Case.branch_result(CREATED, value, failure)

setattr(create_task_created, '__module__', "dex_connectors.doist_todoist.todoistmock")

def create_task_provider_rejected(value: Task, failure: Failure | None = None) -> Case[Task]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(create_task_provider_rejected, '__module__', "dex_connectors.doist_todoist.todoistmock")

def create_task_uncertain(value: Task, failure: Failure) -> Case[Task]:
    return Case.uncertain(value, failure)

setattr(create_task_uncertain, '__module__', "dex_connectors.doist_todoist.todoistmock")

def create_task_defect(value: Task, failure: Failure | None = None) -> Case[Task]:
    return Case.branch_result(DEFECT, value, failure)

setattr(create_task_defect, '__module__', "dex_connectors.doist_todoist.todoistmock")

def create_task_retry(failure: Failure, after_seconds: float = 0) -> Case[Task]:
    return Case.retry(failure, after_seconds)

setattr(create_task_retry, '__module__', "dex_connectors.doist_todoist.todoistmock")

def create_task_disposable_task_created() -> Case[Task]:
    return Case.branch_result(CREATED, Task(id='created-task', project_id='inbox-project', content='Original title', description='Original description', is_completed=False))

setattr(create_task_disposable_task_created, '__module__', "dex_connectors.doist_todoist.todoistmock")

def create_task_task_creation_denied() -> Case[Task]:
    return Case.branch_result(PROVIDER_REJECTED, Task(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "createTask", "Todoist denied task creation."))

setattr(create_task_task_creation_denied, '__module__', "dex_connectors.doist_todoist.todoistmock")

def delete_task_deleted(value: DeleteTaskOutcome, failure: Failure | None = None) -> Case[DeleteTaskOutcome]:
    return Case.branch_result(DELETED, value, failure)

setattr(delete_task_deleted, '__module__', "dex_connectors.doist_todoist.todoistmock")

def delete_task_already_absent(value: DeleteTaskOutcome, failure: Failure | None = None) -> Case[DeleteTaskOutcome]:
    return Case.branch_result(ALREADY_ABSENT, value, failure)

setattr(delete_task_already_absent, '__module__', "dex_connectors.doist_todoist.todoistmock")

def delete_task_provider_rejected(value: DeleteTaskOutcome, failure: Failure | None = None) -> Case[DeleteTaskOutcome]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(delete_task_provider_rejected, '__module__', "dex_connectors.doist_todoist.todoistmock")

def delete_task_defect(value: DeleteTaskOutcome, failure: Failure | None = None) -> Case[DeleteTaskOutcome]:
    return Case.branch_result(DEFECT, value, failure)

setattr(delete_task_defect, '__module__', "dex_connectors.doist_todoist.todoistmock")

def delete_task_retry(failure: Failure, after_seconds: float = 0) -> Case[DeleteTaskOutcome]:
    return Case.retry(failure, after_seconds)

setattr(delete_task_retry, '__module__', "dex_connectors.doist_todoist.todoistmock")

def delete_task_disposable_task_deleted() -> Case[DeleteTaskOutcome]:
    return Case.branch_result(DELETED, DeleteTaskOutcome(task_id='created-task'))

setattr(delete_task_disposable_task_deleted, '__module__', "dex_connectors.doist_todoist.todoistmock")

def delete_task_task_already_absent() -> Case[DeleteTaskOutcome]:
    return Case.branch_result(ALREADY_ABSENT, DeleteTaskOutcome(task_id='missing-task'), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "deleteTask", "Todoist found no active task to delete."))

setattr(delete_task_task_already_absent, '__module__', "dex_connectors.doist_todoist.todoistmock")

def delete_task_task_deletion_denied() -> Case[DeleteTaskOutcome]:
    return Case.branch_result(PROVIDER_REJECTED, DeleteTaskOutcome(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "deleteTask", "Todoist denied task deletion."))

setattr(delete_task_task_deletion_denied, '__module__', "dex_connectors.doist_todoist.todoistmock")


class Mock:

    def __init__(self, connection_name: str = "todoist") -> None:
        self.get_task_script: Query[GetTaskInput, Task] = Query(GET_TASK_DEFINITION)
        self.get_task_script.default(get_task_created_task())
        self.update_task_content_script: Mutation[UpdateTaskInput, Task] = Mutation(UPDATE_TASK_CONTENT_DEFINITION)
        self.update_task_content_script.default(update_task_content_lifecycle_title_updated())
        self.list_tasks_script: Query[ListTasksInput, TaskPage] = Query(LIST_TASKS_DEFINITION)
        self.list_tasks_script.default(list_tasks_inbox_tasks_across_pages())
        self.create_task_script: Mutation[CreateTaskInput, Task] = Mutation(CREATE_TASK_DEFINITION)
        self.create_task_script.default(create_task_disposable_task_created())
        self.delete_task_script: Mutation[DeleteTaskInput, DeleteTaskOutcome] = Mutation(DELETE_TASK_DEFINITION)
        self.delete_task_script.default(delete_task_disposable_task_deleted())
        self._connection = new_connection_with_operations(
            get_task=self.get_task_script,
            update_task_content=self.update_task_content_script,
            list_tasks=self.list_tasks_script,
            create_task=self.create_task_script,
            delete_task=self.delete_task_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.doist_todoist.todoistmock")

__all__ = [
    "Mock",
    "get_task_found",
    "get_task_not_found",
    "get_task_provider_rejected",
    "get_task_invalid_response",
    "get_task_defect",
    "get_task_retry",
    "get_task_created_task",
    "get_task_deleted_task_missing",
    "get_task_task_read_denied",
    "update_task_content_updated",
    "update_task_content_not_found",
    "update_task_content_provider_rejected",
    "update_task_content_defect",
    "update_task_content_retry",
    "update_task_content_lifecycle_title_updated",
    "update_task_content_task_missing_during_update",
    "update_task_content_task_update_denied",
    "list_tasks_listed",
    "list_tasks_provider_rejected",
    "list_tasks_invalid_response",
    "list_tasks_defect",
    "list_tasks_retry",
    "list_tasks_inbox_tasks_across_pages",
    "list_tasks_task_list_denied",
    "create_task_created",
    "create_task_provider_rejected",
    "create_task_uncertain",
    "create_task_defect",
    "create_task_retry",
    "create_task_disposable_task_created",
    "create_task_task_creation_denied",
    "delete_task_deleted",
    "delete_task_already_absent",
    "delete_task_provider_rejected",
    "delete_task_defect",
    "delete_task_retry",
    "delete_task_disposable_task_deleted",
    "delete_task_task_already_absent",
    "delete_task_task_deletion_denied",
]
