# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetPaymentInput, PaymentStatus
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_PAYMENT_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED

def get_payment_found(value: PaymentStatus, failure: Failure | None = None) -> Case[PaymentStatus]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_payment_found, '__module__', "dex_connectors.razorpay.razorpaymock")

def get_payment_not_found(value: PaymentStatus, failure: Failure | None = None) -> Case[PaymentStatus]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_payment_not_found, '__module__', "dex_connectors.razorpay.razorpaymock")

def get_payment_provider_rejected(value: PaymentStatus, failure: Failure | None = None) -> Case[PaymentStatus]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_payment_provider_rejected, '__module__', "dex_connectors.razorpay.razorpaymock")

def get_payment_invalid_response(value: PaymentStatus, failure: Failure | None = None) -> Case[PaymentStatus]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_payment_invalid_response, '__module__', "dex_connectors.razorpay.razorpaymock")

def get_payment_defect(value: PaymentStatus, failure: Failure | None = None) -> Case[PaymentStatus]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_payment_defect, '__module__', "dex_connectors.razorpay.razorpaymock")

def get_payment_retry(failure: Failure, after_seconds: float = 0) -> Case[PaymentStatus]:
    return Case.retry(failure, after_seconds)

setattr(get_payment_retry, '__module__', "dex_connectors.razorpay.razorpaymock")

def get_payment_found_default() -> Case[PaymentStatus]:
    return Case.branch_result(FOUND, PaymentStatus(id='pay_DG4ZdRK8ZnXC3k', status='captured', amount=100, currency_code='INR'))

setattr(get_payment_found_default, '__module__', "dex_connectors.razorpay.razorpaymock")


class Mock:

    def __init__(self, connection_name: str = "razorpay") -> None:
        self.get_payment_script: Query[GetPaymentInput, PaymentStatus] = Query(GET_PAYMENT_DEFINITION)
        self.get_payment_script.default(get_payment_found_default())
        self._connection = new_connection_with_operations(
            get_payment=self.get_payment_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.razorpay.razorpaymock")

__all__ = [
    "Mock",
    "get_payment_found",
    "get_payment_not_found",
    "get_payment_provider_rejected",
    "get_payment_invalid_response",
    "get_payment_defect",
    "get_payment_retry",
    "get_payment_found_default",
]
