# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# ListOrdersInput selects one WooCommerce order page.
@dataclass(frozen=True, slots=True)
class ListOrdersInput:
    # Page is one-based; zero uses the provider default.
    page: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # PerPage bounds one page to 100; zero uses the provider default.
    per_page: int = _dataclass_field(default=0, metadata={'dex_json_name': 'perPage', 'dex_json_omit_default': True})


setattr(ListOrdersInput, '__module__', 'dex_connectors.woocommerce.models')


# Order is a minimal WooCommerce order summary without customer data.
@dataclass(frozen=True, slots=True)
class Order:
    # ID is the numeric WooCommerce order ID.
    id: int = 0
    # Status is the provider order status.
    status: str = ''
    # Total is the decimal order total.
    total: str = ''
    # Currency is the ISO currency code.
    currency: str = ''


setattr(Order, '__module__', 'dex_connectors.woocommerce.models')


# OrderPage contains one WooCommerce order page.
@dataclass(frozen=True, slots=True)
class OrderPage:
    # Orders contains the returned orders, possibly none.
    orders: tuple[Order, ...] | None = None


setattr(OrderPage, '__module__', 'dex_connectors.woocommerce.models')


# GetOrderInput identifies one WooCommerce order.
@dataclass(frozen=True, slots=True)
class GetOrderInput:
    # OrderID is the numeric WooCommerce order ID.
    order_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'orderId'})


setattr(GetOrderInput, '__module__', 'dex_connectors.woocommerce.models')
