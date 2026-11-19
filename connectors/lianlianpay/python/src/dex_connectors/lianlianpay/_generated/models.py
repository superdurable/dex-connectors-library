# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# QueryPaymentStatusInput identifies a payment order owned by the authorized merchant.
@dataclass(frozen=True, slots=True)
class QueryPaymentStatusInput:
    # MerchantOrderID is the merchant's unique order number, at most 32 characters.
    merchant_order_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'merchantOrderId'})


setattr(QueryPaymentStatusInput, '__module__', 'dex_connectors.lianlianpay.models')


# PaymentStatusResult contains the merchant order number and processing status only.
@dataclass(frozen=True, slots=True)
class PaymentStatusResult:
    # MerchantOrderID is the merchant's original order number.
    merchant_order_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'merchantOrderId'})
    # Status is SUCCESS, FAILURE, PROCESSING, CLOSED, or CANCEL.
    status: str = ''


setattr(PaymentStatusResult, '__module__', 'dex_connectors.lianlianpay.models')
