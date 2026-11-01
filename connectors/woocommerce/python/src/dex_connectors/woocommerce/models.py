# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed WooCommerce order pages and orders."""

from dataclasses import dataclass, field


@dataclass(frozen=True, slots=True)
class ListOrdersInput:
    """Order page; zero page or per_page uses WooCommerce's default, per_page at most 100."""

    page: int = field(default=0, metadata={"dex_json_omit_default": True})
    per_page: int = field(default=0, metadata={"dex_json_name": "perPage", "dex_json_omit_default": True})


@dataclass(frozen=True, slots=True)
class GetOrderInput:
    """Positive WooCommerce order ID."""

    order_id: int = field(default=0, metadata={"dex_json_name": "orderId"})


@dataclass(frozen=True, slots=True)
class Order:
    """Order identity, status such as processing, decimal total text, and currency code."""

    id: int = 0
    status: str = ""
    total: str = ""
    currency: str = ""


@dataclass(frozen=True, slots=True)
class OrderPage:
    """One page of orders."""

    orders: tuple[Order, ...] = ()
