# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated Python dataclasses from Go operation types."""

from dataclasses import dataclass, field as _dataclass_field

@dataclass(frozen=True, slots=True)
class ListOrdersInput:
    """Typed ListOrdersInput value from the Go connector contract."""

    page: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    per_page: int = _dataclass_field(default=0, metadata={'dex_json_name': 'perPage', 'dex_json_omit_default': True})


setattr(ListOrdersInput, '__module__', 'dex_connectors.woocommerce.models')


@dataclass(frozen=True, slots=True)
class Order:
    """Typed Order value from the Go connector contract."""

    id: int = 0
    status: str = ''
    total: str = ''
    currency: str = ''


setattr(Order, '__module__', 'dex_connectors.woocommerce.models')


@dataclass(frozen=True, slots=True)
class OrderPage:
    """Typed OrderPage value from the Go connector contract."""

    orders: tuple[Order, ...] | None = None


setattr(OrderPage, '__module__', 'dex_connectors.woocommerce.models')


@dataclass(frozen=True, slots=True)
class GetOrderInput:
    """Typed GetOrderInput value from the Go connector contract."""

    order_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'orderId'})


setattr(GetOrderInput, '__module__', 'dex_connectors.woocommerce.models')
