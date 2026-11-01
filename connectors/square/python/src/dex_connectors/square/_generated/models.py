# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated Python dataclasses from Go operation types."""

from dataclasses import dataclass, field as _dataclass_field

@dataclass(frozen=True, slots=True)
class ListPaymentsInput:
    """Typed ListPaymentsInput value from the Go connector contract."""

    cursor: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(ListPaymentsInput, '__module__', 'dex_connectors.square.models')


@dataclass(frozen=True, slots=True)
class Money:
    """Typed Money value from the Go connector contract."""

    amount: int = 0
    currency: str = ''


setattr(Money, '__module__', 'dex_connectors.square.models')


@dataclass(frozen=True, slots=True)
class Payment:
    """Typed Payment value from the Go connector contract."""

    id: str = ''
    status: str = ''
    amount_money: Money = _dataclass_field(default_factory=Money)


setattr(Payment, '__module__', 'dex_connectors.square.models')


@dataclass(frozen=True, slots=True)
class PaymentPage:
    """Typed PaymentPage value from the Go connector contract."""

    payments: tuple[Payment, ...] | None = None
    cursor: str = ''


setattr(PaymentPage, '__module__', 'dex_connectors.square.models')


@dataclass(frozen=True, slots=True)
class GetPaymentInput:
    """Typed GetPaymentInput value from the Go connector contract."""

    payment_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'paymentId'})


setattr(GetPaymentInput, '__module__', 'dex_connectors.square.models')
