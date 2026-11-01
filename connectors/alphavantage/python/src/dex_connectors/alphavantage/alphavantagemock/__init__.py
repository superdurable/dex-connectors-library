# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest-backed Alpha Vantage scripts for consuming Flow tests."""

from dex_connectors.sdk import ConnectionRef, Failure
from dex_connectors.sdk.connectormock import Case, Query

from ..client import (
    CONNECTOR_ID,
    FOUND,
    GET_GLOBAL_QUOTE_DEFINITION,
    GET_TIME_SERIES_DAILY_DEFINITION,
    NOT_FOUND,
)
from ..connection import Connection, new_connection_with_operations
from ..models import DailyPrice, DailySeriesOutput, GlobalQuoteOutput, StockInput


def get_global_quote_found(
    value: GlobalQuoteOutput, failure: Failure | None = None
) -> Case[GlobalQuoteOutput]:
    """Script the successful found route."""
    return Case.branch_result(FOUND, value, failure)


def get_global_quote_not_found(
    value: GlobalQuoteOutput, failure: Failure | None = None
) -> Case[GlobalQuoteOutput]:
    """Script a symbol without market data."""
    return Case.branch_result(NOT_FOUND, value, failure)


def get_global_quote_default_answer() -> Case[GlobalQuoteOutput]:
    """Return the manifest's getGlobalQuote default answer."""
    return get_global_quote_found(GlobalQuoteOutput("IBM", "255.25", "2026-10-28"))


def get_time_series_daily_found(
    value: DailySeriesOutput, failure: Failure | None = None
) -> Case[DailySeriesOutput]:
    """Script the successful found route."""
    return Case.branch_result(FOUND, value, failure)


def get_time_series_daily_default_answer() -> Case[DailySeriesOutput]:
    """Return the manifest's getTimeSeriesDaily default answer."""
    day = DailyPrice("2026-10-28", "254.00", "256.00", "253.00", "255.25", "1200000")
    return get_time_series_daily_found(DailySeriesOutput("IBM", (day,)))


class Mock:
    """Script Alpha Vantage Queries, defaulting to their manifest answers."""

    def __init__(self, connection_name: str = "alphavantage") -> None:
        self.get_global_quote_script: Query[StockInput, GlobalQuoteOutput] = Query(
            GET_GLOBAL_QUOTE_DEFINITION
        )
        self.get_global_quote_script.default(get_global_quote_default_answer())
        self.get_time_series_daily_script: Query[StockInput, DailySeriesOutput] = Query(
            GET_TIME_SERIES_DAILY_DEFINITION
        )
        self.get_time_series_daily_script.default(get_time_series_daily_default_answer())
        self._connection = new_connection_with_operations(
            self.get_global_quote_script,
            self.get_time_series_daily_script,
            ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        """Return this mock's named connection for Step registration."""
        return self._connection


__all__ = [
    "Mock",
    "get_global_quote_found",
    "get_global_quote_not_found",
    "get_global_quote_default_answer",
    "get_time_series_daily_found",
    "get_time_series_daily_default_answer",
]
