# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from datetime import UTC, datetime
from typing import Any

from .timestamps import parse_go_datetime, validate_go_datetime

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# ListContactsInput filters one page of contacts. Every filter is optional, and filters combine.
@dataclass(frozen=True, slots=True)
class ListContactsInput:
    # SearchTerm is a case-insensitive text search across Name, FirstName, LastName,
    # ContactNumber, CompanyNumber, and EmailAddress; blank lists every contact.
    search_term: str = _dataclass_field(default='', metadata={'dex_json_name': 'searchTerm', 'dex_json_omit_default': True})
    # ContactIDs limits the page to at most 40 contacts by their Xero UUIDs.
    contact_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'contactIds', 'dex_json_omit_default': True})
    # IncludesArchived also returns ARCHIVED contacts, which Xero leaves out by default.
    includes_archived: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'includesArchived', 'dex_json_omit_default': True})
    # ModifiedSince sends If-Modified-Since, so only contacts created or changed at or after this
    # instant, to the second, are returned. Xero does not count balance or IsCustomer changes.
    modified_since: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'modifiedSince', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # Page is the 1-based page to read; zero reads the first page.
    page: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # PageSize is 1 to 100 contacts; zero uses DefaultPageSize.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if isinstance(self.modified_since, str):
            object.__setattr__(self, 'modified_since', parse_go_datetime(self.modified_since, 'modified_since'))
        elif self.modified_since is not None:
            validate_go_datetime(self.modified_since, 'modified_since')


setattr(ListContactsInput, '__module__', 'dex_connectors.xero.models')


# Contact is one Xero customer or supplier. Status and flags keep Xero's values; a status
# Xero adds later is passed through unchanged.
@dataclass(frozen=True, slots=True)
class Contact:
    # ContactID is Xero's stable contact identifier, a UUID; use it to raise an invoice.
    contact_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'contactId'})
    # Name is the contact or organisation name, which Xero may stop treating as unique.
    name: str = ''
    # FirstName is the contact person's first name, when set.
    first_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'firstName', 'dex_json_omit_default': True})
    # LastName is the contact person's last name, when set.
    last_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'lastName', 'dex_json_omit_default': True})
    # EmailAddress is the contact person's email address, when set.
    email_address: str = _dataclass_field(default='', metadata={'dex_json_name': 'emailAddress', 'dex_json_omit_default': True})
    # ContactNumber is an identifier from an external system, shown as Contact Code in Xero.
    contact_number: str = _dataclass_field(default='', metadata={'dex_json_name': 'contactNumber', 'dex_json_omit_default': True})
    # AccountNumber is a user-defined account number.
    account_number: str = _dataclass_field(default='', metadata={'dex_json_name': 'accountNumber', 'dex_json_omit_default': True})
    # Status is ACTIVE, ARCHIVED, or GDPRREQUEST.
    status: str = _dataclass_field(kw_only=True)
    # IsCustomer reports that the contact has sales invoices.
    is_customer: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isCustomer', 'dex_json_omit_default': True})
    # IsSupplier reports that the contact has bills.
    is_supplier: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isSupplier', 'dex_json_omit_default': True})
    # DefaultCurrency is the ISO 4217 currency Xero suggests for the contact's invoices, when set.
    default_currency: str = _dataclass_field(default='', metadata={'dex_json_name': 'defaultCurrency', 'dex_json_omit_default': True})
    # UpdatedAt is Xero's last-modified instant in UTC, or zero when Xero omits it.
    updated_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'updatedAt', 'dex_json_format': 'rfc3339'})

    def __post_init__(self) -> None:
        if isinstance(self.updated_at, str):
            object.__setattr__(self, 'updated_at', parse_go_datetime(self.updated_at, 'updated_at'))
        elif self.updated_at is not None:
            validate_go_datetime(self.updated_at, 'updated_at')


setattr(Contact, '__module__', 'dex_connectors.xero.models')


