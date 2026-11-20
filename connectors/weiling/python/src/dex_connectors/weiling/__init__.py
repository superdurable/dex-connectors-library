# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Weiling connector for Dex Flows."""

from .client import Client, Config, Credentials, ListDepartmentsOperation
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import DepartmentSummary, ListDepartmentsInput, ListDepartmentsOutput
from .steps import new_list_departments_step

__all__ = [
    "Client",
    "Config",
    "Connection",
    "Credentials",
    "DepartmentSummary",
    "ListDepartmentsInput",
    "ListDepartmentsOperation",
    "ListDepartmentsOutput",
    "new_connection",
    "new_connection_with_operations",
    "new_list_departments_step",
    "new_project_connection",
]
