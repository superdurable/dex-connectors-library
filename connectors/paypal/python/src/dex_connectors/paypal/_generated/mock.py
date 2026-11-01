# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated manifest-backed provider outcomes and scripts."""

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetOrderInput, ListTransactionsInput, Order, TransactionAmount, TransactionDetail, TransactionInfo, TransactionPage
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_ORDER_DEFINITION, INVALID_RESPONSE, LISTED, LIST_TRANSACTIONS_DEFINITION, NOT_FOUND, PROVIDER_REJECTED

def get_order_found(value: Order, failure: Failure | None = None) -> Case[Order]:
    """Script the found branch."""
    return Case.branch_result(FOUND, value, failure)

setattr(get_order_found, '__module__', "dex_connectors.paypal.paypalmock")

def get_order_not_found(value: Order, failure: Failure | None = None) -> Case[Order]:
    """Script the notFound branch."""
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_order_not_found, '__module__', "dex_connectors.paypal.paypalmock")

def get_order_provider_rejected(value: Order, failure: Failure | None = None) -> Case[Order]:
    """Script the providerRejected branch."""
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_order_provider_rejected, '__module__', "dex_connectors.paypal.paypalmock")

def get_order_invalid_response(value: Order, failure: Failure | None = None) -> Case[Order]:
    """Script the invalidResponse branch."""
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_order_invalid_response, '__module__', "dex_connectors.paypal.paypalmock")

def get_order_defect(value: Order, failure: Failure | None = None) -> Case[Order]:
    """Script the defect branch."""
    return Case.branch_result(DEFECT, value, failure)

setattr(get_order_defect, '__module__', "dex_connectors.paypal.paypalmock")

def get_order_retry(failure: Failure, after_seconds: float = 0) -> Case[Order]:
    """Script a retry of this provider operation."""
    return Case.retry(failure, after_seconds)

setattr(get_order_retry, '__module__', "dex_connectors.paypal.paypalmock")

def get_order_default_answer() -> Case[Order]:
    """Return the manifest defaultAnswer outcome."""
    return Case.branch_result(FOUND, Order(id='ORDER101', status='COMPLETED'))

setattr(get_order_default_answer, '__module__', "dex_connectors.paypal.paypalmock")

def list_transactions_listed(value: TransactionPage, failure: Failure | None = None) -> Case[TransactionPage]:
    """Script the listed branch."""
    return Case.branch_result(LISTED, value, failure)

setattr(list_transactions_listed, '__module__', "dex_connectors.paypal.paypalmock")

def list_transactions_not_found(value: TransactionPage, failure: Failure | None = None) -> Case[TransactionPage]:
    """Script the notFound branch."""
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(list_transactions_not_found, '__module__', "dex_connectors.paypal.paypalmock")

def list_transactions_provider_rejected(value: TransactionPage, failure: Failure | None = None) -> Case[TransactionPage]:
    """Script the providerRejected branch."""
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_transactions_provider_rejected, '__module__', "dex_connectors.paypal.paypalmock")

def list_transactions_invalid_response(value: TransactionPage, failure: Failure | None = None) -> Case[TransactionPage]:
    """Script the invalidResponse branch."""
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_transactions_invalid_response, '__module__', "dex_connectors.paypal.paypalmock")

def list_transactions_defect(value: TransactionPage, failure: Failure | None = None) -> Case[TransactionPage]:
    """Script the defect branch."""
    return Case.branch_result(DEFECT, value, failure)

setattr(list_transactions_defect, '__module__', "dex_connectors.paypal.paypalmock")

def list_transactions_retry(failure: Failure, after_seconds: float = 0) -> Case[TransactionPage]:
    """Script a retry of this provider operation."""
    return Case.retry(failure, after_seconds)

setattr(list_transactions_retry, '__module__', "dex_connectors.paypal.paypalmock")

def list_transactions_default_answer() -> Case[TransactionPage]:
    """Return the manifest defaultAnswer outcome."""
    return Case.branch_result(LISTED, TransactionPage(transaction_details=(TransactionDetail(transaction_info=TransactionInfo(transaction_id='TXN101', transaction_status='S', transaction_amount=TransactionAmount(currency_code='USD', value='29.00'))),), page=1, total_pages=1))

setattr(list_transactions_default_answer, '__module__', "dex_connectors.paypal.paypalmock")


class Mock:
    """Script manifest outcomes for this connector."""

    def __init__(self, connection_name: str = "paypal") -> None:
        self.get_order_script: Query[GetOrderInput, Order] = Query(GET_ORDER_DEFINITION)
        self.get_order_script.default(get_order_default_answer())
        self.list_transactions_script: Query[ListTransactionsInput, TransactionPage] = Query(LIST_TRANSACTIONS_DEFINITION)
        self.list_transactions_script.default(list_transactions_default_answer())
        self._connection = new_connection_with_operations(
            get_order=self.get_order_script,
            list_transactions=self.list_transactions_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        """Return this mock connection for Dex Step registration."""
        return self._connection

setattr(Mock, '__module__', "dex_connectors.paypal.paypalmock")

__all__ = [
    "Mock",
    "get_order_found",
    "get_order_not_found",
    "get_order_provider_rejected",
    "get_order_invalid_response",
    "get_order_defect",
    "get_order_retry",
    "get_order_default_answer",
    "list_transactions_listed",
    "list_transactions_not_found",
    "list_transactions_provider_rejected",
    "list_transactions_invalid_response",
    "list_transactions_defect",
    "list_transactions_retry",
    "list_transactions_default_answer",
]
