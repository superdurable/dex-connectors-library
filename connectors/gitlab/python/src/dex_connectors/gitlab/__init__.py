# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""GitLab project, issue, and label operations for Dex connector Steps."""

from .client import (
    CONNECTOR_ID,
    DEFECT,
    FOUND,
    GET_PROJECT_DEFINITION,
    INVALID_RESPONSE,
    LIST_ISSUES_DEFINITION,
    LISTED,
    NOT_FOUND,
    PROVIDER_REJECTED,
    UPDATE_ISSUE_LABELS_DEFINITION,
    UPDATED,
    Client,
    Config,
    Credentials,
    GetProjectOperation,
    ListIssuesOperation,
    UpdateIssueLabelsOperation,
)
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import GetProjectInput, Issue, IssuesPage, ListIssuesInput, Project, UpdateIssueLabelsInput
from .steps import new_get_project_step, new_list_issues_step, new_update_issue_labels_step

__all__ = [
    "Client",
    "Config",
    "Connection",
    "Credentials",
    "GetProjectInput",
    "GetProjectOperation",
    "Issue",
    "IssuesPage",
    "ListIssuesInput",
    "ListIssuesOperation",
    "Project",
    "UpdateIssueLabelsInput",
    "UpdateIssueLabelsOperation",
    "new_connection",
    "new_connection_with_operations",
    "new_get_project_step",
    "new_list_issues_step",
    "new_project_connection",
    "new_update_issue_labels_step",
    "CONNECTOR_ID",
    "DEFECT",
    "FOUND",
    "GET_PROJECT_DEFINITION",
    "INVALID_RESPONSE",
    "LIST_ISSUES_DEFINITION",
    "LISTED",
    "NOT_FOUND",
    "PROVIDER_REJECTED",
    "UPDATE_ISSUE_LABELS_DEFINITION",
    "UPDATED",
]
