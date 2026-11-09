# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""WordPress REST connector for Dex Python Flows."""

from .client import Client, Config, CreatePostOperation, Credentials, GetPostOperation, ListPostsOperation
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import CreatedPost, CreatePostInput, GetPostInput, ListPostsInput, Post, PostPage, RenderedText
from .steps import new_create_post_step, new_get_post_step, new_list_posts_step

__all__ = [
    "Client",
    "Config",
    "Credentials",
    "CreatePostOperation",
    "GetPostOperation",
    "ListPostsOperation",
    "Connection",
    "new_connection",
    "new_connection_with_operations",
    "new_project_connection",
    "CreatePostInput",
    "CreatedPost",
    "GetPostInput",
    "ListPostsInput",
    "Post",
    "PostPage",
    "RenderedText",
    "new_create_post_step",
    "new_get_post_step",
    "new_list_posts_step",
]
