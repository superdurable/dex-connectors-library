# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetAccountBalanceInput, GetAccountBalanceOutput
from .definitions import CONNECTOR_ID, DEFECT, GET_ACCOUNT_BALANCE_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED, RETRIEVED

def get_account_balance_retrieved(value: GetAccountBalanceOutput, failure: Failure | None = None) -> Case[GetAccountBalanceOutput]:
    return Case.branch_result(RETRIEVED, value, failure)

setattr(get_account_balance_retrieved, '__module__', "dex_connectors.sendcloud.sendcloudmock")

def get_account_balance_provider_rejected(value: GetAccountBalanceOutput, failure: Failure | None = None) -> Case[GetAccountBalanceOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_account_balance_provider_rejected, '__module__', "dex_connectors.sendcloud.sendcloudmock")

def get_account_balance_invalid_response(value: GetAccountBalanceOutput, failure: Failure | None = None) -> Case[GetAccountBalanceOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_account_balance_invalid_response, '__module__', "dex_connectors.sendcloud.sendcloudmock")

def get_account_balance_defect(value: GetAccountBalanceOutput, failure: Failure | None = None) -> Case[GetAccountBalanceOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_account_balance_defect, '__module__', "dex_connectors.sendcloud.sendcloudmock")

def get_account_balance_retry(failure: Failure, after_seconds: float = 0) -> Case[GetAccountBalanceOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_account_balance_retry, '__module__', "dex_connectors.sendcloud.sendcloudmock")

def get_account_balance_sample_account() -> Case[GetAccountBalanceOutput]:
    return Case.branch_result(RETRIEVED, GetAccountBalanceOutput(available_balance='40.54', balance='44.02', daily_quota=2500, today_used_quota=2))

setattr(get_account_balance_sample_account, '__module__', "dex_connectors.sendcloud.sendcloudmock")


class Mock:

    def __init__(self, connection_name: str = "sendcloud") -> None:
        self.get_account_balance_script: Query[GetAccountBalanceInput, GetAccountBalanceOutput] = Query(GET_ACCOUNT_BALANCE_DEFINITION)
        self.get_account_balance_script.default(get_account_balance_sample_account())
        self._connection = new_connection_with_operations(
            get_account_balance=self.get_account_balance_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.sendcloud.sendcloudmock")

__all__ = [
    "Mock",
    "get_account_balance_retrieved",
    "get_account_balance_provider_rejected",
    "get_account_balance_invalid_response",
    "get_account_balance_defect",
    "get_account_balance_retry",
    "get_account_balance_sample_account",
]
