# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed Square payment pages and payments."""

from dataclasses import dataclass, field


@dataclass(frozen=True, slots=True)
class ListPaymentsInput:
    """Cursor from the previous page; blank reads the first page."""

    cursor: str = field(default="", metadata={"dex_json_omit_default": True})


@dataclass(frozen=True, slots=True)
class GetPaymentInput:
    """Square payment ID."""

    payment_id: str = field(default="", metadata={"dex_json_name": "paymentId"})


@dataclass(frozen=True, slots=True)
class Money:
    """Amount in the currency's smallest unit, such as cents, and its ISO 4217 code."""

    amount: int = 0
    currency: str = ""


@dataclass(frozen=True, slots=True)
class Payment:
    """Square payment identity, status such as COMPLETED, and amount."""

    id: str = ""
    status: str = ""
    amount_money: Money = field(default_factory=Money)


@dataclass(frozen=True, slots=True)
class PaymentPage:
    """One page of payments; cursor is blank on the last page."""

    payments: tuple[Payment, ...] = ()
    cursor: str = ""
