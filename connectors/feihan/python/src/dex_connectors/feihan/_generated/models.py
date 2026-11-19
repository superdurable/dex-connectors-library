# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# QueryRechargeOrderInput identifies an existing merchant order.
@dataclass(frozen=True, slots=True)
class QueryRechargeOrderInput:
    # MerchantOrderID is the merchant's order ID supplied during recharge creation.
    merchant_order_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'merchantOrderId'})


setattr(QueryRechargeOrderInput, '__module__', 'dex_connectors.feihan.models')


# RechargeOrderResult contains the merchant order ID and provider recharge status.
@dataclass(frozen=True, slots=True)
class RechargeOrderResult:
    # MerchantOrderID is the merchant's order ID.
    merchant_order_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'merchantOrderId'})
    # RechargeStatus is 0 processing, 1 successful, or 9 failed.
    recharge_status: int = _dataclass_field(default=0, metadata={'dex_json_name': 'rechargeStatus'})


setattr(RechargeOrderResult, '__module__', 'dex_connectors.feihan.models')
