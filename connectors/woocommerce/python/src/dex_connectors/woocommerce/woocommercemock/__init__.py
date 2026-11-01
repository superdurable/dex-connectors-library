# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest-backed WooCommerce scripts for consuming Flow tests."""

from dex_connectors.sdk import ConnectionRef, Failure
from dex_connectors.sdk.connectormock import Case, Query

from ..client import CONNECTOR_ID, FOUND, GET_ORDER_DEFINITION, LIST_ORDERS_DEFINITION, LISTED, NOT_FOUND
from ..connection import Connection, new_connection_with_operations
from ..models import GetOrderInput, ListOrdersInput, Order, OrderPage

_ORDER = Order(101, "processing", "29.00", "USD")


def list_orders_listed(value: OrderPage, failure: Failure | None = None) -> Case[OrderPage]:
    """Script the successful listed route."""
    return Case.branch_result(LISTED, value, failure)


def list_orders_default_answer() -> Case[OrderPage]:
    """Return the manifest's listPayments defaultAnswer."""
    return list_orders_listed(OrderPage((_ORDER,)))


def get_order_found(value: Order, failure: Failure | None = None) -> Case[Order]:
    """Script the successful found route."""
    return Case.branch_result(FOUND, value, failure)


def get_order_not_found(value: Order, failure: Failure | None = None) -> Case[Order]:
    """Script a missing order."""
    return Case.branch_result(NOT_FOUND, value, failure)


def get_order_default_answer() -> Case[Order]:
    """Return the manifest's getPayment defaultAnswer."""
    return get_order_found(_ORDER)


class Mock:
    """Script WooCommerce Queries, defaulting to their manifest answers."""

    def __init__(self, connection_name: str = "woocommerce") -> None:
        self.list_orders_script: Query[ListOrdersInput, OrderPage] = Query(LIST_ORDERS_DEFINITION)
        self.list_orders_script.default(list_orders_default_answer())
        self.get_order_script: Query[GetOrderInput, Order] = Query(GET_ORDER_DEFINITION)
        self.get_order_script.default(get_order_default_answer())
        self._connection = new_connection_with_operations(
            self.list_orders_script, self.get_order_script, ConnectionRef(CONNECTOR_ID, connection_name)
        )

    def connection(self) -> Connection:
        """Return this mock's named connection for Step registration."""
        return self._connection


__all__ = [
    "Mock",
    "list_orders_listed",
    "list_orders_default_answer",
    "get_order_found",
    "get_order_not_found",
    "get_order_default_answer",
]
