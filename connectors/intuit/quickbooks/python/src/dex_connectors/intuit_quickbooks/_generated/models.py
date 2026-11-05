# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from datetime import UTC, datetime
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# FindCustomerInput identifies a customer by exactly one of its primary email address or display name.
@dataclass(frozen=True, slots=True)
class FindCustomerInput:
    # EmailAddress is one bare address such as jane@example.com, matched against PrimaryEmailAddr.
    # QuickBooks does not require email addresses to be unique, so several customers can match.
    email_address: str = _dataclass_field(default='', metadata={'dex_json_name': 'emailAddress', 'dex_json_omit_default': True})
    # DisplayName is the customer's exact display name, which QuickBooks keeps unique.
    display_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'displayName', 'dex_json_omit_default': True})
    # IncludesInactive also matches customers made inactive, which QuickBooks treats as deleted;
    # false matches only active customers.
    includes_inactive: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'includesInactive', 'dex_json_omit_default': True})


setattr(FindCustomerInput, '__module__', 'dex_connectors.intuit_quickbooks.models')


# Customer is one QuickBooks Online customer. Amounts are exact Decimals in the customer's currency.
@dataclass(frozen=True, slots=True)
class Customer:
    # CustomerID is QuickBooks's Id for the customer, a decimal string such as 58.
    customer_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'customerId'})
    # SyncToken is the version QuickBooks requires for an update of this customer.
    sync_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'syncToken', 'dex_json_omit_default': True})
    # DisplayName is the name QuickBooks shows; it is unique across customers, vendors, and employees.
    display_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'displayName'})
    # GivenName is the contact's first name, when set.
    given_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'givenName', 'dex_json_omit_default': True})
    # FamilyName is the contact's last name, when set.
    family_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'familyName', 'dex_json_omit_default': True})
    # CompanyName is the customer's company, when set.
    company_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'companyName', 'dex_json_omit_default': True})
    # EmailAddress is the primary email address, when set. QuickBooks does not require it to be unique.
    email_address: str = _dataclass_field(default='', metadata={'dex_json_name': 'emailAddress', 'dex_json_omit_default': True})
    # Phone is the primary phone number exactly as entered, when set.
    phone: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # IsActive is false for a customer made inactive, which QuickBooks treats as deleted.
    is_active: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isActive'})
    # Balance is the customer's open balance, when QuickBooks returns it.
    balance: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # CurrencyCode is the customer's ISO 4217 currency, when QuickBooks returns it.
    currency_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'currencyCode', 'dex_json_omit_default': True})
    # CreatedAt is when the customer was created, in UTC.
    created_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'createdAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # UpdatedAt is when the customer last changed, in UTC.
    updated_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'updatedAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})


setattr(Customer, '__module__', 'dex_connectors.intuit_quickbooks.models')


# FindCustomerOutput is the lookup result.
@dataclass(frozen=True, slots=True)
class FindCustomerOutput:
    # EmailAddress echoes the trimmed requested address.
    email_address: str = _dataclass_field(default='', metadata={'dex_json_name': 'emailAddress', 'dex_json_omit_default': True})
    # DisplayName echoes the trimmed requested display name.
    display_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'displayName', 'dex_json_omit_default': True})
    # Customer is the one matching customer on the found branch, and nil otherwise.
    customer: Customer | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Matches lists up to MaxCustomerMatches customers on the ambiguous branch.
    matches: tuple[Customer, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})


setattr(FindCustomerOutput, '__module__', 'dex_connectors.intuit_quickbooks.models')


# CreateCustomerInput describes one new customer.
@dataclass(frozen=True, slots=True)
class CreateCustomerInput:
    # DisplayName is required and must be unique across QuickBooks customers, vendors, and
    # employees; a name in use selects nameConflict.
    display_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'displayName'})
    # EmailAddress is the primary email address, such as accounts@example.com; blank leaves it unset.
    email_address: str = _dataclass_field(default='', metadata={'dex_json_name': 'emailAddress', 'dex_json_omit_default': True})
    # GivenName is the contact's first name, at most 100 characters.
    given_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'givenName', 'dex_json_omit_default': True})
    # FamilyName is the contact's last name, at most 100 characters.
    family_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'familyName', 'dex_json_omit_default': True})
    # CompanyName is the customer's company, at most 100 characters.
    company_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'companyName', 'dex_json_omit_default': True})
    # Phone is the primary phone number exactly as it should appear, at most 30 characters.
    phone: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # CurrencyCode is the customer's ISO 4217 currency when the company uses multicurrency;
    # blank uses the company's home currency. QuickBooks cannot change it later.
    currency_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'currencyCode', 'dex_json_omit_default': True})


