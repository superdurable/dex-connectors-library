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

GetOrderInput.__doc__ = "Positive Shopify order ID."
ListOrdersInput.__doc__ = "Orders per page; zero uses Shopify's default, at most 250."
Order.__doc__ = (
    "Order identity, display name such as #101, financial status, decimal total text, and currency."
)
