# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetPaymentInput selects a payment by its non-secret Razorpay ID.
@dataclass(frozen=True, slots=True)
class GetPaymentInput:
    # PaymentID is a Razorpay payment ID prefixed pay_.
    payment_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'paymentId'})


setattr(GetPaymentInput, '__module__', 'dex_connectors.razorpay.models')


# PaymentStatus excludes customer identities and payment details.
@dataclass(frozen=True, slots=True)
class PaymentStatus:
    # ID is the Razorpay payment ID.
    id: str = ''
    # Status is a Razorpay payment state such as captured.
    status: str = ''
    # Amount is the positive payment amount in currency subunits.
    amount: int = 0
    # CurrencyCode is the payment's ISO 4217 currency.
    currency_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'currencyCode'})


setattr(PaymentStatus, '__module__', 'dex_connectors.razorpay.models')
