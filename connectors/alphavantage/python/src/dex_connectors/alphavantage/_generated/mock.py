# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import DailyPrice, DailySeriesOutput, GlobalQuoteOutput, StockInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_GLOBAL_QUOTE_DEFINITION, GET_TIME_SERIES_DAILY_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED

def get_global_quote_found(value: GlobalQuoteOutput, failure: Failure | None = None) -> Case[GlobalQuoteOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_global_quote_found, '__module__', "dex_connectors.alphavantage.alphavantagemock")

def get_global_quote_not_found(value: GlobalQuoteOutput, failure: Failure | None = None) -> Case[GlobalQuoteOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_global_quote_not_found, '__module__', "dex_connectors.alphavantage.alphavantagemock")

def get_global_quote_provider_rejected(value: GlobalQuoteOutput, failure: Failure | None = None) -> Case[GlobalQuoteOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_global_quote_provider_rejected, '__module__', "dex_connectors.alphavantage.alphavantagemock")

def get_global_quote_invalid_response(value: GlobalQuoteOutput, failure: Failure | None = None) -> Case[GlobalQuoteOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_global_quote_invalid_response, '__module__', "dex_connectors.alphavantage.alphavantagemock")

def get_global_quote_defect(value: GlobalQuoteOutput, failure: Failure | None = None) -> Case[GlobalQuoteOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_global_quote_defect, '__module__', "dex_connectors.alphavantage.alphavantagemock")

def get_global_quote_retry(failure: Failure, after_seconds: float = 0) -> Case[GlobalQuoteOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_global_quote_retry, '__module__', "dex_connectors.alphavantage.alphavantagemock")

def get_global_quote_found_default() -> Case[GlobalQuoteOutput]:
    return Case.branch_result(FOUND, GlobalQuoteOutput(symbol='IBM', price='255.25', latest_trading_day='2026-10-28'))

setattr(get_global_quote_found_default, '__module__', "dex_connectors.alphavantage.alphavantagemock")

def get_time_series_daily_found(value: DailySeriesOutput, failure: Failure | None = None) -> Case[DailySeriesOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_time_series_daily_found, '__module__', "dex_connectors.alphavantage.alphavantagemock")

def get_time_series_daily_not_found(value: DailySeriesOutput, failure: Failure | None = None) -> Case[DailySeriesOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_time_series_daily_not_found, '__module__', "dex_connectors.alphavantage.alphavantagemock")

def get_time_series_daily_provider_rejected(value: DailySeriesOutput, failure: Failure | None = None) -> Case[DailySeriesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_time_series_daily_provider_rejected, '__module__', "dex_connectors.alphavantage.alphavantagemock")

def get_time_series_daily_invalid_response(value: DailySeriesOutput, failure: Failure | None = None) -> Case[DailySeriesOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_time_series_daily_invalid_response, '__module__', "dex_connectors.alphavantage.alphavantagemock")

def get_time_series_daily_defect(value: DailySeriesOutput, failure: Failure | None = None) -> Case[DailySeriesOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_time_series_daily_defect, '__module__', "dex_connectors.alphavantage.alphavantagemock")

def get_time_series_daily_retry(failure: Failure, after_seconds: float = 0) -> Case[DailySeriesOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_time_series_daily_retry, '__module__', "dex_connectors.alphavantage.alphavantagemock")

def get_time_series_daily_found_default() -> Case[DailySeriesOutput]:
    return Case.branch_result(FOUND, DailySeriesOutput(symbol='IBM', days=(DailyPrice(date='2026-10-28', open='254.00', high='256.00', low='253.00', close='255.25', volume='1200000'),)))

setattr(get_time_series_daily_found_default, '__module__', "dex_connectors.alphavantage.alphavantagemock")


class Mock:

    def __init__(self, connection_name: str = "alphavantage") -> None:
        self.get_global_quote_script: Query[StockInput, GlobalQuoteOutput] = Query(GET_GLOBAL_QUOTE_DEFINITION)
        self.get_global_quote_script.default(get_global_quote_found_default())
        self.get_time_series_daily_script: Query[StockInput, DailySeriesOutput] = Query(GET_TIME_SERIES_DAILY_DEFINITION)
        self.get_time_series_daily_script.default(get_time_series_daily_found_default())
        self._connection = new_connection_with_operations(
            get_global_quote=self.get_global_quote_script,
            get_time_series_daily=self.get_time_series_daily_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.alphavantage.alphavantagemock")

__all__ = [
    "Mock",
    "get_global_quote_found",
    "get_global_quote_not_found",
    "get_global_quote_provider_rejected",
    "get_global_quote_invalid_response",
    "get_global_quote_defect",
    "get_global_quote_retry",
    "get_global_quote_found_default",
    "get_time_series_daily_found",
    "get_time_series_daily_not_found",
    "get_time_series_daily_provider_rejected",
    "get_time_series_daily_invalid_response",
    "get_time_series_daily_defect",
    "get_time_series_daily_retry",
    "get_time_series_daily_found_default",
]
