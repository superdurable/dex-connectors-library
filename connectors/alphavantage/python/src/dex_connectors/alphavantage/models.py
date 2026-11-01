# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed Alpha Vantage symbol inputs, quotes, and daily price series."""

from dataclasses import dataclass, field


@dataclass(frozen=True, slots=True)
class StockInput:
    """Ticker symbol such as IBM: 1 to 25 letters, digits, '.', '_', or '-'."""

    symbol: str = ""


@dataclass(frozen=True, slots=True)
class GlobalQuoteOutput:
    """Latest quote; price is Alpha Vantage's decimal text and the day is YYYY-MM-DD."""

    symbol: str = ""
    price: str = ""
    latest_trading_day: str = field(default="", metadata={"dex_json_name": "latestTradingDay"})


@dataclass(frozen=True, slots=True)
class DailyPrice:
    """One trading day's prices and volume as Alpha Vantage's decimal text."""

    date: str = ""
    open: str = ""
    high: str = ""
    low: str = ""
    close: str = ""
    volume: str = ""


@dataclass(frozen=True, slots=True)
class DailySeriesOutput:
    """Symbol and its compact daily series, newest day first."""

    symbol: str = ""
    days: tuple[DailyPrice, ...] = ()
