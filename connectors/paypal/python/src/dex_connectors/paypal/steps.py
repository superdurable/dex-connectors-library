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
    new_get_order_step as new_get_order_step,
)
from ._generated.steps import (
    new_list_transactions_step as new_list_transactions_step,
)
from .connection import Connection as Connection
from .models import GetOrderInput as GetOrderInput
from .models import ListTransactionsInput as ListTransactionsInput
from .models import Order as Order
from .models import TransactionPage as TransactionPage

BranchTarget = TypeVar("BranchTarget")
