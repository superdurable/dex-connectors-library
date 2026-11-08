# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Amplitude user Queries for Dex Flows."""

from .client import Client, Config, Credentials, SearchUsersOperation
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import SearchUsersInput, UserSearchPage
from .steps import new_search_users_step

__all__ = [
    "UserSearchPage",
    "Client",
    "Config",
    "Connection",
    "Credentials",
    "SearchUsersInput",
    "SearchUsersOperation",
    "new_connection",
    "new_connection_with_operations",
    "new_search_users_step",
    "new_project_connection",
]
