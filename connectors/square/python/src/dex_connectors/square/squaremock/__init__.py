# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest-backed Square scripts for consuming Flow tests."""

from dex_connectors.sdk import ConnectionRef, Failure
from dex_connectors.sdk.connectormock import Case, Query

from ..client import CONNECTOR_ID, FOUND, GET_PAYMENT_DEFINITION, LIST_PAYMENTS_DEFINITION, LISTED, NOT_FOUND
from ..connection import Connection, new_connection_with_operations
from ..models import GetPaymentInput, ListPaymentsInput, Money, Payment, PaymentPage

_PAYMENT = Payment("pay_101", "COMPLETED", Money(2900, "USD"))


def list_payments_listed(value: PaymentPage, failure: Failure | None = None) -> Case[PaymentPage]:
    """Script the successful listed route."""
    return Case.branch_result(LISTED, value, failure)


def list_payments_default_answer() -> Case[PaymentPage]:
    """Return the manifest's listPayments defaultAnswer."""
    return list_payments_listed(PaymentPage((_PAYMENT,), ""))


def get_payment_found(value: Payment, failure: Failure | None = None) -> Case[Payment]:
    """Script the successful found route."""
    return Case.branch_result(FOUND, value, failure)


def get_payment_not_found(value: Payment, failure: Failure | None = None) -> Case[Payment]:
    """Script a missing payment."""
    return Case.branch_result(NOT_FOUND, value, failure)


def get_payment_default_answer() -> Case[Payment]:
    """Return the manifest's getPayment defaultAnswer."""
    return get_payment_found(_PAYMENT)


class Mock:
    """Script Square Queries, defaulting to their manifest answers."""

    def __init__(self, connection_name: str = "square") -> None:
        self.list_payments_script: Query[ListPaymentsInput, PaymentPage] = Query(LIST_PAYMENTS_DEFINITION)
        self.list_payments_script.default(list_payments_default_answer())
        self.get_payment_script: Query[GetPaymentInput, Payment] = Query(GET_PAYMENT_DEFINITION)
        self.get_payment_script.default(get_payment_default_answer())
        self._connection = new_connection_with_operations(
            self.list_payments_script, self.get_payment_script, ConnectionRef(CONNECTOR_ID, connection_name)
        )

    def connection(self) -> Connection:
        """Return this mock's named connection for Step registration."""
        return self._connection


__all__ = [
    "Mock",
    "list_payments_listed",
    "list_payments_default_answer",
    "get_payment_found",
    "get_payment_not_found",
    "get_payment_default_answer",
]
