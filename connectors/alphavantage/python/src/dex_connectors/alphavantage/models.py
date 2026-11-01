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
