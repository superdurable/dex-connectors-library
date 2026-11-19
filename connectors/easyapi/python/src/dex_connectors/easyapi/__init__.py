# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""EasyAPI E-Invoice connector."""

from .client import (
    CONNECTOR_ID,
    DEFECT,
    FOUND,
    INVALID_RESPONSE,
    PROVIDER_REJECTED,
    QUERY_SHOP_DEFINITION,
    Client,
    Config,
    Credentials,
    QueryShopOperation,
)
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import QueryShopInput, ShopResult
from .steps import new_query_shop_step

__all__ = [
    "CONNECTOR_ID",
    "DEFECT",
    "INVALID_RESPONSE",
    "FOUND",
    "QUERY_SHOP_DEFINITION",
    "PROVIDER_REJECTED",
    "Client",
    "Config",
    "Credentials",
    "QueryShopOperation",
    "Connection",
    "new_connection",
    "new_connection_with_operations",
    "new_project_connection",
    "QueryShopInput",
    "ShopResult",
    "new_query_shop_step",
]
