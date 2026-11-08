# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetPaymentLinkInput, PaymentLinkStatus
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_PAYMENT_LINK_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED

def get_payment_link_found(value: PaymentLinkStatus, failure: Failure | None = None) -> Case[PaymentLinkStatus]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_payment_link_found, '__module__', "dex_connectors.adyen.adyenmock")

def get_payment_link_not_found(value: PaymentLinkStatus, failure: Failure | None = None) -> Case[PaymentLinkStatus]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_payment_link_not_found, '__module__', "dex_connectors.adyen.adyenmock")

def get_payment_link_provider_rejected(value: PaymentLinkStatus, failure: Failure | None = None) -> Case[PaymentLinkStatus]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_payment_link_provider_rejected, '__module__', "dex_connectors.adyen.adyenmock")

def get_payment_link_invalid_response(value: PaymentLinkStatus, failure: Failure | None = None) -> Case[PaymentLinkStatus]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_payment_link_invalid_response, '__module__', "dex_connectors.adyen.adyenmock")

def get_payment_link_defect(value: PaymentLinkStatus, failure: Failure | None = None) -> Case[PaymentLinkStatus]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_payment_link_defect, '__module__', "dex_connectors.adyen.adyenmock")

def get_payment_link_retry(failure: Failure, after_seconds: float = 0) -> Case[PaymentLinkStatus]:
    return Case.retry(failure, after_seconds)

setattr(get_payment_link_retry, '__module__', "dex_connectors.adyen.adyenmock")

def get_payment_link_found_default() -> Case[PaymentLinkStatus]:
    return Case.branch_result(FOUND, PaymentLinkStatus(id='PL50C5F751CED39G71', status='completed', currency_code='EUR'))

setattr(get_payment_link_found_default, '__module__', "dex_connectors.adyen.adyenmock")


class Mock:

    def __init__(self, connection_name: str = "adyen") -> None:
        self.get_payment_link_script: Query[GetPaymentLinkInput, PaymentLinkStatus] = Query(GET_PAYMENT_LINK_DEFINITION)
        self.get_payment_link_script.default(get_payment_link_found_default())
        self._connection = new_connection_with_operations(
            get_payment_link=self.get_payment_link_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.adyen.adyenmock")

__all__ = [
    "Mock",
    "get_payment_link_found",
    "get_payment_link_not_found",
    "get_payment_link_provider_rejected",
    "get_payment_link_invalid_response",
    "get_payment_link_defect",
    "get_payment_link_retry",
    "get_payment_link_found_default",
]
