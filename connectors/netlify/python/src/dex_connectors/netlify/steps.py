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
    new_list_deploys_step as new_list_deploys_step,
)
from ._generated.steps import (
    new_list_sites_step as new_list_sites_step,
)
from .connection import Connection as Connection
from .models import DeploysPage as DeploysPage
from .models import ListDeploysInput as ListDeploysInput
from .models import ListSitesInput as ListSitesInput
from .models import SitesPage as SitesPage

BranchTarget = TypeVar("BranchTarget")

new_list_sites_step.__doc__ = "Create the typed site page Step; unwired optional outcomes fail the Flow."
new_list_deploys_step.__doc__ = "Create the typed deploy page Step; unwired optional outcomes fail the Flow."
