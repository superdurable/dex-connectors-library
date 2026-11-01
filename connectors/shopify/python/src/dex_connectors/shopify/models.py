# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed Shopify order pages and orders."""

from dataclasses import dataclass, field


@dataclass(frozen=True, slots=True)
class GetOrderInput:
    """Positive Shopify order ID."""

    order_id: int = field(default=0, metadata={"dex_json_name": "orderId"})


@dataclass(frozen=True, slots=True)
class ListOrdersInput:
    """Orders per page; zero uses Shopify's default, at most 250."""

    limit: int = field(default=0, metadata={"dex_json_omit_default": True})


@dataclass(frozen=True, slots=True)
class Order:
    """Order identity, display name such as #101, financial status, decimal total text, and currency."""

    id: int = 0
    name: str = ""
    financial_status: str = ""
    total_price: str = ""
    currency: str = ""


@dataclass(frozen=True, slots=True)
class OrderPage:
    """One page of orders."""

    orders: tuple[Order, ...] = ()
