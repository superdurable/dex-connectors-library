# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed PayPal checkout orders and transaction search pages."""

from dataclasses import dataclass

from ._generated.models import GetOrderInput as GetOrderInput
from ._generated.models import ListTransactionsInput as ListTransactionsInput
from ._generated.models import Order as Order
from ._generated.models import TransactionAmount as TransactionAmount


@dataclass(frozen=True, slots=True)
class TransactionInfo:
    """A transaction's PayPal ID, status code, and amount."""

    transaction_id: str = ""
    transaction_status: str = ""
    transaction_amount: TransactionAmount = TransactionAmount()


@dataclass(frozen=True, slots=True)
class TransactionDetail:
    """The non-secret transaction summary."""

    transaction_info: TransactionInfo = TransactionInfo()


@dataclass(frozen=True, slots=True)
class TransactionPage:
    """One transaction search page, possibly empty, with its one-based number and the page count."""

    transaction_details: tuple[TransactionDetail, ...] = ()
    page: int = 0
    total_pages: int = 0


GetOrderInput.__doc__ = "The PayPal checkout order ID."
ListTransactionsInput.__doc__ = (
    "Inclusive RFC 3339 window of at most 31 days and a one-based page (zero selects page one)."
)
Order.__doc__ = "A minimal checkout order summary: its ID and PayPal state."
TransactionAmount.__doc__ = "A decimal amount and its ISO currency code."
