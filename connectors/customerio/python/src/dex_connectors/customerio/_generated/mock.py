# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import CustomerLookupPage, GetCustomersByEmailInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_CUSTOMERS_BY_EMAIL_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED

def get_customers_by_email_found(value: CustomerLookupPage, failure: Failure | None = None) -> Case[CustomerLookupPage]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_customers_by_email_found, '__module__', "dex_connectors.customerio.customeriomock")

def get_customers_by_email_provider_rejected(value: CustomerLookupPage, failure: Failure | None = None) -> Case[CustomerLookupPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_customers_by_email_provider_rejected, '__module__', "dex_connectors.customerio.customeriomock")

def get_customers_by_email_invalid_response(value: CustomerLookupPage, failure: Failure | None = None) -> Case[CustomerLookupPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_customers_by_email_invalid_response, '__module__', "dex_connectors.customerio.customeriomock")

def get_customers_by_email_defect(value: CustomerLookupPage, failure: Failure | None = None) -> Case[CustomerLookupPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_customers_by_email_defect, '__module__', "dex_connectors.customerio.customeriomock")

def get_customers_by_email_retry(failure: Failure, after_seconds: float = 0) -> Case[CustomerLookupPage]:
    return Case.retry(failure, after_seconds)

setattr(get_customers_by_email_retry, '__module__', "dex_connectors.customerio.customeriomock")

def get_customers_by_email_found_default() -> Case[CustomerLookupPage]:
    return Case.branch_result(FOUND, CustomerLookupPage(email='person@example.com', customer_ids=('a3000001',)))

setattr(get_customers_by_email_found_default, '__module__', "dex_connectors.customerio.customeriomock")


class Mock:

    def __init__(self, connection_name: str = "customerio") -> None:
        self.get_customers_by_email_script: Query[GetCustomersByEmailInput, CustomerLookupPage] = Query(GET_CUSTOMERS_BY_EMAIL_DEFINITION)
        self.get_customers_by_email_script.default(get_customers_by_email_found_default())
        self._connection = new_connection_with_operations(
            get_customers_by_email=self.get_customers_by_email_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.customerio.customeriomock")

__all__ = [
    "Mock",
    "get_customers_by_email_found",
    "get_customers_by_email_provider_rejected",
    "get_customers_by_email_invalid_response",
    "get_customers_by_email_defect",
    "get_customers_by_email_retry",
    "get_customers_by_email_found_default",
]
