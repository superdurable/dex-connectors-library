# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Wangdian connector for Dex Flows."""

from .client import Client, Config, Credentials, ListShopsOperation
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import ListShopsInput, ListShopsOutput, ShopSummary
from .steps import new_list_shops_step

__all__ = [
    "Client",
    "Config",
    "Connection",
    "Credentials",
    "ShopSummary",
    "ListShopsInput",
    "ListShopsOperation",
    "ListShopsOutput",
    "new_connection",
    "new_connection_with_operations",
    "new_list_shops_step",
    "new_project_connection",
]
