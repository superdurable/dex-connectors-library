# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListStoresInput, Store, StoreList
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_STORES_DEFINITION, PROVIDER_REJECTED

def list_stores_listed(value: StoreList, failure: Failure | None = None) -> Case[StoreList]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_stores_listed, '__module__', "dex_connectors.lingxing.lingxingmock")

def list_stores_provider_rejected(value: StoreList, failure: Failure | None = None) -> Case[StoreList]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_stores_provider_rejected, '__module__', "dex_connectors.lingxing.lingxingmock")

def list_stores_invalid_response(value: StoreList, failure: Failure | None = None) -> Case[StoreList]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_stores_invalid_response, '__module__', "dex_connectors.lingxing.lingxingmock")

def list_stores_defect(value: StoreList, failure: Failure | None = None) -> Case[StoreList]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_stores_defect, '__module__', "dex_connectors.lingxing.lingxingmock")

def list_stores_retry(failure: Failure, after_seconds: float = 0) -> Case[StoreList]:
    return Case.retry(failure, after_seconds)

setattr(list_stores_retry, '__module__', "dex_connectors.lingxing.lingxingmock")

def list_stores_listed_store() -> Case[StoreList]:
    return Case.branch_result(LISTED, StoreList(stores=(Store(store_id=1519, name='Sample EU Store', region='EU', country='Spain', status=1),), truncated=False))

setattr(list_stores_listed_store, '__module__', "dex_connectors.lingxing.lingxingmock")


class Mock:

    def __init__(self, connection_name: str = "lingxing") -> None:
        self.list_stores_script: Query[ListStoresInput, StoreList] = Query(LIST_STORES_DEFINITION)
        self.list_stores_script.default(list_stores_listed_store())
        self._connection = new_connection_with_operations(
            list_stores=self.list_stores_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.lingxing.lingxingmock")

__all__ = [
    "Mock",
    "list_stores_listed",
    "list_stores_provider_rejected",
    "list_stores_invalid_response",
    "list_stores_defect",
    "list_stores_retry",
    "list_stores_listed_store",
]
