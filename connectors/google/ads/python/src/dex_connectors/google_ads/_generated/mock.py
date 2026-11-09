# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import AccessibleCustomers, ListAccessibleCustomersInput
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_ACCESSIBLE_CUSTOMERS_DEFINITION, PROVIDER_REJECTED

def list_accessible_customers_listed(value: AccessibleCustomers, failure: Failure | None = None) -> Case[AccessibleCustomers]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_accessible_customers_listed, '__module__', "dex_connectors.google_ads.adsmock")

def list_accessible_customers_provider_rejected(value: AccessibleCustomers, failure: Failure | None = None) -> Case[AccessibleCustomers]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_accessible_customers_provider_rejected, '__module__', "dex_connectors.google_ads.adsmock")

def list_accessible_customers_invalid_response(value: AccessibleCustomers, failure: Failure | None = None) -> Case[AccessibleCustomers]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_accessible_customers_invalid_response, '__module__', "dex_connectors.google_ads.adsmock")

def list_accessible_customers_defect(value: AccessibleCustomers, failure: Failure | None = None) -> Case[AccessibleCustomers]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_accessible_customers_defect, '__module__', "dex_connectors.google_ads.adsmock")

def list_accessible_customers_retry(failure: Failure, after_seconds: float = 0) -> Case[AccessibleCustomers]:
    return Case.retry(failure, after_seconds)

setattr(list_accessible_customers_retry, '__module__', "dex_connectors.google_ads.adsmock")

def list_accessible_customers_listed_default() -> Case[AccessibleCustomers]:
    return Case.branch_result(LISTED, AccessibleCustomers(resource_names=('customers/1234567890',)))

setattr(list_accessible_customers_listed_default, '__module__', "dex_connectors.google_ads.adsmock")


class Mock:

    def __init__(self, connection_name: str = "google-ads") -> None:
        self.list_accessible_customers_script: Query[ListAccessibleCustomersInput, AccessibleCustomers] = Query(LIST_ACCESSIBLE_CUSTOMERS_DEFINITION)
        self.list_accessible_customers_script.default(list_accessible_customers_listed_default())
        self._connection = new_connection_with_operations(
            list_accessible_customers=self.list_accessible_customers_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.google_ads.adsmock")

__all__ = [
    "Mock",
    "list_accessible_customers_listed",
    "list_accessible_customers_provider_rejected",
    "list_accessible_customers_invalid_response",
    "list_accessible_customers_defect",
    "list_accessible_customers_retry",
    "list_accessible_customers_listed_default",
]
