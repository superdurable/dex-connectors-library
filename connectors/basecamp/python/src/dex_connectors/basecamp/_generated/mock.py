# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import Account, AccountsPage, ListAccountsInput
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_ACCOUNTS_DEFINITION, PROVIDER_REJECTED

def list_accounts_listed(value: AccountsPage, failure: Failure | None = None) -> Case[AccountsPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_accounts_listed, '__module__', "dex_connectors.basecamp.basecampmock")

def list_accounts_provider_rejected(value: AccountsPage, failure: Failure | None = None) -> Case[AccountsPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_accounts_provider_rejected, '__module__', "dex_connectors.basecamp.basecampmock")

def list_accounts_invalid_response(value: AccountsPage, failure: Failure | None = None) -> Case[AccountsPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_accounts_invalid_response, '__module__', "dex_connectors.basecamp.basecampmock")

def list_accounts_defect(value: AccountsPage, failure: Failure | None = None) -> Case[AccountsPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_accounts_defect, '__module__', "dex_connectors.basecamp.basecampmock")

def list_accounts_retry(failure: Failure, after_seconds: float = 0) -> Case[AccountsPage]:
    return Case.retry(failure, after_seconds)

setattr(list_accounts_retry, '__module__', "dex_connectors.basecamp.basecampmock")

def list_accounts_listed_default() -> Case[AccountsPage]:
    return Case.branch_result(LISTED, AccountsPage(accounts=(Account(id='99999999', company='Example Company'),), offset=0, limit=100, total=1))

setattr(list_accounts_listed_default, '__module__', "dex_connectors.basecamp.basecampmock")


class Mock:

    def __init__(self, connection_name: str = "basecamp") -> None:
        self.list_accounts_script: Query[ListAccountsInput, AccountsPage] = Query(LIST_ACCOUNTS_DEFINITION)
        self.list_accounts_script.default(list_accounts_listed_default())
        self._connection = new_connection_with_operations(
            list_accounts=self.list_accounts_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.basecamp.basecampmock")

__all__ = [
    "Mock",
    "list_accounts_listed",
    "list_accounts_provider_rejected",
    "list_accounts_invalid_response",
    "list_accounts_defect",
    "list_accounts_retry",
    "list_accounts_listed_default",
]