# ContactPage is one bounded page of contacts.
@dataclass(frozen=True, slots=True)
class ContactPage:
    # Contacts are the contacts on this page in Xero's order, by last modification then ID.
    contacts: tuple[Contact, ...] = ()
    # Page is the 1-based page number that was read.
    page: int = 0
    # PageSize is the requested page size.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize'})
    # PageCount is Xero's total page count, or zero when Xero omits pagination.
    page_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageCount', 'dex_json_omit_default': True})
    # ItemCount is Xero's total number of matching contacts, or zero when Xero omits pagination.
    item_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'itemCount', 'dex_json_omit_default': True})
    # HasMorePages reports that Page+1 may hold more contacts.
    has_more_pages: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasMorePages'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'contacts', _replace_nil_slices(self.contacts, 1))


ContactPage.__annotations__['contacts'] = tuple[Contact, ...] | None
ContactPage.__dataclass_fields__['contacts'].type = tuple[Contact, ...] | None
setattr(ContactPage, '__module__', 'dex_connectors.xero.models')


# FindContactByEmailInput identifies a contact by its email address.
@dataclass(frozen=True, slots=True)
class FindContactByEmailInput:
    # EmailAddress is one bare address such as jane@example.com. Xero's filter ignores case and
    # accents; the connector then keeps only contacts whose address equals it, ignoring case.
    email_address: str = _dataclass_field(default='', metadata={'dex_json_name': 'emailAddress'})
    # IncludesArchived also matches ARCHIVED contacts, which cannot be invoiced; false matches only
    # the contacts Xero lists by default.
    includes_archived: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'includesArchived', 'dex_json_omit_default': True})


setattr(FindContactByEmailInput, '__module__', 'dex_connectors.xero.models')


# FindContactByEmailOutput is the lookup result.
@dataclass(frozen=True, slots=True)
class FindContactByEmailOutput:
    # EmailAddress echoes the trimmed requested address.
    email_address: str = _dataclass_field(default='', metadata={'dex_json_name': 'emailAddress'})
    # Contact is the one matching contact on the found branch, and nil otherwise.
    contact: Contact | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Matches lists up to MaxContactEmailMatches contacts on the ambiguous branch.
    matches: tuple[Contact, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})


setattr(FindContactByEmailOutput, '__module__', 'dex_connectors.xero.models')


# GetInvoiceInput identifies one invoice by exactly one of its identifiers.
@dataclass(frozen=True, slots=True)
class GetInvoiceInput:
    # InvoiceID is Xero's invoice UUID.
    invoice_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'invoiceId', 'dex_json_omit_default': True})
    # InvoiceNumber is the document number, such as INV-0042, used when InvoiceID is blank.
    invoice_number: str = _dataclass_field(default='', metadata={'dex_json_name': 'invoiceNumber', 'dex_json_omit_default': True})


setattr(GetInvoiceInput, '__module__', 'dex_connectors.xero.models')


# InvoiceContact identifies the contact on an invoice.
@dataclass(frozen=True, slots=True)
class InvoiceContact:
    # ContactID is Xero's contact UUID.
    contact_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'contactId'})
    # Name is the contact name Xero returned.
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(InvoiceContact, '__module__', 'dex_connectors.xero.models')


# InvoiceLineItem is one invoice line as Xero stores it.
@dataclass(frozen=True, slots=True)
class InvoiceLineItem:
    # LineItemID is Xero's line identifier.
    line_item_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'lineItemId', 'dex_json_omit_default': True})
    # Description is the line description.
    description: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Quantity is the line quantity, up to four decimal places.
    quantity: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # UnitAmount is the unit price, read with four decimal places.
    unit_amount: str = _dataclass_field(default='', metadata={'dex_json_name': 'unitAmount', 'dex_json_omit_default': True})
    # ItemCode is the inventory item code, when set.
    item_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'itemCode', 'dex_json_omit_default': True})
    # AccountCode is the chart-of-accounts code, when set.
    account_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'accountCode', 'dex_json_omit_default': True})
    # TaxType is the Xero tax type, when set.
    tax_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'taxType', 'dex_json_omit_default': True})
    # TaxAmount is the line tax Xero calculated or was given.
    tax_amount: str = _dataclass_field(default='', metadata={'dex_json_name': 'taxAmount', 'dex_json_omit_default': True})
    # LineAmount is the line total after any discount.
    line_amount: str = _dataclass_field(default='', metadata={'dex_json_name': 'lineAmount', 'dex_json_omit_default': True})
    # DiscountRate is the percentage discount on an ACCREC line, when set.
    discount_rate: str = _dataclass_field(default='', metadata={'dex_json_name': 'discountRate', 'dex_json_omit_default': True})


