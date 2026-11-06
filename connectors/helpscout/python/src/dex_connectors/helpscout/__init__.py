# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Dex-native Help Scout Inbox operations."""

from .client import Client, Config, Credentials
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import *  # noqa: F403
from .steps import *  # noqa: F403

__all__ = [
    "Client",
    "Config",
    "Credentials",
    "Connection",
    "new_connection",
    "new_connection_with_operations",
    "new_project_connection",
]
