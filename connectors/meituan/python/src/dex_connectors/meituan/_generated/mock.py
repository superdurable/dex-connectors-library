# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import OrderStatusResult, QueryOrderStatusInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, INVALID_RESPONSE, PROVIDER_REJECTED, QUERY_ORDER_STATUS_DEFINITION

def query_order_status_found(value: OrderStatusResult, failure: Failure | None = None) -> Case[OrderStatusResult]:
    return Case.branch_result(FOUND, value, failure)

setattr(query_order_status_found, '__module__', "dex_connectors.meituan.meituanmock")

def query_order_status_provider_rejected(value: OrderStatusResult, failure: Failure | None = None) -> Case[OrderStatusResult]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(query_order_status_provider_rejected, '__module__', "dex_connectors.meituan.meituanmock")

def query_order_status_invalid_response(value: OrderStatusResult, failure: Failure | None = None) -> Case[OrderStatusResult]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(query_order_status_invalid_response, '__module__', "dex_connectors.meituan.meituanmock")

def query_order_status_defect(value: OrderStatusResult, failure: Failure | None = None) -> Case[OrderStatusResult]:
    return Case.branch_result(DEFECT, value, failure)

setattr(query_order_status_defect, '__module__', "dex_connectors.meituan.meituanmock")

def query_order_status_retry(failure: Failure, after_seconds: float = 0) -> Case[OrderStatusResult]:
    return Case.retry(failure, after_seconds)

setattr(query_order_status_retry, '__module__', "dex_connectors.meituan.meituanmock")

def query_order_status_found_order() -> Case[OrderStatusResult]:
    return Case.branch_result(FOUND, OrderStatusResult(delivery_id=123456, meituan_order_id='MT123456789', merchant_order_id='ORDER123', status=20, operated_at=1794552600))

setattr(query_order_status_found_order, '__module__', "dex_connectors.meituan.meituanmock")


class Mock:

    def __init__(self, connection_name: str = "meituan") -> None:
        self.query_order_status_script: Query[QueryOrderStatusInput, OrderStatusResult] = Query(QUERY_ORDER_STATUS_DEFINITION)
        self.query_order_status_script.default(query_order_status_found_order())
        self._connection = new_connection_with_operations(
            query_order_status=self.query_order_status_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.meituan.meituanmock")

__all__ = [
    "Mock",
    "query_order_status_found",
    "query_order_status_provider_rejected",
    "query_order_status_invalid_response",
    "query_order_status_defect",
    "query_order_status_retry",
    "query_order_status_found_order",
]
