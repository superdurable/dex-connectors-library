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


# ListInvoicesInput requests the first page of Accounts Receivable invoices.
@dataclass(frozen=True, slots=True)
class ListInvoicesInput:
    pass


setattr(ListInvoicesInput, '__module__', 'dex_connectors.sage_intacct.models')


# InvoiceList contains the keys and displayed IDs in one provider page.
@dataclass(frozen=True, slots=True)
class InvoiceList:
    # Invoices contains the invoice identities exposed by Sage Intacct.
    invoices: tuple[InvoiceSummary, ...] = ()
    # Next is Sage Intacct's opaque link to the next page, or blank at the end.
    next: str = ''

    def __post_init__(self) -> None:
        object.__setattr__(self, 'invoices', _replace_nil_slices(self.invoices, 1))


InvoiceList.__annotations__['invoices'] = tuple[InvoiceSummary, ...] | None
InvoiceList.__dataclass_fields__['invoices'].type = tuple[InvoiceSummary, ...] | None
setattr(InvoiceList, '__module__', 'dex_connectors.sage_intacct.models')
