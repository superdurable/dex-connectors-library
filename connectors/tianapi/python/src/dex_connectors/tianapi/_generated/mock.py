# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetHolidayStatusInput, GetHolidayStatusOutput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_HOLIDAY_STATUS_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED

def get_holiday_status_found(value: GetHolidayStatusOutput, failure: Failure | None = None) -> Case[GetHolidayStatusOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_holiday_status_found, '__module__', "dex_connectors.tianapi.tianapimock")

def get_holiday_status_provider_rejected(value: GetHolidayStatusOutput, failure: Failure | None = None) -> Case[GetHolidayStatusOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_holiday_status_provider_rejected, '__module__', "dex_connectors.tianapi.tianapimock")

def get_holiday_status_invalid_response(value: GetHolidayStatusOutput, failure: Failure | None = None) -> Case[GetHolidayStatusOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_holiday_status_invalid_response, '__module__', "dex_connectors.tianapi.tianapimock")

def get_holiday_status_defect(value: GetHolidayStatusOutput, failure: Failure | None = None) -> Case[GetHolidayStatusOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_holiday_status_defect, '__module__', "dex_connectors.tianapi.tianapimock")

def get_holiday_status_retry(failure: Failure, after_seconds: float = 0) -> Case[GetHolidayStatusOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_holiday_status_retry, '__module__', "dex_connectors.tianapi.tianapimock")

def get_holiday_status_new_years_day() -> Case[GetHolidayStatusOutput]:
    return Case.branch_result(FOUND, GetHolidayStatusOutput(date='2021-01-01', day_code=1, is_not_work=1))

setattr(get_holiday_status_new_years_day, '__module__', "dex_connectors.tianapi.tianapimock")


class Mock:

    def __init__(self, connection_name: str = "tianapi") -> None:
        self.get_holiday_status_script: Query[GetHolidayStatusInput, GetHolidayStatusOutput] = Query(GET_HOLIDAY_STATUS_DEFINITION)
        self.get_holiday_status_script.default(get_holiday_status_new_years_day())
        self._connection = new_connection_with_operations(
            get_holiday_status=self.get_holiday_status_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.tianapi.tianapimock")

__all__ = [
    "Mock",
    "get_holiday_status_found",
    "get_holiday_status_provider_rejected",
    "get_holiday_status_invalid_response",
    "get_holiday_status_defect",
    "get_holiday_status_retry",
    "get_holiday_status_new_years_day",
]
