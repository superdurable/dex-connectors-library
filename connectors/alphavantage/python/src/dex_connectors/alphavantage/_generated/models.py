# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# StockInput selects one equity symbol such as IBM.
@dataclass(frozen=True, slots=True)
class StockInput:
    # Symbol is the provider equity ticker; dots and hyphens support global symbols.
    symbol: str = ''


setattr(StockInput, '__module__', 'dex_connectors.alphavantage.models')


# GlobalQuoteOutput contains the latest provider quote for one equity.
@dataclass(frozen=True, slots=True)
class GlobalQuoteOutput:
    # Symbol is the provider's ticker.
    symbol: str = ''
    # Price is the decimal price string from Alpha Vantage.
    price: str = ''
    # LatestTradingDay is the quote's YYYY-MM-DD trading date.
    latest_trading_day: str = _dataclass_field(default='', metadata={'dex_json_name': 'latestTradingDay'})


setattr(GlobalQuoteOutput, '__module__', 'dex_connectors.alphavantage.models')


# DailyPrice is one raw daily OHLCV record.
@dataclass(frozen=True, slots=True)
class DailyPrice:
    # Date is the YYYY-MM-DD trading date.
    date: str = ''
    # Open is the raw opening price decimal string.
    open: str = ''
    # High is the raw daily high decimal string.
    high: str = ''
    # Low is the raw daily low decimal string.
    low: str = ''
    # Close is the raw closing price decimal string.
    close: str = ''
    # Volume is the traded share count string.
    volume: str = ''


setattr(DailyPrice, '__module__', 'dex_connectors.alphavantage.models')


# DailySeriesOutput contains up to the provider's compact 100 daily points.
@dataclass(frozen=True, slots=True)
class DailySeriesOutput:
    # Symbol is the requested equity ticker.
    symbol: str = ''
    # Days are newest first.
    days: tuple[DailyPrice, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'days', _replace_nil_slices(self.days, 1))


DailySeriesOutput.__annotations__['days'] = tuple[DailyPrice, ...] | None
DailySeriesOutput.__dataclass_fields__['days'].type = tuple[DailyPrice, ...] | None
setattr(DailySeriesOutput, '__module__', 'dex_connectors.alphavantage.models')
