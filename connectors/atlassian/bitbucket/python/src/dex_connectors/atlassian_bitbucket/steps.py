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
    new_list_pull_requests_step as new_list_pull_requests_step,
)
from ._generated.steps import (
    new_list_repositories_step as new_list_repositories_step,
)
from .connection import Connection as Connection
from .models import ListPullRequestsInput as ListPullRequestsInput
from .models import ListRepositoriesInput as ListRepositoriesInput
from .models import PullRequestPage as PullRequestPage
from .models import RepositoryPage as RepositoryPage

BranchTarget = TypeVar("BranchTarget")

new_list_repositories_step.__doc__ = (
    "Create the typed repository page Step; unwired optional outcomes fail the Flow."
)
new_list_pull_requests_step.__doc__ = (
    "Create the typed pull request page Step; unwired optional outcomes fail the Flow."
)
