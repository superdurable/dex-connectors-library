# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from dataclasses import dataclass

from ._generated.models import (
    DailyPrice as DailyPrice,
)
from ._generated.models import (
    GlobalQuoteOutput as GlobalQuoteOutput,
)
from ._generated.models import (
    StockInput as StockInput,
)


@dataclass(frozen=True, slots=True)
class DailySeriesOutput:
    """Symbol and its compact daily series, newest day first."""

    symbol: str = ""
    days: tuple[DailyPrice, ...] = ()