setattr(CreateCustomerInput, '__module__', 'dex_connectors.intuit_quickbooks.models')


# CreateCustomerOutput is the created customer. On every other branch Customer is empty and
# DisplayName echoes the request, so an application can resolve it with findCustomer.
@dataclass(frozen=True, slots=True)
class CreateCustomerOutput:
    # Customer is the customer QuickBooks created or, for a replayed attempt, created earlier for this Step.
    customer: Customer = _dataclass_field(default_factory=Customer)
    # DisplayName echoes the requested display name.
    display_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'displayName'})


setattr(CreateCustomerOutput, '__module__', 'dex_connectors.intuit_quickbooks.models')


# InvoiceLineInput is one item line of a new invoice.
@dataclass(frozen=True, slots=True)
class InvoiceLineInput:
    # ItemID is the product or service item's Id, which supplies the income account.
    item_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'itemId'})
    # Description is the line text, at most 4000 characters; blank uses the item's description.
    description: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Quantity is the item quantity, up to five decimal places.
    quantity: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # UnitPrice is the unit price, up to five decimal places.
    unit_price: str = _dataclass_field(default='', metadata={'dex_json_name': 'unitPrice', 'dex_json_omit_default': True})
    # Amount is the line amount with at most two decimal places. Blank computes Quantity times
    # UnitPrice exactly, rounded half away from zero to two places; both are then required.
    amount: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # TaxCodeID is the line's tax code, such as TAX or NON in a US company; blank uses the item's.
    tax_code_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'taxCodeId', 'dex_json_omit_default': True})
    # ServiceDate is the date of service, YYYY-MM-DD; blank leaves it unset.
    service_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'serviceDate', 'dex_json_omit_default': True})


setattr(InvoiceLineInput, '__module__', 'dex_connectors.intuit_quickbooks.models')


# CreateInvoiceInput describes one new invoice. Amounts are exact Decimals in the customer's currency.
@dataclass(frozen=True, slots=True)
class CreateInvoiceInput:
    # CustomerID is the invoiced customer's Id, such as from findCustomer or createCustomer.
    customer_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'customerId'})
    # Lines are 1 to 100 item lines.
    lines: tuple[InvoiceLineInput, ...] = ()
    # TxnDate is the invoice date, YYYY-MM-DD; blank uses today in the company's time zone.
    txn_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'txnDate', 'dex_json_omit_default': True})
    # DueDate is the payment due date, YYYY-MM-DD; blank applies the customer's or company's terms.
    due_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'dueDate', 'dex_json_omit_default': True})
    # DocNumber is the invoice number, at most 21 characters without a backslash, such as an order ID,
    # so listInvoices can search for it; blank lets QuickBooks assign the next number when the company
    # numbers invoices automatically.
    doc_number: str = _dataclass_field(default='', metadata={'dex_json_name': 'docNumber', 'dex_json_omit_default': True})
    # BillEmail is the address sendInvoice uses by default; blank uses none.
    bill_email: str = _dataclass_field(default='', metadata={'dex_json_name': 'billEmail', 'dex_json_omit_default': True})
    # CurrencyCode is the invoice's ISO 4217 currency, which must be the customer's; blank uses
    # the customer's currency.
    currency_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'currencyCode', 'dex_json_omit_default': True})
    # CustomerMemo is the message shown to the customer on the invoice, at most 1000 characters.
    customer_memo: str = _dataclass_field(default='', metadata={'dex_json_name': 'customerMemo', 'dex_json_omit_default': True})
    # PrivateNote is an internal memo, at most 4000 characters, such as the application's order ID.
    private_note: str = _dataclass_field(default='', metadata={'dex_json_name': 'privateNote', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'lines', _replace_nil_slices(self.lines, 1))


CreateInvoiceInput.__annotations__['lines'] = tuple[InvoiceLineInput, ...] | None
CreateInvoiceInput.__dataclass_fields__['lines'].type = tuple[InvoiceLineInput, ...] | None
setattr(CreateInvoiceInput, '__module__', 'dex_connectors.intuit_quickbooks.models')


# InvoiceLine is one invoice line. Item fields are set only on a SalesItemLineDetail line.
@dataclass(frozen=True, slots=True)
class InvoiceLine:
    # LineID is QuickBooks's Id for the line, when returned.
    line_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'lineId', 'dex_json_omit_default': True})
    # DetailType is QuickBooks's own line type, such as SalesItemLineDetail.
    detail_type: str = _dataclass_field(kw_only=True, metadata={'dex_json_name': 'detailType'})
    # Description is the line text, when set.
    description: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Amount is the line amount.
    amount: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # ItemID is the product or service item's Id.
    item_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'itemId', 'dex_json_omit_default': True})
    # ItemName is the item's name as QuickBooks returns it.
    item_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'itemName', 'dex_json_omit_default': True})
    # Quantity is the item quantity, when set.
    quantity: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # UnitPrice is the item's unit price, when set.
    unit_price: str = _dataclass_field(default='', metadata={'dex_json_name': 'unitPrice', 'dex_json_omit_default': True})
    # TaxCodeID is the line's tax code, such as TAX or NON in a US company, when set.
    tax_code_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'taxCodeId', 'dex_json_omit_default': True})
    # ServiceDate is the date of service, YYYY-MM-DD, when set.
    service_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'serviceDate', 'dex_json_omit_default': True})


