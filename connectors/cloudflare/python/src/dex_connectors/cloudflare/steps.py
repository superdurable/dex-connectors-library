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
    new_list_zones_step as new_list_zones_step,
)
from ._generated.steps import (
    new_purge_cache_by_urls_step as new_purge_cache_by_urls_step,
)
from .connection import Connection as Connection
from .models import CachePurge as CachePurge
from .models import ListZonesInput as ListZonesInput
from .models import PurgeCacheByURLsInput as PurgeCacheByURLsInput
from .models import ZonesPage as ZonesPage

BranchTarget = TypeVar("BranchTarget")
