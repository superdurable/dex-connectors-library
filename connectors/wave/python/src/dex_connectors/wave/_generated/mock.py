# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import CustomerProfile, GetCustomerInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_CUSTOMER_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED

def get_customer_found(value: CustomerProfile, failure: Failure | None = None) -> Case[CustomerProfile]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_customer_found, '__module__', "dex_connectors.wave.wavemock")

def get_customer_not_found(value: CustomerProfile, failure: Failure | None = None) -> Case[CustomerProfile]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_customer_not_found, '__module__', "dex_connectors.wave.wavemock")

def get_customer_provider_rejected(value: CustomerProfile, failure: Failure | None = None) -> Case[CustomerProfile]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_customer_provider_rejected, '__module__', "dex_connectors.wave.wavemock")

def get_customer_invalid_response(value: CustomerProfile, failure: Failure | None = None) -> Case[CustomerProfile]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_customer_invalid_response, '__module__', "dex_connectors.wave.wavemock")

def get_customer_defect(value: CustomerProfile, failure: Failure | None = None) -> Case[CustomerProfile]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_customer_defect, '__module__', "dex_connectors.wave.wavemock")

def get_customer_retry(failure: Failure, after_seconds: float = 0) -> Case[CustomerProfile]:
    return Case.retry(failure, after_seconds)

setattr(get_customer_retry, '__module__', "dex_connectors.wave.wavemock")

def get_customer_found_default() -> Case[CustomerProfile]:
    return Case.branch_result(FOUND, CustomerProfile(business_id='business-1', id='customer-1', name='Example Customer'))

setattr(get_customer_found_default, '__module__', "dex_connectors.wave.wavemock")


class Mock:

    def __init__(self, connection_name: str = "wave") -> None:
        self.get_customer_script: Query[GetCustomerInput, CustomerProfile] = Query(GET_CUSTOMER_DEFINITION)
        self.get_customer_script.default(get_customer_found_default())
        self._connection = new_connection_with_operations(
            get_customer=self.get_customer_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.wave.wavemock")

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