setattr(InvoiceLine, '__module__', 'dex_connectors.intuit_quickbooks.models')


# Invoice is one QuickBooks Online invoice. QuickBooks has no invoice status field: Balance is the
# amount still open, and an invoice is paid when Balance is zero. Amounts are exact Decimals in
# the invoice's currency.
@dataclass(frozen=True, slots=True)
class Invoice:
    # InvoiceID is QuickBooks's Id for the invoice, a decimal string such as 130.
    invoice_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'invoiceId'})
    # SyncToken is the version QuickBooks requires for an update of this invoice.
    sync_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'syncToken', 'dex_json_omit_default': True})
    # DocNumber is the invoice number, when the company assigns or accepts one.
    doc_number: str = _dataclass_field(default='', metadata={'dex_json_name': 'docNumber', 'dex_json_omit_default': True})
    # CustomerID is the invoiced customer's Id.
    customer_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'customerId'})
    # CustomerName is the customer's display name as QuickBooks returns it.
    customer_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'customerName', 'dex_json_omit_default': True})
    # TxnDate is the invoice date, YYYY-MM-DD.
    txn_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'txnDate', 'dex_json_omit_default': True})
    # DueDate is the payment due date, YYYY-MM-DD.
    due_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'dueDate', 'dex_json_omit_default': True})
    # CurrencyCode is the invoice's ISO 4217 currency, when QuickBooks returns it.
    currency_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'currencyCode', 'dex_json_omit_default': True})
    # ExchangeRate is the home-currency rate of a foreign-currency invoice, when returned.
    exchange_rate: str = _dataclass_field(default='', metadata={'dex_json_name': 'exchangeRate', 'dex_json_omit_default': True})
    # Lines are the invoice lines in QuickBooks's order, including the SubTotalLineDetail line.
    lines: tuple[InvoiceLine, ...] = ()
    # TotalAmount is QuickBooks's TotalAmt, the invoice total including tax.
    total_amount: str = _dataclass_field(default='', metadata={'dex_json_name': 'totalAmount'})
    # TotalTax is the tax QuickBooks calculated, when returned.
    total_tax: str = _dataclass_field(default='', metadata={'dex_json_name': 'totalTax', 'dex_json_omit_default': True})
    # Balance is the amount still due; zero means the invoice is paid.
    balance: str = ''
    # EmailStatus is NotSet, NeedToSend, or EmailSent, or a newer QuickBooks value.
    email_status: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'emailStatus', 'dex_json_omit_default': True})
    # BillEmail is the address QuickBooks sends the invoice to, when set.
    bill_email: str = _dataclass_field(default='', metadata={'dex_json_name': 'billEmail', 'dex_json_omit_default': True})
    # DeliveredAt is when QuickBooks last delivered the invoice, in UTC, when it has.
    delivered_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'deliveredAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # CustomerMemo is the message shown to the customer, when set.
    customer_memo: str = _dataclass_field(default='', metadata={'dex_json_name': 'customerMemo', 'dex_json_omit_default': True})
    # PrivateNote is the internal memo, when set.
    private_note: str = _dataclass_field(default='', metadata={'dex_json_name': 'privateNote', 'dex_json_omit_default': True})
    # LinkedPaymentIDs are the payments QuickBooks lists against the invoice.
    linked_payment_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'linkedPaymentIds', 'dex_json_omit_default': True})
    # CreatedAt is when the invoice was created, in UTC.
    created_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'createdAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # UpdatedAt is when the invoice last changed, in UTC.
    updated_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'updatedAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'lines', _replace_nil_slices(self.lines, 1))


Invoice.__annotations__['lines'] = tuple[InvoiceLine, ...] | None
Invoice.__dataclass_fields__['lines'].type = tuple[InvoiceLine, ...] | None
setattr(Invoice, '__module__', 'dex_connectors.intuit_quickbooks.models')


