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
    new_get_issue_step as new_get_issue_step,
)
from ._generated.steps import (
    new_list_issues_step as new_list_issues_step,
)
from ._generated.steps import (
    new_update_issue_status_step as new_update_issue_status_step,
)
from .connection import Connection as Connection
from .models import GetIssueInput as GetIssueInput
from .models import Issue as Issue
from .models import IssuePage as IssuePage
from .models import ListIssuesInput as ListIssuesInput
from .models import UpdateIssueStatusInput as UpdateIssueStatusInput

BranchTarget = TypeVar("BranchTarget")

new_list_issues_step.__doc__ = "Create the typed issue search Step; unwired optional outcomes fail the Flow."
new_get_issue_step.__doc__ = "Create the typed issue lookup Step; unwired optional outcomes fail the Flow."
new_update_issue_status_step.__doc__ = (
    "Create the typed issue status update Step; unwired optional outcomes fail the Flow."
)
