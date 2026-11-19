# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import AreaCodeMatch, AreaCodeMatches, LookupAreaCodesInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, INVALID_RESPONSE, LOOKUP_AREA_CODES_DEFINITION, PROVIDER_REJECTED

def lookup_area_codes_found(value: AreaCodeMatches, failure: Failure | None = None) -> Case[AreaCodeMatches]:
    return Case.branch_result(FOUND, value, failure)

setattr(lookup_area_codes_found, '__module__', "dex_connectors.showapi.showapimock")

def lookup_area_codes_provider_rejected(value: AreaCodeMatches, failure: Failure | None = None) -> Case[AreaCodeMatches]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(lookup_area_codes_provider_rejected, '__module__', "dex_connectors.showapi.showapimock")

def lookup_area_codes_invalid_response(value: AreaCodeMatches, failure: Failure | None = None) -> Case[AreaCodeMatches]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(lookup_area_codes_invalid_response, '__module__', "dex_connectors.showapi.showapimock")

def lookup_area_codes_defect(value: AreaCodeMatches, failure: Failure | None = None) -> Case[AreaCodeMatches]:
    return Case.branch_result(DEFECT, value, failure)

setattr(lookup_area_codes_defect, '__module__', "dex_connectors.showapi.showapimock")

def lookup_area_codes_retry(failure: Failure, after_seconds: float = 0) -> Case[AreaCodeMatches]:
    return Case.retry(failure, after_seconds)

setattr(lookup_area_codes_retry, '__module__', "dex_connectors.showapi.showapimock")

def lookup_area_codes_found_area() -> Case[AreaCodeMatches]:
    return Case.branch_result(FOUND, AreaCodeMatches(matches=(AreaCodeMatch(province='Yunnan', area='Lijiang', area_code='530700'),)))

setattr(lookup_area_codes_found_area, '__module__', "dex_connectors.showapi.showapimock")


class Mock:

    def __init__(self, connection_name: str = "showapi") -> None:
        self.lookup_area_codes_script: Query[LookupAreaCodesInput, AreaCodeMatches] = Query(LOOKUP_AREA_CODES_DEFINITION)
        self.lookup_area_codes_script.default(lookup_area_codes_found_area())
        self._connection = new_connection_with_operations(
            lookup_area_codes=self.lookup_area_codes_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.showapi.showapimock")

__all__ = [
    "Mock",
    "lookup_area_codes_found",
    "lookup_area_codes_provider_rejected",
    "lookup_area_codes_invalid_response",
    "lookup_area_codes_defect",
    "lookup_area_codes_retry",
    "lookup_area_codes_found_area",
]
