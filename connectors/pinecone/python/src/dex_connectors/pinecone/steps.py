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
    new_query_vectors_step as new_query_vectors_step,
)
from ._generated.steps import (
    new_upsert_vectors_step as new_upsert_vectors_step,
)
from .connection import Connection as Connection
from .models import QueryVectorsInput as QueryVectorsInput
from .models import QueryVectorsOutput as QueryVectorsOutput
from .models import UpsertVectorsInput as UpsertVectorsInput
from .models import UpsertVectorsOutput as UpsertVectorsOutput

BranchTarget = TypeVar("BranchTarget")

new_upsert_vectors_step.__doc__ = (
    "Create the typed vector upsert Step; unwired optional outcomes fail the Flow."
)
new_query_vectors_step.__doc__ = (
    "Create the typed vector query Step; unwired optional outcomes fail the Flow."
)