setattr(InvoiceLineItem, '__module__', 'dex_connectors.xero.models')


# InvoicePayment is one payment Xero lists on an invoice.
@dataclass(frozen=True, slots=True)
class InvoicePayment:
    # PaymentID is Xero's payment UUID.
    payment_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'paymentId'})
    # Date is the payment date, YYYY-MM-DD.
    date: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Amount is the payment amount in the invoice's currency.
    amount: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(InvoicePayment, '__module__', 'dex_connectors.xero.models')


# Invoice is one Xero sales invoice or bill. Every amount is an exact Decimal in the invoice's
# currency, and Type, Status, and LineAmountTypes keep Xero's values.
@dataclass(frozen=True, slots=True)
class Invoice:
    # InvoiceID is Xero's stable invoice identifier, a UUID.
    invoice_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'invoiceId'})
    # InvoiceNumber is the document number: unique for ACCREC, assigned from the organisation's
    # invoice settings when none was given, and the supplier's non-unique number for ACCPAY.
    invoice_number: str = _dataclass_field(default='', metadata={'dex_json_name': 'invoiceNumber', 'dex_json_omit_default': True})
    # Type is ACCREC or ACCPAY.
    type: str = _dataclass_field(kw_only=True)
    # Status is DRAFT, SUBMITTED, DELETED, AUTHORISED, PAID, or VOIDED.
    status: str = _dataclass_field(kw_only=True)
    # Contact is the invoiced customer or the billing supplier.
    contact: InvoiceContact = _dataclass_field(default_factory=InvoiceContact)
    # Date is the issue date, YYYY-MM-DD.
    date: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # DueDate is the due date, YYYY-MM-DD, when set.
    due_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'dueDate', 'dex_json_omit_default': True})
    # Reference is the additional reference, when set.
    reference: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # CurrencyCode is the ISO 4217 currency the invoice was raised in.
    currency_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'currencyCode', 'dex_json_omit_default': True})
    # CurrencyRate is 1 for the base currency, or the rate Xero applied.
    currency_rate: str = _dataclass_field(default='', metadata={'dex_json_name': 'currencyRate', 'dex_json_omit_default': True})
    # LineAmountTypes is Exclusive, Inclusive, or NoTax.
    line_amount_types: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'lineAmountTypes', 'dex_json_omit_default': True})
    # LineItems are the invoice lines; Xero returns them on a single read and on paged lists.
    line_items: tuple[InvoiceLineItem, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'lineItems', 'dex_json_omit_default': True})
    # SubTotal is the total excluding tax.
    sub_total: str = _dataclass_field(default='', metadata={'dex_json_name': 'subTotal', 'dex_json_omit_default': True})
    # TotalTax is the tax total.
    total_tax: str = _dataclass_field(default='', metadata={'dex_json_name': 'totalTax', 'dex_json_omit_default': True})
    # Total is SubTotal plus TotalTax plus any rounding.
    total: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # AmountDue is the amount still to be paid.
    amount_due: str = _dataclass_field(default='', metadata={'dex_json_name': 'amountDue', 'dex_json_omit_default': True})
    # AmountPaid is the sum of payments.
    amount_paid: str = _dataclass_field(default='', metadata={'dex_json_name': 'amountPaid', 'dex_json_omit_default': True})
    # AmountCredited is the sum of credit notes, overpayments, and prepayments applied.
    amount_credited: str = _dataclass_field(default='', metadata={'dex_json_name': 'amountCredited', 'dex_json_omit_default': True})
    # Payments are the payments applied to the invoice.
    payments: tuple[InvoicePayment, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # FullyPaidOnDate is the date the invoice became fully paid, YYYY-MM-DD, when it is.
    fully_paid_on_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'fullyPaidOnDate', 'dex_json_omit_default': True})
    # IsSentToContact reports that Xero shows the invoice as sent.
    is_sent_to_contact: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isSentToContact', 'dex_json_omit_default': True})
    # UpdatedAt is Xero's last-modified instant in UTC, or zero when Xero omits it.
    updated_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'updatedAt', 'dex_json_format': 'rfc3339'})

    def __post_init__(self) -> None:
        if isinstance(self.updated_at, str):
            object.__setattr__(self, 'updated_at', parse_go_datetime(self.updated_at, 'updated_at'))
        elif self.updated_at is not None:
            validate_go_datetime(self.updated_at, 'updated_at')


