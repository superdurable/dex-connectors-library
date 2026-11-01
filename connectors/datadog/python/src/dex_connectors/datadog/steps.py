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
    new_list_monitors_step as new_list_monitors_step,
)
from ._generated.steps import (
    new_query_metrics_step as new_query_metrics_step,
)
from .connection import Connection as Connection
from .models import ListMonitorsInput as ListMonitorsInput
from .models import MetricsQuery as MetricsQuery
from .models import MonitorsPage as MonitorsPage
from .models import QueryMetricsInput as QueryMetricsInput

BranchTarget = TypeVar("BranchTarget")

new_query_metrics_step.__doc__ = (
    "Create the typed metric query Step; unwired optional outcomes fail the Flow."
)
new_list_monitors_step.__doc__ = (
    "Create the typed monitor page Step; unwired optional outcomes fail the Flow."
)
