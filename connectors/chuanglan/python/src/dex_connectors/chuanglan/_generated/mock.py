# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetBalanceInput, GetBalanceOutput
from .definitions import CONNECTOR_ID, DEFECT, FETCHED, GET_BALANCE_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED

def get_balance_fetched(value: GetBalanceOutput, failure: Failure | None = None) -> Case[GetBalanceOutput]:
    return Case.branch_result(FETCHED, value, failure)

setattr(get_balance_fetched, '__module__', "dex_connectors.chuanglan.chuanglanmock")

def get_balance_provider_rejected(value: GetBalanceOutput, failure: Failure | None = None) -> Case[GetBalanceOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_balance_provider_rejected, '__module__', "dex_connectors.chuanglan.chuanglanmock")

def get_balance_invalid_response(value: GetBalanceOutput, failure: Failure | None = None) -> Case[GetBalanceOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_balance_invalid_response, '__module__', "dex_connectors.chuanglan.chuanglanmock")

def get_balance_defect(value: GetBalanceOutput, failure: Failure | None = None) -> Case[GetBalanceOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_balance_defect, '__module__', "dex_connectors.chuanglan.chuanglanmock")

def get_balance_retry(failure: Failure, after_seconds: float = 0) -> Case[GetBalanceOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_balance_retry, '__module__', "dex_connectors.chuanglan.chuanglanmock")

def get_balance_sample_balance() -> Case[GetBalanceOutput]:
    return Case.branch_result(FETCHED, GetBalanceOutput(remaining_messages=953))

setattr(get_balance_sample_balance, '__module__', "dex_connectors.chuanglan.chuanglanmock")


class Mock:

    def __init__(self, connection_name: str = "chuanglan") -> None:
        self.get_balance_script: Query[GetBalanceInput, GetBalanceOutput] = Query(GET_BALANCE_DEFINITION)
        self.get_balance_script.default(get_balance_sample_balance())
        self._connection = new_connection_with_operations(
            get_balance=self.get_balance_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.chuanglan.chuanglanmock")

__all__ = [
    "Mock",
    "get_balance_fetched",
    "get_balance_provider_rejected",
    "get_balance_invalid_response",
    "get_balance_defect",
    "get_balance_retry",
    "get_balance_sample_balance",
]
