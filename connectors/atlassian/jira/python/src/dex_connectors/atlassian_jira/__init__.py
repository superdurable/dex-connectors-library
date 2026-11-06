# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Jira Cloud search, issue, workflow, and comment operations for Dex Flows."""

from ._generated.config import Config, Credentials
from .client import Client
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import (
    AddCommentInput,
    AddCommentOutput,
    CreateIssueInput,
    CreateIssueOutput,
    GetIssueInput,
    Issue,
    IssueSearchFilter,
    SearchIssuesInput,
    SearchIssuesOutput,
    TransitionIssueInput,
    TransitionIssueOutput,
)
from .operations import (
    AddCommentOperation,
    CreateIssueOperation,
    GetIssueOperation,
    SearchIssuesOperation,
    TransitionIssueOperation,
)
from .steps import (
    new_add_comment_step,
    new_create_issue_step,
    new_get_issue_step,
    new_search_issues_step,
    new_transition_issue_step,
)

__all__ = [
    "AddCommentInput",
    "AddCommentOperation",
    "AddCommentOutput",
    "Client",
    "Config",
    "Connection",
    "CreateIssueInput",
    "CreateIssueOperation",
    "CreateIssueOutput",
    "Credentials",
    "GetIssueInput",
    "GetIssueOperation",
    "Issue",
    "IssueSearchFilter",
    "SearchIssuesInput",
    "SearchIssuesOperation",
    "SearchIssuesOutput",
    "TransitionIssueInput",
    "TransitionIssueOperation",
    "TransitionIssueOutput",
    "new_add_comment_step",
    "new_connection",
    "new_connection_with_operations",
    "new_create_issue_step",
    "new_get_issue_step",
    "new_project_connection",
    "new_search_issues_step",
    "new_transition_issue_step",
]
