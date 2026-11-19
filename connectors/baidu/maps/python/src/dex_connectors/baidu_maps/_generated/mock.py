# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GeocodeAddressInput, GeocodeAddressOutput
from .definitions import CONNECTOR_ID, DEFECT, GEOCODED, GEOCODE_ADDRESS_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED

def geocode_address_geocoded(value: GeocodeAddressOutput, failure: Failure | None = None) -> Case[GeocodeAddressOutput]:
    return Case.branch_result(GEOCODED, value, failure)

setattr(geocode_address_geocoded, '__module__', "dex_connectors.baidu_maps.baidumapsmock")

def geocode_address_provider_rejected(value: GeocodeAddressOutput, failure: Failure | None = None) -> Case[GeocodeAddressOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(geocode_address_provider_rejected, '__module__', "dex_connectors.baidu_maps.baidumapsmock")

def geocode_address_invalid_response(value: GeocodeAddressOutput, failure: Failure | None = None) -> Case[GeocodeAddressOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(geocode_address_invalid_response, '__module__', "dex_connectors.baidu_maps.baidumapsmock")

def geocode_address_defect(value: GeocodeAddressOutput, failure: Failure | None = None) -> Case[GeocodeAddressOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(geocode_address_defect, '__module__', "dex_connectors.baidu_maps.baidumapsmock")

def geocode_address_retry(failure: Failure, after_seconds: float = 0) -> Case[GeocodeAddressOutput]:
    return Case.retry(failure, after_seconds)

setattr(geocode_address_retry, '__module__', "dex_connectors.baidu_maps.baidumapsmock")

def geocode_address_baidu_office() -> Case[GeocodeAddressOutput]:
    return Case.branch_result(GEOCODED, GeocodeAddressOutput(longitude=116.30783584945, latitude=40.056583335558, precise=True, confidence=80, comprehension=100, level='门址'))

setattr(geocode_address_baidu_office, '__module__', "dex_connectors.baidu_maps.baidumapsmock")


class Mock:

    def __init__(self, connection_name: str = "baidu-maps") -> None:
        self.geocode_address_script: Query[GeocodeAddressInput, GeocodeAddressOutput] = Query(GEOCODE_ADDRESS_DEFINITION)
        self.geocode_address_script.default(geocode_address_baidu_office())
        self._connection = new_connection_with_operations(
            geocode_address=self.geocode_address_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.baidu_maps.baidumapsmock")

__all__ = [
    "Mock",
    "geocode_address_geocoded",
    "geocode_address_provider_rejected",
    "geocode_address_invalid_response",
    "geocode_address_defect",
    "geocode_address_retry",
    "geocode_address_baidu_office",
]
