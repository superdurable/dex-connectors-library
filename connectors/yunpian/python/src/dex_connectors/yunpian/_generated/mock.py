# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import AccountBalance, GetAccountBalanceInput
from .definitions import CONNECTOR_ID, DEFECT, GET_ACCOUNT_BALANCE_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED, READ

def get_account_balance_read(value: AccountBalance, failure: Failure | None = None) -> Case[AccountBalance]:
    return Case.branch_result(READ, value, failure)

setattr(get_account_balance_read, '__module__', "dex_connectors.yunpian.yunpianmock")

def get_account_balance_provider_rejected(value: AccountBalance, failure: Failure | None = None) -> Case[AccountBalance]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_account_balance_provider_rejected, '__module__', "dex_connectors.yunpian.yunpianmock")

def get_account_balance_invalid_response(value: AccountBalance, failure: Failure | None = None) -> Case[AccountBalance]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_account_balance_invalid_response, '__module__', "dex_connectors.yunpian.yunpianmock")

def get_account_balance_defect(value: AccountBalance, failure: Failure | None = None) -> Case[AccountBalance]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_account_balance_defect, '__module__', "dex_connectors.yunpian.yunpianmock")

def get_account_balance_retry(failure: Failure, after_seconds: float = 0) -> Case[AccountBalance]:
    return Case.retry(failure, after_seconds)

setattr(get_account_balance_retry, '__module__', "dex_connectors.yunpian.yunpianmock")

def get_account_balance_read_default() -> Case[AccountBalance]:
    return Case.branch_result(READ, AccountBalance(balance=1000))

setattr(get_account_balance_read_default, '__module__', "dex_connectors.yunpian.yunpianmock")


class Mock:

    def __init__(self, connection_name: str = "yunpian") -> None:
        self.get_account_balance_script: Query[GetAccountBalanceInput, AccountBalance] = Query(GET_ACCOUNT_BALANCE_DEFINITION)
        self.get_account_balance_script.default(get_account_balance_read_default())
        self._connection = new_connection_with_operations(
            get_account_balance=self.get_account_balance_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.yunpian.yunpianmock")

__all__ = [
    "Mock",
    "get_account_balance_read",
    "get_account_balance_provider_rejected",
    "get_account_balance_invalid_response",
    "get_account_balance_defect",
    "get_account_balance_retry",
    "get_account_balance_read_default",
]
