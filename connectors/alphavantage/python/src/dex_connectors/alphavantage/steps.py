# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public operation-specific Step factories from connector.yaml."""

from collections.abc import Callable as Callable
from collections.abc import Mapping as Mapping
from typing import TypeVar as TypeVar

import dex as dex
from dex_connectors.sdk import QueryResult as QueryResult
from dex_connectors.sdk import QueryStep as QueryStep
from dex_connectors.sdk import QueryStepConfig as QueryStepConfig
from dex_connectors.sdk import StepAnnotations as StepAnnotations

from ._generated.steps import (
    StepInput as StepInput,
)
from ._generated.steps import (
    new_get_global_quote_step as new_get_global_quote_step,
)
from ._generated.steps import (
    new_get_time_series_daily_step as new_get_time_series_daily_step,
)
from .connection import Connection as Connection
from .models import DailySeriesOutput as DailySeriesOutput
from .models import GlobalQuoteOutput as GlobalQuoteOutput
from .models import StockInput as StockInput

BranchTarget = TypeVar("BranchTarget")

new_get_global_quote_step.__doc__ = (
    "Create the typed global quote Step; unwired optional outcomes fail the Flow."
)
new_get_time_series_daily_step.__doc__ = (
    "Create the typed daily series Step; unwired optional outcomes fail the Flow."
)
