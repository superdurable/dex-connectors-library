# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from ._generated.models import (
    DailyPrice as DailyPrice,
)
from ._generated.models import DailySeriesOutput as DailySeriesOutput
from ._generated.models import (
    GlobalQuoteOutput as GlobalQuoteOutput,
)
from ._generated.models import (
    StockInput as StockInput,
)

DailySeriesOutput.__doc__ = "Symbol and its compact daily series, newest day first."

StockInput.__doc__ = "Ticker symbol such as IBM: 1 to 25 letters, digits, '.', '_', or '-'."
GlobalQuoteOutput.__doc__ = "Latest quote; price is Alpha Vantage's decimal text and the day is YYYY-MM-DD."
DailyPrice.__doc__ = "One trading day's prices and volume as Alpha Vantage's decimal text."
