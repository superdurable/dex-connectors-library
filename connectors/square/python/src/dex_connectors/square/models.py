# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from dataclasses import dataclass

from ._generated.models import (
    GetPaymentInput as GetPaymentInput,
)
from ._generated.models import (
    ListPaymentsInput as ListPaymentsInput,
)
from ._generated.models import (
    Money as Money,
)
from ._generated.models import (
    Payment as Payment,
)


@dataclass(frozen=True, slots=True)
class PaymentPage:
    """One page of payments; cursor is blank on the last page."""

    payments: tuple[Payment, ...] = ()
    cursor: str = ""
