# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetOrderInput, ListOrdersInput, Order, OrderPage
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_ORDER_DEFINITION, INVALID_RESPONSE, LISTED, LIST_ORDERS_DEFINITION, NOT_FOUND, PROVIDER_REJECTED

def list_orders_listed(value: OrderPage, failure: Failure | None = None) -> Case[OrderPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_orders_listed, '__module__', "dex_connectors.shopify.shopifymock")

def list_orders_not_found(value: OrderPage, failure: Failure | None = None) -> Case[OrderPage]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(list_orders_not_found, '__module__', "dex_connectors.shopify.shopifymock")

def list_orders_provider_rejected(value: OrderPage, failure: Failure | None = None) -> Case[OrderPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_orders_provider_rejected, '__module__', "dex_connectors.shopify.shopifymock")

def list_orders_invalid_response(value: OrderPage, failure: Failure | None = None) -> Case[OrderPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_orders_invalid_response, '__module__', "dex_connectors.shopify.shopifymock")

def list_orders_defect(value: OrderPage, failure: Failure | None = None) -> Case[OrderPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_orders_defect, '__module__', "dex_connectors.shopify.shopifymock")

def list_orders_retry(failure: Failure, after_seconds: float = 0) -> Case[OrderPage]:
    return Case.retry(failure, after_seconds)

setattr(list_orders_retry, '__module__', "dex_connectors.shopify.shopifymock")

def list_orders_default_answer() -> Case[OrderPage]:
    return Case.branch_result(LISTED, OrderPage(orders=(Order(id=101, name='#101', financial_status='paid', total_price='29.00', currency='USD'),)))

setattr(list_orders_default_answer, '__module__', "dex_connectors.shopify.shopifymock")

def get_order_found(value: Order, failure: Failure | None = None) -> Case[Order]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_order_found, '__module__', "dex_connectors.shopify.shopifymock")

def get_order_not_found(value: Order, failure: Failure | None = None) -> Case[Order]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_order_not_found, '__module__', "dex_connectors.shopify.shopifymock")

def get_order_provider_rejected(value: Order, failure: Failure | None = None) -> Case[Order]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_order_provider_rejected, '__module__', "dex_connectors.shopify.shopifymock")

def get_order_invalid_response(value: Order, failure: Failure | None = None) -> Case[Order]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_order_invalid_response, '__module__', "dex_connectors.shopify.shopifymock")

def get_order_defect(value: Order, failure: Failure | None = None) -> Case[Order]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_order_defect, '__module__', "dex_connectors.shopify.shopifymock")

def get_order_retry(failure: Failure, after_seconds: float = 0) -> Case[Order]:
    return Case.retry(failure, after_seconds)

setattr(get_order_retry, '__module__', "dex_connectors.shopify.shopifymock")

def get_order_default_answer() -> Case[Order]:
    return Case.branch_result(FOUND, Order(id=101, name='#101', financial_status='paid', total_price='29.00', currency='USD'))

setattr(get_order_default_answer, '__module__', "dex_connectors.shopify.shopifymock")


class Mock:

    def __init__(self, connection_name: str = "shopify") -> None:
        self.list_orders_script: Query[ListOrdersInput, OrderPage] = Query(LIST_ORDERS_DEFINITION)
        self.list_orders_script.default(list_orders_default_answer())
        self.get_order_script: Query[GetOrderInput, Order] = Query(GET_ORDER_DEFINITION)
        self.get_order_script.default(get_order_default_answer())
        self._connection = new_connection_with_operations(
            list_orders=self.list_orders_script,
            get_order=self.get_order_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.shopify.shopifymock")

__all__ = [
    "Mock",
    "list_orders_listed",
    "list_orders_not_found",
    "list_orders_provider_rejected",
    "list_orders_invalid_response",
    "list_orders_defect",
    "list_orders_retry",
    "list_orders_default_answer",
    "get_order_found",
    "get_order_not_found",
    "get_order_provider_rejected",
    "get_order_invalid_response",
    "get_order_defect",
    "get_order_retry",
    "get_order_default_answer",
]
