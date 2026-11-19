# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Xingyapi connector for Dex Flows."""

from .client import Client, Config, Credentials, GetInstanceOnlineStatusOperation
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import GetInstanceOnlineStatusInput, GetInstanceOnlineStatusOutput
from .steps import new_get_instance_online_status_step

__all__ = [
    "Client",
    "Config",
    "Connection",
    "Credentials",
    "GetInstanceOnlineStatusInput",
    "GetInstanceOnlineStatusOperation",
    "GetInstanceOnlineStatusOutput",
    "new_connection",
    "new_connection_with_operations",
    "new_get_instance_online_status_step",
    "new_project_connection",
]
