# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListVehicleBrandsInput, ListVehicleBrandsOutput, VehicleBrandPrice
from .definitions import CONNECTOR_ID, DEFECT, FOUND, INVALID_RESPONSE, LIST_VEHICLE_BRANDS_DEFINITION, PROVIDER_REJECTED

def list_vehicle_brands_found(value: ListVehicleBrandsOutput, failure: Failure | None = None) -> Case[ListVehicleBrandsOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(list_vehicle_brands_found, '__module__', "dex_connectors.wapi.wapimock")

def list_vehicle_brands_provider_rejected(value: ListVehicleBrandsOutput, failure: Failure | None = None) -> Case[ListVehicleBrandsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_vehicle_brands_provider_rejected, '__module__', "dex_connectors.wapi.wapimock")

def list_vehicle_brands_invalid_response(value: ListVehicleBrandsOutput, failure: Failure | None = None) -> Case[ListVehicleBrandsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_vehicle_brands_invalid_response, '__module__', "dex_connectors.wapi.wapimock")

def list_vehicle_brands_defect(value: ListVehicleBrandsOutput, failure: Failure | None = None) -> Case[ListVehicleBrandsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_vehicle_brands_defect, '__module__', "dex_connectors.wapi.wapimock")

def list_vehicle_brands_retry(failure: Failure, after_seconds: float = 0) -> Case[ListVehicleBrandsOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_vehicle_brands_retry, '__module__', "dex_connectors.wapi.wapimock")

def list_vehicle_brands_one_brand() -> Case[ListVehicleBrandsOutput]:
    return Case.branch_result(FOUND, ListVehicleBrandsOutput(total=1, brands=(VehicleBrandPrice(id='15', name='宝马', price_fen=3400),)))

setattr(list_vehicle_brands_one_brand, '__module__', "dex_connectors.wapi.wapimock")

def list_vehicle_brands_no_brands() -> Case[ListVehicleBrandsOutput]:
    return Case.branch_result(FOUND, ListVehicleBrandsOutput(total=0, brands=()))

setattr(list_vehicle_brands_no_brands, '__module__', "dex_connectors.wapi.wapimock")


class Mock:

    def __init__(self, connection_name: str = "wapi") -> None:
        self.list_vehicle_brands_script: Query[ListVehicleBrandsInput, ListVehicleBrandsOutput] = Query(LIST_VEHICLE_BRANDS_DEFINITION)
        self.list_vehicle_brands_script.default(list_vehicle_brands_one_brand())
        self._connection = new_connection_with_operations(
            list_vehicle_brands=self.list_vehicle_brands_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.wapi.wapimock")

__all__ = [
    "Mock",
    "list_vehicle_brands_found",
    "list_vehicle_brands_provider_rejected",
    "list_vehicle_brands_invalid_response",
    "list_vehicle_brands_defect",
    "list_vehicle_brands_retry",
    "list_vehicle_brands_one_brand",
    "list_vehicle_brands_no_brands",
]