setattr(Invoice, '__module__', 'dex_connectors.xero.models')


# ListInvoicesInput filters one page of invoices. Every filter is optional, and filters combine.
@dataclass(frozen=True, slots=True)
class ListInvoicesInput:
    # Statuses keeps invoices in any of these Xero statuses; empty keeps every status.
    statuses: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # ContactIDs keeps invoices for any of at most 40 contacts, by Xero UUID.
    contact_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'contactIds', 'dex_json_omit_default': True})
    # InvoiceNumbers keeps invoices with any of at most 40 document numbers.
    invoice_numbers: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'invoiceNumbers', 'dex_json_omit_default': True})
    # Type keeps only ACCREC sales invoices or ACCPAY bills; blank keeps both.
    type: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Reference keeps invoices whose Reference equals this value, through Xero's optimised filter.
    reference: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # ModifiedSince sends If-Modified-Since, so only invoices created or changed at or after this
    # instant, to the second, are returned. Xero does not count every change, such as a due date
    # changed on a partly paid invoice.
    modified_since: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'modifiedSince', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # Page is the 1-based page to read; zero reads the first page.
    page: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # PageSize is 1 to 100 invoices; zero uses DefaultPageSize.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if isinstance(self.modified_since, str):
            object.__setattr__(self, 'modified_since', parse_go_datetime(self.modified_since, 'modified_since'))
        elif self.modified_since is not None:
            validate_go_datetime(self.modified_since, 'modified_since')


setattr(ListInvoicesInput, '__module__', 'dex_connectors.xero.models')


# InvoicePage is one bounded page of invoices.
@dataclass(frozen=True, slots=True)
class InvoicePage:
    # Invoices are the invoices on this page in Xero's order, by last modification then ID.
    invoices: tuple[Invoice, ...] = ()
    # Page is the 1-based page number that was read.
    page: int = 0
    # PageSize is the requested page size.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize'})
    # PageCount is Xero's total page count, or zero when Xero omits pagination.
    page_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageCount', 'dex_json_omit_default': True})
    # ItemCount is Xero's total number of matching invoices, or zero when Xero omits pagination.
    item_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'itemCount', 'dex_json_omit_default': True})
    # HasMorePages reports that Page+1 may hold more invoices.
    has_more_pages: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasMorePages'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'invoices', _replace_nil_slices(self.invoices, 1))


InvoicePage.__annotations__['invoices'] = tuple[Invoice, ...] | None
InvoicePage.__dataclass_fields__['invoices'].type = tuple[Invoice, ...] | None
setattr(InvoicePage, '__module__', 'dex_connectors.xero.models')


# InvoiceLineItemInput is one line of a new invoice.
@dataclass(frozen=True, slots=True)
class InvoiceLineItemInput:
    # Description is 1 to 4000 characters.
    description: str = ''
    # Quantity is the line quantity, up to four decimal places; blank leaves it to Xero or the item.
    quantity: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # UnitAmount is the unit price, up to four decimal places, which Xero keeps because the
    # connector sends unitdp=4; blank leaves it to Xero or the item.
    unit_amount: str = _dataclass_field(default='', metadata={'dex_json_name': 'unitAmount', 'dex_json_omit_default': True})
    # AccountCode is the chart-of-accounts code, such as 200; Xero requires one to approve.
    account_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'accountCode', 'dex_json_omit_default': True})
    # ItemCode is an inventory item code, which can supply the description and price.
    item_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'itemCode', 'dex_json_omit_default': True})
    # TaxType overrides the account's default Xero tax type, such as OUTPUT or NONE.
    tax_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'taxType', 'dex_json_omit_default': True})
    # DiscountRate is a percentage from 0 to 100 on an ACCREC line; ACCPAY lines take none.
    discount_rate: str = _dataclass_field(default='', metadata={'dex_json_name': 'discountRate', 'dex_json_omit_default': True})


setattr(InvoiceLineItemInput, '__module__', 'dex_connectors.xero.models')


