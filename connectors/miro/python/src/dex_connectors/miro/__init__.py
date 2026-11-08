# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Miro board Queries for Dex Flows."""

from .client import Client, Config, Credentials, GetBoardOperation
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import BoardMetadata, GetBoardInput
from .steps import new_get_board_step

__all__ = [
    "BoardMetadata",
    "Client",
    "Config",
    "Connection",
    "Credentials",
    "GetBoardInput",
    "GetBoardOperation",
    "new_connection",
    "new_connection_with_operations",
    "new_get_board_step",
    "new_project_connection",
]
