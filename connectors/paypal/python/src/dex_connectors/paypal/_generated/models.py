# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return value
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# ListTransactionsInput selects a PayPal transaction window and page.
@dataclass(frozen=True, slots=True)
class ListTransactionsInput:
    # StartDate is the inclusive RFC 3339 window start.
    start_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'startDate'})
    # EndDate is the inclusive RFC 3339 window end.
    end_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'endDate'})
    # Page is the one-based page number; zero selects page one.
    page: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(ListTransactionsInput, '__module__', 'dex_connectors.paypal.models')


# TransactionAmount is the decimal amount and ISO currency.
@dataclass(frozen=True, slots=True)
class TransactionAmount:
    # CurrencyCode is the ISO currency code.
    currency_code: str = ''
    # Value is the decimal amount.
    value: str = ''


setattr(TransactionAmount, '__module__', 'dex_connectors.paypal.models')


# TransactionInfo is the transaction ID, status, and amount.
@dataclass(frozen=True, slots=True)
class TransactionInfo:
    # TransactionID is the PayPal transaction ID.
    transaction_id: str = ''
    # TransactionStatus is the PayPal status code.
    transaction_status: str = ''
    # TransactionAmount is the transaction amount.
    transaction_amount: TransactionAmount = _dataclass_field(default_factory=TransactionAmount)


setattr(TransactionInfo, '__module__', 'dex_connectors.paypal.models')


# TransactionDetail contains the non-secret transaction summary.
@dataclass(frozen=True, slots=True)
class TransactionDetail:
    # TransactionInfo contains the provider transaction summary.
    transaction_info: TransactionInfo = _dataclass_field(default_factory=TransactionInfo)


setattr(TransactionDetail, '__module__', 'dex_connectors.paypal.models')


# TransactionPage is one PayPal transaction search page.
@dataclass(frozen=True, slots=True)
class TransactionPage:
    # TransactionDetails contains transactions, possibly none.
    transaction_details: tuple[TransactionDetail, ...] = ()
    # Page is the one-based page number.
    page: int = 0
    # TotalPages is the provider's count of pages.
    total_pages: int = 0

    def __post_init__(self) -> None:
        object.__setattr__(self, 'transaction_details', _replace_nil_slices(self.transaction_details, 1))


TransactionPage.__annotations__['transaction_details'] = tuple[TransactionDetail, ...] | None
setattr(TransactionPage, '__module__', 'dex_connectors.paypal.models')


# GetOrderInput identifies one PayPal checkout order.
@dataclass(frozen=True, slots=True)
class GetOrderInput:
    # OrderID is the PayPal checkout order ID.
    order_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'orderId'})


setattr(GetOrderInput, '__module__', 'dex_connectors.paypal.models')


# Order is a minimal PayPal checkout order summary.
@dataclass(frozen=True, slots=True)
class Order:
    # ID is the PayPal order ID.
    id: str = ''
    # Status is the PayPal order state.
    status: str = ''


setattr(Order, '__module__', 'dex_connectors.paypal.models')
