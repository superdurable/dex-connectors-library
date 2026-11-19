# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetWaybillBalanceInput, GetWaybillBalanceOutput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_WAYBILL_BALANCE_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED

def get_waybill_balance_found(value: GetWaybillBalanceOutput, failure: Failure | None = None) -> Case[GetWaybillBalanceOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_waybill_balance_found, '__module__', "dex_connectors.yunda.yundamock")

def get_waybill_balance_provider_rejected(value: GetWaybillBalanceOutput, failure: Failure | None = None) -> Case[GetWaybillBalanceOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_waybill_balance_provider_rejected, '__module__', "dex_connectors.yunda.yundamock")

def get_waybill_balance_invalid_response(value: GetWaybillBalanceOutput, failure: Failure | None = None) -> Case[GetWaybillBalanceOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_waybill_balance_invalid_response, '__module__', "dex_connectors.yunda.yundamock")

def get_waybill_balance_defect(value: GetWaybillBalanceOutput, failure: Failure | None = None) -> Case[GetWaybillBalanceOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_waybill_balance_defect, '__module__', "dex_connectors.yunda.yundamock")

def get_waybill_balance_retry(failure: Failure, after_seconds: float = 0) -> Case[GetWaybillBalanceOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_waybill_balance_retry, '__module__', "dex_connectors.yunda.yundamock")

def get_waybill_balance_balance() -> Case[GetWaybillBalanceOutput]:
    return Case.branch_result(FOUND, GetWaybillBalanceOutput(free_count=1, paid_count=2))

setattr(get_waybill_balance_balance, '__module__', "dex_connectors.yunda.yundamock")


class Mock:

    def __init__(self, connection_name: str = "yunda") -> None:
        self.get_waybill_balance_script: Query[GetWaybillBalanceInput, GetWaybillBalanceOutput] = Query(GET_WAYBILL_BALANCE_DEFINITION)
        self.get_waybill_balance_script.default(get_waybill_balance_balance())
        self._connection = new_connection_with_operations(
            get_waybill_balance=self.get_waybill_balance_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.yunda.yundamock")

__all__ = [
    "Mock",
    "get_waybill_balance_found",
    "get_waybill_balance_provider_rejected",
    "get_waybill_balance_invalid_response",
    "get_waybill_balance_defect",
    "get_waybill_balance_retry",
    "get_waybill_balance_balance",
]
