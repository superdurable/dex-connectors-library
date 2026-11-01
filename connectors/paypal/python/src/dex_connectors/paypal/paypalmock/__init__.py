# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest-backed PayPal scripts for consuming Flow tests."""

from dex_connectors.sdk import ConnectionRef, Failure
from dex_connectors.sdk.connectormock import Case, Query

from ..client import (
    CONNECTOR_ID,
    FOUND,
    GET_ORDER_DEFINITION,
    LIST_TRANSACTIONS_DEFINITION,
    LISTED,
    NOT_FOUND,
)
from ..connection import Connection, new_connection_with_operations
from ..models import (
    GetOrderInput,
    ListTransactionsInput,
    Order,
    TransactionAmount,
    TransactionDetail,
    TransactionInfo,
    TransactionPage,
)


def get_order_found(value: Order, failure: Failure | None = None) -> Case[Order]:
    """Script the found route."""
    return Case.branch_result(FOUND, value, failure)


def get_order_not_found(value: Order, failure: Failure | None = None) -> Case[Order]:
    """Script a missing order."""
    return Case.branch_result(NOT_FOUND, value, failure)


def get_order_default_answer() -> Case[Order]:
    """Return the manifest's getOrder default answer."""
    return get_order_found(Order("ORDER101", "COMPLETED"))


def list_transactions_listed(value: TransactionPage, failure: Failure | None = None) -> Case[TransactionPage]:
    """Script the listed route."""
    return Case.branch_result(LISTED, value, failure)


def list_transactions_not_found(
    value: TransactionPage, failure: Failure | None = None
) -> Case[TransactionPage]:
    """Script a missing transaction resource."""
    return Case.branch_result(NOT_FOUND, value, failure)


def list_transactions_default_answer() -> Case[TransactionPage]:
    """Return the manifest's listTransactions default answer."""
    detail = TransactionDetail(TransactionInfo("TXN101", "S", TransactionAmount("USD", "29.00")))
    return list_transactions_listed(TransactionPage((detail,), 1, 1))


class Mock:
    """Script PayPal operations, defaulting to their manifest answers."""

    def __init__(self, connection_name: str = "paypal") -> None:
        self.get_order_script: Query[GetOrderInput, Order] = Query(GET_ORDER_DEFINITION)
        self.get_order_script.default(get_order_default_answer())
        self.list_transactions_script: Query[ListTransactionsInput, TransactionPage] = Query(
            LIST_TRANSACTIONS_DEFINITION
        )
        self.list_transactions_script.default(list_transactions_default_answer())
        self._connection = new_connection_with_operations(
            self.list_transactions_script, self.get_order_script, ConnectionRef(CONNECTOR_ID, connection_name)
        )

    def connection(self) -> Connection:
        """Return this mock's named connection for Step registration."""
        return self._connection


__all__ = [
    "Mock",
    "get_order_found",
    "get_order_not_found",
    "get_order_default_answer",
    "list_transactions_listed",
    "list_transactions_not_found",
    "list_transactions_default_answer",
]
