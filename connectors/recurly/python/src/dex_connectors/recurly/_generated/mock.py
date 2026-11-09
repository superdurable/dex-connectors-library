# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import AccountIdentity, GetAccountInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_ACCOUNT_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED

def get_account_found(value: AccountIdentity, failure: Failure | None = None) -> Case[AccountIdentity]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_account_found, '__module__', "dex_connectors.recurly.recurlymock")

def get_account_not_found(value: AccountIdentity, failure: Failure | None = None) -> Case[AccountIdentity]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_account_not_found, '__module__', "dex_connectors.recurly.recurlymock")

def get_account_provider_rejected(value: AccountIdentity, failure: Failure | None = None) -> Case[AccountIdentity]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_account_provider_rejected, '__module__', "dex_connectors.recurly.recurlymock")

def get_account_invalid_response(value: AccountIdentity, failure: Failure | None = None) -> Case[AccountIdentity]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_account_invalid_response, '__module__', "dex_connectors.recurly.recurlymock")

def get_account_defect(value: AccountIdentity, failure: Failure | None = None) -> Case[AccountIdentity]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_account_defect, '__module__', "dex_connectors.recurly.recurlymock")

def get_account_retry(failure: Failure, after_seconds: float = 0) -> Case[AccountIdentity]:
    return Case.retry(failure, after_seconds)

setattr(get_account_retry, '__module__', "dex_connectors.recurly.recurlymock")

def get_account_found_default() -> Case[AccountIdentity]:
    return Case.branch_result(FOUND, AccountIdentity(id='e28zov4fw0v2', code='acme-corp'))

setattr(get_account_found_default, '__module__', "dex_connectors.recurly.recurlymock")


class Mock:

    def __init__(self, connection_name: str = "recurly") -> None:
        self.get_account_script: Query[GetAccountInput, AccountIdentity] = Query(GET_ACCOUNT_DEFINITION)
        self.get_account_script.default(get_account_found_default())
        self._connection = new_connection_with_operations(
            get_account=self.get_account_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.recurly.recurlymock")

__all__ = [
    "Mock",
    "get_account_found",
    "get_account_not_found",
    "get_account_provider_rejected",
    "get_account_invalid_response",
    "get_account_defect",
    "get_account_retry",
    "get_account_found_default",
]
