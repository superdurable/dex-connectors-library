# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

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
