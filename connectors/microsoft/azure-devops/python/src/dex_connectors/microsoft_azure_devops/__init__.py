# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Azure DevOps Queries for Dex Flows."""

from .client import Client, Config, Credentials, GetWorkItemOperation, ListProjectsOperation
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import GetWorkItemInput, ListProjectsInput, ProjectMetadata, Projects, WorkItemMetadata
from .steps import new_get_work_item_step, new_list_projects_step

__all__ = [
    "Client", "Config", "Connection", "Credentials", "WorkItemMetadata", "ProjectMetadata", "Projects",
    "GetWorkItemInput", "GetWorkItemOperation", "ListProjectsInput", "ListProjectsOperation",
    "new_connection", "new_connection_with_operations",
    "new_get_work_item_step", "new_list_projects_step", "new_project_connection",
]
