# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Figma file and comment Queries for Dex connector Steps."""

from .client import (
    CONNECTOR_ID,
    DEFECT,
    FOUND,
    GET_FILE_COMMENTS_DEFINITION,
    GET_FILE_DEFINITION,
    INVALID_RESPONSE,
    LISTED,
    NOT_FOUND,
    PROVIDER_REJECTED,
    Client,
    Config,
    Credentials,
    GetFileCommentsOperation,
    GetFileOperation,
)
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import File, FileComment, FileComments, GetFileCommentsInput, GetFileInput
from .steps import new_get_file_comments_step, new_get_file_step

__all__ = [
    "Client",
    "Config",
    "Connection",
    "Credentials",
    "File",
    "FileComment",
    "FileComments",
    "GetFileCommentsInput",
    "GetFileCommentsOperation",
    "GetFileInput",
    "GetFileOperation",
    "new_connection",
    "new_connection_with_operations",
    "new_get_file_comments_step",
    "new_get_file_step",
    "new_project_connection",
    "CONNECTOR_ID",
    "DEFECT",
    "FOUND",
    "GET_FILE_COMMENTS_DEFINITION",
    "GET_FILE_DEFINITION",
    "INVALID_RESPONSE",
    "LISTED",
    "NOT_FOUND",
    "PROVIDER_REJECTED",
]