# CreateInvoiceOutput is the created invoice. On every other branch Invoice is empty and
# DocNumber echoes the request, so an application can reconcile with listInvoices.
@dataclass(frozen=True, slots=True)
class CreateInvoiceOutput:
    # Invoice is the invoice QuickBooks created or, for a replayed attempt, created earlier for this Step.
    invoice: Invoice = _dataclass_field(default_factory=Invoice)
    # DocNumber echoes the requested document number.
    doc_number: str = _dataclass_field(default='', metadata={'dex_json_name': 'docNumber', 'dex_json_omit_default': True})


setattr(CreateInvoiceOutput, '__module__', 'dex_connectors.intuit_quickbooks.models')


# GetInvoiceInput identifies one invoice.
@dataclass(frozen=True, slots=True)
class GetInvoiceInput:
    # InvoiceID is the invoice's QuickBooks Id, such as 130.
    invoice_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'invoiceId'})


setattr(GetInvoiceInput, '__module__', 'dex_connectors.intuit_quickbooks.models')


# ListInvoicesInput filters one page of invoices. Every filter is optional, and filters combine with AND.
@dataclass(frozen=True, slots=True)
class ListInvoicesInput:
    # CustomerID keeps invoices of one customer, by Id.
    customer_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'customerId', 'dex_json_omit_default': True})
    # DocNumber keeps invoices whose invoice number equals this value.
    doc_number: str = _dataclass_field(default='', metadata={'dex_json_name': 'docNumber', 'dex_json_omit_default': True})
    # IsOpenOnly keeps invoices whose Balance is above zero, that is, not fully paid.
    is_open_only: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isOpenOnly', 'dex_json_omit_default': True})
    # TxnDateFrom keeps invoices dated on or after this date, YYYY-MM-DD.
    txn_date_from: str = _dataclass_field(default='', metadata={'dex_json_name': 'txnDateFrom', 'dex_json_omit_default': True})
    # TxnDateTo keeps invoices dated on or before this date, YYYY-MM-DD.
    txn_date_to: str = _dataclass_field(default='', metadata={'dex_json_name': 'txnDateTo', 'dex_json_omit_default': True})
    # UpdatedSince keeps invoices created or changed at or after this instant, to the second.
    updated_since: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'updatedSince', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # StartPosition is the 1-based position of the first invoice to read, such as the previous
    # page's NextStartPosition; zero reads from the first invoice.
    start_position: int = _dataclass_field(default=0, metadata={'dex_json_name': 'startPosition', 'dex_json_omit_default': True})
    # MaxResults is 1 to 100 invoices; zero uses DefaultInvoicesPerPage.
    max_results: int = _dataclass_field(default=0, metadata={'dex_json_name': 'maxResults', 'dex_json_omit_default': True})


setattr(ListInvoicesInput, '__module__', 'dex_connectors.intuit_quickbooks.models')


# InvoicePage is one page of invoices in ascending creation order.
@dataclass(frozen=True, slots=True)
class InvoicePage:
    # Invoices are the page's invoices; the list can be empty.
    invoices: tuple[Invoice, ...] = ()
    # StartPosition is the 1-based position of the first invoice on the page.
    start_position: int = _dataclass_field(default=0, metadata={'dex_json_name': 'startPosition'})
    # MaxResults is the requested page size.
    max_results: int = _dataclass_field(default=0, metadata={'dex_json_name': 'maxResults'})
    # HasMorePages reports a full page, after which more invoices can follow.
    has_more_pages: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasMorePages'})
    # NextStartPosition is the StartPosition of the next page when HasMorePages is true, and zero otherwise.
    next_start_position: int = _dataclass_field(default=0, metadata={'dex_json_name': 'nextStartPosition', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'invoices', _replace_nil_slices(self.invoices, 1))


InvoicePage.__annotations__['invoices'] = tuple[Invoice, ...] | None
InvoicePage.__dataclass_fields__['invoices'].type = tuple[Invoice, ...] | None
setattr(InvoicePage, '__module__', 'dex_connectors.intuit_quickbooks.models')


# SendInvoiceInput identifies the invoice to email and, optionally, its recipient.
@dataclass(frozen=True, slots=True)
class SendInvoiceInput:
    # InvoiceID is the invoice's QuickBooks Id, such as 130.
    invoice_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'invoiceId'})
    # SendTo is the recipient address; QuickBooks also stores it as the invoice's BillEmail.
    # Blank sends to the invoice's BillEmail, and QuickBooks rejects an invoice without one.
    send_to: str = _dataclass_field(default='', metadata={'dex_json_name': 'sendTo', 'dex_json_omit_default': True})


