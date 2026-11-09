# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetInvoiceInput, Invoice
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_INVOICE_DEFINITION, INVALID_RESPONSE, MISSING, PROVIDER_REJECTED

def get_invoice_found(value: Invoice, failure: Failure | None = None) -> Case[Invoice]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_invoice_found, '__module__', "dex_connectors.freshbooks.freshbooksmock")

def get_invoice_missing(value: Invoice, failure: Failure | None = None) -> Case[Invoice]:
    return Case.branch_result(MISSING, value, failure)

setattr(get_invoice_missing, '__module__', "dex_connectors.freshbooks.freshbooksmock")

def get_invoice_provider_rejected(value: Invoice, failure: Failure | None = None) -> Case[Invoice]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_invoice_provider_rejected, '__module__', "dex_connectors.freshbooks.freshbooksmock")

def get_invoice_invalid_response(value: Invoice, failure: Failure | None = None) -> Case[Invoice]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_invoice_invalid_response, '__module__', "dex_connectors.freshbooks.freshbooksmock")

def get_invoice_defect(value: Invoice, failure: Failure | None = None) -> Case[Invoice]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_invoice_defect, '__module__', "dex_connectors.freshbooks.freshbooksmock")

def get_invoice_retry(failure: Failure, after_seconds: float = 0) -> Case[Invoice]:
    return Case.retry(failure, after_seconds)

setattr(get_invoice_retry, '__module__', "dex_connectors.freshbooks.freshbooksmock")

def get_invoice_found_default() -> Case[Invoice]:
    return Case.branch_result(FOUND, Invoice(account_id='KAG77', invoice_id=2201278, invoice_number='0000003', amount='800.00', currency_code='USD', payment_status='unpaid'))

setattr(get_invoice_found_default, '__module__', "dex_connectors.freshbooks.freshbooksmock")


class Mock:

    def __init__(self, connection_name: str = "freshbooks") -> None:
        self.get_invoice_script: Query[GetInvoiceInput, Invoice] = Query(GET_INVOICE_DEFINITION)
        self.get_invoice_script.default(get_invoice_found_default())
        self._connection = new_connection_with_operations(
            get_invoice=self.get_invoice_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.freshbooks.freshbooksmock")

__all__ = [
    "Mock",
    "get_invoice_found",
    "get_invoice_missing",
    "get_invoice_provider_rejected",
    "get_invoice_invalid_response",
    "get_invoice_defect",
    "get_invoice_retry",
    "get_invoice_found_default",
]
