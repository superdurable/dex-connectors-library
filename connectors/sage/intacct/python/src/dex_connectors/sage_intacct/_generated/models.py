# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetInvoiceInput identifies an accounts receivable invoice by its system-assigned key.
@dataclass(frozen=True, slots=True)
class GetInvoiceInput:
    # InvoiceKey is the key returned by the Sage Intacct invoice REST endpoint.
    invoice_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'invoiceKey'})


setattr(GetInvoiceInput, '__module__', 'dex_connectors.sage_intacct.models')


# InvoiceSummary contains the stable system key and displayed invoice ID.
@dataclass(frozen=True, slots=True)
class InvoiceSummary:
    # Key is the system-assigned invoice key.
    key: str = ''
    # ID is the invoice's displayed document ID.
    id: str = ''


setattr(InvoiceSummary, '__module__', 'dex_connectors.sage_intacct.models')
