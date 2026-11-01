# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated Python dataclasses from Go operation types."""

from dataclasses import dataclass, field as _dataclass_field

@dataclass(frozen=True, slots=True)
class StockInput:
    """Typed StockInput value from the Go connector contract."""

    symbol: str = ''


setattr(StockInput, '__module__', 'dex_connectors.alphavantage.models')


@dataclass(frozen=True, slots=True)
class GlobalQuoteOutput:
    """Typed GlobalQuoteOutput value from the Go connector contract."""

    symbol: str = ''
    price: str = ''
    latest_trading_day: str = _dataclass_field(default='', metadata={'dex_json_name': 'latestTradingDay'})


setattr(GlobalQuoteOutput, '__module__', 'dex_connectors.alphavantage.models')


@dataclass(frozen=True, slots=True)
class DailyPrice:
    """Typed DailyPrice value from the Go connector contract."""

    date: str = ''
    open: str = ''
    high: str = ''
    low: str = ''
    close: str = ''
    volume: str = ''


setattr(DailyPrice, '__module__', 'dex_connectors.alphavantage.models')


@dataclass(frozen=True, slots=True)
class DailySeriesOutput:
    """Typed DailySeriesOutput value from the Go connector contract."""

    symbol: str = ''
    days: tuple[DailyPrice, ...] | None = None


setattr(DailySeriesOutput, '__module__', 'dex_connectors.alphavantage.models')
