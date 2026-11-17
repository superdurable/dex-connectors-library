# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetAccountBalanceInput, GetAccountBalanceOutput
from .definitions import CONNECTOR_ID, DEFECT, FETCHED, GET_ACCOUNT_BALANCE_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED

def get_account_balance_fetched(value: GetAccountBalanceOutput, failure: Failure | None = None) -> Case[GetAccountBalanceOutput]:
    return Case.branch_result(FETCHED, value, failure)

setattr(get_account_balance_fetched, '__module__', "dex_connectors.dieyuyun.dieyuyunmock")

def get_account_balance_provider_rejected(value: GetAccountBalanceOutput, failure: Failure | None = None) -> Case[GetAccountBalanceOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_account_balance_provider_rejected, '__module__', "dex_connectors.dieyuyun.dieyuyunmock")

def get_account_balance_invalid_response(value: GetAccountBalanceOutput, failure: Failure | None = None) -> Case[GetAccountBalanceOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_account_balance_invalid_response, '__module__', "dex_connectors.dieyuyun.dieyuyunmock")

def get_account_balance_defect(value: GetAccountBalanceOutput, failure: Failure | None = None) -> Case[GetAccountBalanceOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_account_balance_defect, '__module__', "dex_connectors.dieyuyun.dieyuyunmock")

def get_account_balance_retry(failure: Failure, after_seconds: float = 0) -> Case[GetAccountBalanceOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_account_balance_retry, '__module__', "dex_connectors.dieyuyun.dieyuyunmock")

def get_account_balance_sample_balance() -> Case[GetAccountBalanceOutput]:
    return Case.branch_result(FETCHED, GetAccountBalanceOutput(available_cny='138.5'))

setattr(get_account_balance_sample_balance, '__module__', "dex_connectors.dieyuyun.dieyuyunmock")


class Mock:

    def __init__(self, connection_name: str = "dieyuyun") -> None:
        self.get_account_balance_script: Query[GetAccountBalanceInput, GetAccountBalanceOutput] = Query(GET_ACCOUNT_BALANCE_DEFINITION)
        self.get_account_balance_script.default(get_account_balance_sample_balance())
        self._connection = new_connection_with_operations(
            get_account_balance=self.get_account_balance_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.dieyuyun.dieyuyunmock")

__all__ = [
    "Mock",
    "get_account_balance_fetched",
    "get_account_balance_provider_rejected",
    "get_account_balance_invalid_response",
    "get_account_balance_defect",
    "get_account_balance_retry",
    "get_account_balance_sample_balance",
]
