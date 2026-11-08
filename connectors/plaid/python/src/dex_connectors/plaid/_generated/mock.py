# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import AccountSummary, AccountsSnapshot, GetAccountsInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_ACCOUNTS_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED

def get_accounts_found(value: AccountsSnapshot, failure: Failure | None = None) -> Case[AccountsSnapshot]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_accounts_found, '__module__', "dex_connectors.plaid.plaidmock")

def get_accounts_not_found(value: AccountsSnapshot, failure: Failure | None = None) -> Case[AccountsSnapshot]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_accounts_not_found, '__module__', "dex_connectors.plaid.plaidmock")

def get_accounts_provider_rejected(value: AccountsSnapshot, failure: Failure | None = None) -> Case[AccountsSnapshot]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_accounts_provider_rejected, '__module__', "dex_connectors.plaid.plaidmock")

def get_accounts_invalid_response(value: AccountsSnapshot, failure: Failure | None = None) -> Case[AccountsSnapshot]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_accounts_invalid_response, '__module__', "dex_connectors.plaid.plaidmock")

def get_accounts_defect(value: AccountsSnapshot, failure: Failure | None = None) -> Case[AccountsSnapshot]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_accounts_defect, '__module__', "dex_connectors.plaid.plaidmock")

def get_accounts_retry(failure: Failure, after_seconds: float = 0) -> Case[AccountsSnapshot]:
    return Case.retry(failure, after_seconds)

setattr(get_accounts_retry, '__module__', "dex_connectors.plaid.plaidmock")

def get_accounts_found_default() -> Case[AccountsSnapshot]:
    return Case.branch_result(FOUND, AccountsSnapshot(item_id='item_123456', accounts=(AccountSummary(id='account_123456', type='depository', subtype='checking'),)))

setattr(get_accounts_found_default, '__module__', "dex_connectors.plaid.plaidmock")


class Mock:

    def __init__(self, connection_name: str = "plaid") -> None:
        self.get_accounts_script: Query[GetAccountsInput, AccountsSnapshot] = Query(GET_ACCOUNTS_DEFINITION)
        self.get_accounts_script.default(get_accounts_found_default())
        self._connection = new_connection_with_operations(
            get_accounts=self.get_accounts_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.plaid.plaidmock")

__all__ = [
    "Mock",
    "get_accounts_found",
    "get_accounts_not_found",
    "get_accounts_provider_rejected",
    "get_accounts_invalid_response",
    "get_accounts_defect",
    "get_accounts_retry",
    "get_accounts_found_default",
]
