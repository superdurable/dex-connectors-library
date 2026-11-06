# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Xero Accounting connector for Dex."""

from ._generated.models import Contact as Contact
from ._generated.models import ContactPage as ContactPage
from ._generated.models import CreateInvoiceInput as CreateInvoiceInput
from ._generated.models import CreateInvoiceOutput as CreateInvoiceOutput
from ._generated.models import FindContactByEmailInput as FindContactByEmailInput
from ._generated.models import FindContactByEmailOutput as FindContactByEmailOutput
from ._generated.models import GetInvoiceInput as GetInvoiceInput
from ._generated.models import Invoice as Invoice
from ._generated.models import InvoiceContact as InvoiceContact
from ._generated.models import InvoiceLineItem as InvoiceLineItem
from ._generated.models import InvoiceLineItemInput as InvoiceLineItemInput
from ._generated.models import InvoicePage as InvoicePage
from ._generated.models import InvoicePayment as InvoicePayment
from ._generated.models import ListContactsInput as ListContactsInput
from ._generated.models import ListInvoicesInput as ListInvoicesInput
from ._generated.models import Payment as Payment
from ._generated.models import PaymentAccount as PaymentAccount
from ._generated.models import PaymentInvoice as PaymentInvoice
from ._generated.models import RecordPaymentInput as RecordPaymentInput
from ._generated.models import RecordPaymentOutput as RecordPaymentOutput
from ._generated.steps import new_create_invoice_step as new_create_invoice_step
from ._generated.steps import new_find_contact_by_email_step as new_find_contact_by_email_step
from ._generated.steps import new_get_invoice_step as new_get_invoice_step
from ._generated.steps import new_list_contacts_step as new_list_contacts_step
from ._generated.steps import new_list_invoices_step as new_list_invoices_step
from ._generated.steps import new_record_payment_step as new_record_payment_step
from .client import Client, Config, Credentials
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection

__all__ = [
    "Client",
    "Config",
    "Credentials",
    "Connection",
    "new_connection",
    "new_connection_with_operations",
    "new_project_connection",
    "ListContactsInput",
    "Contact",
    "ContactPage",
    "FindContactByEmailInput",
    "FindContactByEmailOutput",
    "GetInvoiceInput",
    "InvoiceContact",
    "InvoiceLineItem",
    "InvoicePayment",
    "Invoice",
    "ListInvoicesInput",
    "InvoicePage",
    "InvoiceLineItemInput",
    "CreateInvoiceInput",
    "CreateInvoiceOutput",
    "RecordPaymentInput",
    "PaymentInvoice",
    "PaymentAccount",
    "Payment",
    "RecordPaymentOutput",
    "new_list_contacts_step",
    "new_find_contact_by_email_step",
    "new_get_invoice_step",
    "new_list_invoices_step",
    "new_create_invoice_step",
    "new_record_payment_step",
]
