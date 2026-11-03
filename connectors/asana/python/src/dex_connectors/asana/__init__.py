# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Dex-native Asana task and comment operations."""

from ._generated.config import Config, Credentials
from .client import Client
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import (
    AddCommentInput,
    AddCommentOutput,
    CreateTaskInput,
    CreateTaskOutput,
    CustomFieldValue,
    CustomFieldValueInput,
    EnumOptionReference,
    GetTaskInput,
    ListTasksInput,
    ListTasksOutput,
    ProjectReference,
    SectionReference,
    Task,
    TaskMembership,
    TaskReference,
    UpdateTaskInput,
    UpdateTaskOutput,
    UserReference,
    WorkspaceReference,
)
from .operations import (
    AddCommentOperation,
    CreateTaskOperation,
    GetTaskOperation,
    ListTasksOperation,
    UpdateTaskOperation,
)
from .steps import (
    new_add_comment_step,
    new_create_task_step,
    new_get_task_step,
    new_list_tasks_step,
    new_update_task_step,
)

__all__ = [
    "AddCommentInput",
    "AddCommentOperation",
    "AddCommentOutput",
    "Client",
    "Config",
    "Connection",
    "CreateTaskInput",
    "CreateTaskOperation",
    "CreateTaskOutput",
    "Credentials",
    "CustomFieldValue",
    "CustomFieldValueInput",
    "EnumOptionReference",
    "GetTaskInput",
    "GetTaskOperation",
    "ListTasksInput",
    "ListTasksOperation",
    "ListTasksOutput",
    "ProjectReference",
    "SectionReference",
    "Task",
    "TaskMembership",
    "TaskReference",
    "UpdateTaskInput",
    "UpdateTaskOperation",
    "UpdateTaskOutput",
    "UserReference",
    "WorkspaceReference",
    "new_add_comment_step",
    "new_connection",
    "new_connection_with_operations",
    "new_create_task_step",
    "new_get_task_step",
    "new_list_tasks_step",
    "new_project_connection",
    "new_update_task_step",
]
