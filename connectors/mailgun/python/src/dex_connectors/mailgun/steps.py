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
    new_get_events_step as new_get_events_step,
)
from ._generated.steps import (
    new_list_domains_step as new_list_domains_step,
)
from .connection import Connection as Connection
from .models import DomainsOutput as DomainsOutput
from .models import EventsOutput as EventsOutput
from .models import GetEventsInput as GetEventsInput
from .models import ListDomainsInput as ListDomainsInput

BranchTarget = TypeVar("BranchTarget")

new_get_events_step.__doc__ = "Create the typed event page Step; unwired optional outcomes fail the Flow."
new_list_domains_step.__doc__ = "Create the typed domain page Step; unwired optional outcomes fail the Flow."
