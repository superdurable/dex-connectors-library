# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetAccountInfoInput, GetAccountInfoOutput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_ACCOUNT_INFO_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED

def get_account_info_found(value: GetAccountInfoOutput, failure: Failure | None = None) -> Case[GetAccountInfoOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_account_info_found, '__module__', "dex_connectors.apizero.apizeromock")

def get_account_info_provider_rejected(value: GetAccountInfoOutput, failure: Failure | None = None) -> Case[GetAccountInfoOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_account_info_provider_rejected, '__module__', "dex_connectors.apizero.apizeromock")

def get_account_info_invalid_response(value: GetAccountInfoOutput, failure: Failure | None = None) -> Case[GetAccountInfoOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_account_info_invalid_response, '__module__', "dex_connectors.apizero.apizeromock")

def get_account_info_defect(value: GetAccountInfoOutput, failure: Failure | None = None) -> Case[GetAccountInfoOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_account_info_defect, '__module__', "dex_connectors.apizero.apizeromock")

def get_account_info_retry(failure: Failure, after_seconds: float = 0) -> Case[GetAccountInfoOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_account_info_retry, '__module__', "dex_connectors.apizero.apizeromock")

def get_account_info_standard_account() -> Case[GetAccountInfoOutput]:
    return Case.branch_result(FOUND, GetAccountInfoOutput(scope='account', balance_yuan='123.4000'))

setattr(get_account_info_standard_account, '__module__', "dex_connectors.apizero.apizeromock")

def get_account_info_prepaid_key() -> Case[GetAccountInfoOutput]:
    return Case.branch_result(FOUND, GetAccountInfoOutput(scope='key', key_quota_remaining=42))

setattr(get_account_info_prepaid_key, '__module__', "dex_connectors.apizero.apizeromock")


class Mock:

    def __init__(self, connection_name: str = "apizero") -> None:
        self.get_account_info_script: Query[GetAccountInfoInput, GetAccountInfoOutput] = Query(GET_ACCOUNT_INFO_DEFINITION)
        self.get_account_info_script.default(get_account_info_standard_account())
        self._connection = new_connection_with_operations(
            get_account_info=self.get_account_info_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.apizero.apizeromock")

__all__ = [
    "Mock",
    "get_account_info_found",
    "get_account_info_provider_rejected",
    "get_account_info_invalid_response",
    "get_account_info_defect",
    "get_account_info_retry",
    "get_account_info_standard_account",
    "get_account_info_prepaid_key",
]
