# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Teamwork task Queries for Dex Flows."""

from .client import Client, Config, Credentials, GetTaskOperation
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import GetTaskInput, TaskSummary
from .steps import new_get_task_step

__all__ = [
    "TaskSummary",
    "Client",
    "Config",
    "Connection",
    "Credentials",
    "GetTaskInput",
    "GetTaskOperation",
    "new_connection",
    "new_connection_with_operations",
    "new_get_task_step",
    "new_project_connection",
]
