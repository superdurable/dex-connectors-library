# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetStoreBasicInput, StoreBasic
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_STORE_BASIC_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED

def get_store_basic_found(value: StoreBasic, failure: Failure | None = None) -> Case[StoreBasic]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_store_basic_found, '__module__', "dex_connectors.weimob.weimobmock")

def get_store_basic_provider_rejected(value: StoreBasic, failure: Failure | None = None) -> Case[StoreBasic]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_store_basic_provider_rejected, '__module__', "dex_connectors.weimob.weimobmock")

def get_store_basic_invalid_response(value: StoreBasic, failure: Failure | None = None) -> Case[StoreBasic]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_store_basic_invalid_response, '__module__', "dex_connectors.weimob.weimobmock")

def get_store_basic_defect(value: StoreBasic, failure: Failure | None = None) -> Case[StoreBasic]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_store_basic_defect, '__module__', "dex_connectors.weimob.weimobmock")

def get_store_basic_retry(failure: Failure, after_seconds: float = 0) -> Case[StoreBasic]:
    return Case.retry(failure, after_seconds)

setattr(get_store_basic_retry, '__module__', "dex_connectors.weimob.weimobmock")

def get_store_basic_found_default() -> Case[StoreBasic]:
    return Case.branch_result(FOUND, StoreBasic(store_id=19, name='Sample Store', logo='https://example.com/store.png', description='Sample store'))

setattr(get_store_basic_found_default, '__module__', "dex_connectors.weimob.weimobmock")


class Mock:

    def __init__(self, connection_name: str = "weimob") -> None:
        self.get_store_basic_script: Query[GetStoreBasicInput, StoreBasic] = Query(GET_STORE_BASIC_DEFINITION)
        self.get_store_basic_script.default(get_store_basic_found_default())
        self._connection = new_connection_with_operations(
            get_store_basic=self.get_store_basic_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.weimob.weimobmock")

__all__ = [
    "Mock",
    "get_store_basic_found",
    "get_store_basic_provider_rejected",
    "get_store_basic_invalid_response",
    "get_store_basic_defect",
    "get_store_basic_retry",
    "get_store_basic_found_default",
]
