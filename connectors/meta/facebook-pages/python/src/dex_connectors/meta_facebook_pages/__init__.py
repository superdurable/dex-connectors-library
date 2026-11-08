# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Facebook Pages metadata Queries for Dex Flows."""

from .client import Client, Config, Credentials, GetPostOperation, ListPostsOperation
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import GetPostInput, ListPostsInput, PagePostMetadata, PagePostsPage
from .steps import new_get_post_step, new_list_posts_step

__all__ = [
    "Client",
    "Config",
    "Connection",
    "Credentials",
    "GetPostInput",
    "GetPostOperation",
    "PagePostMetadata",
    "PagePostsPage",
    "ListPostsInput",
    "ListPostsOperation",
    "new_connection",
    "new_connection_with_operations",
    "new_get_post_step",
    "new_list_posts_step",
    "new_project_connection",
]
