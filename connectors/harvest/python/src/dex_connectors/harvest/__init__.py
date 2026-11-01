# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Harvest project and time entry Queries for Dex connector Steps."""

from .client import (
    CONNECTOR_ID,
    DEFECT,
    INVALID_RESPONSE,
    LIST_PROJECTS_DEFINITION,
    LIST_TIME_ENTRIES_DEFINITION,
    LISTED,
    PROVIDER_REJECTED,
    Client,
    Config,
    Credentials,
    ListProjectsOperation,
    ListTimeEntriesOperation,
)
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import (
    ListProjectsInput,
    ListTimeEntriesInput,
    Project,
    ProjectPage,
    TimeEntry,
    TimeEntryPage,
    TimeEntryProject,
)
from .steps import new_list_projects_step, new_list_time_entries_step

__all__ = [
    "Client",
    "Config",
    "Connection",
    "Credentials",
    "ListProjectsInput",
    "ListProjectsOperation",
    "ListTimeEntriesInput",
    "ListTimeEntriesOperation",
    "Project",
    "ProjectPage",
    "TimeEntry",
    "TimeEntryPage",
    "TimeEntryProject",
    "new_connection",
    "new_connection_with_operations",
    "new_list_projects_step",
    "new_list_time_entries_step",
    "new_project_connection",
    "CONNECTOR_ID",
    "DEFECT",
    "INVALID_RESPONSE",
    "LIST_PROJECTS_DEFINITION",
    "LIST_TIME_ENTRIES_DEFINITION",
    "LISTED",
    "PROVIDER_REJECTED",
]