setattr(SendInvoiceInput, '__module__', 'dex_connectors.intuit_quickbooks.models')


# RecordPaymentInput describes one received payment applied in full to one invoice.
@dataclass(frozen=True, slots=True)
class RecordPaymentInput:
    # CustomerID is the paying customer's Id; it must be the invoice's customer.
    customer_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'customerId'})
    # InvoiceID is the paid invoice's Id.
    invoice_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'invoiceId'})
    # Amount is the exact amount received and applied to the invoice, with at most two decimal
    # places, such as the invoice's Balance.
    amount: str = ''
    # TxnDate is the payment date, YYYY-MM-DD; blank uses today in the company's time zone.
    txn_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'txnDate', 'dex_json_omit_default': True})
    # PaymentReferenceNumber is QuickBooks's PaymentRefNum, such as a check or charge number, at
    # most 21 characters.
    payment_reference_number: str = _dataclass_field(default='', metadata={'dex_json_name': 'paymentReferenceNumber', 'dex_json_omit_default': True})
    # DepositAccountID is the bank or other current asset account's Id; blank deposits to
    # QuickBooks's Undeposited Funds account.
    deposit_account_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'depositAccountId', 'dex_json_omit_default': True})
    # PaymentMethodID is the payment method's Id, such as for Credit Card; blank leaves it unset.
    payment_method_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'paymentMethodId', 'dex_json_omit_default': True})
    # CurrencyCode is the payment's ISO 4217 currency, which must be the customer's; blank uses
    # the customer's currency.
    currency_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'currencyCode', 'dex_json_omit_default': True})
    # PrivateNote is an internal memo, at most 4000 characters.
    private_note: str = _dataclass_field(default='', metadata={'dex_json_name': 'privateNote', 'dex_json_omit_default': True})


setattr(RecordPaymentInput, '__module__', 'dex_connectors.intuit_quickbooks.models')


# PaymentApplication is the part of a payment applied to one invoice.
@dataclass(frozen=True, slots=True)
class PaymentApplication:
    # InvoiceID is the paid invoice's Id.
    invoice_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'invoiceId'})
    # Amount is the amount applied to the invoice.
    amount: str = ''


setattr(PaymentApplication, '__module__', 'dex_connectors.intuit_quickbooks.models')


# Payment is one QuickBooks Online received payment. Amounts are exact Decimals in the payment's currency.
@dataclass(frozen=True, slots=True)
class Payment:
    # PaymentID is QuickBooks's Id for the payment, a decimal string such as 186.
    payment_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'paymentId'})
    # SyncToken is the version QuickBooks requires for an update of this payment.
    sync_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'syncToken', 'dex_json_omit_default': True})
    # CustomerID is the paying customer's Id.
    customer_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'customerId'})
    # TxnDate is the payment date, YYYY-MM-DD.
    txn_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'txnDate', 'dex_json_omit_default': True})
    # TotalAmount is QuickBooks's TotalAmt, the amount received.
    total_amount: str = _dataclass_field(default='', metadata={'dex_json_name': 'totalAmount'})
    # UnappliedAmount is the part of the payment not applied to any invoice, when returned.
    unapplied_amount: str = _dataclass_field(default='', metadata={'dex_json_name': 'unappliedAmount', 'dex_json_omit_default': True})
    # CurrencyCode is the payment's ISO 4217 currency, when QuickBooks returns it.
    currency_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'currencyCode', 'dex_json_omit_default': True})
    # PaymentReferenceNumber is QuickBooks's PaymentRefNum, such as a check number, when set.
    payment_reference_number: str = _dataclass_field(default='', metadata={'dex_json_name': 'paymentReferenceNumber', 'dex_json_omit_default': True})
    # DepositAccountID is the account the payment was deposited to, when returned.
    deposit_account_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'depositAccountId', 'dex_json_omit_default': True})
    # AppliedInvoices are the invoices the payment pays and the amount applied to each.
    applied_invoices: tuple[PaymentApplication, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'appliedInvoices', 'dex_json_omit_default': True})
    # PrivateNote is the internal memo, when set.
    private_note: str = _dataclass_field(default='', metadata={'dex_json_name': 'privateNote', 'dex_json_omit_default': True})
    # CreatedAt is when the payment was created, in UTC.
    created_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'createdAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # UpdatedAt is when the payment last changed, in UTC.
    updated_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'updatedAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})


setattr(Payment, '__module__', 'dex_connectors.intuit_quickbooks.models')
