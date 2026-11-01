# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

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
from ._generated.models import PaymentPage as PaymentPage

PaymentPage.__doc__ = "One page of payments; cursor is blank on the last page."

ListPaymentsInput.__doc__ = "Cursor from the previous page; blank reads the first page."
GetPaymentInput.__doc__ = "Square payment ID."
Money.__doc__ = "Amount in the currency's smallest unit, such as cents, and its ISO 4217 code."
Payment.__doc__ = "Square payment identity, status such as COMPLETED, and amount."
