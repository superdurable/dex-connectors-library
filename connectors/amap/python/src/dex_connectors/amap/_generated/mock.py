# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GeocodeAddressInput, GeocodeAddressOutput, GeocodedLocation
from .definitions import CONNECTOR_ID, DEFECT, GEOCODED, GEOCODE_ADDRESS_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED

def geocode_address_geocoded(value: GeocodeAddressOutput, failure: Failure | None = None) -> Case[GeocodeAddressOutput]:
    return Case.branch_result(GEOCODED, value, failure)

setattr(geocode_address_geocoded, '__module__', "dex_connectors.amap.amapmock")

def geocode_address_provider_rejected(value: GeocodeAddressOutput, failure: Failure | None = None) -> Case[GeocodeAddressOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(geocode_address_provider_rejected, '__module__', "dex_connectors.amap.amapmock")

def geocode_address_invalid_response(value: GeocodeAddressOutput, failure: Failure | None = None) -> Case[GeocodeAddressOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(geocode_address_invalid_response, '__module__', "dex_connectors.amap.amapmock")

def geocode_address_defect(value: GeocodeAddressOutput, failure: Failure | None = None) -> Case[GeocodeAddressOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(geocode_address_defect, '__module__', "dex_connectors.amap.amapmock")

def geocode_address_retry(failure: Failure, after_seconds: float = 0) -> Case[GeocodeAddressOutput]:
    return Case.retry(failure, after_seconds)

setattr(geocode_address_retry, '__module__', "dex_connectors.amap.amapmock")

def geocode_address_beijing_address() -> Case[GeocodeAddressOutput]:
    return Case.branch_result(GEOCODED, GeocodeAddressOutput(locations=(GeocodedLocation(formatted_address='北京市朝阳区阜通东大街6号', province='北京市', city='北京市', district='朝阳区', adcode='110105', longitude=116.481488, latitude=39.990464, level='门牌号'),)))

setattr(geocode_address_beijing_address, '__module__', "dex_connectors.amap.amapmock")


class Mock:

    def __init__(self, connection_name: str = "amap") -> None:
        self.geocode_address_script: Query[GeocodeAddressInput, GeocodeAddressOutput] = Query(GEOCODE_ADDRESS_DEFINITION)
        self.geocode_address_script.default(geocode_address_beijing_address())
        self._connection = new_connection_with_operations(
            geocode_address=self.geocode_address_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.amap.amapmock")

__all__ = [
    "Mock",
    "geocode_address_geocoded",
    "geocode_address_provider_rejected",
    "geocode_address_invalid_response",
    "geocode_address_defect",
    "geocode_address_retry",
    "geocode_address_beijing_address",
]
