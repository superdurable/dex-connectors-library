# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public operation-specific Step factories generated from connector.yaml."""

from collections.abc import Callable as Callable
from collections.abc import Mapping as Mapping
from typing import TypeVar as TypeVar

import dex as dex
from dex_connectors.sdk import MutationResult as MutationResult
from dex_connectors.sdk import MutationStep as MutationStep
from dex_connectors.sdk import MutationStepConfig as MutationStepConfig
from dex_connectors.sdk import QueryResult as QueryResult
from dex_connectors.sdk import QueryStep as QueryStep
from dex_connectors.sdk import QueryStepConfig as QueryStepConfig
from dex_connectors.sdk import StepAnnotations as StepAnnotations

from ._generated.steps import StepInput as StepInput
from ._generated.steps import new_capture_event_step as new_capture_event_step
from ._generated.steps import new_list_insights_step as new_list_insights_step
from .connection import Connection as Connection
from .models import CapturedEvent as CapturedEvent
from .models import CaptureEventInput as CaptureEventInput
from .models import InsightsPage as InsightsPage
from .models import ListInsightsInput as ListInsightsInput

BranchTarget = TypeVar("BranchTarget")

new_capture_event_step.__doc__ = "Create the typed event capture Step with optional failure branches."
new_list_insights_step.__doc__ = "Create the typed saved insight listing Step."
