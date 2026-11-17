# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# QueryOrderStatusInput identifies a third-party delivery order.
@dataclass(frozen=True, slots=True)
class QueryOrderStatusInput:
    # OrderID is the third-party order_id supplied when creating the delivery order.
    order_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'orderID'})


setattr(QueryOrderStatusInput, '__module__', 'dex_connectors.dada.models')


# OrderStatus contains the current delivery status without rider personal data.
@dataclass(frozen=True, slots=True)
class OrderStatus:
    # OrderID is the third-party order identifier returned by Dada.
    order_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'orderID'})
    # StatusCode is Dada's current delivery state code.
    status_code: int = _dataclass_field(default=0, metadata={'dex_json_name': 'statusCode'})
    # StatusMessage is Dada's human-readable status label.
    status_message: str = _dataclass_field(default='', metadata={'dex_json_name': 'statusMessage'})


setattr(OrderStatus, '__module__', 'dex_connectors.dada.models')
