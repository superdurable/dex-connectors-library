# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Todoist task CRUD through typed Dex connector Steps."""

from .client import Client, Config, Credentials
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import (
    CreateTaskInput,
    DeleteTaskInput,
    DeleteTaskOutcome,
    GetTaskInput,
    ListTasksInput,
    Task,
    TaskPage,
    UpdateTaskInput,
)
from .steps import (
    new_create_task_step,
    new_delete_task_step,
    new_get_task_step,
    new_list_tasks_step,
    new_update_task_content_step,
)

UpdateTaskContentInput = UpdateTaskInput

__all__ = [
    "Client",
    "Config",
    "Credentials",
    "Connection",
    "new_connection",
    "new_connection_with_operations",
    "new_project_connection",
    "CreateTaskInput",
    "DeleteTaskInput",
    "DeleteTaskOutcome",
    "GetTaskInput",
    "ListTasksInput",
    "Task",
    "TaskPage",
    "UpdateTaskInput",
    "new_create_task_step",
    "new_delete_task_step",
    "new_get_task_step",
    "new_list_tasks_step",
    "new_update_task_content_step",
]
