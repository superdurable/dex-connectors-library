# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import CustomerSummary, GetCustomerInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_CUSTOMER_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED

def get_customer_found(value: CustomerSummary, failure: Failure | None = None) -> Case[CustomerSummary]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_customer_found, '__module__', "dex_connectors.oracle_netsuite.oraclenetsuitemock")

def get_customer_not_found(value: CustomerSummary, failure: Failure | None = None) -> Case[CustomerSummary]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_customer_not_found, '__module__', "dex_connectors.oracle_netsuite.oraclenetsuitemock")

def get_customer_provider_rejected(value: CustomerSummary, failure: Failure | None = None) -> Case[CustomerSummary]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_customer_provider_rejected, '__module__', "dex_connectors.oracle_netsuite.oraclenetsuitemock")

def get_customer_invalid_response(value: CustomerSummary, failure: Failure | None = None) -> Case[CustomerSummary]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_customer_invalid_response, '__module__', "dex_connectors.oracle_netsuite.oraclenetsuitemock")

def get_customer_defect(value: CustomerSummary, failure: Failure | None = None) -> Case[CustomerSummary]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_customer_defect, '__module__', "dex_connectors.oracle_netsuite.oraclenetsuitemock")

def get_customer_retry(failure: Failure, after_seconds: float = 0) -> Case[CustomerSummary]:
    return Case.retry(failure, after_seconds)

setattr(get_customer_retry, '__module__', "dex_connectors.oracle_netsuite.oraclenetsuitemock")

def get_customer_found_default() -> Case[CustomerSummary]:
    return Case.branch_result(FOUND, CustomerSummary(id='107', entity_id='CUST107'))

setattr(get_customer_found_default, '__module__', "dex_connectors.oracle_netsuite.oraclenetsuitemock")


class Mock:

    def __init__(self, connection_name: str = "oracle-netsuite") -> None:
        self.get_customer_script: Query[GetCustomerInput, CustomerSummary] = Query(GET_CUSTOMER_DEFINITION)
        self.get_customer_script.default(get_customer_found_default())
        self._connection = new_connection_with_operations(
            get_customer=self.get_customer_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.oracle_netsuite.oraclenetsuitemock")

__all__ = [
    "Mock",
    "get_customer_found",
    "get_customer_not_found",
    "get_customer_provider_rejected",
    "get_customer_invalid_response",
    "get_customer_defect",
    "get_customer_retry",
    "get_customer_found_default",
]
