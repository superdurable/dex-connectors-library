# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Sentry issue operations for Dex connector Steps."""

from .client import (
    CONNECTOR_ID,
    DEFECT,
    FOUND,
    GET_ISSUE_DEFINITION,
    INVALID_RESPONSE,
    ISSUE_STATUSES,
    LIST_ISSUES_DEFINITION,
    LISTED,
    NOT_FOUND,
    PROVIDER_REJECTED,
    UPDATE_ISSUE_STATUS_DEFINITION,
    UPDATED,
    Client,
    Config,
    Credentials,
    GetIssueOperation,
    ListIssuesOperation,
    UpdateIssueStatusOperation,
)
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import GetIssueInput, Issue, IssuePage, ListIssuesInput, UpdateIssueStatusInput
from .steps import new_get_issue_step, new_list_issues_step, new_update_issue_status_step

__all__ = [
    "Client",
    "Config",
    "Connection",
    "Credentials",
    "GetIssueInput",
    "GetIssueOperation",
    "Issue",
    "IssuePage",
    "ListIssuesInput",
    "ListIssuesOperation",
    "UpdateIssueStatusInput",
    "UpdateIssueStatusOperation",
    "new_connection",
    "new_connection_with_operations",
    "new_get_issue_step",
    "new_list_issues_step",
    "new_project_connection",
    "new_update_issue_status_step",
    "CONNECTOR_ID",
    "DEFECT",
    "FOUND",
    "GET_ISSUE_DEFINITION",
    "INVALID_RESPONSE",
    "ISSUE_STATUSES",
    "LIST_ISSUES_DEFINITION",
    "LISTED",
    "NOT_FOUND",
    "PROVIDER_REJECTED",
    "UPDATE_ISSUE_STATUS_DEFINITION",
    "UPDATED",
]
