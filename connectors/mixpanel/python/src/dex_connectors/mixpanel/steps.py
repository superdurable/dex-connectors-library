# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public operation-specific Step factories from connector.yaml."""

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

from ._generated.steps import (
    StepInput as StepInput,
)
from ._generated.steps import (
    new_export_events_step as new_export_events_step,
)
from ._generated.steps import (
    new_track_event_step as new_track_event_step,
)
from .connection import Connection as Connection
from .models import EventImportResult as EventImportResult
from .models import ExportedEvents as ExportedEvents
from .models import ExportEventsInput as ExportEventsInput
from .models import TrackEventInput as TrackEventInput

BranchTarget = TypeVar("BranchTarget")
