# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListTaskListsInput, ListTasksInput, Task, TaskList, TaskListsPage, TasksPage
from .definitions import CONNECTOR_ID, PROVIDER, DEFECT, INVALID_RESPONSE, LISTED, LIST_TASKS_DEFINITION, LIST_TASK_LISTS_DEFINITION, NOT_FOUND, PROVIDER_REJECTED

def list_task_lists_listed(value: TaskListsPage, failure: Failure | None = None) -> Case[TaskListsPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_task_lists_listed, '__module__', "dex_connectors.microsoft_todo.microsoft_todomock")

def list_task_lists_provider_rejected(value: TaskListsPage, failure: Failure | None = None) -> Case[TaskListsPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_task_lists_provider_rejected, '__module__', "dex_connectors.microsoft_todo.microsoft_todomock")

def list_task_lists_invalid_response(value: TaskListsPage, failure: Failure | None = None) -> Case[TaskListsPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_task_lists_invalid_response, '__module__', "dex_connectors.microsoft_todo.microsoft_todomock")

def list_task_lists_defect(value: TaskListsPage, failure: Failure | None = None) -> Case[TaskListsPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_task_lists_defect, '__module__', "dex_connectors.microsoft_todo.microsoft_todomock")

def list_task_lists_retry(failure: Failure, after_seconds: float = 0) -> Case[TaskListsPage]:
    return Case.retry(failure, after_seconds)

setattr(list_task_lists_retry, '__module__', "dex_connectors.microsoft_todo.microsoft_todomock")

def list_task_lists_listed_default() -> Case[TaskListsPage]:
    return Case.branch_result(LISTED, TaskListsPage(lists=(TaskList(id='sample-list-1', display_name='Tasks'),)))

setattr(list_task_lists_listed_default, '__module__', "dex_connectors.microsoft_todo.microsoft_todomock")

def list_tasks_listed(value: TasksPage, failure: Failure | None = None) -> Case[TasksPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_tasks_listed, '__module__', "dex_connectors.microsoft_todo.microsoft_todomock")

def list_tasks_not_found(value: TasksPage, failure: Failure | None = None) -> Case[TasksPage]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(list_tasks_not_found, '__module__', "dex_connectors.microsoft_todo.microsoft_todomock")

def list_tasks_provider_rejected(value: TasksPage, failure: Failure | None = None) -> Case[TasksPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_tasks_provider_rejected, '__module__', "dex_connectors.microsoft_todo.microsoft_todomock")

def list_tasks_invalid_response(value: TasksPage, failure: Failure | None = None) -> Case[TasksPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_tasks_invalid_response, '__module__', "dex_connectors.microsoft_todo.microsoft_todomock")

def list_tasks_defect(value: TasksPage, failure: Failure | None = None) -> Case[TasksPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_tasks_defect, '__module__', "dex_connectors.microsoft_todo.microsoft_todomock")

def list_tasks_retry(failure: Failure, after_seconds: float = 0) -> Case[TasksPage]:
    return Case.retry(failure, after_seconds)

setattr(list_tasks_retry, '__module__', "dex_connectors.microsoft_todo.microsoft_todomock")

def list_tasks_listed_default() -> Case[TasksPage]:
    return Case.branch_result(LISTED, TasksPage(tasks=(Task(id='sample-task-1', title='Review request', status='notStarted'),)))

setattr(list_tasks_listed_default, '__module__', "dex_connectors.microsoft_todo.microsoft_todomock")


class Mock:

    def __init__(self, connection_name: str = "microsoft-todo") -> None:
        self.list_task_lists_script: Query[ListTaskListsInput, TaskListsPage] = Query(LIST_TASK_LISTS_DEFINITION)
        self.list_task_lists_script.default(list_task_lists_listed_default())
        self.list_tasks_script: Query[ListTasksInput, TasksPage] = Query(LIST_TASKS_DEFINITION)
        self.list_tasks_script.default(list_tasks_listed_default())
        self._connection = new_connection_with_operations(
            list_task_lists=self.list_task_lists_script,
            list_tasks=self.list_tasks_script,
            reference=ConnectionRef(PROVIDER, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.microsoft_todo.microsoft_todomock")

__all__ = [
    "Mock",
    "list_task_lists_listed",
    "list_task_lists_provider_rejected",
    "list_task_lists_invalid_response",
    "list_task_lists_defect",
    "list_task_lists_retry",
    "list_task_lists_listed_default",
    "list_tasks_listed",
    "list_tasks_not_found",
    "list_tasks_provider_rejected",
    "list_tasks_invalid_response",
    "list_tasks_defect",
    "list_tasks_retry",
    "list_tasks_listed_default",
]
