# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import QueryRechargeOrderInput, RechargeOrderResult
from .definitions import CONNECTOR_ID, DEFECT, FOUND, INVALID_RESPONSE, PROVIDER_REJECTED, QUERY_RECHARGE_ORDER_DEFINITION

def query_recharge_order_found(value: RechargeOrderResult, failure: Failure | None = None) -> Case[RechargeOrderResult]:
    return Case.branch_result(FOUND, value, failure)

setattr(query_recharge_order_found, '__module__', "dex_connectors.feihan.feihanmock")

def query_recharge_order_provider_rejected(value: RechargeOrderResult, failure: Failure | None = None) -> Case[RechargeOrderResult]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(query_recharge_order_provider_rejected, '__module__', "dex_connectors.feihan.feihanmock")

def query_recharge_order_invalid_response(value: RechargeOrderResult, failure: Failure | None = None) -> Case[RechargeOrderResult]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(query_recharge_order_invalid_response, '__module__', "dex_connectors.feihan.feihanmock")

def query_recharge_order_defect(value: RechargeOrderResult, failure: Failure | None = None) -> Case[RechargeOrderResult]:
    return Case.branch_result(DEFECT, value, failure)

setattr(query_recharge_order_defect, '__module__', "dex_connectors.feihan.feihanmock")

def query_recharge_order_retry(failure: Failure, after_seconds: float = 0) -> Case[RechargeOrderResult]:
    return Case.retry(failure, after_seconds)

setattr(query_recharge_order_retry, '__module__', "dex_connectors.feihan.feihanmock")

def query_recharge_order_found_order() -> Case[RechargeOrderResult]:
    return Case.branch_result(FOUND, RechargeOrderResult(merchant_order_id='ORDER123', recharge_status=1))

setattr(query_recharge_order_found_order, '__module__', "dex_connectors.feihan.feihanmock")


class Mock:

    def __init__(self, connection_name: str = "feihan") -> None:
        self.query_recharge_order_script: Query[QueryRechargeOrderInput, RechargeOrderResult] = Query(QUERY_RECHARGE_ORDER_DEFINITION)
        self.query_recharge_order_script.default(query_recharge_order_found_order())
        self._connection = new_connection_with_operations(
            query_recharge_order=self.query_recharge_order_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.feihan.feihanmock")

__all__ = [
    "Mock",
    "query_recharge_order_found",
    "query_recharge_order_provider_rejected",
    "query_recharge_order_invalid_response",
    "query_recharge_order_defect",
    "query_recharge_order_retry",
    "query_recharge_order_found_order",
]
