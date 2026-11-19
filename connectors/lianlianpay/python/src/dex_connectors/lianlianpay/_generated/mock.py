# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import PaymentStatusResult, QueryPaymentStatusInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, INVALID_RESPONSE, PROVIDER_REJECTED, QUERY_PAYMENT_STATUS_DEFINITION

def query_payment_status_found(value: PaymentStatusResult, failure: Failure | None = None) -> Case[PaymentStatusResult]:
    return Case.branch_result(FOUND, value, failure)

setattr(query_payment_status_found, '__module__', "dex_connectors.lianlianpay.lianlianpaymock")

def query_payment_status_provider_rejected(value: PaymentStatusResult, failure: Failure | None = None) -> Case[PaymentStatusResult]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(query_payment_status_provider_rejected, '__module__', "dex_connectors.lianlianpay.lianlianpaymock")

def query_payment_status_invalid_response(value: PaymentStatusResult, failure: Failure | None = None) -> Case[PaymentStatusResult]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(query_payment_status_invalid_response, '__module__', "dex_connectors.lianlianpay.lianlianpaymock")

def query_payment_status_defect(value: PaymentStatusResult, failure: Failure | None = None) -> Case[PaymentStatusResult]:
    return Case.branch_result(DEFECT, value, failure)

setattr(query_payment_status_defect, '__module__', "dex_connectors.lianlianpay.lianlianpaymock")

def query_payment_status_retry(failure: Failure, after_seconds: float = 0) -> Case[PaymentStatusResult]:
    return Case.retry(failure, after_seconds)

setattr(query_payment_status_retry, '__module__', "dex_connectors.lianlianpay.lianlianpaymock")

def query_payment_status_found_order() -> Case[PaymentStatusResult]:
    return Case.branch_result(FOUND, PaymentStatusResult(merchant_order_id='ORDER123', status='SUCCESS'))

setattr(query_payment_status_found_order, '__module__', "dex_connectors.lianlianpay.lianlianpaymock")


class Mock:

    def __init__(self, connection_name: str = "lianlianpay") -> None:
        self.query_payment_status_script: Query[QueryPaymentStatusInput, PaymentStatusResult] = Query(QUERY_PAYMENT_STATUS_DEFINITION)
        self.query_payment_status_script.default(query_payment_status_found_order())
        self._connection = new_connection_with_operations(
            query_payment_status=self.query_payment_status_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.lianlianpay.lianlianpaymock")

__all__ = [
    "Mock",
    "query_payment_status_found",
    "query_payment_status_provider_rejected",
    "query_payment_status_invalid_response",
    "query_payment_status_defect",
    "query_payment_status_retry",
    "query_payment_status_found_order",
]
