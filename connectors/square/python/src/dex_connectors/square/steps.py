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
    new_get_payment_step as new_get_payment_step,
)
from ._generated.steps import (
    new_list_payments_step as new_list_payments_step,
)
from .connection import Connection as Connection
from .models import GetPaymentInput as GetPaymentInput
from .models import ListPaymentsInput as ListPaymentsInput
from .models import Payment as Payment
from .models import PaymentPage as PaymentPage

BranchTarget = TypeVar("BranchTarget")
