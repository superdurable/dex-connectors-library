# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import CustomerField, CustomerFields, ListCustomerFieldsInput
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_CUSTOMER_FIELDS_DEFINITION, PROVIDER_REJECTED

def list_customer_fields_listed(value: CustomerFields, failure: Failure | None = None) -> Case[CustomerFields]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_customer_fields_listed, '__module__', "dex_connectors.okki.okkimock")

def list_customer_fields_provider_rejected(value: CustomerFields, failure: Failure | None = None) -> Case[CustomerFields]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_customer_fields_provider_rejected, '__module__', "dex_connectors.okki.okkimock")

def list_customer_fields_invalid_response(value: CustomerFields, failure: Failure | None = None) -> Case[CustomerFields]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_customer_fields_invalid_response, '__module__', "dex_connectors.okki.okkimock")

def list_customer_fields_defect(value: CustomerFields, failure: Failure | None = None) -> Case[CustomerFields]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_customer_fields_defect, '__module__', "dex_connectors.okki.okkimock")

def list_customer_fields_retry(failure: Failure, after_seconds: float = 0) -> Case[CustomerFields]:
    return Case.retry(failure, after_seconds)

setattr(list_customer_fields_retry, '__module__', "dex_connectors.okki.okkimock")

def list_customer_fields_listed_default() -> Case[CustomerFields]:
    return Case.branch_result(LISTED, CustomerFields(fields=(CustomerField(id='origin_list', name='客户来源', field_type=7), CustomerField(id='trail_status', name='客户阶段', field_type=3))))

setattr(list_customer_fields_listed_default, '__module__', "dex_connectors.okki.okkimock")


class Mock:

    def __init__(self, connection_name: str = "okki") -> None:
        self.list_customer_fields_script: Query[ListCustomerFieldsInput, CustomerFields] = Query(LIST_CUSTOMER_FIELDS_DEFINITION)
        self.list_customer_fields_script.default(list_customer_fields_listed_default())
        self._connection = new_connection_with_operations(
            list_customer_fields=self.list_customer_fields_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.okki.okkimock")

__all__ = [
    "Mock",
    "list_customer_fields_listed",
    "list_customer_fields_provider_rejected",
    "list_customer_fields_invalid_response",
    "list_customer_fields_defect",
    "list_customer_fields_retry",
    "list_customer_fields_listed_default",
]
