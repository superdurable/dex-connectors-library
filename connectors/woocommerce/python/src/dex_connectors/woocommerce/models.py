# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from dataclasses import dataclass

from ._generated.models import (
    GetOrderInput as GetOrderInput,
)
from ._generated.models import (
    ListOrdersInput as ListOrdersInput,
)
from ._generated.models import (
    Order as Order,
)


@dataclass(frozen=True, slots=True)
class OrderPage:
    """One page of orders."""

    orders: tuple[Order, ...] = ()
