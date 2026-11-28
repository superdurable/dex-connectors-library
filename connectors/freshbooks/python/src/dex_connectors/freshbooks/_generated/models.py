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


# GetInvoiceInput identifies an invoice within an accounting account.
@dataclass(frozen=True, slots=True)
class GetInvoiceInput:
    # AccountID is business_memberships[].business.account_id from FreshBooks /me.
    account_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'accountId'})
    # InvoiceID is the positive numeric ID shown by the invoice API or webhook.
    invoice_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'invoiceId'})


setattr(GetInvoiceInput, '__module__', 'dex_connectors.freshbooks.models')


# Invoice contains selected financial fields from one FreshBooks invoice.
@dataclass(frozen=True, slots=True)
class Invoice:
    # AccountID identifies the accounting account.
    account_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'accountId'})
    # InvoiceID is the stable numeric invoice ID.
    invoice_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'invoiceId'})
    # InvoiceNumber is the displayed document number.
    invoice_number: str = _dataclass_field(default='', metadata={'dex_json_name': 'invoiceNumber'})
    # Amount is the exact decimal total as a string.
    amount: str = ''
    # CurrencyCode is the ISO currency code returned with Amount.
    currency_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'currencyCode'})
    # PaymentStatus is FreshBooks' current payment status.
    payment_status: str = _dataclass_field(default='', metadata={'dex_json_name': 'paymentStatus'})


setattr(Invoice, '__module__', 'dex_connectors.freshbooks.models')


# ListInvoicesInput selects a one-based invoice page; zero selects page one.
@dataclass(frozen=True, slots=True)
class ListInvoicesInput:
    account_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'accountId'})
    page: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(ListInvoicesInput, '__module__', 'dex_connectors.freshbooks.models')


# InvoicePage contains one page of invoices without client contact details.
@dataclass(frozen=True, slots=True)
class InvoicePage:
    account_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'accountId'})
    invoices: tuple[Invoice, ...] = ()
    page: int = 0
    page_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageCount'})
    total: int = 0

    def __post_init__(self) -> None:
        object.__setattr__(self, 'invoices', _replace_nil_slices(self.invoices, 1))


InvoicePage.__annotations__['invoices'] = tuple[Invoice, ...] | None
InvoicePage.__dataclass_fields__['invoices'].type = tuple[Invoice, ...] | None
setattr(InvoicePage, '__module__', 'dex_connectors.freshbooks.models')
