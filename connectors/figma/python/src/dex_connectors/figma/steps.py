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
    new_get_file_comments_step as new_get_file_comments_step,
)
from ._generated.steps import (
    new_get_file_step as new_get_file_step,
)
from .connection import Connection as Connection
from .models import File as File
from .models import FileComments as FileComments
from .models import GetFileCommentsInput as GetFileCommentsInput
from .models import GetFileInput as GetFileInput

BranchTarget = TypeVar("BranchTarget")

new_get_file_step.__doc__ = "Create the typed file Step; unwired optional outcomes fail the Flow."
new_get_file_comments_step.__doc__ = (
    "Create the typed file comments Step; unwired optional outcomes fail the Flow."
)
