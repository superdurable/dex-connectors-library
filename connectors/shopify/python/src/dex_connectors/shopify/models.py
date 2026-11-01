# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from ._generated.models import (
    GetOrderInput as GetOrderInput,
)
from ._generated.models import (
    ListOrdersInput as ListOrdersInput,
)
from ._generated.models import (
    Order as Order,
)
from ._generated.models import OrderPage as OrderPage

OrderPage.__doc__ = "One page of orders."
