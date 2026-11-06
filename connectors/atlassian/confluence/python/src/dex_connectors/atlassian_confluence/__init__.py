# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Dex connector for Atlassian Confluence pages and comments."""

from .client import Client, Config, Credentials
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import (
    AddCommentInput,
    AddCommentOutput,
    CreatePageInput,
    CreatePageOutput,
    GetPageInput,
    Page,
    PageSearchFilter,
    SearchPagesInput,
    SearchPagesOutput,
    UpdatePageInput,
    UpdatePageOutput,
)
from .steps import (
    new_add_comment_step,
    new_create_page_step,
    new_get_page_step,
    new_search_pages_step,
    new_update_page_step,
)

__all__ = [
    "Client",
    "Config",
    "Credentials",
    "Connection",
    "new_connection",
    "new_connection_with_operations",
    "new_project_connection",
    "AddCommentInput",
    "AddCommentOutput",
    "CreatePageInput",
    "CreatePageOutput",
    "GetPageInput",
    "Page",
    "PageSearchFilter",
    "SearchPagesInput",
    "SearchPagesOutput",
    "UpdatePageInput",
    "UpdatePageOutput",
    "new_add_comment_step",
    "new_create_page_step",
    "new_get_page_step",
    "new_search_pages_step",
    "new_update_page_step",
]
