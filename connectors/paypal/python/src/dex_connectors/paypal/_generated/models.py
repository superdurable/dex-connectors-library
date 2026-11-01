# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated Python dataclasses from Go operation types."""

from dataclasses import dataclass, field as _dataclass_field

@dataclass(frozen=True, slots=True)
class GetOrderInput:
    """Typed GetOrderInput value from the Go connector contract."""

    order_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'orderId'})


setattr(GetOrderInput, '__module__', 'dex_connectors.paypal.models')


@dataclass(frozen=True, slots=True)
class Order:
    """Typed Order value from the Go connector contract."""

    id: str = ''
    status: str = ''


setattr(Order, '__module__', 'dex_connectors.paypal.models')


@dataclass(frozen=True, slots=True)
class ListTransactionsInput:
    """Typed ListTransactionsInput value from the Go connector contract."""

    start_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'startDate'})
    end_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'endDate'})
    page: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(ListTransactionsInput, '__module__', 'dex_connectors.paypal.models')


@dataclass(frozen=True, slots=True)
class TransactionAmount:
    """Typed TransactionAmount value from the Go connector contract."""

    currency_code: str = ''
    value: str = ''


setattr(TransactionAmount, '__module__', 'dex_connectors.paypal.models')


@dataclass(frozen=True, slots=True)
class TransactionInfo:
    """Typed TransactionInfo value from the Go connector contract."""

    transaction_id: str = ''
    transaction_status: str = ''
    transaction_amount: TransactionAmount = _dataclass_field(default_factory=TransactionAmount)


setattr(TransactionInfo, '__module__', 'dex_connectors.paypal.models')


@dataclass(frozen=True, slots=True)
class TransactionDetail:
    """Typed TransactionDetail value from the Go connector contract."""

    transaction_info: TransactionInfo = _dataclass_field(default_factory=TransactionInfo)


setattr(TransactionDetail, '__module__', 'dex_connectors.paypal.models')


@dataclass(frozen=True, slots=True)
class TransactionPage:
    """Typed TransactionPage value from the Go connector contract."""

    transaction_details: tuple[TransactionDetail, ...] | None = None
    page: int = 0
    total_pages: int = 0


setattr(TransactionPage, '__module__', 'dex_connectors.paypal.models')
