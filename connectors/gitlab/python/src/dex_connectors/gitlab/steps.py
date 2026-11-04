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
    new_get_project_step as new_get_project_step,
)
from ._generated.steps import (
    new_list_issues_step as new_list_issues_step,
)
from ._generated.steps import (
    new_update_issue_labels_step as new_update_issue_labels_step,
)
from .connection import Connection as Connection
from .models import GetProjectInput as GetProjectInput
from .models import Issue as Issue
from .models import IssuesPage as IssuesPage
from .models import ListIssuesInput as ListIssuesInput
from .models import Project as Project
from .models import UpdateIssueLabelsInput as UpdateIssueLabelsInput

BranchTarget = TypeVar("BranchTarget")

new_get_project_step.__doc__ = (
    "Create the typed project lookup Step; unwired optional outcomes fail the Flow."
)
new_list_issues_step.__doc__ = "Create the typed issue page Step; unwired optional outcomes fail the Flow."
new_update_issue_labels_step.__doc__ = (
    "Create the typed issue label replacement Step; unwired optional outcomes fail the Flow."
)
