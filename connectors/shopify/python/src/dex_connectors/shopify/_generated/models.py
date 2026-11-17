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
    # PageInfo is the opaque cursor returned by a previous listOrders page.
    page_info: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageInfo', 'dex_json_omit_default': True})
    # Status filters open, closed, cancelled, or any orders; blank means any.
    status: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # FinancialStatus filters by Shopify's order financial status; blank omits the filter.
    financial_status: str = _dataclass_field(default='', metadata={'dex_json_name': 'financialStatus', 'dex_json_omit_default': True})
    # FulfillmentStatus filters by Shopify's order fulfillment status; blank omits the filter.
    fulfillment_status: str = _dataclass_field(default='', metadata={'dex_json_name': 'fulfillmentStatus', 'dex_json_omit_default': True})
    # CreatedAtMin filters orders created on or after an RFC3339 timestamp.
    created_at_min: str = _dataclass_field(default='', metadata={'dex_json_name': 'createdAtMin', 'dex_json_omit_default': True})
    # CreatedAtMax filters orders created on or before an RFC3339 timestamp.
    created_at_max: str = _dataclass_field(default='', metadata={'dex_json_name': 'createdAtMax', 'dex_json_omit_default': True})
    # UpdatedAtMin filters orders updated on or after an RFC3339 timestamp.
    updated_at_min: str = _dataclass_field(default='', metadata={'dex_json_name': 'updatedAtMin', 'dex_json_omit_default': True})
    # UpdatedAtMax filters orders updated on or before an RFC3339 timestamp.
    updated_at_max: str = _dataclass_field(default='', metadata={'dex_json_name': 'updatedAtMax', 'dex_json_omit_default': True})
    # ProcessedAtMin filters orders processed on or after an RFC3339 timestamp.
    processed_at_min: str = _dataclass_field(default='', metadata={'dex_json_name': 'processedAtMin', 'dex_json_omit_default': True})
    # ProcessedAtMax filters orders processed on or before an RFC3339 timestamp.
    processed_at_max: str = _dataclass_field(default='', metadata={'dex_json_name': 'processedAtMax', 'dex_json_omit_default': True})
    # SinceID selects orders after a numeric Shopify order ID.
    since_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'sinceId', 'dex_json_omit_default': True})
    # IDs selects a comma-separated list of numeric Shopify order IDs.
    ids: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # AttributionAppID filters orders attributed to this numeric app ID or "current".
    attribution_app_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'attributionAppId', 'dex_json_omit_default': True})


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
    # NextPageInfo is the opaque cursor for the next page; blank means no next page.
    next_page_info: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextPageInfo', 'dex_json_omit_default': True})

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
