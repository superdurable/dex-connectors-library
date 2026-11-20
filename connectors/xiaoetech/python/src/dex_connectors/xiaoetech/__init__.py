# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Xiaoetech connector for Dex Flows."""

from .client import Client, Config, Credentials, ListRootGoodsCategoriesOperation
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import ListRootGoodsCategoriesInput, ListRootGoodsCategoriesOutput
from .steps import new_list_root_goods_categories_step

__all__ = [
    "Client",
    "Config",
    "Credentials",
    "ListRootGoodsCategoriesOperation",
    "Connection",
    "new_connection",
    "new_connection_with_operations",
    "new_project_connection",
    "ListRootGoodsCategoriesInput",
    "ListRootGoodsCategoriesOutput",
    "new_list_root_goods_categories_step",
]
