# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListWarehousesInput, Warehouse, WarehousePage
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_WAREHOUSES_DEFINITION, PROVIDER_REJECTED

def list_warehouses_listed(value: WarehousePage, failure: Failure | None = None) -> Case[WarehousePage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_warehouses_listed, '__module__', "dex_connectors.chukou1.chukou1mock")

def list_warehouses_provider_rejected(value: WarehousePage, failure: Failure | None = None) -> Case[WarehousePage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_warehouses_provider_rejected, '__module__', "dex_connectors.chukou1.chukou1mock")

def list_warehouses_invalid_response(value: WarehousePage, failure: Failure | None = None) -> Case[WarehousePage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_warehouses_invalid_response, '__module__', "dex_connectors.chukou1.chukou1mock")

def list_warehouses_defect(value: WarehousePage, failure: Failure | None = None) -> Case[WarehousePage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_warehouses_defect, '__module__', "dex_connectors.chukou1.chukou1mock")

def list_warehouses_retry(failure: Failure, after_seconds: float = 0) -> Case[WarehousePage]:
    return Case.retry(failure, after_seconds)

setattr(list_warehouses_retry, '__module__', "dex_connectors.chukou1.chukou1mock")

def list_warehouses_listed_warehouse() -> Case[WarehousePage]:
    return Case.branch_result(LISTED, WarehousePage(warehouses=(Warehouse(id='US', name='San Francisco Warehouse', country='United States', country_code='US'),)))

setattr(list_warehouses_listed_warehouse, '__module__', "dex_connectors.chukou1.chukou1mock")


class Mock:

    def __init__(self, connection_name: str = "chukou1") -> None:
        self.list_warehouses_script: Query[ListWarehousesInput, WarehousePage] = Query(LIST_WAREHOUSES_DEFINITION)
        self.list_warehouses_script.default(list_warehouses_listed_warehouse())
        self._connection = new_connection_with_operations(
            list_warehouses=self.list_warehouses_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.chukou1.chukou1mock")

__all__ = [
    "Mock",
    "list_warehouses_listed",
    "list_warehouses_provider_rejected",
    "list_warehouses_invalid_response",
    "list_warehouses_defect",
    "list_warehouses_retry",
    "list_warehouses_listed_warehouse",
]
