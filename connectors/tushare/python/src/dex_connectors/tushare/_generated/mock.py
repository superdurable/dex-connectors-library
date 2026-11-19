# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetTradingDayInput, GetTradingDayOutput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_TRADING_DAY_DEFINITION, INVALID_RESPONSE, NO_DATA, PROVIDER_REJECTED

def get_trading_day_found(value: GetTradingDayOutput, failure: Failure | None = None) -> Case[GetTradingDayOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_trading_day_found, '__module__', "dex_connectors.tushare.tusharemock")

def get_trading_day_no_data(value: GetTradingDayOutput, failure: Failure | None = None) -> Case[GetTradingDayOutput]:
    return Case.branch_result(NO_DATA, value, failure)

setattr(get_trading_day_no_data, '__module__', "dex_connectors.tushare.tusharemock")

def get_trading_day_provider_rejected(value: GetTradingDayOutput, failure: Failure | None = None) -> Case[GetTradingDayOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_trading_day_provider_rejected, '__module__', "dex_connectors.tushare.tusharemock")

def get_trading_day_invalid_response(value: GetTradingDayOutput, failure: Failure | None = None) -> Case[GetTradingDayOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_trading_day_invalid_response, '__module__', "dex_connectors.tushare.tusharemock")

def get_trading_day_defect(value: GetTradingDayOutput, failure: Failure | None = None) -> Case[GetTradingDayOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_trading_day_defect, '__module__', "dex_connectors.tushare.tusharemock")

def get_trading_day_retry(failure: Failure, after_seconds: float = 0) -> Case[GetTradingDayOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_trading_day_retry, '__module__', "dex_connectors.tushare.tusharemock")

def get_trading_day_sample_trading_day() -> Case[GetTradingDayOutput]:
    return Case.branch_result(FOUND, GetTradingDayOutput(exchange='SSE', date='20261113', is_open=True, previous_trading_date='20261112'))

setattr(get_trading_day_sample_trading_day, '__module__', "dex_connectors.tushare.tusharemock")

def get_trading_day_no_date() -> Case[GetTradingDayOutput]:
    return Case.branch_result(NO_DATA, GetTradingDayOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getTradingDay", "Tushare returned no matching calendar date."))

setattr(get_trading_day_no_date, '__module__', "dex_connectors.tushare.tusharemock")


class Mock:

    def __init__(self, connection_name: str = "tushare") -> None:
        self.get_trading_day_script: Query[GetTradingDayInput, GetTradingDayOutput] = Query(GET_TRADING_DAY_DEFINITION)
        self.get_trading_day_script.default(get_trading_day_sample_trading_day())
        self._connection = new_connection_with_operations(
            get_trading_day=self.get_trading_day_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.tushare.tusharemock")

__all__ = [
    "Mock",
    "get_trading_day_found",
    "get_trading_day_no_data",
    "get_trading_day_provider_rejected",
    "get_trading_day_invalid_response",
    "get_trading_day_defect",
    "get_trading_day_retry",
    "get_trading_day_sample_trading_day",
    "get_trading_day_no_date",
]
