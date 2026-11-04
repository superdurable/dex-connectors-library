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
    new_get_collection_item_step as new_get_collection_item_step,
)
from ._generated.steps import (
    new_list_collection_items_step as new_list_collection_items_step,
)
from .connection import Connection as Connection
from .models import GetCollectionItemInput as GetCollectionItemInput
from .models import GetCollectionItemOutput as GetCollectionItemOutput
from .models import ListCollectionItemsInput as ListCollectionItemsInput
from .models import ListCollectionItemsOutput as ListCollectionItemsOutput

BranchTarget = TypeVar("BranchTarget")

new_list_collection_items_step.__doc__ = (
    "Create the typed listCollectionItems Step; unwired optional outcomes fail the Flow."
)
new_get_collection_item_step.__doc__ = (
    "Create the typed getCollectionItem Step; unwired optional outcomes fail the Flow."
)
