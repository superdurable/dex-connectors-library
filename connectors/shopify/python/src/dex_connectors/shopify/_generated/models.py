# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# ListOrdersInput selects one bounded page of Shopify orders.
@dataclass(frozen=True, slots=True)
class ListOrdersInput:
    # Limit bounds one page from 1 through 250; zero uses Shopify default.
    limit: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(ListOrdersInput, '__module__', 'dex_connectors.shopify.models')


# Order is a minimal Shopify order summary; protected customer data is omitted.
@dataclass(frozen=True, slots=True)
class Order:
    # ID is the numeric Shopify order ID.
    id: int = 0
    # Name is the human-readable order number.
    name: str = ''
    # FinancialStatus is the payment state.
    financial_status: str = ''
    # TotalPrice is the decimal order total.
    total_price: str = ''
    # Currency is the ISO currency code.
    currency: str = ''


setattr(Order, '__module__', 'dex_connectors.shopify.models')


# OrderPage contains one Shopify order page.
@dataclass(frozen=True, slots=True)
class OrderPage:
    # Orders contains the returned orders, possibly none.
    orders: tuple[Order, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'orders', _replace_nil_slices(self.orders, 1))


OrderPage.__annotations__['orders'] = tuple[Order, ...] | None
OrderPage.__dataclass_fields__['orders'].type = tuple[Order, ...] | None
setattr(OrderPage, '__module__', 'dex_connectors.shopify.models')


# GetOrderInput identifies one Shopify order.
@dataclass(frozen=True, slots=True)
class GetOrderInput:
    # OrderID is the numeric Shopify order ID.
    order_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'orderId'})


setattr(GetOrderInput, '__module__', 'dex_connectors.shopify.models')
