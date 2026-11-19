# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import OrderStatus, QueryOrderStatusInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, INVALID_RESPONSE, PROVIDER_REJECTED, QUERY_ORDER_STATUS_DEFINITION

def query_order_status_found(value: OrderStatus, failure: Failure | None = None) -> Case[OrderStatus]:
    return Case.branch_result(FOUND, value, failure)

setattr(query_order_status_found, '__module__', "dex_connectors.dada.dadamock")

def query_order_status_provider_rejected(value: OrderStatus, failure: Failure | None = None) -> Case[OrderStatus]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(query_order_status_provider_rejected, '__module__', "dex_connectors.dada.dadamock")

def query_order_status_invalid_response(value: OrderStatus, failure: Failure | None = None) -> Case[OrderStatus]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(query_order_status_invalid_response, '__module__', "dex_connectors.dada.dadamock")

def query_order_status_defect(value: OrderStatus, failure: Failure | None = None) -> Case[OrderStatus]:
    return Case.branch_result(DEFECT, value, failure)

setattr(query_order_status_defect, '__module__', "dex_connectors.dada.dadamock")

def query_order_status_retry(failure: Failure, after_seconds: float = 0) -> Case[OrderStatus]:
    return Case.retry(failure, after_seconds)

setattr(query_order_status_retry, '__module__', "dex_connectors.dada.dadamock")

def query_order_status_found_default() -> Case[OrderStatus]:
    return Case.branch_result(FOUND, OrderStatus(order_id='orderNo1653708880866', status_code=1, status_message='待接单'))

setattr(query_order_status_found_default, '__module__', "dex_connectors.dada.dadamock")


class Mock:

    def __init__(self, connection_name: str = "dada") -> None:
        self.query_order_status_script: Query[QueryOrderStatusInput, OrderStatus] = Query(QUERY_ORDER_STATUS_DEFINITION)
        self.query_order_status_script.default(query_order_status_found_default())
        self._connection = new_connection_with_operations(
            query_order_status=self.query_order_status_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.dada.dadamock")

__all__ = [
    "Mock",
    "query_order_status_found",
    "query_order_status_provider_rejected",
    "query_order_status_invalid_response",
    "query_order_status_defect",
    "query_order_status_retry",
    "query_order_status_found_default",
]
