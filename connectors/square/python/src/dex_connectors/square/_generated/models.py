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


# ListPaymentsInput selects a Square payment page.
@dataclass(frozen=True, slots=True)
class ListPaymentsInput:
    # Cursor continues a previous page; blank starts at the first page.
    cursor: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(ListPaymentsInput, '__module__', 'dex_connectors.square.models')


# Money is a Square integer amount in the currency's smallest denomination.
@dataclass(frozen=True, slots=True)
class Money:
    # Amount is an integer count of the currency's smallest unit.
    amount: int = 0
    # Currency is the ISO currency code.
    currency: str = ''


setattr(Money, '__module__', 'dex_connectors.square.models')


# Payment is a minimal Square payment summary.
@dataclass(frozen=True, slots=True)
class Payment:
    # ID is the Square payment ID.
    id: str = ''
    # Status is the provider payment status.
    status: str = ''
    # AmountMoney is the charged amount.
    amount_money: Money = _dataclass_field(default_factory=Money)


setattr(Payment, '__module__', 'dex_connectors.square.models')


# PaymentPage is one Square payment page.
@dataclass(frozen=True, slots=True)
class PaymentPage:
    # Payments contains payments, possibly none.
    payments: tuple[Payment, ...] = ()
    # Cursor continues listing when set.
    cursor: str = ''

    def __post_init__(self) -> None:
        object.__setattr__(self, 'payments', _replace_nil_slices(self.payments, 1))


PaymentPage.__annotations__['payments'] = tuple[Payment, ...] | None
PaymentPage.__dataclass_fields__['payments'].type = tuple[Payment, ...] | None
setattr(PaymentPage, '__module__', 'dex_connectors.square.models')


# GetPaymentInput identifies one Square payment.
@dataclass(frozen=True, slots=True)
class GetPaymentInput:
    # PaymentID is the Square payment ID.
    payment_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'paymentId'})


setattr(GetPaymentInput, '__module__', 'dex_connectors.square.models')
