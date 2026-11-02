# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetPaymentInput, ListPaymentsInput, Money, Payment, PaymentPage
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_PAYMENT_DEFINITION, INVALID_RESPONSE, LISTED, LIST_PAYMENTS_DEFINITION, NOT_FOUND, PROVIDER_REJECTED

def list_payments_listed(value: PaymentPage, failure: Failure | None = None) -> Case[PaymentPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_payments_listed, '__module__', "dex_connectors.square.squaremock")

def list_payments_not_found(value: PaymentPage, failure: Failure | None = None) -> Case[PaymentPage]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(list_payments_not_found, '__module__', "dex_connectors.square.squaremock")

def list_payments_provider_rejected(value: PaymentPage, failure: Failure | None = None) -> Case[PaymentPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_payments_provider_rejected, '__module__', "dex_connectors.square.squaremock")

def list_payments_invalid_response(value: PaymentPage, failure: Failure | None = None) -> Case[PaymentPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_payments_invalid_response, '__module__', "dex_connectors.square.squaremock")

def list_payments_defect(value: PaymentPage, failure: Failure | None = None) -> Case[PaymentPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_payments_defect, '__module__', "dex_connectors.square.squaremock")

def list_payments_retry(failure: Failure, after_seconds: float = 0) -> Case[PaymentPage]:
    return Case.retry(failure, after_seconds)

setattr(list_payments_retry, '__module__', "dex_connectors.square.squaremock")

def list_payments_default_answer() -> Case[PaymentPage]:
    return Case.branch_result(LISTED, PaymentPage(payments=(Payment(id='pay_101', status='COMPLETED', amount_money=Money(amount=2900, currency='USD')),), cursor=''))

setattr(list_payments_default_answer, '__module__', "dex_connectors.square.squaremock")

def get_payment_found(value: Payment, failure: Failure | None = None) -> Case[Payment]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_payment_found, '__module__', "dex_connectors.square.squaremock")

def get_payment_not_found(value: Payment, failure: Failure | None = None) -> Case[Payment]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_payment_not_found, '__module__', "dex_connectors.square.squaremock")

def get_payment_provider_rejected(value: Payment, failure: Failure | None = None) -> Case[Payment]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_payment_provider_rejected, '__module__', "dex_connectors.square.squaremock")

def get_payment_invalid_response(value: Payment, failure: Failure | None = None) -> Case[Payment]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_payment_invalid_response, '__module__', "dex_connectors.square.squaremock")

def get_payment_defect(value: Payment, failure: Failure | None = None) -> Case[Payment]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_payment_defect, '__module__', "dex_connectors.square.squaremock")

def get_payment_retry(failure: Failure, after_seconds: float = 0) -> Case[Payment]:
    return Case.retry(failure, after_seconds)

setattr(get_payment_retry, '__module__', "dex_connectors.square.squaremock")

def get_payment_default_answer() -> Case[Payment]:
    return Case.branch_result(FOUND, Payment(id='pay_101', status='COMPLETED', amount_money=Money(amount=2900, currency='USD')))

setattr(get_payment_default_answer, '__module__', "dex_connectors.square.squaremock")


class Mock:

    def __init__(self, connection_name: str = "square") -> None:
        self.list_payments_script: Query[ListPaymentsInput, PaymentPage] = Query(LIST_PAYMENTS_DEFINITION)
        self.list_payments_script.default(list_payments_default_answer())
        self.get_payment_script: Query[GetPaymentInput, Payment] = Query(GET_PAYMENT_DEFINITION)
        self.get_payment_script.default(get_payment_default_answer())
        self._connection = new_connection_with_operations(
            list_payments=self.list_payments_script,
            get_payment=self.get_payment_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.square.squaremock")

__all__ = [
    "Mock",
    "list_payments_listed",
    "list_payments_not_found",
    "list_payments_provider_rejected",
    "list_payments_invalid_response",
    "list_payments_defect",
    "list_payments_retry",
    "list_payments_default_answer",
    "get_payment_found",
    "get_payment_not_found",
    "get_payment_provider_rejected",
    "get_payment_invalid_response",
    "get_payment_defect",
    "get_payment_retry",
    "get_payment_default_answer",
]
