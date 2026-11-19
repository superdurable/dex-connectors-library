# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import LookupIPLocationInput, LookupIPLocationOutput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, INVALID_RESPONSE, LOOKUP_IP_LOCATION_DEFINITION, PROVIDER_REJECTED

def lookup_ip_location_found(value: LookupIPLocationOutput, failure: Failure | None = None) -> Case[LookupIPLocationOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(lookup_ip_location_found, '__module__', "dex_connectors.alapi.alapimock")

def lookup_ip_location_provider_rejected(value: LookupIPLocationOutput, failure: Failure | None = None) -> Case[LookupIPLocationOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(lookup_ip_location_provider_rejected, '__module__', "dex_connectors.alapi.alapimock")

def lookup_ip_location_invalid_response(value: LookupIPLocationOutput, failure: Failure | None = None) -> Case[LookupIPLocationOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(lookup_ip_location_invalid_response, '__module__', "dex_connectors.alapi.alapimock")

def lookup_ip_location_defect(value: LookupIPLocationOutput, failure: Failure | None = None) -> Case[LookupIPLocationOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(lookup_ip_location_defect, '__module__', "dex_connectors.alapi.alapimock")

def lookup_ip_location_retry(failure: Failure, after_seconds: float = 0) -> Case[LookupIPLocationOutput]:
    return Case.retry(failure, after_seconds)

setattr(lookup_ip_location_retry, '__module__', "dex_connectors.alapi.alapimock")

def lookup_ip_location_shenzhen() -> Case[LookupIPLocationOutput]:
    return Case.branch_result(FOUND, LookupIPLocationOutput(country='中国', province='广东', city='深圳', district='龙岗区', operator='移动'))

setattr(lookup_ip_location_shenzhen, '__module__', "dex_connectors.alapi.alapimock")


class Mock:

    def __init__(self, connection_name: str = "alapi") -> None:
        self.lookup_ip_location_script: Query[LookupIPLocationInput, LookupIPLocationOutput] = Query(LOOKUP_IP_LOCATION_DEFINITION)
        self.lookup_ip_location_script.default(lookup_ip_location_shenzhen())
        self._connection = new_connection_with_operations(
            lookup_ip_location=self.lookup_ip_location_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.alapi.alapimock")

__all__ = [
    "Mock",
    "lookup_ip_location_found",
    "lookup_ip_location_provider_rejected",
    "lookup_ip_location_invalid_response",
    "lookup_ip_location_defect",
    "lookup_ip_location_retry",
    "lookup_ip_location_shenzhen",
]