# CreateInvoiceInput describes one new sales invoice or bill. Amounts are exact Decimals in the
# invoice's currency and are sent to Xero as the same number literals.
@dataclass(frozen=True, slots=True)
class CreateInvoiceInput:
    # Type is ACCREC for a sales invoice or ACCPAY for a bill.
    type: str
    # ContactID is the Xero UUID of the customer or supplier, such as from findContactByEmail.
    # Xero is sent only this ID, so the contact record is never changed.
    contact_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'contactId'})
    # LineItems are 1 to 100 lines, each with a description.
    line_items: tuple[InvoiceLineItemInput, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'lineItems'})
    # Date is the issue date, YYYY-MM-DD; blank uses today in the organisation's time zone.
    date: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # DueDate is the due date, YYYY-MM-DD; Xero requires one to approve a sales invoice.
    due_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'dueDate', 'dex_json_omit_default': True})
    # LineAmountTypes is Exclusive, Inclusive, or NoTax; blank means Exclusive.
    line_amount_types: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'lineAmountTypes', 'dex_json_omit_default': True})
    # CurrencyCode is an ISO 4217 code enabled in the organisation, such as USD; blank uses the
    # organisation's base currency.
    currency_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'currencyCode', 'dex_json_omit_default': True})
    # InvoiceNumber is the document number. Blank lets Xero number an ACCREC invoice from the
    # organisation's invoice settings; an ACCREC number must be unique.
    invoice_number: str = _dataclass_field(default='', metadata={'dex_json_name': 'invoiceNumber', 'dex_json_omit_default': True})
    # Reference is an additional reference, such as the application's order ID; listInvoices can
    # find the invoice by it.
    reference: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Status is DRAFT, SUBMITTED, or AUTHORISED; blank means DRAFT. Only an AUTHORISED invoice
    # can take a payment.
    status: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # BrandingThemeID is the Xero UUID of a branding theme; blank uses the organisation's default.
    branding_theme_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'brandingThemeId', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'line_items', _replace_nil_slices(self.line_items, 1))


CreateInvoiceInput.__annotations__['line_items'] = tuple[InvoiceLineItemInput, ...] | None
CreateInvoiceInput.__dataclass_fields__['line_items'].type = tuple[InvoiceLineItemInput, ...] | None
setattr(CreateInvoiceInput, '__module__', 'dex_connectors.xero.models')


# CreateInvoiceOutput is the created invoice. On every other branch Invoice is empty and
# Reference echoes the request, so an application can reconcile with listInvoices.
@dataclass(frozen=True, slots=True)
class CreateInvoiceOutput:
    # Invoice is the invoice Xero created or, for a replayed attempt, created earlier for this Step.
    invoice: Invoice
    # Reference echoes the requested reference.
    reference: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # WarningCount is the number of validation warnings Xero attached to the accepted invoice,
    # such as a questionable currency rate; their text is not exposed.
    warning_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'warningCount', 'dex_json_omit_default': True})


setattr(CreateInvoiceOutput, '__module__', 'dex_connectors.xero.models')


# RecordPaymentInput is one payment against an approved invoice.
@dataclass(frozen=True, slots=True)
class RecordPaymentInput:
    # InvoiceID is the Xero UUID of an AUTHORISED invoice or bill.
    invoice_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'invoiceId'})
    # AccountID is the Xero UUID of a BANK account, or of an account with payments enabled.
    # Set exactly one of AccountID and AccountCode.
    account_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'accountId', 'dex_json_omit_default': True})
    # AccountCode is that account's code, such as 090, for accounts that have one.
    account_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'accountCode', 'dex_json_omit_default': True})
    # Date is the payment date, YYYY-MM-DD.
    date: str = ''
    # Amount is the payment in the invoice's currency, with at most two decimal places; it must
    # not exceed the invoice's amount due.
    amount: str = ''
    # BankAmount is the amount in the account's currency for a multicurrency payment, which Xero
    # prefers to a rate; blank lets Xero apply its rate.
    bank_amount: str = _dataclass_field(default='', metadata={'dex_json_name': 'bankAmount', 'dex_json_omit_default': True})
    # Reference is an optional description, such as the card processor's charge ID.
    reference: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # IsReconciled marks the payment reconciled without a bank statement line, for conversions.
    is_reconciled: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isReconciled', 'dex_json_omit_default': True})


setattr(RecordPaymentInput, '__module__', 'dex_connectors.xero.models')


