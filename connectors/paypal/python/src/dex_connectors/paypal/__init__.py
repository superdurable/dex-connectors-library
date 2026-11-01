# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""PayPal order and transaction operations for Dex connector Steps."""

from .client import (
    CONNECTOR_ID,
    DEFECT,
    FOUND,
    GET_ORDER_DEFINITION,
    INVALID_RESPONSE,
    LIST_TRANSACTIONS_DEFINITION,
    LISTED,
    NOT_FOUND,
    PROVIDER_REJECTED,
    Client,
    Config,
    Credentials,
    GetOrderOperation,
    ListTransactionsOperation,
)
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import (
    GetOrderInput,
    ListTransactionsInput,
    Order,
    TransactionAmount,
    TransactionDetail,
    TransactionInfo,
    TransactionPage,
)
from .steps import new_get_order_step, new_list_transactions_step

__all__ = [
    "Client",
    "Config",
    "Connection",
    "Credentials",
    "GetOrderInput",
    "GetOrderOperation",
    "ListTransactionsInput",
    "ListTransactionsOperation",
    "Order",
    "TransactionAmount",
    "TransactionDetail",
    "TransactionInfo",
    "TransactionPage",
    "new_connection",
    "new_connection_with_operations",
    "new_get_order_step",
    "new_list_transactions_step",
    "new_project_connection",
    "CONNECTOR_ID",
    "DEFECT",
    "FOUND",
    "GET_ORDER_DEFINITION",
    "INVALID_RESPONSE",
    "LIST_TRANSACTIONS_DEFINITION",
    "LISTED",
    "NOT_FOUND",
    "PROVIDER_REJECTED",
]
