# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Shoplazza Query connector for Dex Flows."""

from .client import Client, Config, Credentials, GetShopOperation
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import GetShopInput, GetShopOutput
from .steps import new_get_shop_step

__all__ = [
    "Client",
    "Config",
    "Connection",
    "Credentials",
    "GetShopInput",
    "GetShopOperation",
    "GetShopOutput",
    "new_connection",
    "new_connection_with_operations",
    "new_get_shop_step",
    "new_project_connection",
]
