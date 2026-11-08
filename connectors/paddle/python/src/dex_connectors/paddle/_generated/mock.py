# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetTransactionInput, TransactionStatus
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_TRANSACTION_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED

def get_transaction_found(value: TransactionStatus, failure: Failure | None = None) -> Case[TransactionStatus]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_transaction_found, '__module__', "dex_connectors.paddle.paddlemock")

def get_transaction_not_found(value: TransactionStatus, failure: Failure | None = None) -> Case[TransactionStatus]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_transaction_not_found, '__module__', "dex_connectors.paddle.paddlemock")

def get_transaction_provider_rejected(value: TransactionStatus, failure: Failure | None = None) -> Case[TransactionStatus]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_transaction_provider_rejected, '__module__', "dex_connectors.paddle.paddlemock")

def get_transaction_invalid_response(value: TransactionStatus, failure: Failure | None = None) -> Case[TransactionStatus]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_transaction_invalid_response, '__module__', "dex_connectors.paddle.paddlemock")

def get_transaction_defect(value: TransactionStatus, failure: Failure | None = None) -> Case[TransactionStatus]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_transaction_defect, '__module__', "dex_connectors.paddle.paddlemock")

def get_transaction_retry(failure: Failure, after_seconds: float = 0) -> Case[TransactionStatus]:
    return Case.retry(failure, after_seconds)

setattr(get_transaction_retry, '__module__', "dex_connectors.paddle.paddlemock")

def get_transaction_found_default() -> Case[TransactionStatus]:
    return Case.branch_result(FOUND, TransactionStatus(id='txn_01hv8wptq8987qeep44cyrewp9', status='completed', currency_code='USD'))

setattr(get_transaction_found_default, '__module__', "dex_connectors.paddle.paddlemock")


class Mock:

    def __init__(self, connection_name: str = "paddle") -> None:
        self.get_transaction_script: Query[GetTransactionInput, TransactionStatus] = Query(GET_TRANSACTION_DEFINITION)
        self.get_transaction_script.default(get_transaction_found_default())
        self._connection = new_connection_with_operations(
            get_transaction=self.get_transaction_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.paddle.paddlemock")

__all__ = [
    "Mock",
    "get_transaction_found",
    "get_transaction_not_found",
    "get_transaction_provider_rejected",
    "get_transaction_invalid_response",
    "get_transaction_defect",
    "get_transaction_retry",
    "get_transaction_found_default",
]