# PaymentInvoice is the invoice summary Xero returns with a payment.
@dataclass(frozen=True, slots=True)
class PaymentInvoice:
    # InvoiceID is Xero's invoice UUID.
    invoice_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'invoiceId'})
    # InvoiceNumber is the document number, when set.
    invoice_number: str = _dataclass_field(default='', metadata={'dex_json_name': 'invoiceNumber', 'dex_json_omit_default': True})
    # Type is ACCREC or ACCPAY, when Xero returns it.
    type: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Status is the invoice status after the payment, such as PAID, when Xero returns it.
    status: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # AmountDue is the amount still due after the payment, when Xero returns it.
    amount_due: str = _dataclass_field(default='', metadata={'dex_json_name': 'amountDue', 'dex_json_omit_default': True})
    # AmountPaid is the sum of payments after this one, when Xero returns it.
    amount_paid: str = _dataclass_field(default='', metadata={'dex_json_name': 'amountPaid', 'dex_json_omit_default': True})
    # CurrencyCode is the invoice's ISO 4217 currency, when Xero returns it.
    currency_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'currencyCode', 'dex_json_omit_default': True})


setattr(PaymentInvoice, '__module__', 'dex_connectors.xero.models')


# PaymentAccount identifies the account of a payment.
@dataclass(frozen=True, slots=True)
class PaymentAccount:
    # AccountID is Xero's account UUID.
    account_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'accountId', 'dex_json_omit_default': True})
    # Code is the account code, when the account has one.
    code: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(PaymentAccount, '__module__', 'dex_connectors.xero.models')


# Payment is one Xero payment against an invoice. Amounts are exact Decimals.
@dataclass(frozen=True, slots=True)
class Payment:
    # PaymentID is Xero's stable payment identifier, a UUID.
    payment_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'paymentId'})
    # Date is the payment date, YYYY-MM-DD.
    date: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Amount is the payment in the invoice's currency.
    amount: str = ''
    # BankAmount is the payment in the account's currency, when Xero returns it.
    bank_amount: str = _dataclass_field(default='', metadata={'dex_json_name': 'bankAmount', 'dex_json_omit_default': True})
    # CurrencyRate is 1 for a base-currency payment, or the rate Xero applied.
    currency_rate: str = _dataclass_field(default='', metadata={'dex_json_name': 'currencyRate', 'dex_json_omit_default': True})
    # Reference is the payment's description, when set.
    reference: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Status is AUTHORISED or DELETED.
    status: str = _dataclass_field(kw_only=True)
    # PaymentType is Xero's payment type, such as ACCRECPAYMENT for a sales invoice payment.
    payment_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'paymentType', 'dex_json_omit_default': True})
    # IsReconciled reports that the payment is marked reconciled.
    is_reconciled: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isReconciled', 'dex_json_omit_default': True})
    # Invoice is the paid invoice as Xero reports it after the payment.
    invoice: PaymentInvoice = _dataclass_field(default_factory=PaymentInvoice)
    # Account is the account the payment was made from or into.
    account: PaymentAccount = _dataclass_field(default_factory=PaymentAccount)
    # UpdatedAt is Xero's last-modified instant in UTC, or zero when Xero omits it.
    updated_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'updatedAt', 'dex_json_format': 'rfc3339'})

    def __post_init__(self) -> None:
        if isinstance(self.updated_at, str):
            object.__setattr__(self, 'updated_at', parse_go_datetime(self.updated_at, 'updated_at'))
        elif self.updated_at is not None:
            validate_go_datetime(self.updated_at, 'updated_at')


setattr(Payment, '__module__', 'dex_connectors.xero.models')


# RecordPaymentOutput is the recorded payment. On every other branch Payment is empty and
# InvoiceID echoes the request, so an application can read the invoice back.
@dataclass(frozen=True, slots=True)
class RecordPaymentOutput:
    # Payment is the payment Xero recorded or, for a replayed attempt, recorded earlier for this Step.
    payment: Payment
    # InvoiceID echoes the paid invoice.
    invoice_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'invoiceId'})
    # WarningCount is the number of validation warnings Xero attached to the accepted payment;
    # their text is not exposed.
    warning_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'warningCount', 'dex_json_omit_default': True})


setattr(RecordPaymentOutput, '__module__', 'dex_connectors.xero.models')
