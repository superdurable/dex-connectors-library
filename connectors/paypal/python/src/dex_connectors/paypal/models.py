# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed PayPal checkout orders and transaction search pages."""

from dataclasses import dataclass, field


@dataclass(frozen=True, slots=True)
class GetOrderInput:
    """The PayPal checkout order ID."""

    order_id: str = field(default="", metadata={"dex_json_name": "orderId"})


@dataclass(frozen=True, slots=True)
class ListTransactionsInput:
    """Inclusive RFC 3339 window of at most 31 days and a one-based page (zero selects page one)."""

    start_date: str = field(default="", metadata={"dex_json_name": "startDate"})
    end_date: str = field(default="", metadata={"dex_json_name": "endDate"})
    page: int = field(default=0, metadata={"dex_json_omit_default": True})


@dataclass(frozen=True, slots=True)
class Order:
    """A minimal checkout order summary: its ID and PayPal state."""

    id: str = ""
    status: str = ""


@dataclass(frozen=True, slots=True)
class TransactionAmount:
    """A decimal amount and its ISO currency code."""

    currency_code: str = ""
    value: str = ""


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
