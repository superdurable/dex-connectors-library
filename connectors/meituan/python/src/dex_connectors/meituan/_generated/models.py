# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# QueryOrderStatusInput identifies one order from the last 90 days.
@dataclass(frozen=True, slots=True)
class QueryOrderStatusInput:
    # DeliveryID is the positive delivery activity ID used when the order was created.
    delivery_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'deliveryId'})
    # MeituanOrderID is the provider's order ID, at most 32 characters.
    meituan_order_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'meituanOrderId'})


setattr(QueryOrderStatusInput, '__module__', 'dex_connectors.meituan.models')


# OrderStatusResult contains only the delivery status and associated identifiers.
@dataclass(frozen=True, slots=True)
class OrderStatusResult:
    # DeliveryID is the delivery activity ID.
    delivery_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'deliveryId'})
    # MeituanOrderID is the provider's order ID.
    meituan_order_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'meituanOrderId'})
    # MerchantOrderID is the merchant's order ID.
    merchant_order_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'merchantOrderId'})
    # Status is 0 pending, 20 accepted, 30 picked up, 50 delivered, or 99 canceled.
    status: int = 0
    # OperatedAt is the provider's Unix timestamp for the latest status change.
    operated_at: int = _dataclass_field(default=0, metadata={'dex_json_name': 'operatedAt'})


setattr(OrderStatusResult, '__module__', 'dex_connectors.meituan.models')
